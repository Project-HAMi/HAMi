/*
Copyright 2026 The HAMi Authors.

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

package scheduler

import (
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

// mixedCapacityNode holds two NVIDIA cards of different memory capacity,
// registered the way the device plugin registers them: the device type is the
// card's model name, not the "NVIDIA" common word the request carries.
func mixedCapacityNode(gpuPolicy string) *NodeUsage {
	return &NodeUsage{
		Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "mixed-node"}},
		Devices: policy.DeviceUsageList{
			Policy: gpuPolicy,
			DeviceLists: []*policy.DeviceListsScore{
				{Device: &device.DeviceUsage{
					ID:        "gpu-40g",
					Index:     0,
					Type:      "NVIDIA A100-SXM4-40GB",
					Count:     10,
					Totalcore: 100,
					Totalmem:  40960,
					Numa:      0,
					Health:    true,
				}},
				{Device: &device.DeviceUsage{
					ID:        "gpu-80g",
					Index:     1,
					Type:      "NVIDIA A100-SXM4-80GB",
					Count:     10,
					Used:      1,
					Usedcores: 5,
					Usedmem:   4096,
					Totalcore: 100,
					Totalmem:  81920,
					Numa:      0,
					Health:    true,
				}},
			},
		},
	}
}

// Binpack must rank cards by their utilisation *after* the pending request is
// placed. On a node whose cards differ in capacity, scoring on current usage
// alone sends the pod to the 80GB card (0.20 used today vs 0.00) even though
// the 40GB card is the tighter fit for a 20GB request (0.90 vs 0.85 after
// placement) and keeping the large card free is the point of binpack.
func TestBinpackPacksSmallerCardOnMixedCapacityNode(t *testing.T) {
	assert.NilError(t, config.InitDevicesWithConfig(&config.Config{
		NvidiaConfig: nvidia.NvidiaConfig{
			ResourceCountName:            "hami.io/gpu",
			ResourceMemoryName:           "hami.io/gpumem",
			ResourceMemoryPercentageName: "hami.io/gpumem-percentage",
			ResourceCoreName:             "hami.io/gpucores",
			DefaultGPUNum:                1,
		},
	}))

	task := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "trainer", Namespace: "default"}}
	node := mixedCapacityNode(util.GPUSchedulerPolicyBinpack.String())
	requests := device.ContainerDeviceRequests{
		nvidia.NvidiaGPUDevice: {
			Nums:             1,
			Type:             nvidia.NvidiaGPUDevice,
			Memreq:           20480,
			MemPercentagereq: 101,
			Coresreq:         30,
		},
	}

	devinput := &device.PodDevices{}
	fit, reason := fitInDevices(node, requests, task, nil, devinput, util.DefaultDeviceScoringWeights())
	assert.Assert(t, fit, "expected the pod to fit; reason=%s", reason)

	containers := (*devinput)[nvidia.NvidiaGPUDevice]
	assert.Equal(t, len(containers), 1)
	assert.Equal(t, len(containers[0]), 1)
	assert.Equal(t, containers[0][0].UUID, "gpu-40g",
		"binpack placed the pod by current usage instead of usage after placement")
}
