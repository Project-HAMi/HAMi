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

// alignOffset maps a sparse annotation back onto container order. The encoder
// is expected to emit a placeholder per container, but older annotations omit
// leading non-GPU init entries, so index 0 would misclassify the first app
// entry as init and clear exclusive usage. Trim the encoder's trailing empty
// (exactly the surplus beyond total containers) and align any remainder
// shortfall from the end.
func alignOffset(podSingle PodSingleDevice, numInit, numApp int) (PodSingleDevice, int) {
	total := numInit + numApp
	end := len(podSingle)
	for end > total && len(podSingle[end-1]) == 0 {
		end--
	}
	trimmed := podSingle[:end]
	if offset := total - len(trimmed); offset > 0 {
		return trimmed, offset
	}
	return trimmed, 0
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

		entries, offset := alignOffset(podSingle, numInit, len(pod.Spec.Containers))
		for cidx, ctrDevs := range entries {
			eidx := cidx + offset
			switch {
			case eidx < numInit && isSidecarAt(pod, eidx):
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
			case eidx < numInit:
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
		numApp := len(pod.Spec.Containers)
		entries, offset := alignOffset(podSingle, numInit, numApp)
		for cidx, ctrDevs := range entries {
			eidx := cidx + offset
			if eidx < numInit && !isSidecarAt(pod, eidx) {
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
		// Decoded sparse annotations carry the encoder's trailing empty on top
		// of a missing leading init placeholder (len == total, last empty).
		// The base pass then skips the only app entry as init and clears
		// usage. Retry as sparse when the base sums nothing but raw is non-empty.
		if len(sums) == 0 && len(entries) > 0 && len(entries[len(entries)-1]) == 0 {
			hasData := false
			for _, ctrDevs := range podSingle {
				if len(ctrDevs) > 0 {
					hasData = true
					break
				}
			}
			if hasData {
				sparse := entries[:len(entries)-1]
				sparseOffset := numInit + numApp - len(sparse)
				if sparseOffset < 0 {
					sparseOffset = 0
				}
				for cidx, ctrDevs := range sparse {
					if cidx+sparseOffset < numInit && !isSidecarAt(pod, cidx+sparseOffset) {
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
