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

package remotegpu

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// Admission visits init containers before app containers. The library-copy
// container must run before an init container whose LD_PRELOAD needs it.
func TestMutateAdmissionPlacesLibraryBeforeRemoteGPUInit(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	pod := &corev1.Pod{Spec: corev1.PodSpec{
		InitContainers: []corev1.Container{
			{
				Name: "warmup",
				Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
					"nvidia.com/remote-gpu":        resource.MustParse("1"),
					"nvidia.com/remote-gpu-memory": resource.MustParse("2000"),
				}},
			},
			{Name: "other-init"},
		},
		Containers: []corev1.Container{{Name: "app"}},
	}}
	// Follow webhook.Handle's init-then-app mutation order.
	for i := range pod.Spec.InitContainers {
		if _, err := dev.MutateAdmission(&pod.Spec.InitContainers[i], pod); err != nil {
			t.Fatal(err)
		}
	}
	for i := range pod.Spec.Containers {
		if _, err := dev.MutateAdmission(&pod.Spec.Containers[i], pod); err != nil {
			t.Fatal(err)
		}
	}
	if len(pod.Spec.InitContainers) != 3 {
		t.Fatalf("want two original init containers and one library copy, got %d", len(pod.Spec.InitContainers))
	}
	if got := pod.Spec.InitContainers[0].Name; got != libVolumeName {
		t.Fatalf("library must be copied before the requesting init container starts, first init is %q", got)
	}
	if got := pod.Spec.InitContainers[1].Name; got != "warmup" {
		t.Fatalf("requesting init container must retain its order, got %q", got)
	}
	if got := envValue(&pod.Spec.InitContainers[1], ldPreloadEnv); got != libMountPath+"/libvgpu.so" {
		t.Fatalf("requesting init container must keep its preload, got %q", got)
	}
}

func TestMutateAdmissionPlacesLibraryFirstForSingleApp(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	pod := &corev1.Pod{Spec: corev1.PodSpec{
		InitContainers: []corev1.Container{{Name: "setup"}, {Name: "other-init"}},
		Containers: []corev1.Container{{
			Name: "app",
			Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
				"nvidia.com/remote-gpu":        resource.MustParse("1"),
				"nvidia.com/remote-gpu-memory": resource.MustParse("2000"),
			}},
		}},
	}}
	if _, err := dev.MutateAdmission(&pod.Spec.Containers[0], pod); err != nil {
		t.Fatal(err)
	}
	if len(pod.Spec.InitContainers) != 3 {
		t.Fatalf("want two original init containers and one library copy, got %d", len(pod.Spec.InitContainers))
	}
	for i, want := range []string{libVolumeName, "setup", "other-init"} {
		if got := pod.Spec.InitContainers[i].Name; got != want {
			t.Fatalf("init container %d: got %q, want %q", i, got, want)
		}
	}
}

func TestMutateAdmissionDoesNotMoveUnrelatedNamedInit(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	pod := &corev1.Pod{Spec: corev1.PodSpec{
		InitContainers: []corev1.Container{{Name: "setup"}, {Name: libVolumeName, Image: "user-image"}},
		Containers:     []corev1.Container{{Name: "app"}},
	}}
	if _, err := dev.MutateAdmission(&pod.Spec.Containers[0], pod); err != nil {
		t.Fatal(err)
	}
	if pod.Spec.InitContainers[0].Name != "setup" || pod.Spec.InitContainers[1].Name != libVolumeName {
		t.Fatalf("unrelated init container order changed: %q, %q", pod.Spec.InitContainers[0].Name, pod.Spec.InitContainers[1].Name)
	}
}

func TestMutateAdmissionKeepsUserInitMatchingLibraryCopy(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	pod := &corev1.Pod{Spec: corev1.PodSpec{
		InitContainers: []corev1.Container{
			{Name: "setup"},
			{Name: libVolumeName, Command: []string{"sh", "-c", "cp " + libSourceGlob + " " + libMountPath + "/libvgpu.so"}},
		},
		Containers: []corev1.Container{{Name: "app"}},
	}}
	if _, err := dev.MutateAdmission(&pod.Spec.Containers[0], pod); err != nil {
		t.Fatal(err)
	}
	if pod.Spec.InitContainers[0].Name != "setup" || pod.Spec.InitContainers[1].Name != libVolumeName {
		t.Fatalf("user init order changed: %q, %q", pod.Spec.InitContainers[0].Name, pod.Spec.InitContainers[1].Name)
	}
}
