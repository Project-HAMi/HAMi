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
	"strings"
	"time"

	"github.com/NVIDIA/gpu-monitoring-tools/bindings/go/nvml"
	"k8s.io/klog/v2"

	"4pd.io/k8s-vgpu/pkg/api"
	"4pd.io/k8s-vgpu/pkg/util"
)

func (r *NvidiaDevicePlugin) getApiDevices() *[]*api.DeviceInfo {
	devs := r.Devices()
	nvml.Init()
	res := make([]*api.DeviceInfo, 0, len(devs))
	idx := 0
	for idx < len(devs) {
		ndev, err := nvml.NewDevice(uint(idx))
		//klog.V(3).Infoln("ndev type=", ndev.Model)
		if err != nil {
			fmt.Println("nvml new device by uuid error id=", ndev.UUID, "err=", err.Error())
			panic(0)
		} else {
			klog.V(3).Infoln("nvml registered device id=", ndev.UUID, "memory=", *ndev.Memory, "type=", *ndev.Model)
		}
		registeredmem := int32(*ndev.Memory)
		if *util.DeviceMemoryScaling != 1 {
			registeredmem = int32(float64(registeredmem) * *util.DeviceMemoryScaling)
		}
		health := true
		for _, val := range devs {
			if strings.Compare(val.ID, ndev.UUID) == 0 {
				if strings.Compare(val.Health, "healthy") == 0 {
					health = true
				} else {
					health = false
				}
				break
			}
		}
		res = append(res, &api.DeviceInfo{
			Id:      ndev.UUID,
			Count:   int32(*util.DeviceSplitCount),
			Devmem:  registeredmem,
			Devcore: int32(*util.DeviceCoresScaling * 100),
			Type:    fmt.Sprintf("%v-%v", "NVIDIA", *ndev.Model),
			Health:  health,
		})
		idx++
	}
	return &res
}

func (r *NvidiaDevicePlugin) RegistrInAnnotation() error {
	devices := r.getApiDevices()
	annos := make(map[string]string)
	node, err := util.GetNode(util.NodeName)
	if err != nil {
		klog.Errorln("get node error", err.Error())
		return err
	}
	encodeddevices := util.EncodeNodeDevices(*devices)
	annos[util.NodeHandshake] = "Reported " + time.Now().String()
	annos[util.NodeNvidiaDeviceRegistered] = encodeddevices
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
