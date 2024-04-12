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

package util

import (
	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
)

const (
	//ResourceName = "nvidia.com/gpu"
	//ResourceName = "hami.io/vgpu".
	AssignedTimeAnnotations = "hami.io/vgpu-time"
	AssignedNodeAnnotations = "hami.io/vgpu-node"
	BindTimeAnnotations     = "hami.io/bind-time"
	DeviceBindPhase         = "hami.io/bind-phase"

	DeviceBindAllocating = "allocating"
	DeviceBindFailed     = "failed"
	DeviceBindSuccess    = "success"

	//Set default mem to 5000m
	//DefaultMem   = 5000
	//DefaultCores = 0.

	DeviceLimit = 100
	//TimeLayout = "ANSIC"
	//DefaultTimeout = time.Second * 60.

	BestEffort string = "best-effort"
	Restricted string = "restricted"
	Guaranteed string = "guaranteed"

	// NodeNameEnvName define env var name for use get node name.
	NodeNameEnvName = "NODE_NAME"
)

type DevicePluginConfigs struct {
	Nodeconfig []struct {
		Name                string  `json:"name"`
		Devicememoryscaling float64 `json:"devicememoryscaling"`
		Devicecorescaling   float64 `json:"devicecorescaling"`
		Devicesplitcount    uint    `json:"devicesplitcount"`
		Migstrategy         string  `json:"migstrategy"`
	} `json:"nodeconfig"`
}

type DeviceConfig struct {
	*spec.Config

	ResourceName *string
	DebugMode    *bool
}

var (
	DebugMode bool

	DeviceSplitCount    *uint
	DeviceMemoryScaling *float64
	DeviceCoresScaling  *float64
	NodeName            string
	RuntimeSocketFlag   string
	DisableCoreLimit    *bool
)

//	type ContainerDevices struct {
//	   Devices []string `json:"devices,omitempty"`
//	}
//
//	type PodDevices struct {
//	   Containers []ContainerDevices `json:"containers,omitempty"`
//	}
type ContainerDevice struct {
	// TODO current Idx cannot use, because EncodeContainerDevices method not encode this filed.
	Idx       int
	UUID      string
	Type      string
	Usedmem   int32
	Usedcores int32
}

type ContainerDeviceRequest struct {
	Nums             int32
	Type             string
	Memreq           int32
	MemPercentagereq int32
	Coresreq         int32
}

type ContainerDevices []ContainerDevice
type ContainerDeviceRequests map[string]ContainerDeviceRequest

// type ContainerAllDevices map[string]ContainerDevices.
type PodSingleDevice []ContainerDevices

type PodDeviceRequests []ContainerDeviceRequests
type PodDevices map[string]PodSingleDevice

type DeviceUsage struct {
	ID        string
	Index     uint
	Used      int32
	Count     int32
	Usedmem   int32
	Totalmem  int32
	Totalcore int32
	Usedcores int32
	Numa      int
	Type      string
	Health    bool
}

type DeviceInfo struct {
	ID      string
	Index   uint
	Count   int32
	Devmem  int32
	Devcore int32
	Type    string
	Numa    int
	Health  bool
}

type NodeInfo struct {
	ID      string
	Devices []DeviceInfo
}
