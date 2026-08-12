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

package device

import (
	"sort"

	corev1 "k8s.io/api/core/v1"
)

type deviceKey struct {
	devType string
	uuid    string
}
type usage struct {
	mem   int32
	cores int32
	slots int32
}

// CollapseInitContainerUsage returns the effective device usage for a pod.
func CollapseInitContainerUsage(pod *corev1.Pod, raw PodDevices) PodDevices {
	if raw == nil {
		return nil
	}
	numInit := len(pod.Spec.InitContainers)
	initPeak := make(map[deviceKey]usage)
	appSum := make(map[deviceKey]usage)

	for devType, podSingle := range raw {
		for cidx, ctrDevs := range podSingle {
			for _, dev := range ctrDevs {
				key := deviceKey{devType: devType, uuid: dev.UUID}
				if cidx < numInit {
					cur := initPeak[key]
					if dev.Usedmem > cur.mem {
						cur.mem = dev.Usedmem
					}
					if dev.Usedcores > cur.cores {
						cur.cores = dev.Usedcores
					}
					// Init containers run sequentially: peak concurrency is one slot per device.
					cur.slots = 1
					initPeak[key] = cur
				} else {
					cur := appSum[key]
					cur.mem += dev.Usedmem
					cur.cores += dev.Usedcores
					// App containers run concurrently: each occurrence is an additional slot.
					cur.slots++
					appSum[key] = cur
				}
			}
		}
	}

	collapsed := make(PodDevices)
	for devType := range raw {
		uuidSet := make(map[string]struct{})
		for k := range initPeak {
			if k.devType == devType {
				uuidSet[k.uuid] = struct{}{}
			}
		}
		for k := range appSum {
			if k.devType == devType {
				uuidSet[k.uuid] = struct{}{}
			}
		}

		collapsedSingle := make(PodSingleDevice, 1)
		var containerDevs ContainerDevices

		for uuid := range uuidSet {
			initU := initPeak[deviceKey{devType, uuid}]
			appU := appSum[deviceKey{devType, uuid}]

			effMem := max(initU.mem, appU.mem)
			effCores := max(initU.cores, appU.cores)
			// Raw entries carry no slot count; clamp to at least one.
			effSlots := max(initU.slots, appU.slots, 1)

			containerDevs = append(containerDevs, ContainerDevice{
				UUID:      uuid,
				Type:      devType,
				Usedmem:   effMem,
				Usedcores: effCores,
				Slots:     effSlots,
			})
		}
		sort.Slice(containerDevs, func(i, j int) bool {
			return containerDevs[i].UUID < containerDevs[j].UUID
		})
		collapsedSingle[0] = containerDevs
		collapsed[devType] = collapsedSingle
	}
	return collapsed
}

// AppContainersOnlyDeviceUsage returns the device usage considering only app containers.
// Used when init containers have finished and we want to shrink to app-only footprint.
func AppContainersOnlyDeviceUsage(pod *corev1.Pod, raw PodDevices) PodDevices {
	if raw == nil {
		return nil
	}
	numInit := len(pod.Spec.InitContainers)

	collapsed := make(PodDevices)
	for devType, podSingle := range raw {
		sums := make(map[string]usage)
		for cidx, ctrDevs := range podSingle {
			if cidx < numInit {
				continue // skip init containers
			}
			for _, dev := range ctrDevs {
				s := sums[dev.UUID]
				s.mem += dev.Usedmem
				s.cores += dev.Usedcores
				s.slots++ // each concurrent app container occurrence is one slot
				sums[dev.UUID] = s
			}
		}
		collapsedSingle := make(PodSingleDevice, 1)
		var containerDevs ContainerDevices
		for uuid, s := range sums {
			containerDevs = append(containerDevs, ContainerDevice{
				UUID:      uuid,
				Type:      devType,
				Usedmem:   s.mem,
				Usedcores: s.cores,
				Slots:     s.slots,
			})
		}
		sort.Slice(containerDevs, func(i, j int) bool {
			return containerDevs[i].UUID < containerDevs[j].UUID
		})
		collapsedSingle[0] = containerDevs
		collapsed[devType] = collapsedSingle
	}
	return collapsed
}
