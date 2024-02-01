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

	"github.com/Project-HAMi/HAMi/pkg/api"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/mlu/cndev"
	"github.com/Project-HAMi/HAMi/pkg/device/cambricon"
	"github.com/Project-HAMi/HAMi/pkg/util"
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
		res = append(res, &api.DeviceInfo{
			Id:      dev.dev.ID,
			Count:   int32(*util.DeviceSplitCount),
			Devmem:  registeredmem,
			Devcore: 0,
			Numa:    0,
			Type:    fmt.Sprintf("%v-%v", "MLU", cndev.GetDeviceModel(uint(i))),
			Health:  dev.dev.Health == "healthy",
		})
	}
	return &res
}

func (r *DeviceRegister) RegistrInAnnotation() error {
	devices := r.apiDevices()
	annos := make(map[string]string)
	node, err := util.GetNode(util.NodeName)
	if err != nil {
		klog.Errorln("get node error", err.Error())
		return err
	}
	encodeddevices := util.EncodeNodeDevices(*devices)
	annos[cambricon.HandshakeAnnos] = "Reported " + time.Now().String()
	annos[cambricon.RegisterAnnos] = encodeddevices
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
