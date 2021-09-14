/*
 * Copyright © 2021 peizhaoyou <peizhaoyou@4paradigm.com>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package k8sutil

import (
	"4pd.io/k8s-vgpu/pkg/scheduler/config"
	"4pd.io/k8s-vgpu/pkg/util"
	corev1 "k8s.io/api/core/v1"
)

func Resourcereqs(pod *corev1.Pod) (counts []util.ContainerDeviceRequest) {
	resourceName := corev1.ResourceName(util.ResourceName)
	resourceMem := corev1.ResourceName(util.ResourceMem)
	resourceCores := corev1.ResourceName(util.ResourceCores)
	counts = make([]util.ContainerDeviceRequest, len(pod.Spec.Containers))
	for i := 0; i < len(pod.Spec.Containers); i++ {
		v, ok := pod.Spec.Containers[i].Resources.Limits[resourceName]
		if !ok {
			v, ok = pod.Spec.Containers[i].Resources.Requests[resourceName]
		}
		if ok {
			if n, ok := v.AsInt64(); ok {
				memnum := config.DefaultMem
				mem, ok := pod.Spec.Containers[i].Resources.Limits[resourceMem]
				if !ok {
					mem, ok = pod.Spec.Containers[i].Resources.Requests[resourceMem]
				}
				if ok {
					memnums, ok := mem.AsInt64()
					if ok {
						memnum = int32(memnums)
					}
				}
				corenum := config.DefaultCores
				core, ok := pod.Spec.Containers[i].Resources.Limits[resourceCores]
				if !ok {
					core, ok = pod.Spec.Containers[i].Resources.Requests[resourceCores]
				}
				if ok {
					corenums, ok := core.AsInt64()
					if ok {
						corenum = int32(corenums)
					}
				}
				counts[i] = util.ContainerDeviceRequest{
					Nums:     int32(n),
					Memreq:   int32(memnum),
					Coresreq: int32(corenum),
				}
			}
		}
	}
	return counts
}

func ResourceNums(pod *corev1.Pod, resourceName corev1.ResourceName) (counts []int) {
	counts = make([]int, len(pod.Spec.Containers))
	for i := 0; i < len(pod.Spec.Containers); i++ {
		v, ok := pod.Spec.Containers[i].Resources.Limits[resourceName]
		if !ok {
			v, ok = pod.Spec.Containers[i].Resources.Requests[resourceName]
		}
		if ok {
			if n, ok := v.AsInt64(); ok {
				counts[i] = int(n)
			}
		}
	}
	return counts
}

func IsPodInTerminatedState(pod *corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded
}

func AllContainersCreated(pod *corev1.Pod) bool {
	return len(pod.Status.ContainerStatuses) >= len(pod.Spec.Containers)
}
