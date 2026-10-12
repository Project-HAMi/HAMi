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
	"strings"

	corev1 "k8s.io/api/core/v1"
)

const (
	clientVolumeName = "hami-lupine-client"
	clientMountPath  = "/opt/lupine/lib"
	// ClientInjectionAnnotation opts out when a workload delivers its own libraries.
	ClientInjectionAnnotation = "hami.io/remote-gpu-client-injection"
)

func clientInjectionEnabled(pod *corev1.Pod) bool {
	return pod != nil && RemoteGPUClientImage != "" && pod.Annotations[ClientInjectionAnnotation] != "disabled"
}

func clientDeliveryContainer() corev1.Container {
	return corev1.Container{
		Name: clientVolumeName, Image: RemoteGPUClientImage,
		Command:      []string{"sh", "-ec", "cp -a /opt/lupine/lib/. /lupine-lib/; cp -L /usr/lib/x86_64-linux-gnu/libnghttp2.so.14 /lupine-lib/"},
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
	ld := clientMountPath + ":/usr/local/nvidia/lib:/usr/local/nvidia/lib64:/usr/local/cuda/lib64"
	if e := envOf(ctr, "LD_LIBRARY_PATH"); e != nil {
		if e.ValueFrom != nil {
			return fmt.Errorf("container %q takes LD_LIBRARY_PATH from a source; set it literally or disable remote GPU client injection", ctr.Name)
		}
		ld = e.Value
		if !strings.HasPrefix(ld, clientMountPath+":") && ld != clientMountPath {
			ld = clientMountPath + ":" + ld
		}
	}
	mounts := []corev1.VolumeMount{{Name: clientVolumeName, MountPath: clientMountPath, ReadOnly: true}}
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
	setEnv(ctr, corev1.EnvVar{Name: "LD_LIBRARY_PATH", Value: ld})
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
