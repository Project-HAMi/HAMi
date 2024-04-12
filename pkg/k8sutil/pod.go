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
	//Count Nvidia GPU
	for i := 0; i < len(pod.Spec.Containers); i++ {
		devices := device.GetDevices()
		counts[i] = make(util.ContainerDeviceRequests)
		for idx, val := range devices {
			request := val.GenerateResourceRequests(&pod.Spec.Containers[i])
			if request.Nums > 0 {
				counts[i][idx] = val.GenerateResourceRequests(&pod.Spec.Containers[i])
			}
		}
	}
	klog.InfoS("collect requestreqs", "counts", counts)
	return counts
}

func IsPodInTerminatedState(pod *corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded
}

func AllContainersCreated(pod *corev1.Pod) bool {
	return len(pod.Status.ContainerStatuses) >= len(pod.Spec.Containers)
}
