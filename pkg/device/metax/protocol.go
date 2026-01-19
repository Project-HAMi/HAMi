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
	"strings"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

const (
	MetaxSDeviceAnno       = "metax-tech.com/node-gpu-devices"
	MetaxAllocatedSDevices = "metax-tech.com/gpu-devices-allocated"
	MetaxPredicateTime     = "metax-tech.com/predicate-time"

	MetaxUseUUID   = "metax-tech.com/use-gpuuuid"
	MetaxNoUseUUID = "metax-tech.com/nouse-gpuuuid"

	MetaxSGPUQosPolicy     = "metax-tech.com/sgpu-qos-policy"
	MetaxSGPUTopologyAware = "metax-tech.com/sgpu-topology-aware"
	MetaxSGPUAppClass      = "metax-tech.com/sgpu-app-class"
)

const (
	BestEffort = "best-effort"
	FixedShare = "fixed-share"
	BurstShare = "burst-share"
)

const (
	Online  = "online"
	Offline = "offline"
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
	QosPolicy         string `json:"qosPolicy,omitempty"`
	LinkZone          int32  `json:"linkZone,omitempty"`
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
	var str strings.Builder
	str.WriteString("\n")

	for _, i := range ni {
		str.WriteString(fmt.Sprintf("MetaxSDeviceInfo[%s]: TotalDevCount=%d, TotalCompute=%d, TotalVRam=%d, Numa=%d, Healthy=%t, QosPolicy=%s, LinkZone=%d\n",
			i.UUID, i.TotalDevCount, i.TotalCompute, i.TotalVRam, i.Numa, i.Healthy, i.QosPolicy, i.LinkZone))
	}

	return str.String()
}

func (sdev *PodMetaxSDevice) String() string {
	var str strings.Builder
	str.WriteString("\nPodMetaxSDevice:\n")

	for ctrIdx, ctrDevices := range *sdev {
		str.WriteString(fmt.Sprintf("  container[%d]:\n", ctrIdx))

		for _, device := range ctrDevices {
			str.WriteString(fmt.Sprintf("    SDevice[%s]: Compute=%d, VRam=%d\n",
				device.UUID, device.Compute, device.VRam))
		}
	}

	return str.String()
}

func convertMetaxSDeviceToHAMIDevice(metaxSDevices []*MetaxSDeviceInfo) []*device.DeviceInfo {
	hamiDevices := make([]*device.DeviceInfo, len(metaxSDevices))

	for idx, sdevice := range metaxSDevices {
		hamiDevices[idx] = &device.DeviceInfo{
			ID:           sdevice.UUID,
			Index:        uint(idx),
			Count:        sdevice.TotalDevCount,
			Devmem:       sdevice.TotalVRam,
			Devcore:      sdevice.TotalCompute,
			Type:         MetaxSGPUDevice,
			Numa:         int(sdevice.Numa),
			Mode:         "",
			MIGTemplate:  []device.Geometry{},
			Health:       sdevice.Healthy,
			DeviceVendor: MetaxSGPUDevice,
			CustomInfo: map[string]any{
				"QosPolicy": sdevice.QosPolicy,
				"Model":     sdevice.Model,
				"LinkZone":  sdevice.LinkZone,
			},
		}
	}

	return hamiDevices
}

func convertHAMIPodDeviceToMetaxPodDevice(hamiPodDevices device.PodSingleDevice) PodMetaxSDevice {
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
