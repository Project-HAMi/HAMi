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

func TestNodeGPUDevices(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gpu-node-1",
			Annotations: map[string]string{
				"hami.io/node-nvidia-register": nvidiaRegisterAnnoTwoGPUs,
			},
		},
	}

	devices := nodeGPUDevices(node)
	if len(devices) != 2 {
		t.Fatalf("expected 2 devices, got %d: %+v", len(devices), devices)
	}
	if devices[0].ID != "GPU-aaa" || devices[0].Vendor != "NVIDIA" {
		t.Errorf("unexpected device[0]: %+v", devices[0])
	}
	if devices[0].Type != "NVIDIA-A100-SXM4-40GB" {
		t.Errorf("Type = %q, want NVIDIA-A100-SXM4-40GB", devices[0].Type)
	}
	if devices[0].Mode != "hami-core" {
		t.Errorf("Mode = %q, want hami-core", devices[0].Mode)
	}
	if !devices[0].Healthy {
		t.Errorf("expected device[0] to be healthy")
	}
}

func TestNodeGPUDevices_NoRegistration(t *testing.T) {
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "cpu-only"}}
	devices := nodeGPUDevices(node)
	if len(devices) != 0 {
		t.Errorf("expected no devices for an unregistered node, got %+v", devices)
	}
}

func TestResourceListToStrings(t *testing.T) {
	rl := corev1.ResourceList{
		corev1.ResourceCPU:                    resource.MustParse("8"),
		corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("4"),
	}
	out := resourceListToStrings(rl)
	if out["cpu"] != "8" {
		t.Errorf("cpu = %q, want 8", out["cpu"])
	}
	if out["nvidia.com/gpu"] != "4" {
		t.Errorf("nvidia.com/gpu = %q, want 4", out["nvidia.com/gpu"])
	}

	if resourceListToStrings(nil) != nil {
		t.Errorf("expected nil for an empty ResourceList")
	}
}
