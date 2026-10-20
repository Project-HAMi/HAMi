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
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
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
	for range 2 {
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
	if envValue(&pod.Spec.Containers[0], "LD_LIBRARY_PATH") != "/custom/lib" {
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
	for range 2 {
		if _, err := d.MutateAdmission(&pod.Spec.Containers[0], pod); err != nil {
			t.Fatal(err)
		}
	}
	if len(pod.Spec.Containers[0].VolumeMounts) != 6 || !reflect.DeepEqual(pod.Spec.Containers[0].VolumeMounts[0], mount) {
		t.Fatal("workload library mount changed or duplicated")
	}
	if len(pod.Spec.Volumes) != 1 || pod.Spec.Volumes[0].Name != mount.Name || pod.Spec.Volumes[0].EmptyDir == nil {
		t.Fatal("referenced client volume not supplied")
	}
}

// Library delivery must also work when enforcement is disabled globally.
func TestClientDeliveryWithoutCore(t *testing.T) {
	d := clientTestDevice(t)
	previous := RemoteGPULibImage
	RemoteGPULibImage = ""
	t.Cleanup(func() { RemoteGPULibImage = previous })
	pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{remoteClientContainer("app")}}}
	ctr := &pod.Spec.Containers[0]
	if _, err := d.MutateAdmission(ctr, pod); err != nil {
		t.Fatal(err)
	}
	FinalizeAdmission(pod)
	if len(pod.Spec.InitContainers) != 1 || len(ctr.VolumeMounts) != 5 {
		t.Fatal("unexpected preparation or cache mounts")
	}
	if !reflect.DeepEqual(ctr.VolumeMounts[len(ctr.VolumeMounts)-1], corev1.VolumeMount{Name: clientVolumeName, MountPath: clientSMIMountPath, SubPath: clientSMIWrapperName, ReadOnly: true}) {
		t.Fatal("nvidia-smi not mounted from the client image")
	}
	if envOf(ctr, ldPreloadEnv) != nil {
		t.Fatal("client delivery must not preload Lupine")
	}
	if envOf(ctr, "LD_LIBRARY_PATH") != nil {
		t.Fatal("must preserve image library search paths")
	}
}

func TestClientDeliveryPreservesSourcedLibraryPath(t *testing.T) {
	d := clientTestDevice(t)
	pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{remoteClientContainer("app")}}}
	original := corev1.EnvVar{Name: "LD_LIBRARY_PATH", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{Key: "path"}}}
	pod.Spec.Containers[0].Env = []corev1.EnvVar{original}
	if _, err := d.MutateAdmission(&pod.Spec.Containers[0], pod); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(envOf(&pod.Spec.Containers[0], "LD_LIBRARY_PATH"), &original) {
		t.Fatal("changed sourced library path")
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
		"mount collision": func(p *corev1.Pod) {
			p.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "mine", MountPath: clientMountPath}}
		},
		"driver mount collision": func(p *corev1.Pod) {
			p.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "mine", MountPath: "/usr/local/nvidia/lib64/libcuda.so.1"}}
		},
		"nvidia-smi mount collision": func(p *corev1.Pod) {
			p.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "mine", MountPath: clientSMIMountPath}}
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

// Exercise the copy command against common client-image layouts.
func TestClientDeliveryKnownDependency(t *testing.T) {
	for _, layout := range []string{"usr/lib/x86_64-linux-gnu", "usr/lib/aarch64-linux-gnu", "usr/lib64", "lib64", "usr/lib", "lib"} {
		t.Run(layout, func(t *testing.T) {
			root := t.TempDir()
			source, target, libraries := filepath.Join(root, "driver"), filepath.Join(root, "delivered"), filepath.Join(root, layout)
			for _, path := range []string{source, target, libraries} {
				if err := os.MkdirAll(path, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			smi := filepath.Join(root, "nvidia-smi")
			for path, data := range map[string]string{
				filepath.Join(source, "libcuda.so.1"):            "driver",
				filepath.Join(libraries, "libnghttp2.so.14.0.0"): "nghttp2",
				filepath.Join(libraries, "libssl.so.3"):          "unrelated",
				smi:                                              "smi",
			} {
				if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink("libnghttp2.so.14.0.0", filepath.Join(libraries, "libnghttp2.so.14")); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("sh", "-ec", clientDeliveryScript, "delivery-test", source, target, filepath.Join(root, "missing"), libraries)
			cmd.Env = append(os.Environ(), "NVIDIA_SMI_SOURCE="+smi)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("delivery: %v: %s", err, output)
			}
			got, err := os.ReadFile(filepath.Join(target, "libnghttp2.so.14"))
			if err != nil || string(got) != "nghttp2" {
				t.Fatalf("dependency: %q, %v", got, err)
			}
			got, err = os.ReadFile(filepath.Join(target, clientSMIBinaryName))
			if err != nil || string(got) != "smi" {
				t.Fatalf("nvidia-smi: %q, %v", got, err)
			}
			got, err = os.ReadFile(filepath.Join(target, clientSMIWrapperName))
			if err != nil || string(got) != clientSMIWrapper {
				t.Fatalf("nvidia-smi wrapper: %q, %v", got, err)
			}
			if _, err := os.Stat(filepath.Join(target, "libssl.so.3")); !os.IsNotExist(err) {
				t.Fatalf("must not copy unrelated libraries: %v", err)
			}
			cmd = exec.Command("sh", "-ec", clientDeliveryScript, "delivery-test", source, target, filepath.Join(root, "missing"))
			output, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(output), "libnghttp2.so.14 not found") {
				t.Fatalf("missing dependency: %v: %s", err, output)
			}
			cmd = exec.Command("sh", "-ec", clientDeliveryScript, "delivery-test", source, target, libraries)
			cmd.Env = append(os.Environ(), "NVIDIA_SMI_SOURCE="+filepath.Join(root, "missing-smi"))
			output, err = cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(output), "nvidia-smi not found") {
				t.Fatalf("missing nvidia-smi: %v: %s", err, output)
			}
		})
	}
}

// A failing nvidia-smi copy must fail delivery even when the wrapper write would succeed.
// Calling the installer in an || list disables errexit for the whole function.
func TestClientDeliveryNVIDIASmiCopyFailure(t *testing.T) {
	root := t.TempDir()
	source, target, libraries, bin := filepath.Join(root, "driver"), filepath.Join(root, "delivered"), filepath.Join(root, "lib"), filepath.Join(root, "bin")
	for _, path := range []string{source, target, libraries, bin} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	smi := filepath.Join(root, "nvidia-smi")
	for path, data := range map[string]string{
		filepath.Join(source, "libcuda.so.1"):        "driver",
		filepath.Join(libraries, "libnghttp2.so.14"): "nghttp2",
		smi: "smi",
	} {
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stub := "#!/bin/sh\n" +
		"if [ \"$1\" = -L ] && [ \"$(basename \"$2\")\" = " + clientSMIBinaryName + " ]; then\n" +
		"echo \"cp nvidia-smi failed\" >&2\n" +
		"exit 1\n" +
		"fi\n" +
		"exec /bin/cp \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "cp"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-ec", clientDeliveryScript, "delivery-test", source, target, libraries)
	cmd.Env = append(os.Environ(), "NVIDIA_SMI_SOURCE="+smi, "PATH="+bin+":"+os.Getenv("PATH"))
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "cp nvidia-smi failed") {
		t.Fatalf("nvidia-smi copy failure: %v: %s", err, output)
	}
}
