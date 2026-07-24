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

package device

import (
	corev1 "k8s.io/api/core/v1"
)

// Init-container GPU resource accounting.
//
// Kubernetes runs init containers sequentially to completion before any app
// container starts, so an init container and an app container never use GPU at
// the same instant. HAMi's raw per-container device list (PodDevices) records
// one entry per container, and the accounting consumers (countPodDevices for
// quota, getNodesUsage for node capacity) naively sum every entry. That double
// counts init+app usage.
//
// The correct footprint at any instant, per device UUID and per resource
// dimension, is:
//
//	effective = max( sum(app container usage), max(single init container usage) )
//
// CollapseInitContainerUsage rewrites a PodDevices into an accounting-only view
// whose per-UUID sums equal that effective value, so the existing summing
// consumers become correct without change. See docs/develop/initContainer-design.md.

// effectiveUsage holds the collapsed per-UUID accounting values for a single
// device type. Count is the number of shared instances on the UUID (each maps
// to one Used++ in getNodesUsage); mem/cores are the peak resource footprint.
type effectiveUsage struct {
	uuid  string
	typ   string
	count int32
	mem   int32
	cores int32
}

// collapseSingleDevice computes the effective per-UUID usage for one device
// type's PodSingleDevice. numInit is the number of leading entries that belong
// to init containers (annotation index order matches pod.Spec order: init
// containers first, then app containers). When appOnly is true the init
// contribution is ignored entirely (used once init containers are confirmed
// finished).
func collapseSingleDevice(psd PodSingleDevice, numInit int, appOnly bool) []effectiveUsage {
	// Per-UUID cumulative app usage (sum across app containers).
	type acc struct {
		typ         string
		appCount    int32
		appMem      int32
		appCores    int32
		initCount   int32
		initMem     int32
		initCores   int32
		firstSeenIx int
	}
	order := make([]string, 0)
	byUUID := make(map[string]*acc)

	get := func(uuid string) *acc {
		a, ok := byUUID[uuid]
		if !ok {
			a = &acc{firstSeenIx: len(order)}
			byUUID[uuid] = a
			order = append(order, uuid)
		}
		return a
	}

	for ctrIdx, ctrDevs := range psd {
		isInit := ctrIdx < numInit
		if isInit && appOnly {
			continue
		}
		// Aggregate this single container's usage per UUID first, then fold
		// into app-sum or init-max accordingly.
		perCtr := make(map[string]*effectiveUsage)
		ctrOrder := make([]string, 0)
		for _, d := range ctrDevs {
			e, ok := perCtr[d.UUID]
			if !ok {
				e = &effectiveUsage{uuid: d.UUID, typ: d.Type}
				perCtr[d.UUID] = e
				ctrOrder = append(ctrOrder, d.UUID)
			}
			e.count++
			e.mem += d.Usedmem
			e.cores += d.Usedcores
		}
		for _, uuid := range ctrOrder {
			e := perCtr[uuid]
			a := get(uuid)
			if a.typ == "" {
				a.typ = e.typ
			}
			if isInit {
				// max across individual init containers
				if e.count > a.initCount {
					a.initCount = e.count
				}
				if e.mem > a.initMem {
					a.initMem = e.mem
				}
				if e.cores > a.initCores {
					a.initCores = e.cores
				}
			} else {
				// sum across app containers
				a.appCount += e.count
				a.appMem += e.mem
				a.appCores += e.cores
			}
		}
	}

	res := make([]effectiveUsage, 0, len(order))
	for _, uuid := range order {
		a := byUUID[uuid]
		eff := effectiveUsage{
			uuid:  uuid,
			typ:   a.typ,
			count: max(a.appCount, a.initCount),
			mem:   max(a.appMem, a.initMem),
			cores: max(a.appCores, a.initCores),
		}
		if eff.count == 0 {
			continue
		}
		res = append(res, eff)
	}
	return res
}

// buildCollapsedSingleDevice turns effective per-UUID usage back into a valid
// PodSingleDevice. It synthesizes a single container-devices list: for each
// UUID it emits `count` ContainerDevice entries so that getNodesUsage records
// the correct Used count, carrying the full effective mem/cores on the first
// entry (the consumers sum per UUID, so the split is irrelevant to totals).
func buildCollapsedSingleDevice(effs []effectiveUsage) PodSingleDevice {
	if len(effs) == 0 {
		return PodSingleDevice{}
	}
	cd := make(ContainerDevices, 0, len(effs))
	for _, e := range effs {
		for i := int32(0); i < e.count; i++ {
			dev := ContainerDevice{
				UUID: e.uuid,
				Type: e.typ,
			}
			if i == 0 {
				dev.Usedmem = e.mem
				dev.Usedcores = e.cores
			}
			cd = append(cd, dev)
		}
	}
	return PodSingleDevice{cd}
}

// collapse rewrites podDev into an accounting-only view. When appOnly is true
// the init containers' contribution is dropped (used after they finish).
func collapse(pod *corev1.Pod, podDev PodDevices, appOnly bool) PodDevices {
	if podDev == nil {
		return nil
	}
	numInit := len(pod.Spec.InitContainers)
	// No init containers: only meaningful when appOnly==false, and the app-sum
	// view is identical to the raw structure's per-UUID totals. Still collapse
	// so behaviour is uniform, but this is a no-op for correctness.
	out := make(PodDevices, len(podDev))
	for devType, psd := range podDev {
		effs := collapseSingleDevice(psd, numInit, appOnly)
		out[devType] = buildCollapsedSingleDevice(effs)
	}
	return out
}

// CollapseInitContainerUsage returns an accounting view of podDev where, per
// device UUID and per resource dimension, usage equals
// max(sum(app entries), max(init entry)). It never merges usage across distinct
// UUIDs, so a multi-GPU pod whose init and app containers land on different
// physical devices keeps them separate.
func CollapseInitContainerUsage(pod *corev1.Pod, podDev PodDevices) PodDevices {
	return collapse(pod, podDev, false)
}

// AppContainersOnly returns an accounting view counting app containers only.
// Used to shrink recorded usage once a pod's init containers are confirmed
// finished.
func AppContainersOnly(pod *corev1.Pod, podDev PodDevices) PodDevices {
	return collapse(pod, podDev, true)
}

// InitContainersAllTerminated reports whether every init container has a
// Terminated status set, regardless of exit code. Checked against the current
// object state on every reconcile (Phase is unreliable here per the design).
func InitContainersAllTerminated(pod *corev1.Pod) bool {
	if len(pod.Spec.InitContainers) == 0 {
		return false
	}
	if len(pod.Status.InitContainerStatuses) < len(pod.Spec.InitContainers) {
		return false
	}
	for _, st := range pod.Status.InitContainerStatuses {
		if st.State.Terminated == nil {
			return false
		}
	}
	return true
}

// InitContainersAllSucceeded reports whether every init container has
// terminated with ExitCode 0. When true (and the pod is not yet terminal) the
// app containers have started, so recorded usage can shrink to app-only.
func InitContainersAllSucceeded(pod *corev1.Pod) bool {
	if !InitContainersAllTerminated(pod) {
		return false
	}
	for _, st := range pod.Status.InitContainerStatuses {
		if st.State.Terminated == nil || st.State.Terminated.ExitCode != 0 {
			return false
		}
	}
	return true
}
