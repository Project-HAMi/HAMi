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

package mlu

import (
	"fmt"
	"time"

	"k8s.io/klog/v2"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

	"4pd.io/k8s-vgpu/pkg/api"
	"4pd.io/k8s-vgpu/pkg/device-plugin/config"
	"4pd.io/k8s-vgpu/pkg/device-plugin/mlu/cndev"
	"4pd.io/k8s-vgpu/pkg/util"
)

type DevListFunc func() []*pluginapi.Device

type DeviceRegister struct {
	deviceCache *DeviceCache
	unhealthy   chan *pluginapi.Device
	stopCh      chan struct{}
}

func NewDeviceRegister(deviceCache *DeviceCache) *DeviceRegister {
	return &DeviceRegister{
		deviceCache: deviceCache,
		unhealthy:   make(chan *pluginapi.Device),
		stopCh:      make(chan struct{}),
	}
}

func (r *DeviceRegister) Start(opt Options) {
	r.deviceCache.AddNotifyChannel("register", r.unhealthy)
	go r.WatchAndRegister(opt)
}

func (r *DeviceRegister) Stop() {
	close(r.stopCh)
}

func (r *DeviceRegister) apiDevices() *[]*api.DeviceInfo {
	devs := r.deviceCache.GetCache()
	res := make([]*api.DeviceInfo, 0, len(devs))
	for i, dev := range devs {
		//klog.V(3).Infoln("ndev type=", ndev.Model)
		memory, _ := cndev.GetDeviceMemory(uint(i))
		fmt.Println("mlu registered device id=", dev.dev.ID, "memory=", memory, "type=", cndev.GetDeviceModel(uint(i)))
		registeredmem := int32(memory)
		if config.DeviceMemoryScaling > 1 {
			fmt.Println("Memory Scaling to", config.DeviceMemoryScaling)
			registeredmem = int32(float64(registeredmem) * config.DeviceMemoryScaling)
		}
		res = append(res, &api.DeviceInfo{
			Id:     dev.dev.ID,
			Count:  int32(config.DeviceSplitCount),
			Devmem: registeredmem,
			Type:   fmt.Sprintf("%v-%v", "MLU", cndev.GetDeviceModel(uint(i))),
			Health: dev.dev.Health == "healthy",
		})
	}
	return &res
}

func (r *DeviceRegister) RegistrInAnnotation() error {
	devices := r.apiDevices()
	annos := make(map[string]string)
	node, err := util.GetNode(config.NodeName)
	if err != nil {
		klog.Errorln("get node error", err.Error())
		return err
	}
	encodeddevices := util.EncodeNodeDevices(*devices)
	annos[util.NodeMLUHandshake] = "Reported " + time.Now().String()
	annos[util.NodeMLUDeviceRegistered] = encodeddevices
	klog.Infoln("Reporting devices", encodeddevices, "in", time.Now().String())
	err = util.PatchNodeAnnotations(node, annos)

	if err != nil {
		klog.Errorln("patch node error", err.Error())
	}
	return err
}

func (r *DeviceRegister) WatchAndRegister(opt Options) {
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
