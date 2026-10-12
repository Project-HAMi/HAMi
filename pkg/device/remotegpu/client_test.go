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
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func clientTestDevice(t *testing.T) *RemoteGPUDevices {
	cfg := testConfig()
	cfg.ClientImage = "lupine-client:test"
	d := InitRemoteGPUDevice(cfg)
	t.Cleanup(func() { RemoteGPUClientImage = "" })
	return d
}

func remoteClientContainer(name string) corev1.Container {
	return corev1.Container{Name: name, Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
		"nvidia.com/remote-gpu": resource.MustParse("2"),
	}}}
}

func TestClientDeliverySharedAndIdempotent(t *testing.T) {
	d := clientTestDevice(t)
	pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{remoteClientContainer("a"), remoteClientContainer("b"), {Name: "sidecar"}}}}
	pod.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "LD_LIBRARY_PATH", Value: "/custom/lib"}, {Name: ldPreloadEnv, Value: "/custom/preload.so"}}
	pod.Spec.Containers[0].Resources.Limits["nvidia.com/remote-gpu-memory"] = resource.MustParse("3000")
	for round := 0; round < 2; round++ {
		for i := range pod.Spec.Containers {
			if _, err := d.MutateAdmission(&pod.Spec.Containers[i], pod); err != nil {
				t.Fatal(err)
			}
		}
		FinalizeAdmission(pod)
	}
	if len(pod.Spec.InitContainers) != 2 || len(pod.Spec.Volumes) != 2 {
		t.Fatalf("delivery duplicated: %+v", pod.Spec)
	}
	if envValue(&pod.Spec.Containers[0], ldPreloadEnv) != libMountPath+"/libvgpu.so /custom/preload.so" {
		t.Fatal("preload duplicated or lost")
	}
	if envValue(&pod.Spec.Containers[0], "LD_LIBRARY_PATH") != clientMountPath+":/custom/lib" {
		t.Fatal("lost workload library path")
	}
	if len(pod.Spec.Containers[2].VolumeMounts) != 0 || len(pod.Spec.Containers[2].Env) != 0 {
		t.Fatal("modified non-GPU sidecar")
	}
	for _, c := range pod.Spec.Containers[:2] {
		if envValue(&c, "LUPINE_LIBCUDA") != clientMountPath+"/libcuda.so.1" || envValue(&c, "NVIDIA_VISIBLE_DEVICES") != "void" {
			t.Fatal("client environment missing")
		}
	}
}

func TestClientDeliveryBeforeGPUInitContainers(t *testing.T) {
	d := clientTestDevice(t)
	pod := &corev1.Pod{Spec: corev1.PodSpec{InitContainers: []corev1.Container{remoteClientContainer("warmup-a"), remoteClientContainer("warmup-b")}}}
	pod.Spec.InitContainers[0].Resources.Limits["nvidia.com/remote-gpu-memory"] = resource.MustParse("3000")
	// Match webhook iteration: appending delivery cannot skip the second init.
	for i := range pod.Spec.InitContainers {
		if _, err := d.MutateAdmission(&pod.Spec.InitContainers[i], pod); err != nil {
			t.Fatal(err)
		}
	}
	FinalizeAdmission(pod)
	if len(pod.Spec.InitContainers) != 4 {
		t.Fatalf("missing delivery: %+v", pod.Spec.InitContainers)
	}
	for i, name := range []string{"warmup-a", "warmup-b"} {
		c := pod.Spec.InitContainers[i+2]
		if c.Name != name || envValue(&c, "LUPINE_LIBCUDA") == "" {
			t.Fatalf("init lost or unwired: %+v", c)
		}
	}
	if pod.Spec.InitContainers[0].Name != libVolumeName || pod.Spec.InitContainers[1].Name != clientVolumeName {
		t.Fatal("delivery must precede workload init")
	}
}

// Workloads can use ordinary mounts to replace any library loaded by absolute
// path. Admission supplies the referenced volume without altering that mount.
func TestClientDeliveryPreservesExplicitLibraryMount(t *testing.T) {
	d := clientTestDevice(t)
	mount := corev1.VolumeMount{Name: clientVolumeName, MountPath: "/usr/local/lib/python3.12/dist-packages/nvidia/nccl/lib/libnccl.so.2", SubPath: "libnccl.so.2", ReadOnly: true}
	pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{remoteClientContainer("vllm")}}}
	pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{mount}
	for round := 0; round < 2; round++ {
		if _, err := d.MutateAdmission(&pod.Spec.Containers[0], pod); err != nil {
			t.Fatal(err)
		}
	}
	if len(pod.Spec.Containers[0].VolumeMounts) != 2 || !reflect.DeepEqual(pod.Spec.Containers[0].VolumeMounts[0], mount) {
		t.Fatal("workload library mount changed or duplicated")
	}
	if len(pod.Spec.Volumes) != 1 || pod.Spec.Volumes[0].Name != mount.Name || pod.Spec.Volumes[0].EmptyDir == nil {
		t.Fatal("referenced client volume not supplied")
	}
}

func TestClientDeliveryOptOut(t *testing.T) {
	d := clientTestDevice(t)
	for _, global := range []bool{false, true} {
		pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{remoteClientContainer("app")}}}
		if global {
			RemoteGPUClientImage = ""
		} else {
			pod.Annotations = map[string]string{ClientInjectionAnnotation: "disabled"}
		}
		if _, err := d.MutateAdmission(&pod.Spec.Containers[0], pod); err != nil {
			t.Fatal(err)
		}
		if len(pod.Spec.InitContainers) != 0 || envOf(&pod.Spec.Containers[0], "LUPINE_LIBCUDA") != nil {
			t.Fatal("opt-out ignored")
		}
		if envOf(&pod.Spec.Containers[0], lupineWorkloadIDEnv) == nil || envOf(&pod.Spec.Containers[0], lupineServerEnv) == nil {
			t.Fatal("opt-out disabled identity/endpoint")
		}
	}
}

func TestClientDeliveryRejectsConflicts(t *testing.T) {
	d := clientTestDevice(t)
	cases := map[string]func(*corev1.Pod){
		"sourced library path": func(p *corev1.Pod) {
			p.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "LD_LIBRARY_PATH", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{Key: "path"}}}}
		},
		"mount collision": func(p *corev1.Pod) {
			p.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "mine", MountPath: clientMountPath}}
		},
		"volume collision": func(p *corev1.Pod) {
			p.Spec.Volumes = []corev1.Volume{{Name: clientVolumeName, VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/mine"}}}}
		},
		"initializer collision": func(p *corev1.Pod) {
			p.Spec.InitContainers = []corev1.Container{{Name: clientVolumeName, Image: "mine"}}
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			p := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{remoteClientContainer("app")}}}
			setup(p)
			before := p.DeepCopy()
			if _, err := d.MutateAdmission(&p.Spec.Containers[0], p); err == nil {
				t.Fatal("conflict accepted")
			}
			if !reflect.DeepEqual(before, p) {
				t.Fatal("conflict partly mutated Pod")
			}
		})
	}
}

func TestFinalizeAdmissionLeavesNonGPUInitOrderUnchanged(t *testing.T) {
	clientTestDevice(t)
	p := &corev1.Pod{Spec: corev1.PodSpec{InitContainers: []corev1.Container{{Name: "setup"}, {Name: libVolumeName}, {Name: clientVolumeName}}}}
	before := p.DeepCopy()
	FinalizeAdmission(p)
	if !reflect.DeepEqual(before, p) {
		t.Fatal("reordered non-GPU workload")
	}
}
