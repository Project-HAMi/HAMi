/*
<<<<<<< HEAD
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
=======
 * Copyright © 2021 peizhaoyou <peizhaoyou@4paradigm.com>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
<<<<<<< HEAD
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
=======
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
 */

package plugin

import (
<<<<<<< HEAD
<<<<<<< HEAD
	"bytes"
	"encoding/json"
=======
>>>>>>> c7a3893 (Remake this repo to HAMi)
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
<<<<<<< HEAD
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
		return false, 0, err
	}
	node, err := strconv.Atoi(string(bytes.TrimSpace(b)))
	if err != nil {
		return false, 0, fmt.Errorf("error parsing value for NUMA node: %v", err)
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
		if *plugin.schedulerConfig.DeviceMemoryScaling != 1 && plugin.operatingMode != nvidia.MigMode {
			registeredmem = int32(float64(registeredmem) * *plugin.schedulerConfig.DeviceMemoryScaling)
			klog.Infoln("MemoryScaling=", plugin.schedulerConfig.DeviceMemoryScaling, "registeredmem=", registeredmem)
		} else {
			klog.Warningln("mig mode enabled, the memory scaling is not applied")
		}
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
			klog.ErrorS(err, "failed to get numa information from sysfs", "idx", idx)
		}
		if !strings.HasPrefix(Model, "NVIDIA") {
			// If the model name does not start with "NVIDIA ", we assume it is a virtual GPU or a non-NVIDIA device.
			// This is to handle cases where the model name might not be in the expected format.
			Model = fmt.Sprintf("NVIDIA-%s", Model)
		}
		devcore := int32(100)
		if plugin.operatingMode != nvidia.MigMode {
			devcore = int32(*plugin.schedulerConfig.DeviceCoreScaling * 100)
		} else {
			klog.Warning("mig mode enabled, the core scaling is not applied")
		}
		res = append(res, &device.DeviceInfo{
			ID:      UUID,
			Index:   uint(idx),
			Count:   int32(*plugin.schedulerConfig.DeviceSplitCount),
			Devmem:  registeredmem,
			Devcore: devcore,
			Type:    Model,
			Numa:    numa,
			Mode:    plugin.operatingMode,
			Health:  health,
		})
		klog.Infof("nvml registered device id=%v, memory=%v, type=%v, numa=%v", idx, registeredmem, Model, numa)
=======
	"fmt"
	"time"

	"github.com/NVIDIA/gpu-monitoring-tools/bindings/go/nvml"
=======
>>>>>>> c7a3893 (Remake this repo to HAMi)
	"k8s.io/klog/v2"

	"4pd.io/k8s-vgpu/pkg/api"
	"4pd.io/k8s-vgpu/pkg/device/nvidia"
	"4pd.io/k8s-vgpu/pkg/util"
)

<<<<<<< HEAD
=======
func (r *NvidiaDevicePlugin) getNumaInformation(idx int) (int, error) {
	cmd := exec.Command("nvidia-smi", "topo", "-m")
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatalf("cmd.Run() failed with %s\n", err)
	}
	klog.V(5).InfoS("nvidia-smi topo -m ouput", "result", string(out))
	return parseNvidiaNumaInfo(idx, string(out))
}

func parseNvidiaNumaInfo(idx int, nvidiaTopoStr string) (int, error) {
	result := 0
	numaAffinityColumnIndex := 0
	for index, val := range strings.Split(nvidiaTopoStr, "\n") {
		if !strings.Contains(val, "GPU") {
			continue
		}
		// Example: GPU0	 X 	0-7		N/A		N/A
		// Many values are separated by two tabs, but this actually represents 5 values instead of 7
		// So add logic to remove multiple tabs
		words := strings.Split(strings.ReplaceAll(val, "\t\t", "\t"), "\t")
		klog.V(5).InfoS("parseNumaInfo", "words", words)
		// get numa affinity column number
		if index == 0 {
			for columnIndex, headerVal := range words {
				// The topology output of a single card is as follows:
				// 			GPU0	CPU Affinity	NUMA Affinity	GPU NUMA ID
				// GPU0	 X 	0-7		N/A		N/A
				//Legend: Other content omitted

				// The topology output in the case of multiple cards is as follows:
				// 			GPU0	GPU1	CPU Affinity	NUMA Affinity
				// GPU0	 X 	PHB	0-31		N/A
				// GPU1	PHB	 X 	0-31		N/A
				// Legend: Other content omitted

				// We need to get the value of the NUMA Affinity column, but their column indexes are inconsistent,
				// so we need to get the index first and then get the value.
				if strings.Contains(headerVal, "NUMA Affinity") {
					// The header is one column less than the actual row.
					numaAffinityColumnIndex = columnIndex
					continue
				}
			}
			continue
		}
		klog.V(5).InfoS("nvidia-smi topo -m row output", "row output", words, "length", len(words))
		if strings.Contains(words[0], fmt.Sprint(idx)) {
			if words[numaAffinityColumnIndex] == "N/A" {
				klog.InfoS("current card has not established numa topology", "gpu row info", words, "index", idx)
				return 0, nil
			}
			result, err := strconv.Atoi(words[numaAffinityColumnIndex])
			if err != nil {
				return result, err
			}
		}
	}
	return result, nil
}

>>>>>>> c7a3893 (Remake this repo to HAMi)
func (r *NvidiaDevicePlugin) getApiDevices() *[]*api.DeviceInfo {
	devs := r.Devices()
	nvml.Init()
	res := make([]*api.DeviceInfo, 0, len(devs))
<<<<<<< HEAD
	for _, dev := range devs {
		ndev, err := nvml.NewDeviceByUUID(dev.ID)
		//klog.V(3).Infoln("ndev type=", ndev.Model)
		if err != nil {
			fmt.Println("nvml new device by uuid error id=", dev.ID, "err=", err.Error())
			panic(0)
		} else {
			klog.V(3).Infoln("nvml registered device id=", dev.ID, "memory=", *ndev.Memory, "type=", *ndev.Model)
=======
	idx := 0
	for idx < len(devs) {
		ndev, ret := nvml.DeviceGetHandleByIndex(idx)
		//ndev, err := nvml.NewDevice(uint(idx))
		//klog.V(3).Infoln("ndev type=", ndev.Model)
		if ret != nvml.SUCCESS {
			klog.Errorln("nvml new device by index error idx=", idx, "err=", ret)
			panic(0)
>>>>>>> c7a3893 (Remake this repo to HAMi)
		}
		memoryTotal := 0
		memory, ret := ndev.GetMemoryInfo_v2()
		if ret == nvml.SUCCESS {
			memoryTotal = int(memory.Total)
		} else {
			klog.Error("nvml get memory_v2 error ret=", ret)
			memory_v1, ret := ndev.GetMemoryInfo()
			if ret != nvml.SUCCESS {
				klog.Error("nvml get memory_v2 error ret=", ret)
				panic(0)
			}
			memoryTotal = int(memory_v1.Total)
		}
		UUID, ret := ndev.GetUUID()
		if ret != nvml.SUCCESS {
			klog.Error("nvml get uuid error ret=", ret)
			panic(0)
		}
		Model, ret := ndev.GetName()
		if ret != nvml.SUCCESS {
			klog.Error("nvml get name error ret=", ret)
			panic(0)
		}

		registeredmem := int32(memoryTotal / 1024 / 1024)
		if *util.DeviceMemoryScaling != 1 {
			registeredmem = int32(float64(registeredmem) * *util.DeviceMemoryScaling)
		}
<<<<<<< HEAD
		res = append(res, &api.DeviceInfo{
			Id:      dev.ID,
			Count:   int32(*util.DeviceSplitCount),
			Devmem:  registeredmem,
			Devcore: int32(*util.DeviceCoresScaling * 100),
			Type:    fmt.Sprintf("%v-%v", "NVIDIA", *ndev.Model),
			Health:  dev.Health == "healthy",
		})
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
=======
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
		numa, err := r.getNumaInformation(idx)
		if err != nil {
			klog.ErrorS(err, "failed to get numa information", "idx", idx)
		}
		res = append(res, &api.DeviceInfo{
			Id:      UUID,
			Count:   int32(*util.DeviceSplitCount),
			Devmem:  registeredmem,
			Devcore: int32(*util.DeviceCoresScaling * 100),
			Type:    fmt.Sprintf("%v-%v", "NVIDIA", Model),
			Numa:    numa,
			Health:  health,
		})
		idx++
		klog.Infof("nvml registered device id=%v, memory=%v, type=%v, numa=%v", idx, memory, Model, numa)
>>>>>>> c7a3893 (Remake this repo to HAMi)
	}
	return &res
}

<<<<<<< HEAD
func (plugin *NvidiaDevicePlugin) RegisterInAnnotation() error {
	devices := plugin.getAPIDevices()
	klog.InfoS("start working on the devices", "devices", devices)
=======
func (r *NvidiaDevicePlugin) RegistrInAnnotation() error {
	devices := r.getApiDevices()
<<<<<<< HEAD
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
=======
	klog.InfoS("start working on the devices", "devices", devices)
>>>>>>> c7a3893 (Remake this repo to HAMi)
	annos := make(map[string]string)
	node, err := util.GetNode(util.NodeName)
	if err != nil {
		klog.Errorln("get node error", err.Error())
		return err
	}
<<<<<<< HEAD
	encodeddevices := device.MarshalNodeDevices(*devices)
	if encodeddevices == plugin.deviceCache {
		return nil
	}
	plugin.deviceCache = encodeddevices

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
	annos[nvidia.RegisterAnnos] = encodeddevices
	if len(data) > 0 {
		annos[nvidia.RegisterGPUPairScore] = string(data)
	}
	klog.Infof("patch node with the following annos %v", fmt.Sprintf("%v", annos))
=======
	encodeddevices := util.EncodeNodeDevices(*devices)
	annos[nvidia.HandshakeAnnos] = "Reported " + time.Now().String()
	annos[nvidia.RegisterAnnos] = encodeddevices
<<<<<<< HEAD
	klog.Infoln("Reporting devices", encodeddevices, "in", time.Now().String())
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
=======
	klog.Infof("patch node with the following annos %v", fmt.Sprintf("%v", annos))
>>>>>>> c7a3893 (Remake this repo to HAMi)
	err = util.PatchNodeAnnotations(node, annos)

	if err != nil {
		klog.Errorln("patch node error", err.Error())
	}
	return err
}

<<<<<<< HEAD
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
		err := plugin.RegisterInAnnotation()
		if err != nil {
			klog.Errorf("Failed to register annotation: %v", err)
			klog.Infof("Retrying in %v seconds...", errorSleepInterval)
			time.Sleep(errorSleepInterval)
		} else {
			klog.Infof("Successfully registered annotation. Next check in %v seconds...", successSleepInterval)
			time.Sleep(successSleepInterval)
=======
func (r *NvidiaDevicePlugin) WatchAndRegister() {
	klog.Infof("Starting WatchAndRegister")
	errorSleepInterval := time.Second * 5
	successSleepInterval := time.Second * 30
	for {
		err := r.RegistrInAnnotation()
		if err != nil {
			klog.Errorf("Failed to register annotation: %v", err)
			klog.Infof("Retrying in %v seconds...", errorSleepInterval/time.Second)
			time.Sleep(errorSleepInterval)
		} else {
<<<<<<< HEAD
			time.Sleep(time.Second * 30)
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
=======
			klog.Infof("Successfully registered annotation. Next check in %v seconds...", successSleepInterval/time.Second)
			time.Sleep(successSleepInterval)
>>>>>>> c7a3893 (Remake this repo to HAMi)
		}
	}
}
