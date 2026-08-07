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

package tools

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// nvidiaRegisterAnno is a real HAMi NVIDIA node-registration annotation
// value: a JSON array of device.DeviceInfo, keyed under
// hami.io/node-nvidia-register (nvidia.RegisterAnnos). Two devices, to
// exercise multi-GPU aggregation.
const nvidiaRegisterAnnoTwoGPUs = `[` +
	`{"id":"GPU-aaa","index":0,"count":10,"devmem":40960,"devcore":100,"type":"NVIDIA-A100-SXM4-40GB","numa":0,"mode":"hami-core","health":true},` +
	`{"id":"GPU-bbb","index":1,"count":10,"devmem":40960,"devcore":100,"type":"NVIDIA-A100-SXM4-40GB","numa":0,"mode":"hami-core","health":true}` +
	`]`

func gpuNodeFixture(name string, registerAnno string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: map[string]string{"hami.io/node-nvidia-register": registerAnno},
		},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
			},
		},
	}
}

func TestExtractGPUNodeInfo(t *testing.T) {
	node := gpuNodeFixture("gpu-node-1", nvidiaRegisterAnnoTwoGPUs)

	info := extractGPUNodeInfo(node)

	if info.Name != "gpu-node-1" {
		t.Errorf("Name = %q, want %q", info.Name, "gpu-node-1")
	}
	if info.GPUVendor != "NVIDIA" {
		t.Errorf("GPUVendor = %q, want %q", info.GPUVendor, "NVIDIA")
	}
	if info.GPUCount != 20 { // two devices, count=10 each
		t.Errorf("GPUCount = %d, want 20", info.GPUCount)
	}
	wantTotalMiB := float64(40960 * 10 * 2) // devmem * count, summed over 2 devices
	if info.TotalMemoryMiB != wantTotalMiB {
		t.Errorf("TotalMemoryMiB = %v, want %v", info.TotalMemoryMiB, wantTotalMiB)
	}
	// Allocated fields are populated separately by aggregateAllocationsByNode,
	// not by extractGPUNodeInfo, so they should be zero here.
	if info.AllocatedMemoryMiB != 0 || info.AllocatedCoresPct != 0 {
		t.Errorf("expected zero allocated fields from extractGPUNodeInfo alone, got mem=%v cores=%v",
			info.AllocatedMemoryMiB, info.AllocatedCoresPct)
	}
}

func TestExtractGPUNodeInfo_NoRegistration(t *testing.T) {
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "cpu-only"}}
	info := extractGPUNodeInfo(node)
	if info.GPUVendor != "" || info.GPUCount != 0 {
		t.Errorf("expected empty GPU info for a node with no registration, got %+v", info)
	}
}

func TestAggregateAllocationsByNode(t *testing.T) {
	runningPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "training-job",
			Namespace: "ai-team",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-allocated": "GPU-aaa,NVIDIA,4096,80:GPU-bbb,NVIDIA,4096,80:",
			},
		},
		Spec:   corev1.PodSpec{NodeName: "gpu-node-1"},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	succeededPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "finished-job",
			Namespace: "ai-team",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-allocated": "GPU-ccc,NVIDIA,9999,99:",
			},
		},
		Spec:   corev1.PodSpec{NodeName: "gpu-node-1"},
		Status: corev1.PodStatus{Phase: corev1.PodSucceeded}, // must be excluded
	}
	unboundPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pending-job",
			Namespace: "ai-team",
			Annotations: map[string]string{
				"hami.io/vgpu-node":              "gpu-node-1", // must NOT be trusted
				"hami.io/vgpu-devices-allocated": "GPU-ddd,NVIDIA,1111,11:",
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodPending}, // Spec.NodeName is empty
	}

	got := aggregateAllocationsByNode([]*corev1.Pod{runningPod, succeededPod, unboundPod})

	alloc, ok := got["gpu-node-1"]
	if !ok {
		t.Fatalf("expected an entry for gpu-node-1, got %+v", got)
	}
	// Only runningPod counts: two devices at 4096 MiB / 80% each.
	if alloc.memMiB != 8192 {
		t.Errorf("memMiB = %v, want 8192 (succeeded/unbound pods must be excluded)", alloc.memMiB)
	}
	if alloc.coresPct != 160 {
		t.Errorf("coresPct = %v, want 160 (sum across 2 devices, pre-normalization)", alloc.coresPct)
	}

	if len(got) != 1 {
		t.Errorf("expected exactly 1 node entry (unbound pod must not create one), got %d: %+v", len(got), got)
	}
}
