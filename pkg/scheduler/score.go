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
package scheduler

import (
	"fmt"
	"math/bits"
	"sort"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/awsneuron"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

func viewStatus(usage NodeUsage) {
	klog.V(5).Info("devices status")
	for _, val := range usage.Devices.DeviceLists {
		klog.V(5).InfoS("device status", "device id", val.Device.ID, "device detail", val)
	}
}

// deviceTypeMatches preserves HAMi's case-insensitive substring matching for
// mapping a registered request type to the corresponding node devices.
func deviceTypeMatches(availableType, requestedType string) bool {
	return strings.Contains(strings.ToLower(availableType), strings.ToLower(requestedType))
}

func getNodeResources(list NodeUsage, t string) []*device.DeviceUsage {
	l := []*device.DeviceUsage{}
	for _, val := range list.Devices.DeviceLists {
		if deviceTypeMatches(val.Device.Type, t) {
			l = append(l, val.Device)
		}
	}
	return l
}

func isMIGRequest(request device.ContainerDeviceRequest, devices []*device.DeviceUsage, pod *corev1.Pod) bool {
	if request.Type != nvidia.NvidiaGPUDevice {
		return false
	}
	if pod != nil {
		if mode, ok := pod.GetAnnotations()[nvidia.AllocateMode]; ok && !strings.Contains(mode, nvidia.MigMode) {
			return false
		}
	}
	for _, dev := range devices {
		if dev.Mode == nvidia.MigMode {
			return true
		}
	}
	return false
}

func nodeDeviceBaseTypes(list policy.DeviceUsageList) map[string]struct{} {
	types := make(map[string]struct{})
	for _, dl := range list.DeviceLists {
		if dl.Device != nil {
			types[dl.Device.Type] = struct{}{}
		}
	}
	return types
}

type quotaAwareDevice interface {
	FitWithQuota(devices []*device.DeviceUsage, request device.ContainerDeviceRequest, pod *corev1.Pod, nodeInfo *device.NodeInfo, allocated, quotaAllocated *device.PodDevices) (bool, map[string]device.ContainerDevices, string)
}

func fitInDevices(node *NodeUsage, requests device.ContainerDeviceRequests, pod *corev1.Pod, nodeInfo *device.NodeInfo, devinput *device.PodDevices, weights util.DeviceScoringWeights) (bool, string) {
	return fitInDevicesWithQuota(node, requests, pod, nodeInfo, devinput, devinput, weights)
}

func fitInDevicesWithQuota(node *NodeUsage, requests device.ContainerDeviceRequests, pod *corev1.Pod, nodeInfo *device.NodeInfo, devinput, quotaInput *device.PodDevices, weights util.DeviceScoringWeights) (bool, string) {
	// Compute scores for all devices based on the request.
	for index := range node.Devices.DeviceLists {
		node.Devices.DeviceLists[index].ComputeScore(requests, weights)
	}

	// Process each device type in the request.
	for _, k := range requests {
		sort.Sort(node.Devices)

		devPlugin, ok := device.GetDevices()[k.Type]
		if !ok {
			errMsg := "Device type not found"
			klog.ErrorS(nil, errMsg, "pod", klog.KObj(pod), "type", k.Type, "node", node.Node.Name)
			return false, errMsg
		}

		typeDevices := getNodeResources(*node, k.Type)
		if int(k.Nums) > len(typeDevices) && !isMIGRequest(k, typeDevices, pod) {
			klog.V(5).InfoS(common.NodeInsufficientDevice, "pod", klog.KObj(pod),
				"request devices nums", k.Nums, "node device nums (type)", len(typeDevices), "type", k.Type)
			// Report it in the same form every device backend uses. common.ParseReason
			// reads only "<count>/<total> <Reason>", so the bare constant this used to
			// return was dropped and the pod got no event naming why the node was rejected.
			return false, common.GenReason(map[string]int{common.NodeInsufficientDevice: len(typeDevices)}, int(k.Nums))
		}

		var fit bool
		var tmpDevs map[string]device.ContainerDevices
		var reason string
		if quotaAware, ok := devPlugin.(quotaAwareDevice); ok {
			fit, tmpDevs, reason = quotaAware.FitWithQuota(typeDevices, k, pod, nodeInfo, devinput, quotaInput)
		} else {
			fit, tmpDevs, reason = devPlugin.Fit(typeDevices, k, pod, nodeInfo, devinput)
		}
		if !fit {
			return false, reason
		}

		// Note: need idx to pass &tmpDevs[k.Type][idx] to AddResourceUsage
		for idx, val := range tmpDevs[k.Type] {
			targetIdx := -1
			for nidx, v := range node.Devices.DeviceLists {
				if v.Device.ID == val.UUID {
					targetIdx = nidx
					break
				}
			}
			if targetIdx == -1 {
				errMsg := fmt.Sprintf("Device with UUID %q not found on node %s after Fit", val.UUID, node.Node.Name)
				klog.ErrorS(nil, errMsg, "pod", klog.KObj(pod))
				return false, errMsg
			}
			d := node.Devices.DeviceLists[targetIdx].Device

			if err := devPlugin.AddResourceUsage(pod, d, &tmpDevs[k.Type][idx]); err != nil {
				klog.Errorf("AddResourceUsage failed for device %s: %v", d.ID, err)
				return false, fmt.Sprintf("AddResourceUsage failed for device %s: %v", d.ID, err)
			}
			klog.V(5).Infof("Allocated device %s: used=%d, cores=%d, mem=%d",
				d.ID, d.Used, d.Usedcores, d.Usedmem)
		}

		(*devinput)[k.Type] = append((*devinput)[k.Type], tmpDevs[k.Type])
	}
	return true, ""
}

// resolveNodeSchedulerPolicy returns the node scheduling policy to apply to
// task, preferring its NodeSchedulerPolicyAnnotationKey annotation over
// config.NodeSchedulerPolicy. An annotation that does not name a known policy
// is ignored with a warning, so the configured policy stands. The accepted
// value is trimmed, because NodeScoreList.Less compares the whole string.
func resolveNodeSchedulerPolicy(task *corev1.Pod) string {
	if task.GetAnnotations() != nil {
		if value, ok := task.GetAnnotations()[util.NodeSchedulerPolicyAnnotationKey]; ok {
			if util.IsValidNodeSchedulerPolicy(value) {
				return strings.TrimSpace(value)
			}
			klog.Warningf("ignoring unrecognized %s=%q on pod %s/%s, using configured policy %q",
				util.NodeSchedulerPolicyAnnotationKey, value, task.Namespace, task.Name, config.NodeSchedulerPolicy)
		}
	}
	return config.NodeSchedulerPolicy
}

// resolvePreferredSchedulingTerm returns the pod's preferred nodeAffinity
// terms, or nil when the pod declares none. Required terms are not returned:
// kube-scheduler already enforces them before calling the extender.
func resolvePreferredSchedulingTerm(task *corev1.Pod) []corev1.PreferredSchedulingTerm {
	if task.Spec.Affinity == nil || task.Spec.Affinity.NodeAffinity == nil {
		return nil
	}
	return task.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution
}

// resolvePreferredNodeAffinity parses the pod's preferred nodeAffinity once per
// scheduling request. It returns nil when --node-affinity-score-weight is 0 or
// the pod has no usable preferred term, so scoreNode skips the bonus at no
// cost. Malformed terms are logged and dropped; the remaining valid terms are
// still returned.
//
// It is a separate function so calcScoreWithOptions assigns the result exactly
// once. A variable that is reassigned after its declaration is captured by
// reference in the per-node goroutine closure and escapes to the heap, which
// costs one allocation per call even when the feature is off.
func resolvePreferredNodeAffinity(task *corev1.Pod) *policy.PreferredNodeAffinity {
	if config.NodeAffinityScoreWeight == 0 {
		return nil
	}
	terms := resolvePreferredSchedulingTerm(task)
	if len(terms) == 0 {
		return nil
	}
	parsed, err := policy.NewPreferredNodeAffinity(terms)
	if err != nil {
		klog.Warningf("ignoring invalid preferred nodeAffinity terms on pod %s/%s: %v", task.Namespace, task.Name, err)
	}
	return parsed
}

type peakUsageSnapshot struct {
	used      int64
	usedcores int64
	usedmem   int64
}

func snapshotPeakUsage(devices policy.DeviceUsageList) map[string]peakUsageSnapshot {
	peakUsage := make(map[string]peakUsageSnapshot)
	for _, dl := range devices.DeviceLists {
		peakUsage[dl.Device.ID] = peakUsageSnapshot{
			used:      dl.Device.Used,
			usedcores: dl.Device.Usedcores,
			usedmem:   dl.Device.Usedmem,
		}
	}
	return peakUsage
}

func updatePeakUsage(peakUsage map[string]peakUsageSnapshot, nodeCopy *NodeUsage) {
	for _, dl := range nodeCopy.Devices.DeviceLists {
		id := dl.Device.ID
		p, ok := peakUsage[id]
		if !ok {
			peakUsage[id] = peakUsageSnapshot{
				used:      dl.Device.Used,
				usedcores: dl.Device.Usedcores,
				usedmem:   dl.Device.Usedmem,
			}
			continue
		}
		if dl.Device.Used > p.used {
			p.used = dl.Device.Used
		}
		if dl.Device.Usedcores > p.usedcores {
			p.usedcores = dl.Device.Usedcores
		}
		if dl.Device.Usedmem > p.usedmem {
			p.usedmem = dl.Device.Usedmem
		}
		peakUsage[id] = p
	}
}

func applyPeakUsage(node *NodeUsage, appNodeCopy *NodeUsage, peakUsage map[string]peakUsageSnapshot) {
	for _, dl := range node.Devices.DeviceLists {
		id := dl.Device.ID
		var appDev *device.DeviceUsage
		for _, adl := range appNodeCopy.Devices.DeviceLists {
			if adl.Device.ID == id {
				appDev = adl.Device
				break
			}
		}
		finalUsed := int64(0)
		finalCores := int64(0)
		finalMem := int64(0)
		if appDev != nil {
			finalUsed = appDev.Used
			finalCores = appDev.Usedcores
			finalMem = appDev.Usedmem
		}
		if p, ok := peakUsage[id]; ok {
			if p.used > finalUsed {
				finalUsed = p.used
			}
			if p.usedcores > finalCores {
				finalCores = p.usedcores
			}
			if p.usedmem > finalMem {
				finalMem = p.usedmem
			}
		}
		dl.Device.Used = finalUsed
		dl.Device.Usedcores = finalCores
		dl.Device.Usedmem = finalMem
	}
}

// fitInDevices keys rows by request type ("NVIDIA") while nodes may register model-specific types ("NVIDIA A100-SXM4-40GB"); union both so padding never drops fitted rows.
func allocationTypeKeys(resourceReqs device.PodDeviceRequests, baseTypes map[string]struct{}) map[string]struct{} {
	allocTypes := make(map[string]struct{}, len(baseTypes))
	for typ := range baseTypes {
		allocTypes[typ] = struct{}{}
	}
	for _, ctrReqs := range resourceReqs {
		for _, req := range ctrReqs {
			allocTypes[req.Type] = struct{}{}
		}
	}
	return allocTypes
}

func sidecarInitIndexes(task *corev1.Pod) map[int]struct{} {
	idx := make(map[int]struct{})
	for i := range task.Spec.InitContainers {
		if util.IsSidecarContainer(&task.Spec.InitContainers[i]) {
			idx[i] = struct{}{}
		}
	}
	return idx
}

// appFitAllocations keeps the full init history for backends whose Fit method
// still uses it for quota checks or Pod-wide placement rules. NVIDIA can use
// only concurrent init allocations for placement because FitWithQuota receives
// the complete history separately.
func appFitAllocations(initAllocs device.PodDevices, sidecarIdx map[int]struct{}) device.PodDevices {
	active := make(device.PodDevices, len(initAllocs))
	for devType, rows := range initAllocs {
		if devType != nvidia.NvidiaGPUDevice {
			active[devType] = rows.DeepCopy()
			continue
		}
		active[devType] = make(device.PodSingleDevice, len(rows))
		for idx := range rows {
			if _, isSidecar := sidecarIdx[idx]; isSidecar {
				active[devType][idx] = rows[idx].DeepCopy()
			}
		}
	}
	return active
}

func completeAllocationHistory(initAllocs, activeAllocs device.PodDevices, numInitContainers int) device.PodDevices {
	complete := initAllocs.DeepCopy()
	for devType, rows := range activeAllocs {
		if _, ok := complete[devType]; !ok {
			complete[devType] = make(device.PodSingleDevice, numInitContainers)
		}
		if len(rows) > numInitContainers {
			complete[devType] = append(complete[devType], rows[numInitContainers:].DeepCopy()...)
		}
	}
	return complete
}

func allocateInitContainers(appNodeCopy *NodeUsage, nodeID string, resourceReqs device.PodDeviceRequests, task *corev1.Pod, nodeInfo *device.NodeInfo, allocTypes map[string]struct{}, sidecarIdx map[int]struct{}, numInitContainers int, peakUsage map[string]peakUsageSnapshot, weights util.DeviceScoringWeights) (device.PodDevices, bool, string) {
	initAllocs := make(device.PodDevices)

	for i, req := range resourceReqs {
		if i >= numInitContainers {
			break
		}
		if len(req) == 0 {
			for typ := range allocTypes {
				initAllocs[typ] = append(initAllocs[typ], device.ContainerDevices{})
			}
			continue
		}
		if _, isSidecar := sidecarIdx[i]; isSidecar {
			fit, reason := fitInDevices(appNodeCopy, req, task, nodeInfo, &initAllocs, weights)
			if !fit {
				klog.V(4).InfoS("Sidecar container does not fit",
					"pod", klog.KObj(task), "node", nodeID, "containerIndex", i, "reason", reason)
				return nil, false, reason
			}
			updatePeakUsage(peakUsage, appNodeCopy)
		} else {
			nodeCopy := appNodeCopy.DeepCopy()
			fit, reason := fitInDevices(nodeCopy, req, task, nodeInfo, &initAllocs, weights)
			if !fit {
				klog.V(4).InfoS("Init container does not fit",
					"pod", klog.KObj(task), "node", nodeID, "containerIndex", i, "reason", reason)
				return nil, false, reason
			}
			updatePeakUsage(peakUsage, nodeCopy)
		}
		for typ := range allocTypes {
			if len(initAllocs[typ]) == i {
				initAllocs[typ] = append(initAllocs[typ], device.ContainerDevices{})
			}
		}
	}
	return initAllocs, true, ""
}

func allocateAppContainers(score *policy.NodeScore, appNodeCopy *NodeUsage, resourceReqs device.PodDeviceRequests, task *corev1.Pod, nodeInfo *device.NodeInfo, initAllocs device.PodDevices, allocTypes map[string]struct{}, numInitContainers int, nodeID string, weights util.DeviceScoringWeights) (string, bool) {
	appIndex := 0
	for ctrid, n := range resourceReqs {
		if ctrid < numInitContainers {
			continue
		}
		sums := 0
		for _, k := range n {
			sums += int(k.Nums)
		}
		if sums == 0 {
			for typ := range allocTypes {
				score.Devices[typ] = append(score.Devices[typ], device.ContainerDevices{})
			}
			appIndex++
			continue
		}
		quotaAllocs := score.Devices
		if numInitContainers > 0 {
			quotaAllocs = completeAllocationHistory(initAllocs, score.Devices, numInitContainers)
		}
		fit, reason := fitInDevicesWithQuota(appNodeCopy, n, task, nodeInfo, &score.Devices, &quotaAllocs, weights)
		if !fit {
			klog.V(4).InfoS(common.NodeUnfitPod, "pod", klog.KObj(task), "node", nodeID, "reason", reason)
			return reason, false
		}
		for typ := range allocTypes {
			if len(score.Devices[typ]) == numInitContainers+appIndex {
				score.Devices[typ] = append(score.Devices[typ], device.ContainerDevices{})
			}
		}
		appIndex++
	}
	return "", true
}

// neuronCorePeak counts actual allocated cores after Fit has resolved the
// node's geometry. Init containers use the same peak/sidecar rule as admission.
func neuronCorePeak(pod *corev1.Pod, allocations device.PodSingleDevice) int64 {
	var sidecars, initPeak, app int64
	for i, containerDevices := range allocations {
		var cores int64
		for _, allocation := range containerDevices {
			cores += int64(bits.OnesCount32(uint32(allocation.Usedcores)))
		}
		if i >= len(pod.Spec.InitContainers) {
			app += cores
		} else if util.IsSidecarContainer(&pod.Spec.InitContainers[i]) {
			sidecars += cores
			initPeak = max(initPeak, sidecars)
		} else {
			initPeak = max(initPeak, sidecars+cores)
		}
	}
	return max(initPeak, sidecars+app)
}

// currentNode returns the informer's copy of the node, whose labels track
// `kubectl label` immediately, and falls back to the cached copy otherwise.
func (s *Scheduler) currentNode(nodeID string, cached *corev1.Node) *corev1.Node {
	if s.nodeLister == nil {
		return cached
	}
	if n, err := s.nodeLister.Get(nodeID); err == nil {
		return n
	}
	return cached
}

type nodeScoreResult struct {
	score  *policy.NodeScore
	reason string
	err    error
}

func (s *Scheduler) scoreNode(nodeID string, node *NodeUsage, resourceReqs device.PodDeviceRequests, task *corev1.Pod, userNodePolicy string, pref *policy.PreferredNodeAffinity, weights util.DeviceScoringWeights) nodeScoreResult {
	viewStatus(*node)

	nodeInfo := node.NodeInfo
	if nodeInfo == nil {
		var err error
		nodeInfo, err = s.GetNode(nodeID)
		if err != nil {
			klog.ErrorS(err, "Failed to get node", "nodeID", nodeID)
			return nodeScoreResult{reason: fmt.Sprintf("failed to fetch node info: %v", err), err: err}
		}
	}

	baseTypes := nodeDeviceBaseTypes(node.Devices)
	allocTypes := allocationTypeKeys(resourceReqs, baseTypes)
	peakUsage := snapshotPeakUsage(node.Devices)
	numInitContainers := len(task.Spec.InitContainers)
	sidecarIdx := sidecarInitIndexes(task)

	appNodeCopy := node.DeepCopy()

	score := policy.NodeScore{
		NodeID:  nodeID,
		Node:    node.Node,
		Devices: make(device.PodDevices),
		Score:   0,
	}
	score.ComputeDefaultScore(appNodeCopy.Devices)
	snapshot := score.SnapshotDevice(appNodeCopy.Devices)

	var initAllocs device.PodDevices
	if numInitContainers > 0 {
		allocs, fit, reason := allocateInitContainers(appNodeCopy, nodeID, resourceReqs, task, nodeInfo, allocTypes, sidecarIdx, numInitContainers, peakUsage, weights)
		if !fit {
			return nodeScoreResult{reason: reason}
		}
		initAllocs = allocs
		score.Devices = appFitAllocations(initAllocs, sidecarIdx)
	}

	if reason, fit := allocateAppContainers(&score, appNodeCopy, resourceReqs, task, nodeInfo, initAllocs, allocTypes, numInitContainers, nodeID, weights); !fit {
		return nodeScoreResult{reason: reason}
	}
	if numInitContainers > 0 {
		score.Devices = completeAllocationHistory(initAllocs, score.Devices, numInitContainers)
	}
	if cores := neuronCorePeak(task, score.Devices[awsneuron.AWSNeuronDevice]); cores > 0 &&
		!s.quotaManager.FitQuota(task.Namespace, 0, cores, awsneuron.AWSNeuronDevice) {
		return nodeScoreResult{reason: "AWS NeuronCore quota exceeded"}
	}

	applyPeakUsage(node, appNodeCopy, peakUsage)

	score.OverrideScore(snapshot, userNodePolicy)
	if pref != nil {
		score.Node = s.currentNode(nodeID, node.Node)
	}
	score.AddNodeAffinityScore(pref, userNodePolicy, config.NodeAffinityScoreWeight)
	klog.V(4).InfoS(common.NodeFitPod, "pod", klog.KObj(task), "node", nodeID, "score", score.Score)

	return nodeScoreResult{score: &score}
}

func (s *Scheduler) recordFilteringFailures(task *corev1.Pod, failureReason map[string][]string) {
	for reasonType, failureNodes := range failureReason {
		sort.Strings(failureNodes)
		reason := fmt.Errorf("%d nodes %s(%s)", len(failureNodes), reasonType, strings.Join(failureNodes, ","))
		s.recordScheduleFilterResultEvent(task, EventReasonFilteringFailed, "", reason)
	}
}

func (s *Scheduler) calcScore(nodes *map[string]*NodeUsage, resourceReqs device.PodDeviceRequests, task *corev1.Pod, failedNodes map[string]string) (*policy.NodeScoreList, error) {
	return s.calcScoreWithOptions(nodes, resourceReqs, task, failedNodes, true, false)
}

func (s *Scheduler) calcScoreWithOptions(nodes *map[string]*NodeUsage, resourceReqs device.PodDeviceRequests, task *corev1.Pod, failedNodes map[string]string, recordEvents bool, detailedFailureReason bool) (*policy.NodeScoreList, error) {
	deviceScoringWeights, err := util.GetDeviceScoringWeightsByPod(task)
	if err != nil {
		return nil, err
	}

	pref := resolvePreferredNodeAffinity(task)
	userNodePolicy := resolveNodeSchedulerPolicy(task)
	res := policy.NodeScoreList{
		Policy:   userNodePolicy,
		NodeList: make([]*policy.NodeScore, 0),
	}

	wg := sync.WaitGroup{}
	fitNodesMutex := sync.Mutex{}
	failedNodesMutex := sync.Mutex{}
	failureReason := make(map[string][]string)
	errCh := make(chan error, len(*nodes))

	for nodeID, node := range *nodes {
		wg.Add(1)
		go func(nodeID string, node *NodeUsage) {
			defer wg.Done()

			result := s.scoreNode(nodeID, node, resourceReqs, task, userNodePolicy, pref, deviceScoringWeights)

			switch {
			case result.err != nil:
				failedNodesMutex.Lock()
				failedNodes[nodeID] = result.reason
				failedNodesMutex.Unlock()
				errCh <- result.err

			case result.score == nil:
				failedNodesMutex.Lock()
				failedNodes[nodeID] = result.reason
				for reasonType := range common.ParseReason(result.reason) {
					failureReason[reasonType] = append(failureReason[reasonType], nodeID)
				}
				failedNodesMutex.Unlock()

			default:
				fitNodesMutex.Lock()
				res.NodeList = append(res.NodeList, result.score)
				fitNodesMutex.Unlock()
			}
		}(nodeID, node)
	}
	wg.Wait()
	close(errCh)

	if recordEvents && len(res.NodeList) == 0 {
		s.recordFilteringFailures(task, failureReason)
	}

	var errorsSlice []error
	for e := range errCh {
		errorsSlice = append(errorsSlice, e)
	}
	return &res, utilerrors.NewAggregate(errorsSlice)
}
