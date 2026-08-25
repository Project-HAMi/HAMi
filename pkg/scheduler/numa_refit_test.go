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
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

func refitTestNode(policyName string, numaBind bool, devices ...*device.DeviceUsage) *NodeUsage {
	lists := make([]*policy.DeviceListsScore, 0, len(devices))
	for _, d := range devices {
		lists = append(lists, &policy.DeviceListsScore{Device: d})
	}
	return &NodeUsage{
		Node:    &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
		Devices: policy.DeviceUsageList{Policy: policyName, NumaBind: numaBind, DeviceLists: lists},
	}
}

func refitTestPod(annotations map[string]string) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name:        "refit-pod",
		Namespace:   "default",
		Annotations: annotations,
	}}
}

func refitTestRequest(nums, memreq, coresreq int32) device.ContainerDeviceRequests {
	return device.ContainerDeviceRequests{
		nvidia.NvidiaGPUDevice: device.ContainerDeviceRequest{
			Nums:     nums,
			Type:     nvidia.NvidiaGPUDevice,
			Memreq:   memreq,
			Coresreq: coresreq,
		},
	}
}

func selectedUUIDs(devinput device.PodDevices) []string {
	uuids := []string{}
	for _, containers := range devinput {
		for _, containerDevices := range containers {
			for _, d := range containerDevices {
				uuids = append(uuids, d.UUID)
			}
		}
	}
	return uuids
}

func TestRestrictNodeUsagePreservesPolicyAndOriginal(t *testing.T) {
	deviceA := makeDevice("GPU-a", 0, nvidia.NvidiaGPUDevice, 3, 10, 40000, 30000, 30, 100)
	deviceB := makeDevice("GPU-b", 0, nvidia.NvidiaGPUDevice, 2, 10, 40000, 20000, 20, 100)
	deviceC := makeDevice("GPU-c", 1, nvidia.NvidiaGPUDevice, 1, 10, 40000, 10000, 10, 100)
	node := refitTestNode("binpack", true, deviceA, deviceB, deviceC)

	restricted := restrictNodeUsage(node, nvidia.NvidiaGPUDevice, []string{"GPU-b", "GPU-c", "GPU-unknown"})

	assert.Equal(t, len(restricted.Devices.DeviceLists), 2)
	assert.Equal(t, restricted.Devices.DeviceLists[0].Device.ID, "GPU-b")
	assert.Equal(t, restricted.Devices.DeviceLists[1].Device.ID, "GPU-c")
	assert.Equal(t, restricted.Devices.Policy, "binpack")
	assert.Equal(t, restricted.Devices.NumaBind, true)

	// The original node keeps its full device list and is not aliased.
	assert.Equal(t, len(node.Devices.DeviceLists), 3)
	restricted.Devices.DeviceLists[0].Device.Used = 99
	assert.Equal(t, deviceB.Used, int32(2))
}

func TestRestrictNodeUsageEmptyAllowedSet(t *testing.T) {
	node := refitTestNode("binpack", false,
		makeDevice("GPU-a", 0, nvidia.NvidiaGPUDevice, 0, 10, 40000, 0, 0, 100))

	restricted := restrictNodeUsage(node, nvidia.NvidiaGPUDevice, nil)
	assert.Equal(t, len(restricted.Devices.DeviceLists), 0)
}

func TestFitInRestrictedDevicesMatchesFitOnSmallerNode(t *testing.T) {
	build := func() []*device.DeviceUsage {
		return []*device.DeviceUsage{
			makeDevice("GPU-a", 0, nvidia.NvidiaGPUDevice, 3, 10, 40000, 30000, 30, 100),
			makeDevice("GPU-b", 0, nvidia.NvidiaGPUDevice, 2, 10, 40000, 20000, 20, 100),
			makeDevice("GPU-c", 1, nvidia.NvidiaGPUDevice, 1, 10, 40000, 10000, 10, 100),
		}
	}
	allowed := []string{"GPU-b", "GPU-c"}

	for _, policyName := range []string{"binpack", "spread", "binpack,numa", "spread,numa"} {
		t.Run(policyName, func(t *testing.T) {
			restrictedInput := device.PodDevices{}
			fit, reason := fitInRestrictedDevices(
				refitTestNode(policyName, false, build()...),
				refitTestRequest(1, 4096, 10), nvidia.NvidiaGPUDevice, allowed,
				refitTestPod(nil), &device.NodeInfo{}, &restrictedInput,
				util.DefaultDeviceScoringWeights())
			assert.Assert(t, fit, "restricted fit failed: %s", reason)

			smallerNodeInput := device.PodDevices{}
			devices := build()
			smallerFit, smallerReason := fitInDevices(
				refitTestNode(policyName, false, devices[1], devices[2]),
				refitTestRequest(1, 4096, 10),
				refitTestPod(nil), &device.NodeInfo{}, &smallerNodeInput,
				util.DefaultDeviceScoringWeights())
			assert.Assert(t, smallerFit, "smaller-node fit failed: %s", smallerReason)

			assert.DeepEqual(t, selectedUUIDs(restrictedInput), selectedUUIDs(smallerNodeInput))
		})
	}
}

func TestFitInRestrictedDevicesHonorsPolicyOrdering(t *testing.T) {
	build := func() *NodeUsage {
		return refitTestNode("binpack", false,
			makeDevice("GPU-a", 0, nvidia.NvidiaGPUDevice, 3, 10, 40000, 30000, 30, 100),
			makeDevice("GPU-b", 0, nvidia.NvidiaGPUDevice, 2, 10, 40000, 20000, 20, 100),
			makeDevice("GPU-c", 1, nvidia.NvidiaGPUDevice, 1, 10, 40000, 10000, 10, 100))
	}

	// Unrestricted binpack prefers the most utilized device that fits.
	unrestrictedInput := device.PodDevices{}
	fit, reason := fitInDevices(build(), refitTestRequest(1, 4096, 10),
		refitTestPod(nil), &device.NodeInfo{}, &unrestrictedInput,
		util.DefaultDeviceScoringWeights())
	assert.Assert(t, fit, "unrestricted fit failed: %s", reason)
	assert.DeepEqual(t, selectedUUIDs(unrestrictedInput), []string{"GPU-a"})

	// Restricted to the other two devices, binpack still applies among them.
	restrictedInput := device.PodDevices{}
	fit, reason = fitInRestrictedDevices(build(), refitTestRequest(1, 4096, 10),
		nvidia.NvidiaGPUDevice, []string{"GPU-b", "GPU-c"},
		refitTestPod(nil), &device.NodeInfo{}, &restrictedInput,
		util.DefaultDeviceScoringWeights())
	assert.Assert(t, fit, "restricted fit failed: %s", reason)
	assert.DeepEqual(t, selectedUUIDs(restrictedInput), []string{"GPU-b"})
}

func TestFitInRestrictedDevicesMultiGPUWithoutNumaBind(t *testing.T) {
	node := refitTestNode("binpack", false,
		makeDevice("GPU-a", 0, nvidia.NvidiaGPUDevice, 3, 10, 40000, 30000, 30, 100),
		makeDevice("GPU-b", 0, nvidia.NvidiaGPUDevice, 2, 10, 40000, 20000, 20, 100),
		makeDevice("GPU-c", 1, nvidia.NvidiaGPUDevice, 1, 10, 40000, 10000, 10, 100))

	devinput := device.PodDevices{}
	fit, reason := fitInRestrictedDevices(node, refitTestRequest(2, 4096, 10),
		nvidia.NvidiaGPUDevice, []string{"GPU-b", "GPU-c"},
		refitTestPod(nil), &device.NodeInfo{}, &devinput,
		util.DefaultDeviceScoringWeights())

	assert.Assert(t, fit, "multi-GPU restricted fit failed: %s", reason)
	uuids := selectedUUIDs(devinput)
	assert.Equal(t, len(uuids), 2)
	for _, id := range uuids {
		assert.Assert(t, id == "GPU-b" || id == "GPU-c", "device %s is outside the allowed set", id)
	}
}

func TestFitInRestrictedDevicesNoOpWhenPickAllowed(t *testing.T) {
	build := func() *NodeUsage {
		return refitTestNode("binpack", false,
			makeDevice("GPU-a", 0, nvidia.NvidiaGPUDevice, 3, 10, 40000, 30000, 30, 100),
			makeDevice("GPU-b", 0, nvidia.NvidiaGPUDevice, 2, 10, 40000, 20000, 20, 100),
			makeDevice("GPU-c", 1, nvidia.NvidiaGPUDevice, 1, 10, 40000, 10000, 10, 100))
	}

	unrestrictedInput := device.PodDevices{}
	fit, reason := fitInDevices(build(), refitTestRequest(1, 4096, 10),
		refitTestPod(nil), &device.NodeInfo{}, &unrestrictedInput,
		util.DefaultDeviceScoringWeights())
	assert.Assert(t, fit, "unrestricted fit failed: %s", reason)

	// Allowing every device makes the restricted fit a no-op: it must pick
	// exactly what the unrestricted fit picks.
	restrictedInput := device.PodDevices{}
	fit, reason = fitInRestrictedDevices(build(), refitTestRequest(1, 4096, 10),
		nvidia.NvidiaGPUDevice, []string{"GPU-a", "GPU-b", "GPU-c"},
		refitTestPod(nil), &device.NodeInfo{}, &restrictedInput,
		util.DefaultDeviceScoringWeights())
	assert.Assert(t, fit, "superset restricted fit failed: %s", reason)

	assert.DeepEqual(t, selectedUUIDs(restrictedInput), selectedUUIDs(unrestrictedInput))
}

func TestFitInRestrictedDevicesEnforcesCapacity(t *testing.T) {
	// GPU-a would fit but is not allowed; the allowed GPU-b lacks memory.
	node := refitTestNode("binpack", false,
		makeDevice("GPU-a", 0, nvidia.NvidiaGPUDevice, 0, 10, 40000, 0, 0, 100),
		makeDevice("GPU-b", 0, nvidia.NvidiaGPUDevice, 9, 10, 8192, 8000, 20, 100))

	devinput := device.PodDevices{}
	fit, reason := fitInRestrictedDevices(node, refitTestRequest(1, 4096, 10),
		nvidia.NvidiaGPUDevice, []string{"GPU-b"},
		refitTestPod(nil), &device.NodeInfo{}, &devinput,
		util.DefaultDeviceScoringWeights())

	assert.Equal(t, fit, false)
	assert.Assert(t, strings.Contains(reason, common.CardInsufficientMemory),
		"reason %q should mention %s", reason, common.CardInsufficientMemory)
}

func TestFitInRestrictedDevicesEmptyAllowedSet(t *testing.T) {
	node := refitTestNode("binpack", false,
		makeDevice("GPU-a", 0, nvidia.NvidiaGPUDevice, 0, 10, 40000, 0, 0, 100))

	devinput := device.PodDevices{}
	fit, reason := fitInRestrictedDevices(node, refitTestRequest(1, 4096, 10),
		nvidia.NvidiaGPUDevice, nil, refitTestPod(nil), &device.NodeInfo{}, &devinput,
		util.DefaultDeviceScoringWeights())

	assert.Equal(t, fit, false)
	assert.Equal(t, reason, AllowedSetUnmatched)
}

func TestRestrictNodeUsageKeepsOtherDeviceTypes(t *testing.T) {
	otherDevice := makeDevice("OTHER-a", 0, "SomeOtherVendor", 0, 10, 40000, 0, 0, 100)
	node := refitTestNode("binpack", false,
		makeDevice("GPU-a", 0, nvidia.NvidiaGPUDevice, 0, 10, 40000, 0, 0, 100),
		makeDevice("GPU-b", 0, nvidia.NvidiaGPUDevice, 0, 10, 40000, 0, 0, 100),
		otherDevice)

	restricted := restrictNodeUsage(node, nvidia.NvidiaGPUDevice, []string{"GPU-b"})

	// Only the NVIDIA list is restricted; the other vendor's device stays.
	assert.Equal(t, len(restricted.Devices.DeviceLists), 2)
	assert.Equal(t, restricted.Devices.DeviceLists[0].Device.ID, "GPU-b")
	assert.Equal(t, restricted.Devices.DeviceLists[1].Device.ID, "OTHER-a")
}

func TestFitInRestrictedDevicesUnmatchedAllowedSet(t *testing.T) {
	node := refitTestNode("binpack", false,
		makeDevice("GPU-a", 0, nvidia.NvidiaGPUDevice, 0, 10, 40000, 0, 0, 100))

	// Replica-style IDs instead of physical UUIDs must fail loudly instead
	// of reading as capacity exhaustion.
	devinput := device.PodDevices{}
	fit, reason := fitInRestrictedDevices(node, refitTestRequest(1, 4096, 10),
		nvidia.NvidiaGPUDevice, []string{"GPU-a-0", "GPU-a-1"},
		refitTestPod(nil), &device.NodeInfo{}, &devinput,
		util.DefaultDeviceScoringWeights())

	assert.Equal(t, fit, false)
	assert.Equal(t, reason, AllowedSetUnmatched)
}

func TestFitInRestrictedDevicesIgnoresOtherTypeRequests(t *testing.T) {
	node := refitTestNode("binpack", false,
		makeDevice("GPU-a", 0, nvidia.NvidiaGPUDevice, 0, 10, 40000, 0, 0, 100))

	requests := device.ContainerDeviceRequests{
		"OtherVendor": device.ContainerDeviceRequest{Nums: 1, Type: "OtherVendor", Memreq: 100},
	}
	devinput := device.PodDevices{}
	fit, reason := fitInRestrictedDevices(node, requests, nvidia.NvidiaGPUDevice,
		[]string{"GPU-a"}, refitTestPod(nil), &device.NodeInfo{}, &devinput,
		util.DefaultDeviceScoringWeights())

	// A single-type refit must not silently fit unrelated vendors' requests.
	assert.Equal(t, fit, false)
	assert.Assert(t, strings.Contains(reason, "request to refit"), "reason: %s", reason)
	assert.Equal(t, len(devinput), 0)
}

func TestFitInRestrictedDevicesUsesPodPolicyAnnotation(t *testing.T) {
	nodeInfo := &device.NodeInfo{
		ID: "node-1", Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
		Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: {
			{ID: "GPU-a", Count: 10, Devmem: 40000, Devcore: 100, Numa: 0, Type: nvidia.NvidiaGPUDevice, Health: true},
			{ID: "GPU-b", Count: 10, Devmem: 40000, Devcore: 100, Numa: 0, Type: nvidia.NvidiaGPUDevice, Health: true},
			{ID: "GPU-c", Count: 10, Devmem: 40000, Devcore: 100, Numa: 1, Type: nvidia.NvidiaGPUDevice, Health: true},
		}},
	}
	usedByID := map[string]int32{"GPU-a": 3, "GPU-b": 2, "GPU-c": 1}

	run := func(policy string) []string {
		pod := refitTestPod(map[string]string{util.GPUSchedulerPolicyAnnotationKey: policy})
		// The production caller must hand fitInRestrictedDevices a NodeUsage
		// freshly built for this pod, so its policy annotation is inherited.
		node := buildNodeUsage(nodeInfo, pod)
		for _, deviceList := range node.Devices.DeviceLists {
			used := usedByID[deviceList.Device.ID]
			deviceList.Device.Used = used
			deviceList.Device.Usedmem = used * 10000
			deviceList.Device.Usedcores = used * 10
		}
		devinput := device.PodDevices{}
		fit, reason := fitInRestrictedDevices(node, refitTestRequest(1, 4096, 10),
			nvidia.NvidiaGPUDevice, []string{"GPU-b", "GPU-c"},
			pod, &device.NodeInfo{}, &devinput, util.DefaultDeviceScoringWeights())
		assert.Assert(t, fit, "fit failed under policy %q: %s", policy, reason)
		return selectedUUIDs(devinput)
	}

	assert.DeepEqual(t, run("binpack"), []string{"GPU-b"})
	assert.DeepEqual(t, run("spread"), []string{"GPU-c"})
}

func TestFitInRestrictedDevicesMutexPolicy(t *testing.T) {
	mutexPod := refitTestPod(map[string]string{util.GPUSchedulerPolicyAnnotationKey: "mutex"})
	build := func() *NodeUsage {
		return refitTestNode("mutex", false,
			makeDevice("GPU-a", 0, nvidia.NvidiaGPUDevice, 1, 10, 40000, 10000, 10, 100),
			makeDevice("GPU-b", 0, nvidia.NvidiaGPUDevice, 0, 10, 40000, 0, 0, 100),
			makeDevice("GPU-c", 1, nvidia.NvidiaGPUDevice, 2, 10, 40000, 20000, 20, 100))
	}

	// Mutex only allocates idle GPUs: the busy allowed device is skipped.
	devinput := device.PodDevices{}
	fit, reason := fitInRestrictedDevices(build(), refitTestRequest(1, 4096, 10),
		nvidia.NvidiaGPUDevice, []string{"GPU-a", "GPU-b"},
		mutexPod, &device.NodeInfo{}, &devinput, util.DefaultDeviceScoringWeights())
	assert.Assert(t, fit, "mutex restricted fit failed: %s", reason)
	assert.DeepEqual(t, selectedUUIDs(devinput), []string{"GPU-b"})

	// When every allowed device is busy, mutex refuses the refit.
	busyInput := device.PodDevices{}
	fit, reason = fitInRestrictedDevices(build(), refitTestRequest(1, 4096, 10),
		nvidia.NvidiaGPUDevice, []string{"GPU-a", "GPU-c"},
		mutexPod, &device.NodeInfo{}, &busyInput, util.DefaultDeviceScoringWeights())
	assert.Equal(t, fit, false)
	assert.Assert(t, strings.Contains(reason, common.ExclusiveDeviceAllocateConflict), "reason: %s", reason)
}

func TestFitInRestrictedDevicesKeepsNumaBind(t *testing.T) {
	numaBindPod := refitTestPod(map[string]string{nvidia.NumaBind: "true"})
	build := func() *NodeUsage {
		return refitTestNode("binpack", true,
			makeDevice("GPU-a0", 0, nvidia.NvidiaGPUDevice, 0, 10, 40000, 0, 0, 100),
			makeDevice("GPU-b0", 0, nvidia.NvidiaGPUDevice, 0, 10, 40000, 0, 0, 100),
			makeDevice("GPU-c1", 1, nvidia.NvidiaGPUDevice, 0, 10, 40000, 0, 0, 100),
			makeDevice("GPU-d1", 1, nvidia.NvidiaGPUDevice, 0, 10, 40000, 0, 0, 100))
	}

	// Both allowed devices share NUMA node 0, so a 2-GPU numa-bind request fits.
	devinput := device.PodDevices{}
	fit, reason := fitInRestrictedDevices(build(), refitTestRequest(2, 4096, 10),
		nvidia.NvidiaGPUDevice, []string{"GPU-a0", "GPU-b0"},
		numaBindPod, &device.NodeInfo{}, &devinput,
		util.DefaultDeviceScoringWeights())
	assert.Assert(t, fit, "same-numa restricted fit failed: %s", reason)
	uuids := selectedUUIDs(devinput)
	assert.Equal(t, len(uuids), 2)
	for _, id := range uuids {
		assert.Assert(t, id == "GPU-a0" || id == "GPU-b0", "unexpected device %s", id)
	}

	// One allowed device per NUMA node cannot satisfy numa-bind for 2 GPUs.
	crossNumaInput := device.PodDevices{}
	fit, _ = fitInRestrictedDevices(build(), refitTestRequest(2, 4096, 10),
		nvidia.NvidiaGPUDevice, []string{"GPU-a0", "GPU-c1"},
		numaBindPod, &device.NodeInfo{}, &crossNumaInput,
		util.DefaultDeviceScoringWeights())
	assert.Equal(t, fit, false)
}
