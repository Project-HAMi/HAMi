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
	corev1 "k8s.io/api/core/v1"
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

	DeviceLimit = 100
	//TimeLayout = "ANSIC"
	//DefaultTimeout = time.Second * 60.

	BestEffort string = "best-effort"
	Restricted string = "restricted"
	Guaranteed string = "guaranteed"

	// NodeNameEnvName define env var name for use get node name.
	NodeNameEnvName = "NODE_NAME"
	TaskPriority    = "CUDA_TASK_PRIORITY"
	CoreLimitSwitch = "GPU_CORE_UTILIZATION_POLICY"
)

var (
	DebugMode bool

	NodeName          string
	RuntimeSocketFlag string
)

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

type MigTemplate struct {
	Name   string `yaml:"name"`
	Memory int32  `yaml:"memory"`
	Count  int32  `yaml:"count"`
}

type MigTemplateUsage struct {
	Name   string `json:"name,omitempty"`
	Memory int32  `json:"memory,omitempty"`
	InUse  bool   `json:"inuse,omitempty"`
}

type Geometry []MigTemplate

type MIGS []MigTemplateUsage

type MigInUse struct {
	Index     int32
	UsageList MIGS
}

type AllowedMigGeometries struct {
	Models     []string   `yaml:"models"`
	Geometries []Geometry `yaml:"allowedGeometries"`
}

type DeviceUsage struct {
	ID          string
	Index       uint
	Used        int32
	Count       int32
	Usedmem     int32
	Totalmem    int32
	Totalcore   int32
	Usedcores   int32
	Mode        string
	MigTemplate []Geometry
	MigUsage    MigInUse
	Numa        int
	Type        string
	Health      bool
}

type DeviceInfo struct {
	ID              string          `json:"id,omitempty"`
	Index           uint            `json:"index,omitempty"`
	Count           int32           `json:"count,omitempty"`
	Devmem          int32           `json:"devmem,omitempty"`
	Devcore         int32           `json:"devcore,omitempty"`
	Type            string          `json:"type,omitempty"`
	Numa            int             `json:"numa,omitempty"`
	Mode            string          `json:"mode,omitempty"`
	MIGTemplate     []Geometry      `json:"migtemplate,omitempty"`
	Health          bool            `json:"health,omitempty"`
	DeviceVendor    string          `json:"devicevendor,omitempty"`
	DevicePairScore DevicePairScore `json:"devicepairscore,omitempty"`
}

type DevicePairScores []DevicePairScore
type DevicePairScore struct {
	ID     string         `json:"uuid,omitempty"`
	Scores map[string]int `json:"score,omitempty"`
}

type NodeInfo struct {
	ID      string
	Node    *corev1.Node
	Devices []DeviceInfo
}

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
)

func (s SchedulerPolicyName) String() string {
	return string(s)
}
