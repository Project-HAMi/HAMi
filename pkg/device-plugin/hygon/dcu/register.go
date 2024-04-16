/*
Copyright 2024 The HAMi Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package dcu

import (
	"fmt"
	"os"
	"time"

	"k8s.io/klog/v2"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

	"github.com/Project-HAMi/HAMi/pkg/api"
	"github.com/Project-HAMi/HAMi/pkg/device/hygon"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

type DevListFunc func() []*kubeletdevicepluginv1beta1.Device

func (r *Plugin) apiDevices() *[]*api.DeviceInfo {
	res := []*api.DeviceInfo{}
	for idx, val := range r.totalmem {
		if val > 0 {
			res = append(res, &api.DeviceInfo{
				Index:   idx,
				Id:      "DCU-" + fmt.Sprint(idx),
				Count:   30,
				Devmem:  int32(val),
				Devcore: 100,
				Numa:    0,
				Type:    r.cardtype[idx],
				Health:  true,
			})
		}
	}
	return &res
}

func (r *Plugin) RegistrInAnnotation() error {
	devices := r.apiDevices()
	annos := make(map[string]string)
	if len(util.NodeName) == 0 {
		util.NodeName = os.Getenv(util.NodeNameEnvName)
	}
	node, err := util.GetNode(util.NodeName)
	if err != nil {
		klog.Errorln("get node error", err.Error())
		return err
	}
	encodeddevices := util.EncodeNodeDevices(*devices)
	annos[hygon.HandshakeAnnos] = "Reported " + time.Now().String()
	annos[hygon.RegisterAnnos] = encodeddevices
	klog.Infoln("Reporting devices", encodeddevices, "in", time.Now().String())
	err = util.PatchNodeAnnotations(node, annos)

	if err != nil {
		klog.Errorln("patch node error", err.Error())
	}
	return err
}

func (r *Plugin) WatchAndRegister() {
	klog.Info("into WatchAndRegister")
	for {
		r.RefreshContainerDevices()
		err := r.RegistrInAnnotation()
		if err != nil {
			klog.Errorf("register error, %v", err)
			time.Sleep(time.Second * 5)
		} else {
			time.Sleep(time.Second * 30)
		}
	}
}
