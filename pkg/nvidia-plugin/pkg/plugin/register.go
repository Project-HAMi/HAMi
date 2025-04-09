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
	"fmt"
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
	// Initialize NVML and get device list
	devs := plugin.Devices()
	klog.InfoS("Starting to collect GPU device information", "deviceCount", len(devs))

	// Initialize NVML library
	if ret := nvml.Init(); ret != nvml.SUCCESS {
		errMsg := nvml.ErrorString(ret)
		klog.ErrorS(fmt.Errorf(errMsg), "Failed to initialize NVML")
		return &[]*util.DeviceInfo{}
	}
	defer nvml.Shutdown()

	res := make([]*util.DeviceInfo, 0, len(devs))
	var errorCount int

	// Process each GPU device
	for UUID := range devs {
		// Get device handle by UUID
		ndev, ret := nvml.DeviceGetHandleByUUID(UUID)
		if ret != nvml.SUCCESS {
			errMsg := nvml.ErrorString(ret)
			klog.ErrorS(fmt.Errorf(errMsg), "Failed to get device handle",
				"uuid", UUID, "errorCode", ret)
			errorCount++
			continue
		}

		// Get device index
		idx, ret := ndev.GetIndex()
		if ret != nvml.SUCCESS {
			errMsg := nvml.ErrorString(ret)
			klog.ErrorS(fmt.Errorf(errMsg), "Failed to get device index",
				"uuid", UUID, "errorCode", ret)
			errorCount++
			continue
		}

		// Get memory information
		memory, ret := ndev.GetMemoryInfo()
		if ret != nvml.SUCCESS {
			errMsg := nvml.ErrorString(ret)
			klog.ErrorS(fmt.Errorf(errMsg), "Failed to get memory info",
				"uuid", UUID, "index", idx)
			errorCount++
			continue
		}
		memoryTotal := int(memory.Total)

		// Calculate registered memory with scaling factor
		registeredmem := int32(memoryTotal / 1024 / 1024)
		if plugin.schedulerConfig.DeviceMemoryScaling != 1 {
			original := registeredmem
			registeredmem = int32(float64(registeredmem) * plugin.schedulerConfig.DeviceMemoryScaling)
			klog.V(4).InfoS("Applied memory scaling",
				"originalMB", original,
				"scaledMB", registeredmem,
				"scalingFactor", plugin.schedulerConfig.DeviceMemoryScaling)
		}

		// Get device model name
		Model, ret := ndev.GetName()
		if ret != nvml.SUCCESS {
			errMsg := nvml.ErrorString(ret)
			klog.ErrorS(fmt.Errorf(errMsg), "Failed to get device name",
				"uuid", UUID, "index", idx)
			errorCount++
			continue
		}

		// Check device health status
		health := true
		for _, val := range devs {
			if strings.Compare(val.ID, UUID) == 0 {
				health = strings.EqualFold(val.Health, "healthy")
				if !health {
					klog.Warning("Device is not healthy",
						"uuid", UUID, "index", idx,
						"healthStatus", val.Health)
				}
				break
			}
		}

		// Get NUMA affinity information
		numa, err := plugin.getNumaInformation(idx)
		if err != nil {
			klog.ErrorS(err, "Failed to get NUMA information",
				"uuid", UUID, "index", idx)
		}

		// Log successful device collection
		klog.InfoS("Successfully collected GPU device info",
			"uuid", UUID,
			"index", idx,
			"model", Model,
			"memoryMB", registeredmem,
			"numaNode", numa,
			"healthStatus", health)

		// Add device info to result
		res = append(res, &util.DeviceInfo{
			ID:      UUID,
			Index:   uint(idx),
			Count:   int32(plugin.schedulerConfig.DeviceSplitCount),
			Devmem:  registeredmem,
			Devcore: int32(plugin.schedulerConfig.DeviceCoreScaling * 100),
			Type:    fmt.Sprintf("%v-%v", "NVIDIA", Model),
			Numa:    numa,
			Mode:    plugin.operatingMode,
			Health:  health,
		})
	}

	// Log summary of device collection
	if errorCount > 0 {
		klog.Warning("Failed to collect some GPU device information",
			"errorCount", errorCount,
			"totalDevices", len(devs),
			"successfulDevices", len(res))
	} else {
		klog.InfoS("Successfully collected all GPU device information",
			"deviceCount", len(res))
	}

	return &res
}

func (plugin *NvidiaDevicePlugin) RegistrInAnnotation() error {
	devices := plugin.getAPIDevices()
	klog.Infof("Starting to register %d devices in node annotation", len(*devices))

	if len(*devices) == 0 {
		klog.Warning("No GPU devices found to register")
		return nil
	}
	for i, dev := range *devices {
		klog.InfoS("Device details",
			"index", i,
			"uuid", dev.ID,
			"type", dev.Type,
			"memoryMB", dev.Devmem,
			"numaNode", dev.Numa,
			"health", dev.Health)
	}
	annos := make(map[string]string)
	node, err := util.GetNode(util.NodeName)
	if err != nil {
		klog.Errorln("get node error", err.Error())
		return err
	}
	encodedDevices := util.EncodeNodeDevices(*devices)
	annos[nvidia.HandshakeAnnos] = "Reported " + time.Now().String()
	annos[nvidia.RegisterAnnos] = encodedDevices
	err = util.PatchNodeAnnotations(node, annos)
	if err != nil {
		klog.Errorln("patch node error", err.Error())
	}
	klog.InfoS("Successfully registered devices in node annotation",
		"deviceCount", len(*devices),
		"nodeName", util.NodeName)
	return err
}

func (plugin *NvidiaDevicePlugin) WatchAndRegister() {
	klog.Info("Starting WatchAndRegister")
	errorSleepInterval := time.Second * 5
	successSleepInterval := time.Second * 30
	for {
		err := plugin.RegistrInAnnotation()
		if err != nil {
			klog.Errorf("Failed to register annotation: %v. Retrying in %v...", err, errorSleepInterval)
			time.Sleep(errorSleepInterval)
		} else {
			time.Sleep(successSleepInterval)
		}
	}
}
