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

package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

// patchPodAnnotations patches only the given annotations, with the pod's
// resourceVersion as a precondition so a stale refit cannot overwrite a
// newer update to the same annotations (for example Allocate consuming a
// to-allocate entry). Conflicts fail the refit; there is no retry with
// cached values. Also a test seam.
var patchPodAnnotations = func(pod *corev1.Pod, annotations map[string]string) error {
	metadata := map[string]any{"annotations": annotations}
	if pod.ResourceVersion != "" {
		metadata["resourceVersion"] = pod.ResourceVersion
	}
	payload, err := json.Marshal(map[string]any{"metadata": metadata})
	if err != nil {
		return err
	}
	// The refit holds the allocation lock that Filter also takes, so this
	// call must not block scheduling on an unreachable API server. The
	// device plugin gives up after numaRefitTimeout anyway.
	ctx, cancel := context.WithTimeout(context.Background(), refitPatchTimeout)
	defer cancel()
	_, err = client.GetClient().CoreV1().Pods(pod.Namespace).
		Patch(ctx, pod.Name, k8stypes.MergePatchType, payload, metav1.PatchOptions{})
	return err
}

// maxAllowedDeviceUUIDs bounds the allowed set a refit request may carry.
const maxAllowedDeviceUUIDs = 512

// refitPatchTimeout bounds the annotation patch. It is deliberately no longer
// than the device plugin's own refit budget: a patch that has not landed by
// then cannot be used, and waiting longer would hold the allocation lock
// against every concurrent Filter call.
const refitPatchTimeout = 2 * time.Second

// RefitNumaAllocation moves one container's device reservation onto a device
// from the caller-supplied allowed set, re-running the pod's normal
// policy-chain fit restricted to that set. The device plugin calls it (via
// the /refit route) when kubelet's Topology Manager restricted an allocation
// to replicas of devices the scheduler did not annotate; see issue #2080.
//
// The scheduler stays authoritative: the refit runs the same fit and
// capacity checks as scheduling, patches hami.io/vgpu-devices-to-allocate
// and hami.io/vgpu-devices-allocated together in one merge patch, and only
// then moves the in-memory reservation. Failures are reported in-band and
// leave both annotations and accounting untouched.
func (s *Scheduler) RefitNumaAllocation(req device.NumaRefitRequest) device.NumaRefitResponse {
	if req.PodUID == "" || req.PodNamespace == "" || req.PodName == "" || req.NodeName == "" {
		return numaRefitFailure(nil, "incomplete refit request: pod UID, namespace, name, and node are required")
	}
	if len(req.AllowedDeviceUUIDs) == 0 {
		return numaRefitFailure(nil, "refit request carries an empty allowed device set")
	}
	if len(req.AllowedDeviceUUIDs) > maxAllowedDeviceUUIDs {
		return numaRefitFailure(nil, "refit request carries %d allowed devices, limit is %d", len(req.AllowedDeviceUUIDs), maxAllowedDeviceUUIDs)
	}
	refitDevice, ok := device.GetDevices()[req.DeviceType]
	if !ok {
		return numaRefitFailure(nil, "unknown device type %q", req.DeviceType)
	}
	// HAMi's type matching is substring based, so a refit for one type could
	// restrict sibling types whose names contain it. Only the NVIDIA plugin
	// sends refits today; widening this needs exact type identity first.
	if req.DeviceType != nvidia.NvidiaGPUDevice {
		return numaRefitFailure(nil, "device type %q is not supported by the NUMA refit yet", req.DeviceType)
	}

	s.allocLock.Lock()
	defer s.allocLock.Unlock()

	key := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: k8stypes.UID(req.PodUID)}}
	pi, ok := s.podManager.GetPod(key)
	if !ok {
		return numaRefitFailure(nil, "pod %s is not tracked by the scheduler", req.PodUID)
	}
	pod := pi.Pod
	if pod.Namespace != req.PodNamespace || pod.Name != req.PodName {
		return numaRefitFailure(nil, "pod %s does not match %s/%s", req.PodUID, req.PodNamespace, req.PodName)
	}
	if pi.NodeID != req.NodeName {
		return s.numaRefitFailureEvent(pod, "pod %s/%s is tracked on node %s, not %s", pod.Namespace, pod.Name, pi.NodeID, req.NodeName)
	}

	name, ok := containerNameAt(pod, req.ContainerIndex)
	if !ok {
		return s.numaRefitFailureEvent(pod, "container index %d is outside the pod's containers", req.ContainerIndex)
	}
	if req.ContainerName != "" && name != req.ContainerName {
		return s.numaRefitFailureEvent(pod, "container index %d is %q, not %q", req.ContainerIndex, name, req.ContainerName)
	}

	// Per-container state comes from the annotations: podManager stores
	// init-collapsed aggregates whose entries do not map to container
	// positions. The refit only applies while this container's allocation is
	// still pending: a blank to-allocate entry means Allocate consumed it.
	toAllocate, err := device.DecodePodDevices(device.InRequestDevices, pod.Annotations)
	if err != nil {
		return s.numaRefitFailureEvent(pod, "cannot decode pending allocation annotation: %v", err)
	}
	allocated, err := device.DecodePodDevices(device.SupportDevices, pod.Annotations)
	if err != nil {
		return s.numaRefitFailureEvent(pod, "cannot decode allocated annotation: %v", err)
	}
	pendingSingle := toAllocate[req.DeviceType]
	allocatedSingle := allocated[req.DeviceType]
	if req.ContainerIndex < 0 || req.ContainerIndex >= len(pendingSingle) || len(pendingSingle[req.ContainerIndex]) == 0 {
		return s.numaRefitFailureEvent(pod, "container %d has no pending %s allocation to refit", req.ContainerIndex, req.DeviceType)
	}
	if req.ContainerIndex >= len(allocatedSingle) || len(allocatedSingle[req.ContainerIndex]) == 0 {
		return s.numaRefitFailureEvent(pod, "container %d has no recorded %s allocation", req.ContainerIndex, req.DeviceType)
	}
	current := allocatedSingle[req.ContainerIndex]

	allowed := make(map[string]struct{}, len(req.AllowedDeviceUUIDs))
	for _, id := range req.AllowedDeviceUUIDs {
		allowed[id] = struct{}{}
	}
	alreadyAllowed := true
	for _, d := range current {
		if _, ok := allowed[d.UUID]; !ok {
			alreadyAllowed = false
			break
		}
	}
	if alreadyAllowed {
		return device.NumaRefitResponse{Succeeded: true, ContainerDevices: device.EncodeContainerDevices(current)}
	}
	// A fit request carries one memory/core amount for all requested
	// devices, so a reservation with differing per-device amounts (possible
	// with percentage requests on mixed GPUs) cannot be re-fit faithfully:
	// replaying current[0] would rewrite the other devices' accounting.
	for _, d := range current[1:] {
		if d.Usedmem != current[0].Usedmem || d.Usedcores != current[0].Usedcores {
			return s.numaRefitFailureEvent(pod, "container %d reserves differing amounts per device; the refit does not support heterogeneous reservations", req.ContainerIndex)
		}
	}

	nodeUsageMap, _, failedNodes, err := s.getNodesUsage(&[]string{req.NodeName}, pod)
	if err != nil {
		return s.numaRefitFailureEvent(pod, "cannot compute node usage: %v", err)
	}
	nodeUsage, ok := (*nodeUsageMap)[req.NodeName]
	if !ok {
		return s.numaRefitFailureEvent(pod, "node %s unavailable: %s", req.NodeName, failedNodes[req.NodeName])
	}
	for _, deviceList := range nodeUsage.Devices.DeviceLists {
		if _, ok := allowed[deviceList.Device.ID]; ok && deviceList.Device.Mode == nvidia.MigMode {
			return s.numaRefitFailureEvent(pod, "allowed device %s is in MIG mode; the NUMA refit does not support MIG", deviceList.Device.ID)
		}
	}
	// The snapshot includes this container's own reservation; release it so
	// capacity checks do not double count the pod against itself.
	releaseContainerUsage(nodeUsage, current)

	weights, err := util.GetDeviceScoringWeightsByPod(pod)
	if err != nil {
		return s.numaRefitFailureEvent(pod, "invalid device scoring weights: %v", err)
	}

	// Seed the fit with the pod's other container allocations so exclusivity
	// and custom filter rules see them, and release the pod's quota usage so
	// the namespace quota check does not count the pod against itself.
	devinput := device.PodDevices{}
	for deviceType, single := range allocated {
		for containerIndex, containerDevices := range single {
			if deviceType == req.DeviceType && containerIndex == req.ContainerIndex {
				continue
			}
			if len(containerDevices) == 0 {
				continue
			}
			devinput[deviceType] = append(devinput[deviceType], containerDevices)
		}
	}
	seeded := len(devinput[req.DeviceType])
	s.quotaManager.RmUsage(pod, pi.Devices)
	failWithQuotaRestore := func(format string, args ...any) device.NumaRefitResponse {
		s.quotaManager.AddUsage(pod, pi.Devices)
		return s.numaRefitFailureEvent(pod, format, args...)
	}

	// Preserve the reservation's accounted amounts; only the device moves.
	requests := device.ContainerDeviceRequests{req.DeviceType: {
		Nums:     int32(len(current)),
		Type:     req.DeviceType,
		Memreq:   current[0].Usedmem,
		Coresreq: current[0].Usedcores,
	}}
	fit, reason := fitInRestrictedDevices(nodeUsage, requests, req.DeviceType, req.AllowedDeviceUUIDs, pod, nodeUsage.NodeInfo, &devinput, weights)
	if !fit {
		return failWithQuotaRestore("no allowed device fits: %s", reason)
	}
	selected := devinput[req.DeviceType]
	if len(selected) != seeded+1 || len(selected[seeded]) != len(current) {
		return failWithQuotaRestore("restricted fit selected %d container sets, want %d with %d devices", len(selected), seeded+1, len(current))
	}
	newDevices := selected[seeded]

	// The restricted fit seeds only non-empty, non-target allocations, so its
	// quota check cannot preserve the container indexes used to distinguish init
	// and app usage. Validate the selected devices in the original annotation
	// layout before changing annotations or cached accounting.
	refitted := append(device.PodSingleDevice{}, allocatedSingle...)
	refitted[req.ContainerIndex] = newDevices
	hypothetical := make(device.PodDevices, len(allocated))
	maps.Copy(hypothetical, allocated)
	hypothetical[req.DeviceType] = refitted
	var quotaMem, quotaCores int64
	for _, ctrDevs := range effectivePodDeviceUsage(pod, hypothetical, pi.InitContainerResourceReleased)[req.DeviceType] {
		for _, d := range ctrDevs {
			quotaMem += int64(d.Usedmem)
			quotaCores += int64(d.Usedcores)
		}
	}
	resourceNames := refitDevice.GetResourceNames()
	if !s.quotaManager.FitQuota(pod.Namespace, quotaMem, resourceNames.MemoryFactor, quotaCores, req.DeviceType) {
		return failWithQuotaRestore("refit would exceed the %s resource quota in namespace %s", req.DeviceType, pod.Namespace)
	}

	// Patch both annotations by replacing only this container's entry inside
	// the current raw values: entries Allocate already consumed stay blank,
	// the separator layout survives byte for byte, and a scheduler restart
	// rebuilds accounting onto the refitted device.
	pendingValue, err := replaceContainerDeviceEntry(pod.Annotations[device.InRequestDevices[req.DeviceType]], req.ContainerIndex, newDevices)
	if err != nil {
		return failWithQuotaRestore("cannot rewrite pending allocation annotation: %v", err)
	}
	allocatedValue, err := replaceContainerDeviceEntry(pod.Annotations[device.SupportDevices[req.DeviceType]], req.ContainerIndex, newDevices)
	if err != nil {
		return failWithQuotaRestore("cannot rewrite allocated annotation: %v", err)
	}
	annotations := map[string]string{
		device.InRequestDevices[req.DeviceType]: pendingValue,
		device.SupportDevices[req.DeviceType]:   allocatedValue,
	}
	if err := patchPodAnnotations(pod, annotations); err != nil {
		return failWithQuotaRestore("cannot patch pod annotations: %v", err)
	}

	// Rebuild the in-memory reservation from the patched annotations exactly
	// like the informer's add path does, so cached accounting and the
	// durable record cannot drift apart.
	patchedAnnotations := make(map[string]string, len(pod.Annotations)+len(annotations))
	maps.Copy(patchedAnnotations, pod.Annotations)
	maps.Copy(patchedAnnotations, annotations)
	if rawDevices, decodeErr := device.DecodePodDevices(device.SupportDevices, patchedAnnotations); decodeErr == nil {
		// Mirror whichever accounting shape the pod already has: once the
		// init-container usage has been released, collapsing again would
		// re-inflate the reservation back to the init peak, the same hazard
		// PodManager.AddPod guards against on a re-add.
		effective := effectivePodDeviceUsage(pod, rawDevices, pi.InitContainerResourceReleased)
		if _, ok := s.podManager.ReplacePodDevices(key, effective); ok {
			s.quotaManager.AddUsage(pod, effective)
		} else {
			// The pod left the cache between lookup and update; the informer
			// rebuilds accounting from the patched annotations on re-add.
			klog.InfoS("pod left the scheduler cache during NUMA refit; annotations remain authoritative", "pod", klog.KObj(pod))
		}
	} else {
		s.quotaManager.AddUsage(pod, pi.Devices)
		klog.ErrorS(decodeErr, "cannot rebuild accounting from patched annotations; keeping previous usage", "pod", klog.KObj(pod))
	}

	message := fmt.Sprintf("NUMA refit moved container %d from %s to %s", req.ContainerIndex, containerDeviceIDs(current), containerDeviceIDs(newDevices))
	klog.InfoS(message, "pod", klog.KObj(pod), "node", req.NodeName)
	s.recordNumaRefitResultEvent(pod, message, nil)
	return device.NumaRefitResponse{Succeeded: true, ContainerDevices: device.EncodeContainerDevices(newDevices)}
}

// effectivePodDeviceUsage mirrors the accounting shape stored by PodManager.
// Non-sidecar init-container usage is part of the phase peak until the
// informer records its release, and excluded from steady-state usage after it.
func effectivePodDeviceUsage(pod *corev1.Pod, raw device.PodDevices, initReleased bool) device.PodDevices {
	if initReleased {
		return device.SteadyStateDeviceUsage(pod, raw)
	}
	return device.CollapseInitContainerUsage(pod, raw)
}

// replaceContainerDeviceEntry swaps one container's entry inside an encoded
// pod-device annotation value, preserving every other byte, so blanked
// entries and the separator layout survive the round trip unchanged.
func replaceContainerDeviceEntry(annotation string, index int, devices device.ContainerDevices) (string, error) {
	parts := strings.Split(annotation, device.OnePodMultiContainerSplitSymbol)
	if index < 0 || index >= len(parts) || parts[index] == "" {
		return "", fmt.Errorf("annotation has no container entry at index %d", index)
	}
	parts[index] = device.EncodeContainerDevices(devices)
	return strings.Join(parts, device.OnePodMultiContainerSplitSymbol), nil
}

// containerNameAt returns the pod's container name at the PodDevices
// position, counting init containers first.
func containerNameAt(pod *corev1.Pod, index int) (string, bool) {
	if index < 0 {
		return "", false
	}
	if index < len(pod.Spec.InitContainers) {
		return pod.Spec.InitContainers[index].Name, true
	}
	index -= len(pod.Spec.InitContainers)
	if index < len(pod.Spec.Containers) {
		return pod.Spec.Containers[index].Name, true
	}
	return "", false
}

// releaseContainerUsage subtracts one container's reserved usage from the
// node usage snapshot in place.
func releaseContainerUsage(node *NodeUsage, reserved device.ContainerDevices) {
	for _, r := range reserved {
		for _, deviceList := range node.Devices.DeviceLists {
			if deviceList.Device.ID != r.UUID {
				continue
			}
			if deviceList.Device.Used > 0 {
				deviceList.Device.Used--
			}
			deviceList.Device.Usedmem = max(deviceList.Device.Usedmem-r.Usedmem, 0)
			deviceList.Device.Usedcores = max(deviceList.Device.Usedcores-r.Usedcores, 0)
			break
		}
	}
}

func containerDeviceIDs(devices device.ContainerDevices) []string {
	ids := make([]string, 0, len(devices))
	for _, d := range devices {
		ids = append(ids, d.UUID)
	}
	return ids
}

// numaRefitFailure logs and wraps a refit refusal for the wire.
func numaRefitFailure(pod *corev1.Pod, format string, args ...any) device.NumaRefitResponse {
	reason := fmt.Sprintf(format, args...)
	if pod != nil {
		klog.InfoS("NUMA refit refused", "pod", klog.KObj(pod), "reason", reason)
	} else {
		klog.InfoS("NUMA refit refused", "reason", reason)
	}
	return device.NumaRefitResponse{Succeeded: false, FailureReason: reason}
}

// numaRefitFailureEvent additionally records a warning event on the pod.
func (s *Scheduler) numaRefitFailureEvent(pod *corev1.Pod, format string, args ...any) device.NumaRefitResponse {
	response := numaRefitFailure(pod, format, args...)
	s.recordNumaRefitResultEvent(pod, "", fmt.Errorf("%s", response.FailureReason))
	return response
}
