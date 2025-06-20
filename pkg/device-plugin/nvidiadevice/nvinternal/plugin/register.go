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
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

func (plugin *NvidiaDevicePlugin) getNumaInformation(idx int) (int, error) {
	cmd := exec.Command("nvidia-smi", "topo", "-m")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, err
	}
	klog.V(5).InfoS("nvidia-smi topo -m output", "result", string(out))
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
			if len(words) <= numaAffinityColumnIndex || words[numaAffinityColumnIndex] == "N/A" {
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

func (plugin *NvidiaDevicePlugin) getAPIDevices() *[]*util.DeviceInfo {
	devs := plugin.Devices()
	defer nvml.Shutdown()
	klog.V(5).InfoS("getAPIDevices", "devices", devs)
	if nvret := nvml.Init(); nvret != nvml.SUCCESS {
		klog.Errorln("nvml Init err: ", nvret)
		panic(0)
	}
	res := make([]*util.DeviceInfo, 0, len(devs))
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
		if plugin.schedulerConfig.DeviceMemoryScaling != 1 {
			registeredmem = int32(float64(registeredmem) * plugin.schedulerConfig.DeviceMemoryScaling)
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
		numa, err := plugin.getNumaInformation(idx)
		if err != nil {
			klog.ErrorS(err, "failed to get numa information", "idx", idx)
		}
		if !strings.HasPrefix(Model, "NVIDIA") {
			// If the model name does not start with "NVIDIA ", we assume it is a virtual GPU or a non-NVIDIA device.
			// This is to handle cases where the model name might not be in the expected format.
			Model = fmt.Sprintf("NVIDIA-%s", Model)
		}
		res = append(res, &util.DeviceInfo{
			ID:      UUID,
			Index:   uint(idx),
			Count:   int32(plugin.schedulerConfig.DeviceSplitCount),
			Devmem:  registeredmem,
			Devcore: int32(plugin.schedulerConfig.DeviceCoreScaling * 100),
			Type:    Model,
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
	encodeddevices := util.EncodeNodeDevices(*devices)
	var data []byte
	if os.Getenv("ENABLE_TOPOLOGY_SCORE") == "true" {
		gpuScore, err := nvidia.CalculateGPUScore(util.GetDevicesUUIDList(*devices))
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
