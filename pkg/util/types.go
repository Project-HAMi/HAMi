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

	// LeaderRoleLabel is the label key used to identify the leader pod.
	HAMiRoleLabel = "hami.io/scheduler-role"
	// LeaderRoleLabelValueLeader is the label value used to identify the leader pod.
	HAMiRoleLabelValueLeader = "leader"
	// LeaderRoleLabelValueFollower is the label value used to identify the follower pod.
	HAMiRoleLabelValueFollower = "follower"

	// HAMiSchedulerLabelValue is the label key to identify the component of HAMi in Kubernetes.
	HAMiComponentLabel = "app.kubernetes.io/component"
	// HAMiComponentScheduler the label value for hami-scheduler.
	HAMiComponentScheduler = "hami-scheduler"

	// PodGroupLabel is the label used by scheduler-plugins Coscheduling to mark
	// a pod as a member of a PodGroup. See
	// https://github.com/kubernetes-sigs/scheduler-plugins/blob/master/apis/scheduling/v1alpha1/types.go
	PodGroupLabel = "scheduling.x-k8s.io/pod-group"
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
	// GPUSchedulerPolicyMutex is GPU use mutex scheduler, only idle GPUs are allocated.
	GPUSchedulerPolicyMutex SchedulerPolicyName = "mutex"
	// GPUSchedulerPolicyNuma is GPU use numa scheduler, chained as a sort key alongside binpack/spread.
	GPUSchedulerPolicyNuma SchedulerPolicyName = "numa"
)

const (
	// NodeSchedulerPolicyAnnotationKey is user set Pod annotation to change this default node policy.
	NodeSchedulerPolicyAnnotationKey = "hami.io/node-scheduler-policy"
	// GPUSchedulerPolicyAnnotationKey is user set Pod annotation to change this default GPU policy.
	GPUSchedulerPolicyAnnotationKey = "hami.io/gpu-scheduler-policy"
	// DeviceScoringWeightsAnnotationKey configures per-Pod slot, core, and memory scoring weights.
	DeviceScoringWeightsAnnotationKey = "hami.io/device-scoring-weights"
	// NumaAlignmentAnnotationKey is a per-Pod annotation selecting how HAMi
	// reconciles the scheduler-selected GPU with the NUMA-aligned replica
	// preferred by kubelet's Topology Manager. See NumaAlignmentMode.
	NumaAlignmentAnnotationKey = "hami.io/numa-alignment"
	// TopologyModeAnnotationKey is a per-Pod annotation opting the Pod into NPU
	// topology aware scheduling. See TopologyModeHard and TopologyModeSoft for
	// the accepted values.
	TopologyModeAnnotationKey = "hami.io/npu-topology-mode"
	// TopologyHighestTierAnnotationKey is a per-Pod annotation capping how far up
	// the NPU interconnect hierarchy the group of the Pod may spread. Tier 1 is
	// the smallest domain and a larger tier a wider one; a node is only eligible
	// when its lowest common domain with the group is not above that tier.
	TopologyHighestTierAnnotationKey = "hami.io/npu-highest-tier-allowed"
	// TopologyGroupAnnotationKey overrides how the group of Pods that has to
	// stay close to each other is identified. Without it the group is derived
	// from the usual job labels, see topologyGroupLabels in the scheduler.
	TopologyGroupAnnotationKey = "hami.io/npu-topology-group"
	// TopologyGangSizeAnnotationKey declares how many Pods make up the gang
	// that has to be placed as a whole. When it is set with a value greater
	// than one, the topology hard filter proves before binding the first Pod
	// that one allowed topology domain can hold every member of the gang.
	// Without it the existing incremental topology behavior is kept.
	TopologyGangSizeAnnotationKey = "hami.io/npu-topology-gang-size"
)

// Topology modes accepted by TopologyModeAnnotationKey, and the values of the
// scheduler's --npu-topology-mode flag.
const (
	// TopologyModeHard rejects every node that would place the Pod outside the
	// tier its group allows.
	TopologyModeHard = "hard"
	// TopologyModeSoft keeps every node eligible and only ranks the closer ones
	// higher.
	TopologyModeSoft = "soft"
)

func (s SchedulerPolicyName) String() string {
	return string(s)
}

const (
	Weight int = 10
)
