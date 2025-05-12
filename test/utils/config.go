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

package utils

// test data.
const (
	GPUNodeLabelKey      = "gpu"
	GPUNodeLabelValue    = "on"
	GPUExecuteNvidiaSMI  = "nvidia-smi"
	GPUExecuteCudaSample = "/cuda-samples/sample"
	GPUPodMemory         = "300"
	GPUPodMemoryUnit     = "MiB"
	GPUPodCore           = "40"
	GPUNameSpace         = "hami-system"
	GPUNode              = "gpu-master"
	GPUCudaTestPass      = "Test PASSED"
)

// hami related.
const (
	HamiScheduler              = "hami-scheduler"
	HamiDevicePlugin           = "hami-device-plugin"
	ErrReasonFilteringFailed   = "FilteringFailed"
	ErrMessageFilteringFailed  = "no available node"
	ErrReasonFailedScheduling  = "FilteringFailed"
	ErrMessageFailedScheduling = "0/1 nodes are available"
)
