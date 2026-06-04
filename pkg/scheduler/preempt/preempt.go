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

package preempt

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"
	"sync/atomic"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	corelisters "k8s.io/client-go/listers/core/v1"
	policylisters "k8s.io/client-go/listers/policy/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

const Name = "PreemptPredicate"

// criticalPriorityThreshold is the numeric value of the system-cluster-critical
// PriorityClass. Any pod whose Spec.Priority is at or above this value is
// treated as a critical system pod and is protected from preemption.
const criticalPriorityThreshold = int32(2000000000)

// Sentinel error for distinguishing "pod has no native GPU request" from "insufficient native GPUs".
var errNoNativeGPURequested = errors.New("no native whole GPU resources requested")

// nativeWholeGPUResources lists resource names that represent an entire
// physical GPU and therefore consume the whole device.
// Expanded to cover all major HAMi-supported hardware vendors.
var nativeWholeGPUResources = map[string]bool{
	// NVIDIA
	"nvidia.com/gpu": true,

	// AMD
	"amd.com/gpu": true,

	// Intel
	"intel.com/gpu": true,

	// Huawei Ascend (includes all variants)
	"huawei.com/Ascend310":  true,
	"huawei.com/Ascend310B": true,
	"huawei.com/Ascend310P": true,
	"huawei.com/Ascend910":  true,
	"huawei.com/Ascend910B": true,
	"huawei.com/Ascend910C": true,

	// Cambricon MLU
	"cambricon.com/mlu": true,

	// Hygon DCU
	"hygon.com/dcu": true,

	// Iluvatar
	"iluvatar.ai/gpu": true,
}

// VgpuPreempt refines victim selection proposed by kube-scheduler.
type VgpuPreempt struct {
	nodeLister  corelisters.NodeLister
	podLister   corelisters.PodLister
	pdbLister   policylisters.PodDisruptionBudgetLister
	recorder    record.EventRecorder
	hasSyncFunc func(ctx context.Context) bool
}

// New creates a new VgpuPreempt plugin.
func New(
	factory informers.SharedInformerFactory,
	recorder record.EventRecorder,
) (*VgpuPreempt, error) {
	podInformer := factory.Core().V1().Pods().Informer()
	nodeInformer := factory.Core().V1().Nodes().Informer()
	pdbInformer := factory.Policy().V1().PodDisruptionBudgets().Informer()

	nodeLister := factory.Core().V1().Nodes().Lister()
	podLister := factory.Core().V1().Pods().Lister()
	pdbLister := factory.Policy().V1().PodDisruptionBudgets().Lister()

	hasSyncFunc := func(ctx context.Context) bool {
		return cache.WaitForCacheSync(
			ctx.Done(),
			podInformer.HasSynced,
			nodeInformer.HasSynced,
			pdbInformer.HasSynced,
		)
	}

	return &VgpuPreempt{
		nodeLister:  nodeLister,
		podLister:   podLister,
		pdbLister:   pdbLister,
		recorder:    recorder,
		hasSyncFunc: hasSyncFunc,
	}, nil
}

func (p *VgpuPreempt) Name() string { return Name }

func (p *VgpuPreempt) IsReady(ctx context.Context) bool { return p.hasSyncFunc(ctx) }

// getDevicesSnapshot returns a thread-safe snapshot of registered device handlers.
func (p *VgpuPreempt) getDevicesSnapshot() map[string]device.Devices {
	// GetDevices() returns the live global map. To avoid data races,
	// create a snapshot at the start of preemption evaluation.
	source := device.GetDevices()
	snapshot := make(map[string]device.Devices, len(source))
	maps.Copy(snapshot, source)
	return snapshot
}

var (
	preemptionAttempts   atomic.Int64
	preemptionSucceeded  atomic.Int64
	preemptionDropped    atomic.Int64
	victimsFittedWOEvict atomic.Int64
)

// IncPreemptionCounter increments the appropriate preemption counter in a thread-safe manner.
func IncPreemptionCounter(counterType string) {
	switch counterType {
	case "attempts":
		preemptionAttempts.Add(1)
	case "succeeded":
		preemptionSucceeded.Add(1)
	case "dropped":
		preemptionDropped.Add(1)
	case "fitted_no_evict":
		victimsFittedWOEvict.Add(1)
	}
}

// GetPreemptionMetrics returns current preemption metrics for monitoring.
func GetPreemptionMetrics() map[string]int64 {
	return map[string]int64{
		"preemption_attempts":     preemptionAttempts.Load(),
		"preemption_succeeded":    preemptionSucceeded.Load(),
		"preemption_dropped":      preemptionDropped.Load(),
		"victims_fitted_no_evict": victimsFittedWOEvict.Load(),
	}
}

func (p *VgpuPreempt) Preempt(
	ctx context.Context,
	args extenderv1.ExtenderPreemptionArgs,
) *extenderv1.ExtenderPreemptionResult {
	result := &extenderv1.ExtenderPreemptionResult{
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{},
	}

	IncPreemptionCounter("attempts")

	pod := args.Pod
	if pod == nil {
		klog.V(4).InfoS("Preempt called with nil pod, passing input through")
		return passthrough(args)
	}

	if !isVGPUResourcePod(pod) {
		klog.V(5).InfoS("Preempt: pod is not a GPU/device pod, passing input through",
			"pod", klog.KObj(pod))
		return passthrough(args)
	}

	victimsMap, err := p.resolveVictimsMap(args)
	if err != nil {
		klog.ErrorS(err, "Preempt: failed to resolve victims, cannot preempt")
		return &extenderv1.ExtenderPreemptionResult{}
	}
	if len(victimsMap) == 0 {
		return result
	}

	// Single full cluster scan to avoid O(N²) lookups.
	allPods, err := p.podLister.List(labels.Everything())
	if err != nil {
		klog.ErrorS(err, "PodLister list pods failed in preempt, cannot evaluate preemption")
		return &extenderv1.ExtenderPreemptionResult{}
	}

	podsByNode := make(map[string][]*corev1.Pod)
	for _, pObj := range allPods {
		if pObj.Spec.NodeName != "" && isVGPUResourcePod(pObj) {
			podsByNode[pObj.Spec.NodeName] = append(podsByNode[pObj.Spec.NodeName], pObj)
		}
	}

	// Capture device snapshot once per preemption evaluation for thread safety.
	devMap := p.getDevicesSnapshot()

	for nodeName, victims := range victimsMap {
		if victims == nil {
			continue
		}

		nodeVGPUPods := podsByNode[nodeName]

		refined, pdbViolations, ok := p.refineForNode(pod, nodeName, victims, nodeVGPUPods, devMap)
		if !ok {
			klog.V(3).InfoS("Preempt: node cannot fit pod even after preemption, dropping",
				"pod", klog.KObj(pod), "node", nodeName)
			IncPreemptionCounter("dropped")
			continue
		}

		// Always include the node if ok is true, even if refined is empty.
		meta := &extenderv1.MetaVictims{
			Pods:             make([]*extenderv1.MetaPod, 0, len(refined)),
			NumPDBViolations: pdbViolations,
		}
		for _, vp := range refined {
			meta.Pods = append(meta.Pods, &extenderv1.MetaPod{UID: string(vp.UID)})
		}
		result.NodeNameToMetaVictims[nodeName] = meta
		IncPreemptionCounter("succeeded")

		// Emit event on the preemptor to aid observability.
		if len(refined) > 0 {
			p.recorder.Eventf(pod, corev1.EventTypeNormal, "Preempting",
				"Preempting %d pod(s) on node %s to accommodate %s/%s",
				len(refined), nodeName, pod.Namespace, pod.Name)
		} else {
			IncPreemptionCounter("fitted_no_evict")
			p.recorder.Eventf(pod, corev1.EventTypeNormal, "SchedulableFitNoEviction",
				"Pod fits on node %s without evicting any pods",
				nodeName)
		}
	}
	return result
}

// resolveVictimsMap converts MetaVictims format to Victims format by looking up pod UIDs.
func (p *VgpuPreempt) resolveVictimsMap(
	args extenderv1.ExtenderPreemptionArgs,
) (map[string]*extenderv1.Victims, error) {
	if len(args.NodeNameToVictims) > 0 {
		return args.NodeNameToVictims, nil
	}
	if len(args.NodeNameToMetaVictims) == 0 {
		return nil, nil
	}

	allPods, err := p.podLister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}
	byUID := make(map[types.UID]*corev1.Pod, len(allPods))
	for _, pod := range allPods {
		byUID[pod.UID] = pod
	}

	resolved := make(map[string]*extenderv1.Victims, len(args.NodeNameToMetaVictims))
	for nodeName, meta := range args.NodeNameToMetaVictims {
		if meta == nil {
			continue
		}
		pods := make([]*corev1.Pod, 0, len(meta.Pods))
		for _, mp := range meta.Pods {
			if mp == nil {
				continue
			}
			pod, ok := byUID[types.UID(mp.UID)]
			if !ok {
				klog.V(3).InfoS("Preempt: victim UID not found in cache, skipping",
					"uid", mp.UID, "node", nodeName)
				continue
			}
			pods = append(pods, pod)
		}
		// Always include node, even with empty pods, so refinement can check fit without victims.
		resolved[nodeName] = &extenderv1.Victims{
			Pods:             pods,
			NumPDBViolations: meta.NumPDBViolations,
		}
	}
	return resolved, nil
}

// refineForNode evaluates and refines victim selection for a single node.
func (p *VgpuPreempt) refineForNode(
	preemptor *corev1.Pod,
	nodeName string,
	victims *extenderv1.Victims,
	nodeVGPUPods []*corev1.Pod,
	devMap map[string]device.Devices,
) ([]*corev1.Pod, int64, bool) {
	node, err := p.nodeLister.Get(nodeName)
	if err != nil {
		klog.V(3).ErrorS(err, "Preempt: get node failed", "node", nodeName)
		return nil, 0, false
	}

	// Track whether the scheduler originally proposed any victims for this node.
	hadProposedVictims := len(victims.Pods) > 0

	keep := make([]*corev1.Pod, 0, len(victims.Pods))
	excluded := make(map[types.UID]struct{}, len(victims.Pods))
	preemptorPriority := getPodPriority(preemptor)

	for _, v := range victims.Pods {
		if v == nil {
			continue
		}
		if isProtectedFromPreemption(v) {
			klog.V(4).InfoS("Preempt: refusing to evict protected pod",
				"pod", klog.KObj(v), "node", nodeName)
			continue
		}
		// Reject victims that have equal or higher priority than the preemptor.
		if getPodPriority(v) >= preemptorPriority {
			klog.V(4).InfoS("Preempt: refusing to evict pod with equal or higher priority",
				"pod", klog.KObj(v), "node", nodeName)
			continue
		}
		keep = append(keep, v)
		excluded[v.UID] = struct{}{}
	}
	keptFromInput := len(keep)

	if p.podFitsAfterPreemption(preemptor, *node, nodeVGPUPods, excluded, devMap) {
		return keep, pdbViolationsUpperBound(victims.NumPDBViolations, keptFromInput, 0), true
	}

	// Only search for additional victims when the scheduler originally proposed at least one victim.
	if !hadProposedVictims {
		klog.V(4).InfoS("Preempt: pod does not fit and scheduler proposed no victims; dropping node",
			"pod", klog.KObj(preemptor), "node", nodeName)
		return nil, 0, false
	}

	additional := p.findAdditionalVictims(preemptor, node, nodeVGPUPods, excluded)
	for _, cand := range additional {
		excluded[cand.UID] = struct{}{}
		keep = append(keep, cand)
		if p.podFitsAfterPreemption(preemptor, *node, nodeVGPUPods, excluded, devMap) {
			added := len(keep) - keptFromInput
			return keep, pdbViolationsUpperBound(victims.NumPDBViolations, keptFromInput, added), true
		}
	}
	return nil, 0, false
}

// podFitsAfterPreemption simulates resource allocation after evicting the excluded pods.
func (p *VgpuPreempt) podFitsAfterPreemption(
	preemptor *corev1.Pod,
	node corev1.Node,
	nodeVGPUPods []*corev1.Pod,
	excluded map[types.UID]struct{},
	devMap map[string]device.Devices,
) bool {
	ni := &device.NodeInfo{
		ID:      node.Name,
		Node:    &node,
		Devices: make(map[string][]device.DeviceInfo, len(devMap)),
	}

	// Determine which device types the preemptor actually requires.
	preemptorReqs := device.Resourcereqs(preemptor)
	requestedDevTypes := make(map[string]bool)
	for _, cReqs := range preemptorReqs {
		for devType, req := range cReqs {
			if req.Nums > 0 || req.Memreq > 0 || req.Coresreq > 0 {
				requestedDevTypes[devType] = true
			}
		}
	}

	// Also add native whole GPU requests to requestedDevTypes
	for _, c := range append(preemptor.Spec.Containers, preemptor.Spec.InitContainers...) {
		for rName := range c.Resources.Requests {
			if nativeWholeGPUResources[string(rName)] {
				devType := extractDeviceTypeFromResourceName(string(rName))
				if devType != "" {
					requestedDevTypes[devType] = true
				}
			}
		}
	}

	// Populate device state from node.
	for devType, dev := range devMap {
		infos, err := dev.GetNodeDevices(node)
		if err != nil {
			if requestedDevTypes[devType] {
				klog.V(3).ErrorS(err, "Failed to get node devices for required type",
					"node", node.Name, "type", devType)
				return false
			}
			klog.V(5).InfoS("Ignoring device plugin error for unrequested type",
				"node", node.Name, "type", devType)
			continue
		}
		ni.Devices[devType] = make([]device.DeviceInfo, len(infos))
		for i, info := range infos {
			if info != nil {
				ni.Devices[devType][i] = *info
			}
		}
	}

	state := make(map[string][]*device.DeviceUsage)
	for devType, devInfos := range ni.Devices {
		usages := make([]*device.DeviceUsage, len(devInfos))
		for i, info := range devInfos {
			usages[i] = &device.DeviceUsage{
				ID:        info.ID,
				Index:     info.Index,
				Count:     info.Count,
				Totalmem:  info.Devmem,
				Totalcore: info.Devcore,
				Type:      info.Type,
				Mode:      info.Mode,
				Health:    info.Health,
			}
		}
		state[devType] = usages
	}

	// Group co-located pods to process them in a deterministic, optimal order.
	var annotatedPods []*corev1.Pod
	var nativePods []*corev1.Pod
	var vgpuPods []*corev1.Pod

	hasNativeGPU := func(pod *corev1.Pod) bool {
		for _, c := range append(pod.Spec.Containers, pod.Spec.InitContainers...) {
			for rName := range c.Resources.Requests {
				if nativeWholeGPUResources[string(rName)] {
					return true
				}
			}
		}
		return false
	}

	for _, vPod := range nodeVGPUPods {
		if _, skip := excluded[vPod.UID]; skip {
			continue
		}
		podDevs, err := device.DecodePodDevices(device.SupportDevices, vPod.Annotations)
		if err == nil && len(podDevs) > 0 {
			annotatedPods = append(annotatedPods, vPod)
		} else if hasNativeGPU(vPod) {
			nativePods = append(nativePods, vPod)
		} else {
			vgpuPods = append(vgpuPods, vPod)
		}
	}

	// 1. Process annotated pods first (fixed UUIDs)
	for _, vPod := range annotatedPods {
		podDevs, _ := device.DecodePodDevices(device.SupportDevices, vPod.Annotations)
		for devType, pds := range podDevs {
			usages := state[devType]
			if usages == nil {
				continue
			}
			for _, ctrDevs := range pds {
				for _, cd := range ctrDevs {
					var du *device.DeviceUsage
					for _, u := range usages {
						if u.ID == cd.UUID {
							du = u
							break
						}
					}
					if du == nil {
						continue
					}
					du.Used++
					du.Usedmem += cd.Usedmem
					du.Usedcores += cd.Usedcores
				}
			}
		}
	}

	// 2. Process native GPU pods (require whole unused devices)
	for _, vPod := range nativePods {
		if err := p.accountNativeWholeGPUs(state, vPod, devMap); err != nil {
			klog.V(3).ErrorS(err, "Failed to account native pod resource usage", "pod", klog.KObj(vPod))
			return false
		}
	}

	// 3. Process unannotated vGPU pods (can share remaining capacity)
	for _, vPod := range vgpuPods {
		if err := p.accountVGPURequests(state, vPod, devMap); err != nil {
			klog.V(3).ErrorS(err, "Failed to account pod resource usage", "pod", klog.KObj(vPod))
			return false
		}
	}

	// Evaluate the preemptor's native whole-GPU requests against the remaining state.
	errNative := p.accountNativeWholeGPUs(state, preemptor, devMap)
	if errNative != nil && !errors.Is(errNative, errNoNativeGPURequested) {
		return false
	}

	if len(preemptorReqs) == 0 {
		return true
	}

	// Attempt to allocate for each container request.
	for _, cReqs := range preemptorReqs {
		if len(cReqs) == 0 {
			continue
		}
		for devType, cReq := range cReqs {
			if cReq.Nums == 0 {
				continue
			}
			if state[devType] == nil {
				return false
			}
			curCopy := deepCopyUsageSlice(state[devType])

			var alloc device.PodDevices
			fitOK, _, _ := devMap[devType].Fit(curCopy, cReq, preemptor, ni, &alloc)
			if !fitOK {
				return false
			}
			state[devType] = curCopy
		}
	}
	return true
}

// accountNativeWholeGPUs reserves full GPU devices for pods that request native whole-GPU resources.
func (p *VgpuPreempt) accountNativeWholeGPUs(
	state map[string][]*device.DeviceUsage,
	pod *corev1.Pod,
	devMap map[string]device.Devices,
) error {
	wholeRequested := map[string]int64{}

	for _, c := range append(pod.Spec.Containers, pod.Spec.InitContainers...) {
		for rName, rQuant := range c.Resources.Requests {
			if !nativeWholeGPUResources[string(rName)] {
				continue
			}
			devType := extractDeviceTypeFromResourceName(string(rName))
			if devType == "" {
				continue
			}
			wholeRequested[devType] += rQuant.Value()
		}
	}

	if len(wholeRequested) == 0 {
		return fmt.Errorf("%w: %s/%s", errNoNativeGPURequested, pod.Namespace, pod.Name)
	}

	for devType, count := range wholeRequested {
		usages := state[devType]
		if usages == nil {
			return fmt.Errorf("no device state for type %q", devType)
		}
		used := int64(0)
		for _, du := range usages {
			if du.Used > 0 {
				continue
			}
			du.Used = du.Count
			du.Usedmem = du.Totalmem
			du.Usedcores = du.Totalcore
			used++
			if used >= count {
				break
			}
		}
		if used < count {
			return fmt.Errorf("insufficient free devices for %d whole GPU(s) of type %q", count, devType)
		}
	}
	return nil
}

// accountVGPURequests accounts for pods with HAMi resource requests but without device annotations.
func (p *VgpuPreempt) accountVGPURequests(
	state map[string][]*device.DeviceUsage,
	pod *corev1.Pod,
	devMap map[string]device.Devices,
) error {
	reqs := device.Resourcereqs(pod)
	for _, cReqs := range reqs {
		for devType, req := range cReqs {
			usages := state[devType]
			if usages == nil {
				continue
			}
			satisfied := false
			for _, du := range usages {
				if du.Count-du.Used >= 1 &&
					du.Totalmem-du.Usedmem >= req.Memreq &&
					du.Totalcore-du.Usedcores >= req.Coresreq {
					du.Used++
					du.Usedmem += req.Memreq
					du.Usedcores += req.Coresreq
					satisfied = true
					break
				}
			}
			if !satisfied {
				klog.V(4).InfoS("no device could satisfy vGPU request; usage may be understated",
					"pod", klog.KObj(pod), "devType", devType, "request", fmt.Sprintf("%+v", req))
			}
		}
	}
	return nil
}

// deepCopyUsageSlice creates a deep copy of the slice with cloned DeviceUsage elements.
func deepCopyUsageSlice(src []*device.DeviceUsage) []*device.DeviceUsage {
	if src == nil {
		return nil
	}
	dst := make([]*device.DeviceUsage, len(src))
	for i, u := range src {
		if u == nil {
			continue
		}
		// Manual deep copy of the struct
		copy := *u
		dst[i] = &copy
	}
	return dst
}

// findAdditionalVictims searches for lower-priority pods that are not PDB-protected.
func (p *VgpuPreempt) findAdditionalVictims(
	preemptor *corev1.Pod,
	node *corev1.Node,
	nodeVGPUPods []*corev1.Pod,
	excluded map[types.UID]struct{},
) []*corev1.Pod {
	preemptorPriority := getPodPriority(preemptor)
	candidates := make([]*corev1.Pod, 0)

	for _, candidate := range nodeVGPUPods {
		if candidate.UID == preemptor.UID {
			continue
		}
		if _, dup := excluded[candidate.UID]; dup {
			continue
		}
		if getPodPriority(candidate) >= preemptorPriority {
			continue
		}
		if isProtectedFromPreemption(candidate) {
			continue
		}

		if p.violatesPDB(candidate) {
			klog.V(4).InfoS("Preempt: candidate would violate PDB, skipping",
				"pod", klog.KObj(candidate), "node", node.Name)
			continue
		}

		candidates = append(candidates, candidate)
	}

	sortVictimsByPreference(candidates)
	return candidates
}

// violatesPDB checks if evicting the pod would violate any applicable PodDisruptionBudget.
func (p *VgpuPreempt) violatesPDB(pod *corev1.Pod) bool {
	if pod == nil || pod.Namespace == "" {
		return false
	}

	pdbs, err := p.pdbLister.PodDisruptionBudgets(pod.Namespace).List(labels.Everything())
	if err != nil {
		klog.V(4).ErrorS(err, "Failed to list PDBs; assuming no PDB violation", "pod", klog.KObj(pod))
		return false
	}

	for _, pdb := range pdbs {
		if pdb == nil {
			continue
		}

		selector, err := metav1.LabelSelectorAsSelector(pdb.Spec.Selector)
		if err != nil {
			klog.V(4).ErrorS(err, "Invalid PDB selector", "pdb", klog.KObj(pdb))
			continue
		}

		if !selector.Matches(labels.Set(pod.Labels)) {
			continue
		}

		if pdb.Status.DisruptionsAllowed == 0 {
			klog.V(4).InfoS("Preempt: pod matches PDB with zero disruptions allowed",
				"pod", klog.KObj(pod), "pdb", klog.KObj(pdb))
			return true
		}
	}

	return false
}

// extractDeviceTypeFromResourceName maps a resource name to its device type.
func extractDeviceTypeFromResourceName(rName string) string {
	lower := strings.ToLower(rName)

	if strings.Contains(lower, "nvidia") {
		return "nvidia"
	}
	if strings.Contains(lower, "amd") {
		return "amd"
	}
	if strings.Contains(lower, "intel") {
		return "intel"
	}
	if strings.Contains(lower, "huawei") || strings.Contains(lower, "ascend") {
		return "huawei"
	}
	if strings.Contains(lower, "cambricon") {
		return "cambricon"
	}
	if strings.Contains(lower, "hygon") || strings.Contains(lower, "dcu") {
		return "hygon"
	}
	if strings.Contains(lower, "iluvatar") {
		return "iluvatar"
	}
	return ""
}

// isVGPUResourcePod checks if a pod requests GPU or device resources.
func isVGPUResourcePod(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	if _, ok := pod.Annotations["hami.io/container-devices"]; ok {
		return true
	}

	checkContainers := func(containers []corev1.Container) bool {
		for _, c := range containers {
			for rName := range c.Resources.Requests {
				sName := string(rName)
				if strings.HasPrefix(sName, "hami.io/") ||
					strings.HasPrefix(sName, "nvidia.com/") ||
					strings.HasPrefix(sName, "amd.com/") ||
					strings.HasPrefix(sName, "intel.com/") ||
					strings.HasPrefix(sName, "huawei.com/") ||
					strings.HasPrefix(sName, "cambricon.com/") ||
					strings.HasPrefix(sName, "hygon.com/") ||
					strings.HasPrefix(sName, "iluvatar.ai/") {
					return true
				}
			}
		}
		return false
	}
	return checkContainers(pod.Spec.Containers) || checkContainers(pod.Spec.InitContainers)
}

// isProtectedFromPreemption returns true if the pod cannot be preempted.
func isProtectedFromPreemption(pod *corev1.Pod) bool {
	if pod == nil {
		return true
	}
	if pod.DeletionTimestamp != nil {
		return true
	}
	if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		return true
	}
	if isCriticalPod(pod) {
		return true
	}
	if owner := metav1.GetControllerOf(pod); owner != nil && owner.Kind == "DaemonSet" {
		return true
	}
	return false
}

// isCriticalPod checks if a pod has critical priority or is in kube-system.
func isCriticalPod(pod *corev1.Pod) bool {
	if pod.Spec.PriorityClassName == "system-node-critical" ||
		pod.Spec.PriorityClassName == "system-cluster-critical" {
		return true
	}
	// Guard by numeric value so pods with high-priority values (>= system-cluster-critical)
	// are always protected, regardless of whether the PriorityClass name is set.
	if pod.Spec.Priority != nil && *pod.Spec.Priority >= criticalPriorityThreshold {
		return true
	}
	return pod.Namespace == "kube-system" || pod.Namespace == "kube-public"
}

// getPodPriority returns the pod's priority value.
func getPodPriority(pod *corev1.Pod) int32 {
	if pod.Spec.Priority != nil {
		return *pod.Spec.Priority
	}
	return 0
}

// sortVictimsByPreference sorts victims by priority (ascending), then by creation time
// (newer first), then by total resource request (descending).
func sortVictimsByPreference(pods []*corev1.Pod) {
	sort.SliceStable(pods, func(i, j int) bool {
		a, b := pods[i], pods[j]
		ap, bp := getPodPriority(a), getPodPriority(b)
		if ap != bp {
			return ap < bp
		}
		if !a.CreationTimestamp.Equal(&b.CreationTimestamp) {
			return a.CreationTimestamp.After(b.CreationTimestamp.Time)
		}
		aMem, aCores, aCount := getPodVGPURequest(a)
		bMem, bCores, bCount := getPodVGPURequest(b)
		return aMem+aCores+aCount > bMem+bCores+bCount
	})
}

// getPodVGPURequest extracts total vGPU-related resource requests from a pod.
func getPodVGPURequest(pod *corev1.Pod) (mem, cores, count int64) {
	for _, c := range append(pod.Spec.Containers, pod.Spec.InitContainers...) {
		for rName, rQuant := range c.Resources.Requests {
			sName := string(rName)
			if strings.Contains(sName, "mem") || strings.Contains(sName, "memory") {
				mem += rQuant.Value()
			} else if strings.Contains(sName, "cores") {
				cores += rQuant.Value()
			} else if strings.Contains(sName, "vgpu") || strings.Contains(sName, "gpu") {
				count += rQuant.Value()
			}
		}
	}
	if mem == 0 && cores == 0 && count == 0 {
		if pd, err := device.DecodePodDevices(device.SupportDevices, pod.Annotations); err == nil {
			for _, single := range pd {
				for _, ctrDevs := range single {
					for _, cd := range ctrDevs {
						mem += int64(cd.Usedmem)
						cores += int64(cd.Usedcores)
						count++
					}
				}
			}
		}
	}
	return
}

// pdbViolationsUpperBound computes a conservative upper bound on PDB violations.
func pdbViolationsUpperBound(originalCount int64, keptLen, addedLen int) int64 {
	return min(originalCount, int64(keptLen))
}

// passthrough returns the original victim list in MetaVictims format.
func passthrough(args extenderv1.ExtenderPreemptionArgs) *extenderv1.ExtenderPreemptionResult {
	out := &extenderv1.ExtenderPreemptionResult{
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{},
	}
	if len(args.NodeNameToMetaVictims) > 0 {
		maps.Copy(out.NodeNameToMetaVictims, args.NodeNameToMetaVictims)
		return out
	}
	for nodeName, v := range args.NodeNameToVictims {
		if v == nil {
			continue
		}
		meta := &extenderv1.MetaVictims{
			Pods:             make([]*extenderv1.MetaPod, 0, len(v.Pods)),
			NumPDBViolations: v.NumPDBViolations,
		}
		for _, pod := range v.Pods {
			if pod == nil {
				continue
			}
			meta.Pods = append(meta.Pods, &extenderv1.MetaPod{UID: string(pod.UID)})
		}
		out.NodeNameToMetaVictims[nodeName] = meta
	}
	return out
}
