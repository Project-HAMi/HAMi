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
<<<<<<< HEAD
	AssignedTimeAnnotations = "hami.io/vgpu-time"
	AssignedNodeAnnotations = "hami.io/vgpu-node"
	BindTimeAnnotations     = "hami.io/bind-time"
	DeviceBindPhase         = "hami.io/bind-phase"
=======
	//ResourceName = "nvidia.com/gpu"
	//ResourceName = "4pd.io/vgpu"
	AssignedTimeAnnotations          = "4pd.io/vgpu-time"
	AssignedIDsAnnotations           = "4pd.io/vgpu-ids-new"
	AssignedIDsToAllocateAnnotations = "4pd.io/devices-to-allocate"
	AssignedNodeAnnotations          = "4pd.io/vgpu-node"
	BindTimeAnnotations              = "4pd.io/bind-time"
	DeviceBindPhase                  = "4pd.io/bind-phase"
>>>>>>> 21785f7 (update to v2.3.2)

	DeviceBindAllocating = "allocating"
	DeviceBindFailed     = "failed"
	DeviceBindSuccess    = "success"

	DeviceLimit = 100
	//TimeLayout = "ANSIC"
	//DefaultTimeout = time.Second * 60.

	BestEffort string = "best-effort"
	Restricted string = "restricted"
	Guaranteed string = "guaranteed"
<<<<<<< HEAD

	// NodeNameEnvName define env var name for use get node name.
	NodeNameEnvName = "NODE_NAME"
	TaskPriority    = "CUDA_TASK_PRIORITY"
	CoreLimitSwitch = "GPU_CORE_UTILIZATION_POLICY"
=======
>>>>>>> 21785f7 (update to v2.3.2)
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
<<<<<<< HEAD
<<<<<<< HEAD
	DebugMode         bool
	NodeName          string
	RuntimeSocketFlag string
=======
	ResourceName          string
	ResourceMem           string
	ResourceCores         string
	ResourceMemPercentage string
	ResourcePriority      string
	DebugMode             bool

	MLUResourceCount  string
	MLUResourceMemory string

	KnownDevice = map[string]string{
		NodeHandshake:    NodeNvidiaDeviceRegistered,
		NodeMLUHandshake: NodeMLUDeviceRegistered,
	}
=======
	DebugMode bool
>>>>>>> 21785f7 (update to v2.3.2)

	DeviceSplitCount    *uint
	DeviceMemoryScaling *float64
	DeviceCoresScaling  *float64
	NodeName            string
	RuntimeSocketFlag   string
	DisableCoreLimit    *bool
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
)

type SchedulerPolicyName string

const (
	// NodeSchedulerPolicyBinpack is node use binpack scheduler policy.
	NodeSchedulerPolicyBinpack SchedulerPolicyName = "binpack"
	// NodeSchedulerPolicySpread is node use spread scheduler policy.
	NodeSchedulerPolicySpread SchedulerPolicyName = "spread"
	// GPUSchedulerPolicyBinpack is GPU use binpack scheduler.
	GPUSchedulerPolicyBinpack SchedulerPolicyName = "binpack"
	// GPUSchedulerPolicySpread is GPU use spread scheduler.
	GPUSchedulerPolicySpread SchedulerPolicyName = "spread"
	// GPUSchedulerPolicyTopology is GPU use topology scheduler.
	GPUSchedulerPolicyTopology SchedulerPolicyName = "topology-aware"
)

const (
	// NodeSchedulerPolicyAnnotationKey is user set Pod annotation to change this default node policy.
	NodeSchedulerPolicyAnnotationKey = "hami.io/node-scheduler-policy"
	// GPUSchedulerPolicyAnnotationKey is user set Pod annotation to change this default GPU policy.
	GPUSchedulerPolicyAnnotationKey = "hami.io/gpu-scheduler-policy"
)

func (s SchedulerPolicyName) String() string {
	return string(s)
}

<<<<<<< HEAD
const (
	Weight int = 10
)
=======
type ContainerDeviceRequest struct {
	Nums             int32
	Type             string
	Memreq           int32
	MemPercentagereq int32
	Coresreq         int32
}

type ContainerDevices []ContainerDevice

type PodDevices []ContainerDevices

type DeviceUsage struct {
	Id        string
	Index     uint
	Used      int32
	Count     int32
	Usedmem   int32
	Totalmem  int32
	Totalcore int32
	Usedcores int32
	Type      string
	Health    bool
}
>>>>>>> 21785f7 (update to v2.3.2)
