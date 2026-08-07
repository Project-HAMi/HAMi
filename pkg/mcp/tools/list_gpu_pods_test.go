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

func TestPodAllocations_SingleContainer(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "training-job",
			Namespace: "ai-team",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-allocated": "GPU-aaa,NVIDIA,4096,80:",
			},
		},
	}

	got := podAllocations(pod)
	if len(got) != 1 {
		t.Fatalf("expected 1 allocated device, got %d: %+v", len(got), got)
	}
	want := AllocatedDevice{
		DeviceName:     "NVIDIA",
		ContainerIndex: 0,
		UUID:           "GPU-aaa",
		Type:           "NVIDIA",
		UsedMemMiB:     4096,
		UsedCoresPct:   80,
	}
	if got[0] != want {
		t.Errorf("got %+v, want %+v", got[0], want)
	}
}

func TestPodAllocations_MultiContainerMultiDevice(t *testing.T) {
	// container 0 uses two devices, container 1 uses none, container 2 uses one.
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "multi-ctr",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-allocated": "GPU-aaa,NVIDIA,4096,80:GPU-bbb,NVIDIA,2048,40:;;GPU-ccc,NVIDIA,8192,100:",
			},
		},
	}

	got := podAllocations(pod)
	if len(got) != 3 {
		t.Fatalf("expected 3 allocated devices across containers, got %d: %+v", len(got), got)
	}

	idxCounts := map[int]int{}
	for _, ad := range got {
		idxCounts[ad.ContainerIndex]++
	}
	if idxCounts[0] != 2 {
		t.Errorf("expected 2 devices on container 0, got %d", idxCounts[0])
	}
	if idxCounts[1] != 0 {
		t.Errorf("expected 0 devices on container 1, got %d", idxCounts[1])
	}
	if idxCounts[2] != 1 {
		t.Errorf("expected 1 device on container 2, got %d", idxCounts[2])
	}
}

func TestPodAllocations_NoAnnotations(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "no-gpu-pod"}}
	got := podAllocations(pod)
	if got != nil {
		t.Errorf("expected nil for a pod with no device annotations, got %+v", got)
	}
}

func TestExtractGPUPodInfo_BoundPod(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "training-job",
			Namespace: "ai-team",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-allocated": "GPU-aaa,NVIDIA,4096,80:",
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "gpu-node-1",
			Containers: []corev1.Container{{
				Name: "trainer",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
					},
				},
			}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	info := extractGPUPodInfo(pod)

	if info.Namespace != "ai-team" || info.Name != "training-job" {
		t.Errorf("unexpected identity: %+v", info)
	}
	if info.Node != "gpu-node-1" {
		t.Errorf("Node = %q, want %q", info.Node, "gpu-node-1")
	}
	if info.Status != "Running" {
		t.Errorf("Status = %q, want Running", info.Status)
	}
	if info.RequestedGPU != 1 {
		t.Errorf("RequestedGPU = %d, want 1", info.RequestedGPU)
	}
	if len(info.AllocatedDevices) != 1 || info.AllocatedDevices[0].UUID != "GPU-aaa" {
		t.Errorf("unexpected AllocatedDevices: %+v", info.AllocatedDevices)
	}
}

func TestExtractGPUPodInfo_PendingPodStillReportsRequest(t *testing.T) {
	// A Pending pod has no NodeName and no allocation annotations yet, but
	// requestedGPUCount reads the pod spec directly, so it must still
	// report what was asked for.
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pending-job", Namespace: "ai-team"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "trainer",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
					},
				},
			}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodPending},
	}

	info := extractGPUPodInfo(pod)
	if info.RequestedGPU != 2 {
		t.Errorf("RequestedGPU = %d, want 2 for a Pending pod with limits set", info.RequestedGPU)
	}
	if info.Node != "" {
		t.Errorf("Node = %q, want empty for an unbound pod", info.Node)
	}
	if len(info.AllocatedDevices) != 0 {
		t.Errorf("expected no allocated devices for a Pending pod, got %+v", info.AllocatedDevices)
	}
}

func TestRequestedGPUCount_GPUNotInFirstContainer(t *testing.T) {
	// Regression test: RequestedGPU must not depend on container index —
	// it previously only counted when the GPU was on container 0.
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "sidecar"}, // no GPU request
				{
					Name: "worker",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("3"),
						},
					},
				},
			},
		},
	}

	got := requestedGPUCount(pod)
	if got != 3 {
		t.Errorf("requestedGPUCount = %d, want 3 (GPU request on non-first container)", got)
	}
}

func TestRequestedGPUCount_LimitsAndRequestsNotDoubleCounted(t *testing.T) {
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
					},
				},
			}},
		},
	}

	got := requestedGPUCount(pod)
	if got != 1 {
		t.Errorf("requestedGPUCount = %d, want 1 (limits and requests for the same resource must not double-count)", got)
	}
}
