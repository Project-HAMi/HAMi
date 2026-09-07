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

	"github.com/Project-HAMi/HAMi/pkg/util"
)

type usage struct {
	mem   int32
	cores int32
	slots int32
}

// slotsOf returns the concurrent-task count an entry occupies on its device.
// Raw allocations leave Slots unset, which means a single slot.
func slotsOf(dev ContainerDevice) int32 {
	return max(dev.Slots, 1)
}

func isSidecarAt(pod *corev1.Pod, cidx int) bool {
	if cidx < 0 || cidx >= len(pod.Spec.InitContainers) {
		return false
	}
	return util.IsSidecarContainer(&pod.Spec.InitContainers[cidx])
}

// CollapseInitContainerUsage returns the effective device usage for a pod.
func CollapseInitContainerUsage(pod *corev1.Pod, raw PodDevices) PodDevices {
	if raw == nil {
		return nil
	}
	numInit := len(pod.Spec.InitContainers)

	type devState struct {
		sc   usage // running sum of sidecars declared so far
		peak usage // peak concurrent usage observed during the init phase
		app  usage // sum over app containers
	}

	collapsed := make(PodDevices)
	for devType, podSingle := range raw {
		states := make(map[string]*devState)
		get := func(uuid string) *devState {
			s, ok := states[uuid]
			if !ok {
				s = &devState{}
				states[uuid] = s
			}
			return s
		}

		for cidx, ctrDevs := range podSingle {
			switch {
			case cidx < numInit && isSidecarAt(pod, cidx):
				for _, dev := range ctrDevs {
					s := get(dev.UUID)
					// A sidecar starts and never exits: it permanently
					// joins the set of running containers.
					s.sc.mem += dev.Usedmem
					s.sc.cores += dev.Usedcores
					s.sc.slots += slotsOf(dev)
					s.peak.mem = max(s.peak.mem, s.sc.mem)
					s.peak.cores = max(s.peak.cores, s.sc.cores)
					s.peak.slots = max(s.peak.slots, s.sc.slots)
				}
			case cidx < numInit:
				for _, dev := range ctrDevs {
					s := get(dev.UUID)
					s.peak.mem = max(s.peak.mem, s.sc.mem+dev.Usedmem)
					s.peak.cores = max(s.peak.cores, s.sc.cores+dev.Usedcores)
					s.peak.slots = max(s.peak.slots, s.sc.slots+slotsOf(dev))
				}
			default:
				for _, dev := range ctrDevs {
					s := get(dev.UUID)
					s.app.mem += dev.Usedmem
					s.app.cores += dev.Usedcores
					s.app.slots += slotsOf(dev)
				}
			}
		}

		collapsedSingle := make(PodSingleDevice, 1)
		var containerDevs ContainerDevices
		for uuid, s := range states {
			containerDevs = append(containerDevs, ContainerDevice{
				UUID:      uuid,
				Type:      devType,
				Usedmem:   max(s.peak.mem, s.sc.mem+s.app.mem),
				Usedcores: max(s.peak.cores, s.sc.cores+s.app.cores),
				Slots:     max(max(s.peak.slots, s.sc.slots+s.app.slots), 1),
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

func SteadyStateDeviceUsage(pod *corev1.Pod, raw PodDevices) PodDevices {
	if raw == nil {
		return nil
	}
	numInit := len(pod.Spec.InitContainers)

	collapsed := make(PodDevices)
	for devType, podSingle := range raw {
		sums := make(map[string]usage)
		for cidx, ctrDevs := range podSingle {
			if cidx < numInit && !isSidecarAt(pod, cidx) {
				continue
			}
			for _, dev := range ctrDevs {
				s := sums[dev.UUID]
				s.mem += dev.Usedmem
				s.cores += dev.Usedcores
				s.slots += slotsOf(dev)
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
