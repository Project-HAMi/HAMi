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
	"4pd.io/k8s-vgpu/pkg/device"
	"4pd.io/k8s-vgpu/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

func Resourcereqs(pod *corev1.Pod) (counts [][]util.ContainerDeviceRequest) {
	counts = make([][]util.ContainerDeviceRequest, len(pod.Spec.Containers))
	//Count Nvidia GPU
	for i := 0; i < len(pod.Spec.Containers); i++ {
<<<<<<< HEAD
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
				mempnum := int32(101)
				if !ok {
					mem, ok = pod.Spec.Containers[i].Resources.Requests[resourceMemPercentage]
					if ok {
						mempnums, ok := mem.AsInt64()
						if ok {
							mempnum = int32(mempnums)
						}
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
				counts[i] = append(counts[i], util.ContainerDeviceRequest{
					Nums:             int32(n),
					Type:             util.NvidiaGPUDevice,
					Memreq:           int32(memnum),
					MemPercentagereq: int32(mempnum),
					Coresreq:         int32(corenum),
				})
			}
		}
		//Count Cambricon MLU
		klog.Infof("Counting mlu devices")
		mluResourceCount := corev1.ResourceName(util.MLUResourceCount)
		mluResourceMem := corev1.ResourceName(util.MLUResourceMemory)
		v, ok = pod.Spec.Containers[i].Resources.Limits[mluResourceCount]
		if !ok {
			v, ok = pod.Spec.Containers[i].Resources.Requests[mluResourceCount]
		}
		if ok {
			if n, ok := v.AsInt64(); ok {
				klog.Info("Found mlu devices")
				memnum := 0
				mem, ok := pod.Spec.Containers[i].Resources.Limits[mluResourceMem]
				if !ok {
					mem, ok = pod.Spec.Containers[i].Resources.Requests[mluResourceMem]
				}
				if ok {
					memnums, ok := mem.AsInt64()
					if ok {
						memnum = int(memnums)
					}
				}
				counts[i] = append(counts[i], util.ContainerDeviceRequest{
					Nums:   int32(n),
					Type:   util.CambriconMLUDevice,
					Memreq: int32(memnum),
				})
=======
		devices := device.GetDevices()
		for _, val := range devices {
			request := val.GenerateResourceRequests(&pod.Spec.Containers[i])
			if request.Nums > 0 {
				counts[i] = append(counts[i], val.GenerateResourceRequests(&pod.Spec.Containers[i]))
>>>>>>> 21785f7 (update to v2.3.2)
			}
		}
	}
	klog.Infoln("counts=", counts)
	return counts
}

func IsPodInTerminatedState(pod *corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded
}

func AllContainersCreated(pod *corev1.Pod) bool {
	return len(pod.Status.ContainerStatuses) >= len(pod.Spec.Containers)
}
