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

const (
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
	DebugMode         bool
	NodeName          string
	RuntimeSocketFlag string
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
