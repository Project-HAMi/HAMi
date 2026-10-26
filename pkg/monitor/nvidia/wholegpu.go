/*
Copyright 2025 The HAMi Authors.

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

package nvidia

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	nv "github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

// wholeGPUDisableHAMiCore turns on synthesizing per-pod NVML metrics for
// whole-GPU containers, covering the libvgpu cache data they stop writing
// once the hook is disabled for them.
var wholeGPUDisableHAMiCore = os.Getenv("DISABLE_HAMI_CORE_FOR_WHOLE_GPU") == "true"

// nvidiaDeviceChecklist mirrors the single NVIDIA entry of device.SupportDevices.
// That global map is only populated by nvidia.InitNvidiaDevice, which the
// vGPUmonitor process never calls, so a private copy is kept here for
// device.DecodePodDevices.
var nvidiaDeviceChecklist = map[string]string{nv.NvidiaGPUDevice: nv.AllocatedDevicesAnnotation}

// wholeGPUVerdict records whether a container has been determined to hold
// one or more whole physical GPUs.
type wholeGPUVerdict int

const (
	// notWholeGPU and confirmedWholeGPU are terminal: a pod's device
	// annotations never change after admission, so once either is reached
	// for a container it is cached for that container's lifetime and never
	// re-evaluated.
	notWholeGPU wholeGPUVerdict = iota
	confirmedWholeGPU

	// indeterminate means the verdict cannot be reached this cycle and is
	// retried on the next reconcile cycle rather than cached. How long it
	// may stay pending depends on the cause (see indeterminateCause).
	indeterminate
)

// indeterminateCause reports why a container's verdict is still pending.
type indeterminateCause int

const (
	// causeNotIndeterminate accompanies every non-indeterminate verdict.
	causeNotIndeterminate indeterminateCause = iota

	// causeRegistryMiss means the node's device registry did not yet
	// contain an entry for one of the container's UUIDs. This is the
	// forgery-sensitive case, so indeterminateRetryWindow caps how long it
	// may retry before the verdict locks to notWholeGPU.
	causeRegistryMiss

	// causePhysUnreadable means the device's physical memory could not be
	// read through NVML. That check compares two annotation-independent
	// sources (NVML vs the node registry), so it carries no
	// annotation-forgery surface; it is retried indefinitely rather than
	// being locked out, otherwise a container the device plugin already
	// unhooked would lose its metrics for good — the exact gap this
	// feature exists to close.
	causePhysUnreadable
)

// wholeGPUState holds the whole-GPU reconciliation state for one
// ContainerLister. It is created lazily on first use and lives for the
// lifetime of the ContainerLister.
type wholeGPUState struct {
	nvmllib         nvml.Interface
	nvmlInitialized bool
	getNodeDevices  func() (map[string]*device.DeviceInfo, error)

	// dcgm lazily brings up an embedded host engine the first time a MIG
	// device's utilization is actually requested; created together with the
	// state itself so synthesized wholeGPUUsage values can reference it.
	dcgm *dcgmWholeGPUCollector

	// verdicts caches terminal per-container decisions, keyed the same way
	// as ContainerLister.containers ("{podUID}_{containerName}").
	verdicts map[string]wholeGPUVerdict

	// firstSeenAt records, per verdict key, when a registry-miss
	// indeterminate was first observed. Together with
	// indeterminateRetryWindow it caps how long a registry miss may keep
	// retrying — without the cap, a forged annotation written after pod
	// admission could flip a deferred verdict into confirmedWholeGPU on a
	// later retry, causing the monitor to attribute another device's
	// utilization to this container. Phys-unreadable pendings do not
	// consume this window; see causePhysUnreadable.
	firstSeenAt map[string]time.Time

	// ownedContainerKeys tracks keys in ContainerLister.containers that were
	// installed by reconcileWholeGPULocked. Only these keys are garbage-collected
	// when the pod's NVIDIA allocation annotation disappears; cache-file-backed
	// entries remain owned by Update's cache-dir scan.
	//
	// Guarded by ContainerLister.mutex: reconcileWholeGPULocked runs inside Update().
	ownedContainerKeys map[string]struct{}

	// physTotalMiB resolves a device's physical memory in MiB via NVML;
	// declared as a field so tests can stub it. Populated when the state
	// is created.
	physTotalMiB func(uuid string) (int64, bool)
}

// indeterminateRetryWindow caps how long a pending verdict caused by a
// registry miss may keep retrying before reconcileWholeGPU locks it to
// notWholeGPU for the rest of the container's lifetime. It applies only
// to registry misses (causeRegistryMiss), never to NVML read failures.
const indeterminateRetryWindow = 30 * time.Second

// wholeGPUUsage implements UsageInfo over NVML queries for a container that
// has been confirmed to hold one or more whole physical GPUs. It is
// stateless beyond the UUID list: every getter queries NVML directly, since
// Observe() calls these outside ContainerLister's lock.
//
// dcgm is consulted only for MIG-allocated devices, whose utilization NVML
// cannot report at instance granularity; it may be nil when the caller has
// no DCGM available yet.
type wholeGPUUsage struct {
	nvmllib nvml.Interface
	dcgm    *dcgmWholeGPUCollector
	uuids   []string
}

func (u *wholeGPUUsage) DeviceMax() int { return len(u.uuids) }
func (u *wholeGPUUsage) DeviceNum() int { return len(u.uuids) }

func (u *wholeGPUUsage) DeviceMemoryContextSize(idx int) uint64 { return 0 }
func (u *wholeGPUUsage) DeviceMemoryModuleSize(idx int) uint64  { return 0 }
func (u *wholeGPUUsage) DeviceMemoryBufferSize(idx int) uint64  { return 0 }
func (u *wholeGPUUsage) DeviceMemoryOffset(idx int) uint64      { return 0 }

// DeviceMemoryTotal reports bytes currently used on the device. Despite the
// name, that is what the v0/v1 Spec implementations report here too: the
// value metrics.go sends as hami_vgpu_memory_used_bytes.
func (u *wholeGPUUsage) DeviceMemoryTotal(idx int) uint64 {
	mem, ok := u.deviceMemory(idx)
	if !ok {
		return 0
	}
	return mem.Used
}

// DeviceMemoryLimit reports the device's total memory capacity.
func (u *wholeGPUUsage) DeviceMemoryLimit(idx int) uint64 {
	mem, ok := u.deviceMemory(idx)
	if !ok {
		return 0
	}
	return mem.Total
}

func (u *wholeGPUUsage) SetDeviceMemoryLimit(l uint64) {}

// DeviceSmUtil reports the device's current SM utilization percentage.
func (u *wholeGPUUsage) DeviceSmUtil(idx int) uint64 {
	handle, ok := u.deviceHandle(idx)
	if !ok {
		return 0
	}

	// MIG devices do not support whole-device utilization rates in NVML, so
	// query them from DCGM at GPU-instance granularity instead.
	isMig, ret := handle.IsMigDeviceHandle()
	if errors.Is(ret, nvml.SUCCESS) && isMig {
		if u.dcgm == nil {
			return 0
		}
		giID, ret := handle.GetGpuInstanceId()
		if !errors.Is(ret, nvml.SUCCESS) {
			klog.V(4).Infof("wholegpu: MIG device %s GetGpuInstanceId failed: %v", u.uuids[idx], ret)
			return 0
		}
		// DCGM uses global entity IDs, so pass the parent GPU UUID along
		// with the per-parent giID to disambiguate MIG instances.
		parentHandle, ret := handle.GetDeviceHandleFromMigDeviceHandle()
		if !errors.Is(ret, nvml.SUCCESS) {
			klog.V(4).Infof("wholegpu: MIG device %s GetDeviceHandleFromMigDeviceHandle failed: %v", u.uuids[idx], ret)
			return 0
		}
		parentUUID, ret := parentHandle.GetUUID()
		if !errors.Is(ret, nvml.SUCCESS) {
			klog.V(4).Infof("wholegpu: MIG parent device GetUUID failed: %v", ret)
			return 0
		}
		return u.dcgm.GpuInstanceSmUtil(parentUUID, uint(giID), u.uuids[idx])
	}

	rates, ret := handle.GetUtilizationRates()
	if !errors.Is(ret, nvml.SUCCESS) {
		klog.V(4).Infof("wholegpu: GetUtilizationRates(%s) failed: %v", u.uuids[idx], ret)
		return 0
	}
	return uint64(rates.Gpu)

}

func (u *wholeGPUUsage) SetDeviceSmLimit(l uint64) {}

// IsValidUUID reports whether the UUID at idx looks like a real NVML UUID.
// Pod annotations are external input, so this guards metrics.go's
// uuid[0:40] slicing against a malformed or truncated annotation value.
func (u *wholeGPUUsage) IsValidUUID(idx int) bool {
	return idx >= 0 && idx < len(u.uuids) && len(u.uuids[idx]) >= 40
}

func (u *wholeGPUUsage) DeviceUUID(idx int) string {
	if idx < 0 || idx >= len(u.uuids) {
		return ""
	}
	return u.uuids[idx]
}

func (u *wholeGPUUsage) LastKernelTime() int64        { return 0 }
func (u *wholeGPUUsage) GetPriority() int             { return 0 }
func (u *wholeGPUUsage) GetRecentKernel() int32       { return 0 }
func (u *wholeGPUUsage) SetRecentKernel(v int32)      {}
func (u *wholeGPUUsage) GetUtilizationSwitch() int32  { return 0 }
func (u *wholeGPUUsage) SetUtilizationSwitch(v int32) {}

func (u *wholeGPUUsage) deviceHandle(idx int) (nvml.Device, bool) {
	if idx < 0 || idx >= len(u.uuids) {
		return nil, false
	}
	handle, ret := u.nvmllib.DeviceGetHandleByUUID(u.uuids[idx])
	if !errors.Is(ret, nvml.SUCCESS) {
		klog.V(4).Infof("wholegpu: DeviceGetHandleByUUID(%s) failed: %v", u.uuids[idx], ret)
		return nil, false
	}
	return handle, true
}

func (u *wholeGPUUsage) deviceMemory(idx int) (nvml.Memory, bool) {
	handle, ok := u.deviceHandle(idx)
	if !ok {
		return nvml.Memory{}, false
	}
	mem, ret := handle.GetMemoryInfo()
	if !errors.Is(ret, nvml.SUCCESS) {
		klog.V(4).Infof("wholegpu: GetMemoryInfo(%s) failed: %v", u.uuids[idx], ret)
		return nvml.Memory{}, false
	}
	return mem, true
}

// wholeGPUInputs carries one reconcile cycle's inputs across the l.mutex
// boundary: prefetchWholeGPUInputs gathers them with the registry GET (network
// IO) outside the lock; reconcileWholeGPULocked consumes them under it.
type wholeGPUInputs struct {
	stillAnnotated map[string]bool
	candidates     []wholeGPUCandidate
	nodeDevs       map[string]*device.DeviceInfo
	nodeDevsErr    error
}

type wholeGPUCandidate struct {
	pod       *corev1.Pod
	container string
	devices   device.ContainerDevices
}

// reconcileWholeGPU composes prefetchWholeGPUInputs and reconcileWholeGPULocked
// for tests that do not drive the result through Update(). Production paths
// call the two functions directly around their own locked section so the
// cache-dir scan can skip over synthesized entries.
//
// pods is scoped to this node by the pod informer's field selector.
func (l *ContainerLister) reconcileWholeGPU(pods []*corev1.Pod) {
	inputs := l.prefetchWholeGPUInputs(pods)
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.reconcileWholeGPULocked(inputs)
}

// prefetchWholeGPUInputs gathers the current cycle's still-annotated containers and
// unresolved candidates. Verdicts are read in a short critical section; the
// registry GET runs unlocked and only while some verdict is still uncached.
func (l *ContainerLister) prefetchWholeGPUInputs(pods []*corev1.Pod) *wholeGPUInputs {
	inputs := &wholeGPUInputs{stillAnnotated: make(map[string]bool)}
	var annotated []wholeGPUCandidate
	for _, pod := range pods {
		if len(pod.Annotations) == 0 {
			continue
		}
		podDevices, err := device.DecodePodDevices(nvidiaDeviceChecklist, pod.Annotations)
		if err != nil {
			klog.Warningf("wholegpu: failed to decode device annotations for pod %s/%s: %v", pod.Namespace, pod.Name, err)
			continue
		}
		for ctrIdx, ctrDevs := range podDevices[nv.NvidiaGPUDevice] {
			if len(ctrDevs) == 0 {
				continue
			}
			ctrName, ok := containerNameByIndex(pod, ctrIdx)
			if !ok {
				klog.Warningf("wholegpu: container index %d out of range for pod %s/%s", ctrIdx, pod.Namespace, pod.Name)
				continue
			}
			inputs.stillAnnotated[string(pod.UID)+"_"+ctrName] = true
			annotated = append(annotated, wholeGPUCandidate{pod: pod, container: ctrName, devices: ctrDevs})
		}
	}
	if len(annotated) == 0 {
		return inputs
	}

	getNodeDevices := l.fetchNodeDevices
	l.mutex.Lock()
	if state := l.wholeGPU; state != nil {
		if state.getNodeDevices != nil {
			getNodeDevices = state.getNodeDevices
		}
		// While NVML stays down (reconcileWholeGPULocked retries init), defer
		// evaluation entirely, sparing the registry GET.
		if state.nvmlInitialized {
			for _, cand := range annotated {
				if _, cached := state.verdicts[string(cand.pod.UID)+"_"+cand.container]; !cached {
					inputs.candidates = append(inputs.candidates, cand)
				}
			}
		}
	} else {
		inputs.candidates = annotated
	}
	l.mutex.Unlock()

	if len(inputs.candidates) > 0 {
		inputs.nodeDevs, inputs.nodeDevsErr = getNodeDevices()
	}
	return inputs
}

// reconcileWholeGPULocked consumes prefetched inputs, mutating
// containers/verdicts; the caller holds l.mutex. No network IO.
func (l *ContainerLister) reconcileWholeGPULocked(inputs *wholeGPUInputs) {
	if l.wholeGPU == nil {
		l.wholeGPU = &wholeGPUState{
			nvmllib:            nvml.New(),
			dcgm:               newDCGMWholeGPUCollector(nil),
			verdicts:           make(map[string]wholeGPUVerdict),
			firstSeenAt:        make(map[string]time.Time),
			ownedContainerKeys: make(map[string]struct{}),
		}
		l.wholeGPU.getNodeDevices = l.fetchNodeDevices
		l.wholeGPU.physTotalMiB = func(uuid string) (int64, bool) {
			return physTotalMiB(l.wholeGPU.nvmllib, uuid)
		}
	}
	state := l.wholeGPU
	if state.ownedContainerKeys == nil {
		state.ownedContainerKeys = make(map[string]struct{})
	}
	now := time.Now()

	if !state.nvmlInitialized {
		ret := state.nvmllib.Init()
		if !errors.Is(ret, nvml.SUCCESS) {
			klog.Warningf("wholegpu: nvml.Init failed, will retry next cycle: %v", ret)
			return
		}
		state.nvmlInitialized = true
	}

	if len(inputs.candidates) > 0 {
		if inputs.nodeDevsErr != nil {
			klog.Warningf("wholegpu: failed to fetch node device registry, skipping new candidates this cycle: %v", inputs.nodeDevsErr)
		} else {
			for _, cand := range inputs.candidates {
				key := string(cand.pod.UID) + "_" + cand.container
				// Another reconcile cycle may have resolved the verdict since prefetch.
				if _, cached := state.verdicts[key]; cached {
					continue
				}
				resolved, registry, ready := resolveDynamicMIGCandidate(cand, inputs.nodeDevs)
				if !ready {
					continue
				}
				verdict, cause := evaluateContainerWholeGPU(resolved, registry, state.physTotalMiB)
				if verdict == indeterminate && cause == causeRegistryMiss {
					// Registry misses are forgery-sensitive, so their wait
					// is capped: past the window the verdict locks to
					// notWholeGPU for the rest of the container's lifetime.
					if first, seen := state.firstSeenAt[key]; !seen {
						state.firstSeenAt[key] = now
					} else if now.Sub(first) > indeterminateRetryWindow {
						verdict = notWholeGPU
					}
				}
				if verdict != indeterminate {
					state.verdicts[key] = verdict
					delete(state.firstSeenAt, key)
				}
				if verdict != confirmedWholeGPU {
					continue
				}

				uuids := make([]string, len(resolved))
				for i, cd := range resolved {
					uuids[i] = cd.UUID
				}
				if existing, ok := l.containers[key]; ok && existing.data != nil {
					_ = syscall.Munmap(existing.data)
				}
				l.containers[key] = &ContainerUsage{
					PodUID:        string(cand.pod.UID),
					ContainerName: cand.container,
					Info:          &wholeGPUUsage{nvmllib: state.nvmllib, dcgm: state.dcgm, uuids: uuids},
				}
				state.ownedContainerKeys[key] = struct{}{}
				klog.Infof("wholegpu: synthesized whole-GPU usage for %s (%d device(s))", key, len(uuids))
			}
		}
	}

	for key := range state.ownedContainerKeys {
		if !inputs.stillAnnotated[key] {
			delete(l.containers, key)
			delete(state.ownedContainerKeys, key)
		}
	}
	for key := range state.verdicts {
		if !inputs.stillAnnotated[key] {
			delete(state.verdicts, key)
			delete(state.firstSeenAt, key)
		}
	}
}

// fetchNodeDevices reads this node's registered-device table, written by the
// device plugin, so reconcileWholeGPU can look up each candidate device's
// Devmem and Mode by UUID.
func (l *ContainerLister) fetchNodeDevices() (map[string]*device.DeviceInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	node, err := l.clientset.CoreV1().Nodes().Get(ctx, l.nodeName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get node %s: %w", l.nodeName, err)
	}
	anno, ok := node.Annotations[nv.RegisterAnnos]
	if !ok || anno == "" {
		return map[string]*device.DeviceInfo{}, nil
	}
	dlist, err := device.UnMarshalNodeDevices(anno)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal node %s register annotation: %w", l.nodeName, err)
	}
	devs := make(map[string]*device.DeviceInfo, len(dlist))
	for _, d := range dlist {
		devs[d.ID] = d
	}
	return devs, nil
}

// physTotalMiB reports a device's physical total memory in MiB via NVML.
// Over-subscribed nodes register scaled memory instead, so comparing this
// against the registry's Devmem detects DeviceMemoryScaling != 1.
func physTotalMiB(nvmllib nvml.Interface, uuid string) (int64, bool) {
	handle, ret := nvmllib.DeviceGetHandleByUUID(uuid)
	if !errors.Is(ret, nvml.SUCCESS) {
		return 0, false
	}
	mem, ret := handle.GetMemoryInfo()
	if !errors.Is(ret, nvml.SUCCESS) {
		return 0, false
	}
	return int64(mem.Total / 1024 / 1024), true
}

// evaluateContainerWholeGPU reports whether a container holds every device it
// was allocated, so that device's whole NVML utilization is its own. MIG
// instances are hardware-isolated and always handed over whole, so they qualify
// at instance granularity; anything else needs full memory and cores, matching
// the device plugin's isWholeGPUAllocation.
//
// The returned cause distinguishes an indeterminate verdict: a registry miss or
// an unreadable NVML reading is transient, so the caller retries rather than
// failing closed. physMiB may be nil, skipping the over-subscription check.
func evaluateContainerWholeGPU(ctrDevs device.ContainerDevices, nodeDevs map[string]*device.DeviceInfo, physMiB func(uuid string) (int64, bool)) (wholeGPUVerdict, indeterminateCause) {
	if len(ctrDevs) == 0 {
		return notWholeGPU, causeNotIndeterminate
	}
	cause := causeNotIndeterminate
	for _, cd := range ctrDevs {
		nodeDev, ok := nodeDevs[cd.UUID]
		if !ok || nodeDev == nil {
			// Not in the node registry this cycle: the annotation may simply
			// be older than the registration, so stay pending and retry.
			cause = causeRegistryMiss
			continue
		}
		if nodeDev.Mode == nv.MigMode {
			// MIG registers each instance as its own device, and an instance is
			// hardware-isolated, so holding one means holding it whole. Its core
			// figure is the profile's physical SM count (1g.10gb -> 14) rather than
			// the 100-percent marker, so the core checks below do not apply.
			if cd.Usedmem != nodeDev.Devmem {
				return notWholeGPU, causeNotIndeterminate
			}
			continue
		}
		// Cores fully requested or unrequested (0 is unbounded) both mean
		// the whole card; only an explicit partial core request disqualifies.
		if cd.Usedmem != nodeDev.Devmem || (cd.Usedcores != 0 && cd.Usedcores != nv.WholeGPUUsedCores) {
			return notWholeGPU, causeNotIndeterminate
		}
		// Over-subscribed (core-scaled) devices register a scaled core
		// count; they need the hook to enforce it, so never qualify.
		if nodeDev.Devcore != nv.WholeGPUUsedCores {
			return notWholeGPU, causeNotIndeterminate
		}
		// No probe means the caller classifies without NVML: scaling cannot be
		// detected, so skip it rather than leave the verdict pending forever.
		if physMiB == nil {
			continue
		}
		totalMiB, ok := physMiB(cd.UUID)
		if !ok {
			// Physical memory unreadable this cycle: retry on the next
			// reconcile rather than fail closed.
			if cause == causeNotIndeterminate {
				cause = causePhysUnreadable
			}
			continue
		}
		if totalMiB != nodeDev.Devmem {
			// Registry memory is over-subscription scaled: hook required.
			return notWholeGPU, causeNotIndeterminate
		}
	}
	// A pending cause outranks confirming the container; a not-whole device
	// anywhere in the request already returned above.
	if cause != causeNotIndeterminate {
		return indeterminate, cause
	}
	return confirmedWholeGPU, causeNotIndeterminate
}

// containerNameByIndex maps a ContainerDevices slot from device.DecodePodDevices
// back to the pod's container name. Init containers occupy the leading
// indices, matching the encoding order in the device plugin's Allocate path.
func containerNameByIndex(pod *corev1.Pod, idx int) (string, bool) {
	if idx < 0 {
		return "", false
	}
	if idx < len(pod.Spec.InitContainers) {
		return pod.Spec.InitContainers[idx].Name, true
	}
	idx -= len(pod.Spec.InitContainers)
	if idx >= len(pod.Spec.Containers) {
		return "", false
	}
	return pod.Spec.Containers[idx].Name, true
}

// resolveDynamicMIGCandidate joins scheduler reservations with realized runtime
// identities. Pending identities remain retryable rather than caching rejection.
func resolveDynamicMIGCandidate(c wholeGPUCandidate, registry map[string]*device.DeviceInfo) (device.ContainerDevices, map[string]*device.DeviceInfo, bool) {
	allocations, err := nv.DecodeMigAllocations(c.pod.Annotations[nv.MigAllocationsAnnotation])
	if err != nil {
		return nil, nil, false
	}
	if len(allocations) == 0 {
		for _, dev := range c.devices {
			if parent := registry[dev.UUID]; parent != nil && parent.Mode == nv.MigMode && strings.HasPrefix(dev.UUID, "GPU-") {
				return nil, nil, false
			}
		}
		return c.devices, registry, true
	}
	ci := -1
	for i := range len(c.pod.Spec.InitContainers) + len(c.pod.Spec.Containers) {
		if name, ok := containerNameByIndex(c.pod, i); ok && name == c.container {
			ci = i
			break
		}
	}
	resolved := append(device.ContainerDevices(nil), c.devices...)
	nodes := make(map[string]*device.DeviceInfo, len(registry)+len(resolved))
	maps.Copy(nodes, registry)
	for di, dev := range resolved {
		parent := registry[dev.UUID]
		if parent == nil || parent.Mode != nv.MigMode {
			continue
		}
		found := false
		for _, a := range allocations {
			if a.ContainerIndex != ci || a.DeviceIndex != di {
				continue
			}
			if a.GPUUUID != dev.UUID || !strings.HasPrefix(a.MigUUID, "MIG-") {
				return nil, nil, false
			}
			valid := false
			for _, profile := range parent.MIGProfiles {
				if profile.Name == a.Profile && profile.MemoryMB == dev.Usedmem {
					valid = true
				}
			}
			if !valid {
				return nil, nil, false
			}
			resolved[di].UUID = a.MigUUID
			instance := *parent
			instance.ID = a.MigUUID
			instance.Devmem = dev.Usedmem
			nodes[a.MigUUID] = &instance
			found = true
			break
		}
		if !found {
			return nil, nil, false
		}
	}
	return resolved, nodes, true
}
