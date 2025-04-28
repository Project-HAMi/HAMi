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

package k8sutil

import (
	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/util"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

func Resourcereqs(pod *corev1.Pod) (counts util.PodDeviceRequests) {
	counts = make(util.PodDeviceRequests, len(pod.Spec.Containers))
	klog.V(4).InfoS("Processing resource requirements",
		"pod", klog.KObj(pod),
		"containerCount", len(pod.Spec.Containers))
	//Count Nvidia GPU
	cnt := int32(0)
	for i := range pod.Spec.Containers {
		devices := device.GetDevices()
		counts[i] = make(util.ContainerDeviceRequests)
		klog.V(5).InfoS("Processing container resources",
			"pod", klog.KObj(pod),
			"containerIndex", i,
			"containerName", pod.Spec.Containers[i].Name)
		for idx, val := range devices {
			request := val.GenerateResourceRequests(&pod.Spec.Containers[i])
			if request.Nums > 0 {
				cnt += request.Nums
				counts[i][idx] = val.GenerateResourceRequests(&pod.Spec.Containers[i])
			}
		}
	}
	if cnt == 0 {
		klog.V(4).InfoS("No device requests found", "pod", klog.KObj(pod))
	} else {
		klog.V(4).InfoS("Resource requirements collected", "pod", klog.KObj(pod), "requests", counts)
	}
	return counts
}

func IsPodInTerminatedState(pod *corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded
}

func AllContainersCreated(pod *corev1.Pod) bool {
	return len(pod.Status.ContainerStatuses) >= len(pod.Spec.Containers)
}
