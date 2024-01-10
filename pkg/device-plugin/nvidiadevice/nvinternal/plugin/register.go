/*
 * Copyright © 2021 peizhaoyou <peizhaoyou@4paradigm.com>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package plugin

import (
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"k8s.io/klog/v2"

	"4pd.io/k8s-vgpu/pkg/api"
	"4pd.io/k8s-vgpu/pkg/device/nvidia"
	"4pd.io/k8s-vgpu/pkg/util"
)

func (r *NvidiaDevicePlugin) getNumaInformation(idx int) (int, error) {
	cmd := exec.Command("nvidia-smi", "topo", "-m")
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatalf("cmd.Run() failed with %s\n", err)
	}
	klog.InfoS("nvidia-smi topo -m ouput", "result", string(out))
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
		klog.InfoS("parseNumaInfo", "words", words)
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
					klog.Infoln("numaAffinityColumn", numaAffinityColumnIndex)
					continue
				}
			}
			continue
		}
		klog.InfoS("nvidia-smi topo -m row output", "row output", words, "length", len(words))
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

func (r *NvidiaDevicePlugin) getApiDevices() *[]*api.DeviceInfo {
	devs := r.Devices()
	nvml.Init()
	res := make([]*api.DeviceInfo, 0, len(devs))
	idx := 0
	for idx < len(devs) {
		ndev, ret := nvml.DeviceGetHandleByIndex(idx)
		//ndev, err := nvml.NewDevice(uint(idx))
		//klog.V(3).Infoln("ndev type=", ndev.Model)
		if ret != nvml.SUCCESS {
			klog.Errorln("nvml new device by index error idx=", idx, "err=", ret)
			panic(0)
		}
		memory, ret := ndev.GetMemoryInfo_v2()
		if ret != nvml.SUCCESS {
			klog.Error("nvml get memory error ret=", ret)
			panic(0)
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
		registeredmem := int32(memory.Total)
		if *util.DeviceMemoryScaling != 1 {
			registeredmem = int32(float64(registeredmem) * *util.DeviceMemoryScaling)
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
		klog.V(3).Infoln("nvml registered device id=", idx, "memory=", memory, "type=", Model)
	}
	return &res
}

func (r *NvidiaDevicePlugin) RegistrInAnnotation() error {
	devices := r.getApiDevices()
	klog.InfoS("node devices", "devices", devices)
	annos := make(map[string]string)
	node, err := util.GetNode(util.NodeName)
	if err != nil {
		klog.Errorln("get node error", err.Error())
		return err
	}
	encodeddevices := util.EncodeNodeDevices(*devices)
	annos[nvidia.HandshakeAnnos] = "Reported " + time.Now().String()
	annos[nvidia.RegisterAnnos] = encodeddevices
	klog.Infoln("Reporting devices", encodeddevices, "in", time.Now().String())
	err = util.PatchNodeAnnotations(node, annos)

	if err != nil {
		klog.Errorln("patch node error", err.Error())
	}
	return err
}

func (r *NvidiaDevicePlugin) WatchAndRegister() {
	klog.Infof("into WatchAndRegister")
	for {
		err := r.RegistrInAnnotation()
		if err != nil {
			klog.Errorf("register error, %v", err)
			time.Sleep(time.Second * 5)
		} else {
			time.Sleep(time.Second * 30)
		}
	}
}
