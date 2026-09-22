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
	"os"
	"syscall"
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	nv "github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

// wholeGPUSkipHookEnabled turns on synthesizing per-pod NVML metrics for
// containers that hold one or more entire physical GPUs. It shares the
// device plugin's gate: pods only stop writing libvgpu shared-memory cache
// files when the hook is skipped for whole-GPU allocations, and those are
// exactly the containers this synthesized path exists to cover.
var wholeGPUSkipHookEnabled = os.Getenv("HAMI_WHOLE_GPU_SKIP_HOOK") == "true"

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

	// physTotalMiB resolves a device's physical memory in MiB via NVML;
	// declared as a field so tests can stub it. Populated when the state
	// is created.
	physTotalMiB func(uuid string) (int32, bool)
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
type wholeGPUUsage struct {
	nvmllib nvml.Interface
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

// reconcileWholeGPU synthesizes NVML-backed ContainerUsage entries for
// containers that hold one or more whole physical GPUs. It is called once per
// Update() cycle, inside l.mutex, before the cache-dir
// scan runs so the scan's "already in l.containers" check skips over
// synthesized entries and leaves them untouched.
//
// pods is scoped to this node by the pod informer's field selector.
func (l *ContainerLister) reconcileWholeGPU(pods []*corev1.Pod) {
	if l.wholeGPU == nil {
		l.wholeGPU = &wholeGPUState{
			nvmllib:     nvml.New(),
			verdicts:    make(map[string]wholeGPUVerdict),
			firstSeenAt: make(map[string]time.Time),
		}
		l.wholeGPU.getNodeDevices = l.fetchNodeDevices
		l.wholeGPU.physTotalMiB = func(uuid string) (int32, bool) {
			return physTotalMiB(l.wholeGPU.nvmllib, uuid)
		}
	}
	state := l.wholeGPU
	now := time.Now()

	if !state.nvmlInitialized {
		ret := state.nvmllib.Init()
		if !errors.Is(ret, nvml.SUCCESS) {
			klog.Warningf("wholegpu: nvml.Init failed, will retry next cycle: %v", ret)
			return
		}
		state.nvmlInitialized = true
	}

	type candidate struct {
		key     string
		podUID  string
		ctrName string
		devs    device.ContainerDevices
	}

	stillAnnotated := make(map[string]bool)
	var candidates []candidate

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
			key := string(pod.UID) + "_" + ctrName
			stillAnnotated[key] = true

			// The verdicts map only ever holds terminal verdicts, so a hit
			// always means the container's classification is final.
			if _, cached := state.verdicts[key]; cached {
				continue
			}
			candidates = append(candidates, candidate{key: key, podUID: string(pod.UID), ctrName: ctrName, devs: ctrDevs})
		}
	}

	if len(candidates) > 0 {
		nodeDevs, err := state.getNodeDevices()
		if err != nil {
			klog.Warningf("wholegpu: failed to fetch node device registry, skipping new candidates this cycle: %v", err)
		} else {
			for _, cand := range candidates {
				verdict, cause := evaluateContainerWholeGPU(cand.devs, nodeDevs, state.physTotalMiB)
				if verdict == indeterminate && cause == causeRegistryMiss {
					// Registry misses are forgery-sensitive, so their wait
					// is capped: past the window the verdict locks to
					// notWholeGPU for the rest of the container's lifetime.
					if first, seen := state.firstSeenAt[cand.key]; !seen {
						state.firstSeenAt[cand.key] = now
					} else if now.Sub(first) > indeterminateRetryWindow {
						verdict = notWholeGPU
					}
				}
				if verdict != indeterminate {
					state.verdicts[cand.key] = verdict
					delete(state.firstSeenAt, cand.key)
				}
				if verdict != confirmedWholeGPU {
					continue
				}

				uuids := make([]string, len(cand.devs))
				for i, cd := range cand.devs {
					uuids[i] = cd.UUID
				}
				if existing, ok := l.containers[cand.key]; ok && existing.data != nil {
					_ = syscall.Munmap(existing.data)
				}
				l.containers[cand.key] = &ContainerUsage{
					PodUID:        cand.podUID,
					ContainerName: cand.ctrName,
					synthesized:   true,
					Info:          &wholeGPUUsage{nvmllib: state.nvmllib, uuids: uuids},
				}
				klog.Infof("wholegpu: synthesized whole-GPU usage for %s (%d device(s))", cand.key, len(uuids))
			}
		}
	}

	for key, c := range l.containers {
		if c.synthesized && !stillAnnotated[key] {
			delete(l.containers, key)
		}
	}
	for key := range state.verdicts {
		if !stillAnnotated[key] {
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
func physTotalMiB(nvmllib nvml.Interface, uuid string) (int32, bool) {
	handle, ret := nvmllib.DeviceGetHandleByUUID(uuid)
	if !errors.Is(ret, nvml.SUCCESS) {
		return 0, false
	}
	mem, ret := handle.GetMemoryInfo()
	if !errors.Is(ret, nvml.SUCCESS) {
		return 0, false
	}
	return int32(mem.Total / 1024 / 1024), true
}

// evaluateContainerWholeGPU decides whether every device a container was
// allocated is a whole physical GPU: not a MIG slice, allocated its full
// memory and core quota, and free of memory/core over-subscription — a
// scaled device requires the interception library by construction, so it
// is never whole-GPU. All figures must match the node's registered device
// exactly, so stale or forged annotation values fail closed. The device
// plugin's isWholeGPUAllocation reaches the same verdict from physical
// totals and its scheduler config, so both components agree on which
// containers are whole-GPU.
//
// The second return value reports why an indeterminate verdict is pending:
// causeRegistryMiss or causePhysUnreadable (causeNotIndeterminate for any
// terminal verdict).
func evaluateContainerWholeGPU(ctrDevs device.ContainerDevices, nodeDevs map[string]*device.DeviceInfo, physMiB func(uuid string) (int32, bool)) (wholeGPUVerdict, indeterminateCause) {
	if len(ctrDevs) == 0 {
		return notWholeGPU, causeNotIndeterminate
	}
	cause := causeNotIndeterminate
	for _, cd := range ctrDevs {
		nodeDev, ok := nodeDevs[cd.UUID]
		if !ok || nodeDev == nil {
			cause = causeRegistryMiss
			continue
		}
		if nodeDev.Mode == nv.MigMode || cd.Usedmem != nodeDev.Devmem || cd.Usedcores != nv.WholeGPUUsedCores {
			return notWholeGPU, causeNotIndeterminate
		}
		// Over-subscribed (core-scaled) devices register a scaled core
		// count; they need the hook to enforce it, so never qualify.
		if nodeDev.Devcore != nv.WholeGPUUsedCores {
			return notWholeGPU, causeNotIndeterminate
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
