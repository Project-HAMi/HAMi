/*
Copyright 2025 The HAMi Authors.

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

package metax

import (
	"fmt"

	"github.com/Project-HAMi/HAMi/pkg/util"
)

const (
	MetaxSDeviceAnno       = "metax-tech.com/node-gpu-devices"
	MetaxAllocatedSDevices = "metax-tech.com/gpu-devices-allocated"
	MetaxPredicateTime     = "metax-tech.com/predicate-time"

	MetaxUseUUID   = "metax-tech.com/use-gpuuuid"
	MetaxNoUseUUID = "metax-tech.com/nouse-gpuuuid"
)

type MetaxSDeviceInfo struct {
	UUID              string `json:"uuid"`
	BDF               string `json:"bdf,omitempty"`
	Model             string `json:"model,omitempty"`
	TotalDevCount     int32  `json:"totalDevCount,omitempty"`
	TotalCompute      int32  `json:"totalCompute,omitempty"`
	TotalVRam         int32  `json:"totalVRam,omitempty"`
	AvailableDevCount int32  `json:"availableDevCount,omitempty"`
	AvailableCompute  int32  `json:"availableCompute,omitempty"`
	AvailableVRam     int32  `json:"availableVRam,omitempty"`
	Numa              int32  `json:"numa,omitempty"`
	Healthy           bool   `json:"healthy,omitempty"`
}
type NodeMetaxSDeviceInfo []*MetaxSDeviceInfo

type ContainerMetaxSDevice struct {
	UUID    string `json:"uuid"`
	Compute int32  `json:"compute,omitempty"`
	VRam    int32  `json:"vRam,omitempty"`
}
type ContainerMetaxSDevices []ContainerMetaxSDevice
type PodMetaxSDevice []ContainerMetaxSDevices

func (ni NodeMetaxSDeviceInfo) String() string {
	str := "\n"

	for _, i := range ni {
		str += fmt.Sprintf("MetaxSDeviceInfo[%s]: TotalDevCount=%d, TotalCompute=%d, TotalVRam=%d, Numa=%d, Healthy=%t\n",
			i.UUID, i.TotalDevCount, i.TotalCompute, i.TotalVRam, i.Numa, i.Healthy)
	}

	return str
}

func (sdev *PodMetaxSDevice) String() string {
	str := fmt.Sprintf("\nPodMetaxSDevice:\n")

	for ctrIdx, ctrDevices := range *sdev {
		str += fmt.Sprintf("  container[%d]:\n", ctrIdx)

		for _, device := range ctrDevices {
			str += fmt.Sprintf("    SDevice[%s]: Compute=%d, VRam=%d\n",
				device.UUID, device.Compute, device.VRam)
		}
	}

	return str
}

func convertMetaxSDeviceToHAMIDevice(metaxSDevices []*MetaxSDeviceInfo) []*util.DeviceInfo {
	hamiDevices := make([]*util.DeviceInfo, len(metaxSDevices))

	for idx, sdevice := range metaxSDevices {
		hamiDevices[idx] = &util.DeviceInfo{
			ID:           sdevice.UUID,
			Index:        uint(idx),
			Count:        sdevice.TotalDevCount,
			Devmem:       sdevice.TotalVRam,
			Devcore:      sdevice.TotalCompute,
			Type:         MetaxSGPUDevice,
			Numa:         int(sdevice.Numa),
			Mode:         "",
			MIGTemplate:  []util.Geometry{},
			Health:       sdevice.Healthy,
			DeviceVendor: MetaxSGPUDevice,
		}
	}

	return hamiDevices
}

func convertHAMIPodDeviceToMetaxPodDevice(hamiPodDevices util.PodSingleDevice) PodMetaxSDevice {
	metaxDevices := make(PodMetaxSDevice, len(hamiPodDevices))

	for ctrIdx, ctrDevices := range hamiPodDevices {
		metaxDevices[ctrIdx] = make(ContainerMetaxSDevices, len(ctrDevices))
		for deviceIdx, device := range ctrDevices {
			metaxDevices[ctrIdx][deviceIdx] = ContainerMetaxSDevice{
				UUID:    device.UUID,
				VRam:    device.Usedmem,
				Compute: device.Usedcores,
			}
		}
	}

	return metaxDevices
}
