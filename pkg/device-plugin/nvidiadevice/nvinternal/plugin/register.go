/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * The HAMi Contributors require contributions made to
 * this file be licensed under the Apache-2.0 license or a
 * compatible open source license.
 */

/*
 * Licensed to NVIDIA CORPORATION under one or more contributor
 * license agreements. See the NOTICE file distributed with
 * this work for additional information regarding copyright
 * ownership. NVIDIA CORPORATION licenses this file to you under
 * the Apache License, Version 2.0 (the "License"); you may
 * not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/*
 * Modifications Copyright The HAMi Authors. See
 * GitHub history for details.
 */

package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

// uint8Slice wraps an []uint8 with more functions.
type uint8Slice []uint8

// String turns a nil terminated uint8Slice into a string
func (s uint8Slice) String() string {
	var b []byte
	for _, c := range s {
		if c == 0 {
			break
		}
		b = append(b, c)
	}
	return string(b)
}

// GetNumaNode returns the NUMA node associated with the GPU device
func GetNumaNode(d nvml.Device) (bool, int, error) {
	info, ret := d.GetPciInfo()
	if ret != nvml.SUCCESS {
		return false, 0, fmt.Errorf("error getting PCI Bus Info of device: %v", ret)
	}
	// Discard leading zeros.
	busID := strings.ToLower(strings.TrimPrefix(uint8Slice(info.BusId[:]).String(), "0000"))
	b, err := os.ReadFile(fmt.Sprintf("/sys/bus/pci/devices/%s/numa_node", busID))
	if err != nil {
		return false, 0, nil
	}
	node, err := strconv.Atoi(string(bytes.TrimSpace(b)))
	if err != nil {
		return false, 0, fmt.Errorf("eror parsing value for NUMA node: %v", err)
	}
	if node < 0 {
		return false, 0, nil
	}
	return true, node, nil
}

func (plugin *NvidiaDevicePlugin) getAPIDevices() *[]*device.DeviceInfo {
	devs := plugin.Devices()
	defer nvml.Shutdown()
	klog.V(5).InfoS("getAPIDevices", "devices", devs)
	if nvret := nvml.Init(); nvret != nvml.SUCCESS {
		klog.Errorln("nvml Init err: ", nvret)
		panic(0)
	}
	res := make([]*device.DeviceInfo, 0, len(devs))
	for UUID := range devs {
		ndev, ret := nvml.DeviceGetHandleByUUID(UUID)
		if ret != nvml.SUCCESS {
			klog.Errorln("nvml new device by index error uuid=", UUID, "err=", ret)
			panic(0)
		}
		idx, ret := ndev.GetIndex()
		if ret != nvml.SUCCESS {
			klog.Errorln("nvml get index error ret=", ret)
			panic(0)
		}
		memoryTotal := 0
		memory, ret := ndev.GetMemoryInfo()
		if ret == nvml.SUCCESS {
			memoryTotal = int(memory.Total)
		} else {
			klog.Error("nvml get memory error ret=", ret)
			panic(0)
		}
		Model, ret := ndev.GetName()
		if ret != nvml.SUCCESS {
			klog.Error("nvml get name error ret=", ret)
			panic(0)
		}

		registeredmem := int32(memoryTotal / 1024 / 1024)
		if *plugin.schedulerConfig.DeviceMemoryScaling != 1 {
			registeredmem = int32(float64(registeredmem) * *plugin.schedulerConfig.DeviceMemoryScaling)
		}
		klog.Infoln("MemoryScaling=", plugin.schedulerConfig.DeviceMemoryScaling, "registeredmem=", registeredmem)
		health := true
		for _, val := range devs {
			if strings.Compare(val.ID, UUID) == 0 {
				// when NVIDIA-Tesla P4, the device info is : ID:GPU-e290caca-2f0c-9582-acab-67a142b61ffa,Health:Healthy,Topology:nil,
				// it is more reasonable to think of healthy as case-insensitive
				if strings.EqualFold(val.Health, "healthy") {
					health = true
				} else {
					health = false
				}
				break
			}
		}
		ok, numa, err := GetNumaNode(ndev)
		if !ok {
			klog.ErrorS(err, "failed to get numa information", "idx", idx)
		}
		res = append(res, &device.DeviceInfo{
			ID:      UUID,
			Index:   uint(idx),
			Count:   int32(*plugin.schedulerConfig.DeviceSplitCount),
			Devmem:  registeredmem,
			Devcore: int32(*plugin.schedulerConfig.DeviceCoreScaling * 100),
			Type:    fmt.Sprintf("%v-%v", "NVIDIA", Model),
			Numa:    numa,
			Mode:    plugin.operatingMode,
			Health:  health,
		})
		klog.Infof("nvml registered device id=%v, memory=%v, type=%v, numa=%v", idx, registeredmem, Model, numa)
	}
	return &res
}

func (plugin *NvidiaDevicePlugin) RegistrInAnnotation() error {
	devices := plugin.getAPIDevices()
	klog.InfoS("start working on the devices", "devices", devices)
	annos := make(map[string]string)
	node, err := util.GetNode(util.NodeName)
	if err != nil {
		klog.Errorln("get node error", err.Error())
		return err
	}
	encodeddevices := device.EncodeNodeDevices(*devices)
	var data []byte
	if os.Getenv("ENABLE_TOPOLOGY_SCORE") == "true" {
		gpuScore, err := nvidia.CalculateGPUScore(device.GetDevicesUUIDList(*devices))
		if err != nil {
			klog.ErrorS(err, "calculate gpu topo score error")
			return err
		}
		data, err = json.Marshal(gpuScore)
		if err != nil {
			klog.ErrorS(err, "marshal gpu score error.")
			return err
		}
	}
	klog.V(4).InfoS("patch nvidia  topo score to node", "hami.io/node-nvidia-score", string(data))
	annos[nvidia.HandshakeAnnos] = "Reported " + time.Now().String()
	annos[nvidia.RegisterAnnos] = encodeddevices
	if len(data) > 0 {
		annos[nvidia.RegisterGPUPairScore] = string(data)
	}
	klog.Infof("patch node with the following annos %v", fmt.Sprintf("%v", annos))
	err = util.PatchNodeAnnotations(node, annos)

	if err != nil {
		klog.Errorln("patch node error", err.Error())
	}
	return err
}

func (plugin *NvidiaDevicePlugin) WatchAndRegister(disableNVML <-chan bool, ackDisableWatchAndRegister chan<- bool) {
	klog.Info("Starting WatchAndRegister")
	errorSleepInterval := time.Second * 5
	successSleepInterval := time.Second * 30
	var disableWatchAndRegister bool
	for {
		select {
		case disable := <-disableNVML:
			if disable {
				// when received disableNVML signal, stop the watch and register all the time
				klog.Info("Received disableNVML signal, stopping WatchAndRegister")
				disableWatchAndRegister = true
			} else {
				// when received enableNVML signal, start the watch and register again
				klog.Info("Received enableNVML signal, resuming WatchAndRegister")
				disableWatchAndRegister = false
			}

		default:
		}
		if disableWatchAndRegister {
			klog.Info("WatchAndRegister is disabled by disableWatchAndRegister signal, sleep a success interval")
			ackDisableWatchAndRegister <- true
			time.Sleep(successSleepInterval)
			continue
		}
		err := plugin.RegistrInAnnotation()
		if err != nil {
			klog.Errorf("Failed to register annotation: %v", err)
			klog.Infof("Retrying in %v seconds...", errorSleepInterval)
			time.Sleep(errorSleepInterval)
		} else {
			klog.Infof("Successfully registered annotation. Next check in %v seconds...", successSleepInterval)
			time.Sleep(successSleepInterval)
		}
	}
}
