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
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
)

const (
	clientVolumeName = "hami-lupine-client"
	clientMountPath  = "/opt/lupine/lib"
	// ClientInjectionAnnotation opts out when a workload delivers its own libraries.
	ClientInjectionAnnotation = "hami.io/remote-gpu-client-injection"
	clientSMIMountPath        = "/usr/bin/nvidia-smi"
	clientSMIBinaryName       = "nvidia-smi"
	clientSMIWrapperName      = "nvidia-smi.wrapper"
)

// clientSMIWrapper is mounted at /usr/bin/nvidia-smi. LD_LIBRARY_PATH applies
// only to that process, so nvidia-smi loads the Lupine NVML shim and the
// workload's own library search path stays unchanged.
const clientSMIWrapper = "#!/bin/sh\nLD_LIBRARY_PATH=" + clientMountPath + "${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH} exec " + clientMountPath + "/" + clientSMIBinaryName + " \"$@\"\n"

func clientInjectionEnabled(pod *corev1.Pod) bool {
	return pod != nil && RemoteGPUClientImage != "" && pod.Annotations[ClientInjectionAnnotation] != "disabled"
}

// Copy the known missing dependency from common Debian/Ubuntu and RPM layouts.
// This intentionally does not attempt to bundle every runtime dependency.
// When client injection is enabled, the client image must provide /opt/lupine/lib,
// sh, cp, libnghttp2.so.14, and nvidia-smi. A missing nvidia-smi fails delivery.
// NVIDIA_SMI_SOURCE overrides the nvidia-smi search and is set only by tests.
const clientDeliveryScript = `
source=$1
target=$2
shift 2
cp -a "$source/." "$target/"
install_nvidia_smi() {
    smi=${NVIDIA_SMI_SOURCE:-}
    if [ -z "$smi" ]; then
        for candidate in /usr/bin/nvidia-smi /usr/local/bin/nvidia-smi; do
            if [ -f "$candidate" ]; then
                smi=$candidate
                break
            fi
        done
    fi
    if [ ! -f "$smi" ]; then
        echo "nvidia-smi not found in the client image" >&2
        return 1
    fi
    cp -L "$smi" "$target/` + clientSMIBinaryName + `"
    chmod 755 "$target/` + clientSMIBinaryName + `"
    cat > "$target/` + clientSMIWrapperName + `" <<'EOF'
` + clientSMIWrapper + `EOF
    chmod 755 "$target/` + clientSMIWrapperName + `"
}
[ "$#" -gt 0 ] || set -- /usr/lib/*-linux-gnu /lib/*-linux-gnu /usr/lib64 /lib64 /usr/lib /lib
for directory in "$@"; do
    if [ -f "$directory/libnghttp2.so.14" ]; then
        cp -L "$directory/libnghttp2.so.14" "$target/libnghttp2.so.14"
        install_nvidia_smi || exit 1
        exit 0
    fi
done
echo "libnghttp2.so.14 not found in common client image library directories" >&2
exit 1
`

func clientDeliveryContainer() corev1.Container {
	return corev1.Container{
		Name: clientVolumeName, Image: RemoteGPUClientImage,
		Command:      []string{"sh", "-ec", clientDeliveryScript, clientVolumeName, clientMountPath, "/lupine-lib"},
		Env:          []corev1.EnvVar{{Name: "NVIDIA_VISIBLE_DEVICES", Value: "void"}},
		VolumeMounts: []corev1.VolumeMount{{Name: clientVolumeName, MountPath: "/lupine-lib"}},
	}
}

// Validate collisions before modifying the container. Never silently use a
// user-owned volume or initializer as the source of the injected driver.
func armClientLibraries(ctr *corev1.Container, pod *corev1.Pod) error {
	if !clientInjectionEnabled(pod) {
		return nil
	}
	for _, v := range pod.Spec.Volumes {
		if v.Name == clientVolumeName && !reflect.DeepEqual(v.VolumeSource, corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}) {
			return fmt.Errorf("reserved remote GPU volume %q conflicts with workload", clientVolumeName)
		}
	}
	expected := clientDeliveryContainer()
	for _, c := range pod.Spec.InitContainers {
		if c.Name == clientVolumeName && (c.Image != expected.Image || !reflect.DeepEqual(c.Command, expected.Command) || !reflect.DeepEqual(c.VolumeMounts, expected.VolumeMounts)) {
			return fmt.Errorf("reserved remote GPU init container %q conflicts with workload", clientVolumeName)
		}
	}
	for _, c := range pod.Spec.Containers {
		if c.Name == clientVolumeName {
			return fmt.Errorf("reserved remote GPU container name %q conflicts with workload", clientVolumeName)
		}
	}
	// CUDA and vLLM example images already search this directory. Mount files
	// individually so unrelated image libraries remain visible.
	mounts := []corev1.VolumeMount{{Name: clientVolumeName, MountPath: clientMountPath, ReadOnly: true}}
	for _, library := range []string{"libcuda.so.1", "libnvidia-ml.so.1", "libnghttp2.so.14"} {
		mounts = append(mounts, corev1.VolumeMount{Name: clientVolumeName, MountPath: "/usr/local/nvidia/lib64/" + library, SubPath: library, ReadOnly: true})
	}
	mounts = append(mounts, corev1.VolumeMount{Name: clientVolumeName, MountPath: clientSMIMountPath, SubPath: clientSMIWrapperName, ReadOnly: true})
	for _, desired := range mounts {
		for _, existing := range ctr.VolumeMounts {
			if existing.MountPath == desired.MountPath && !reflect.DeepEqual(existing, desired) {
				return fmt.Errorf("container %q already mounts %s; disable remote GPU client injection to deliver libraries manually", ctr.Name, desired.MountPath)
			}
		}
	}
	for _, desired := range mounts {
		found := false
		for _, existing := range ctr.VolumeMounts {
			found = found || reflect.DeepEqual(existing, desired)
		}
		if !found {
			ctr.VolumeMounts = append(ctr.VolumeMounts, desired)
		}
	}
	setEnv(ctr, corev1.EnvVar{Name: "NVIDIA_VISIBLE_DEVICES", Value: "void"})
	for _, name := range []string{"LUPINE_LIB", "LUPINE_LIBCUDA"} {
		setEnv(ctr, corev1.EnvVar{Name: name, Value: clientMountPath + "/libcuda.so.1"})
	}
	return nil
}

func addClientDelivery(pod *corev1.Pod) {
	for _, c := range pod.Spec.InitContainers {
		if c.Name == clientVolumeName {
			return
		}
	}
	found := false
	for _, v := range pod.Spec.Volumes {
		found = found || v.Name == clientVolumeName
	}
	if !found {
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{Name: clientVolumeName, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}})
	}
	pod.Spec.InitContainers = append(pod.Spec.InitContainers, clientDeliveryContainer())
}

// FinalizeAdmission runs after all device mutations so inserting delivery
// containers cannot invalidate pointers or skip GPU-using init containers.
func FinalizeAdmission(pod *corev1.Pod) {
	if pod == nil {
		return
	}
	hasRemoteGPU := false
	for _, containers := range [][]corev1.Container{pod.Spec.InitContainers, pod.Spec.Containers} {
		for _, c := range containers {
			if count, ok := resourceValue(&c, RemoteGPUResourceCount); ok && count > 0 {
				hasRemoteGPU = true
			}
		}
	}
	if !hasRemoteGPU {
		return
	}
	var delivery, workload []corev1.Container
	for _, c := range pod.Spec.InitContainers {
		if (c.Name == clientVolumeName && clientInjectionEnabled(pod)) || (c.Name == libVolumeName && RemoteGPULibImage != "") {
			delivery = append(delivery, c)
		} else {
			workload = append(workload, c)
		}
	}
	if len(delivery) > 0 {
		pod.Spec.InitContainers = append(delivery, workload...)
	}
}
