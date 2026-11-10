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
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

// superpodContext holds the superpod constraints resolved for one scheduling
// attempt. A nil context means the superpod policy is inactive for this pod.
type superpodContext struct {
	// TaskID is the hami.io/taskid label value identifying the task.
	TaskID string
	// TaskAnchored reports whether at least one pod of the task has landed on a
	// superpod node. It is the single source of truth for the filter and
	// rack-boost stages.
	TaskAnchored bool
	// Superpods holds every superpod where the task's pods already run.
	// Empty means no pod has landed on a superpod node yet (not anchored).
	Superpods map[string]struct{}
	// PreferredRacks holds every rack where the task's pods already run,
	// used to boost same-rack nodes in anchored mode.
	PreferredRacks map[string]struct{}
	// RackSlotCounts maps each superpod to a per-rack available device slot
	// count, populated during the filter stage for first-pod rack preference.
	RackSlotCounts map[string]map[string]int64

	// podRequest is the current pod's single-pod device request, resolved
	// during the filter stage.
	podRequest int64
	// totalRequest is the whole task's device request, resolved during the
	// filter stage for unanchored capacity validation.
	totalRequest int64
	// superpodCapacities maps each candidate superpod to its total usable
	// capacity, populated during the filter stage for unanchored mode.
	superpodCapacities map[string]int64
}

// prepareSuperpodContext resolves the superpod constraints for task. Returns
// nil unless the superpod node-scheduler policy is active and the pod carries
// a hami.io/taskid label.
func (s *Scheduler) prepareSuperpodContext(task *corev1.Pod, userNodePolicy string) *superpodContext {
	if !util.PolicyContains(userNodePolicy, util.NodeSchedulerPolicySuperpod) {
		return nil
	}

	taskid, ok := task.Labels[util.TaskIDLabel]
	if !ok || taskid == "" {
		klog.V(4).InfoS("Superpod policy skipped: pod has no taskid label", "pod", klog.KObj(task))
		return nil
	}

	ctx := &superpodContext{
		TaskID:         taskid,
		Superpods:      make(map[string]struct{}),
		PreferredRacks: make(map[string]struct{}),
		RackSlotCounts: make(map[string]map[string]int64),
	}
	selector := labels.SelectorFromSet(labels.Set{util.TaskIDLabel: taskid})
	pods, err := s.podLister.Pods(task.Namespace).List(selector)
	if err != nil {
		// Degrade to the plain node policy instead of failing the pod.
		klog.ErrorS(err, "Superpod context: failed to list task pods, skipping superpod convergence",
			"pod", klog.KObj(task), "taskid", taskid)
		return nil
	}
	for _, p := range pods {
		// The hami.io/vgpu-node annotation is patched onto the pod right after
		// the scheduler picks a node, before the Bind lands, so it becomes
		// visible in the informer cache earlier than spec.nodeName. Reading it
		// first closes the cache-lag window where a sibling pod scheduled right
		// after the first one would still see "no superpod established" and
		// wrongly re-run the capacity check.
		nodeName := p.Annotations[util.AssignedNodeAnnotations]
		if nodeName == "" {
			nodeName = p.Spec.NodeName
		}
		if nodeName == "" || p.UID == task.UID || util.IsPodTerminating(p) {
			continue
		}
		node, err := s.nodeLister.Get(nodeName)
		if err != nil {
			klog.V(4).InfoS("Superpod context: node not found for task pod",
				"pod", klog.KObj(p), "node", nodeName)
			continue
		}
		if spid, ok := node.Labels[util.SuperPodIDLabel]; ok && spid != "" {
			ctx.Superpods[spid] = struct{}{}
			ctx.TaskAnchored = true
			if rack, ok := node.Labels[util.RackIDLabel]; ok && rack != "" {
				ctx.PreferredRacks[rack] = struct{}{}
			}
		}
	}
	klog.V(2).InfoS("Superpod context ready",
		"pod", klog.KObj(task),
		"taskid", taskid,
		"taskAnchored", ctx.TaskAnchored,
		"superpods", len(ctx.Superpods),
		"preferredRacks", len(ctx.PreferredRacks))
	return ctx
}

// applySuperpodFilter enforces the superpod node-scheduler policy in two
// stages, driven by whether the task is already anchored:
//
//   - Not anchored: reject superpods whose total usable capacity cannot hold
//     the task's total device request, so the first pod never anchors the
//     task into a superpod too small to fit the whole task.
//   - Anchored: reject nodes outside the task's superpods so the task stays
//     converged. The capacity check is skipped because the superpod was
//     already validated when the task anchored into it.
//
// In both modes, nodes without a superpodid label are rejected.
func (s *Scheduler) applySuperpodFilter(ctx *superpodContext, task *corev1.Pod, nodes *map[string]*NodeUsage, failedNodes map[string]string) error {
	if ctx == nil {
		return nil
	}
	taskAnchored := ctx.TaskAnchored
	podRequest, err := singleSuperpodPodRequest(task)
	if err != nil {
		return fmt.Errorf("failed to calculate superpod request for pod %s: %w", task.Name, err)
	}
	ctx.podRequest = podRequest

	if !taskAnchored {
		ctx.totalRequest, err = s.resolveSuperpodTotalRequest(task, ctx.podRequest)
		if err != nil {
			return fmt.Errorf("failed to resolve superpod total request for pod %s: %w", task.Name, err)
		}
		candidateSuperpods := make(map[string]struct{})
		for _, node := range *nodes {
			if spid, ok := node.Node.Labels[util.SuperPodIDLabel]; ok && spid != "" {
				candidateSuperpods[spid] = struct{}{}
			}
		}
		if ctx.totalRequest > 0 {
			ctx.superpodCapacities = s.collectSuperpodCapacities(candidateSuperpods)
		}
		klog.V(4).InfoS("Superpod capacity filter",
			"taskid", ctx.TaskID,
			"taskAnchored", taskAnchored,
			"totalRequest", ctx.totalRequest,
			"podRequest", ctx.podRequest)
	}

	rejected := s.rejectNodesBySuperpod(ctx, task, nodes, failedNodes)

	// Rack counting runs after rejectNodesBySuperpod so only surviving nodes
	// are counted, avoiding bias from nodes that fail the capacity check.
	if !taskAnchored {
		s.collectRackNodeCounts(ctx, task, nodes, ctx.podRequest)
	}

	klog.V(2).InfoS("Superpod filter applied",
		"taskid", ctx.TaskID,
		"taskAnchored", taskAnchored,
		"superpods", len(ctx.Superpods),
		"rejectedNodes", rejected,
		"remainingNodes", len(*nodes))
	return nil
}

// rejectNodesBySuperpod walks the candidate nodes once and mutates *nodes and
// failedNodes in place:
//
//   - Nodes without a superpodid label are always rejected (SuperpodNodeOutside).
//   - Anchored: reject nodes outside the task's superpods (SuperpodMismatch).
//   - Unanchored: reject nodes of superpods whose capacity cannot hold the
//     task's total request (SuperpodInsufficientCapacity).
//
// The derived filtering state (podRequest, totalRequest, superpodCapacities)
// is carried by ctx, populated by applySuperpodFilter, so it does not need to
// be passed as loose parameters.
//
// Returns the number of nodes rejected.
func (s *Scheduler) rejectNodesBySuperpod(ctx *superpodContext, task *corev1.Pod, nodes *map[string]*NodeUsage, failedNodes map[string]string) int {
	rejected := 0
	for nodeID, node := range *nodes {
		spid, ok := node.Node.Labels[util.SuperPodIDLabel]
		if !ok || spid == "" {
			failedNodes[nodeID] = common.SuperpodNodeOutside
			klog.V(5).InfoS(common.SuperpodNodeOutside,
				"pod", klog.KObj(task), "node", nodeID,
				"taskid", ctx.TaskID,
				"reason", "node has no superpod label")
			delete(*nodes, nodeID)
			rejected++
			continue
		}
		if ctx.TaskAnchored {
			if _, inTask := ctx.Superpods[spid]; !inTask {
				failedNodes[nodeID] = common.SuperpodMismatch
				klog.V(5).InfoS(common.SuperpodMismatch,
					"pod", klog.KObj(task), "node", nodeID,
					"taskid", ctx.TaskID,
					"nodeSuperpod", spid)
				delete(*nodes, nodeID)
				rejected++
				continue
			}
		} else {
			if ctx.totalRequest > 0 && ctx.superpodCapacities[spid] < ctx.totalRequest {
				failedNodes[nodeID] = common.SuperpodInsufficientCapacity
				klog.V(5).InfoS(common.SuperpodInsufficientCapacity,
					"pod", klog.KObj(task), "node", nodeID,
					"taskid", ctx.TaskID,
					"superpod", spid,
					"capacity", ctx.superpodCapacities[spid],
					"podRequest", ctx.podRequest,
					"totalRequest", ctx.totalRequest)
				delete(*nodes, nodeID)
				rejected++
			}
		}
	}
	return rejected
}

// collectRackNodeCounts accumulates, per superpod, the total remaining device
// slots each rack holds, for first-pod rack preference. Only nodes that can
// hold the current pod's single-pod request contribute their nodeUsableSlots.
// podRequest is passed in to avoid recomputing.
func (s *Scheduler) collectRackNodeCounts(ctx *superpodContext, task *corev1.Pod, nodes *map[string]*NodeUsage, podRequest int64) {
	for nodeID, node := range *nodes {
		spid, ok := node.Node.Labels[util.SuperPodIDLabel]
		if !ok || spid == "" {
			continue
		}
		rack, ok := node.Node.Labels[util.RackIDLabel]
		if !ok || rack == "" {
			continue
		}
		slots := nodeUsableSlots(node)
		if podRequest > 0 && slots < podRequest {
			klog.V(5).InfoS("Superpod rack count: node skipped, insufficient single-pod capacity",
				"node", nodeID,
				"superpod", spid,
				"rack", rack,
				"nodeUsable", slots,
				"podRequest", podRequest)
			continue
		}
		if ctx.RackSlotCounts[spid] == nil {
			ctx.RackSlotCounts[spid] = make(map[string]int64)
		}
		ctx.RackSlotCounts[spid][rack] += slots
	}
	klog.V(5).InfoS("Superpod rack device counts",
		"taskid", ctx.TaskID,
		"podRequest", podRequest,
		"rackNodeCounts", ctx.RackSlotCounts)
}

// nodeUsableSlots returns the total available device slots on a node by
// summing Count - Used across all healthy devices.
func nodeUsableSlots(node *NodeUsage) int64 {
	var total int64
	for _, ds := range node.Devices.DeviceLists {
		if ds == nil || ds.Device == nil || !ds.Device.Health {
			continue
		}
		avail := int64(ds.Device.Count - ds.Device.Used)
		if avail < 0 {
			avail = 0
		}
		total += avail
	}
	return total
}

// singleSuperpodPodRequest counts the device slots one pod requests across
// all HAMi backends. App container requests are summed, init container
// requests take the running-max, and the larger of the two wins.
func singleSuperpodPodRequest(pod *corev1.Pod) (int64, error) {
	counts, err := device.Resourcereqs(pod)
	if err != nil {
		return 0, err
	}
	initCnt := len(pod.Spec.InitContainers)
	var appTotal, initMax int64
	for i, reqMap := range counts {
		var containerReq int64
		for _, r := range reqMap {
			if r.Nums > 0 {
				containerReq += int64(r.Nums)
			}
		}
		if i < initCnt {
			if containerReq > initMax {
				initMax = containerReq
			}
		} else {
			appTotal += containerReq
		}
	}
	if initMax > appTotal {
		return initMax, nil
	}
	return appTotal, nil
}

// resolveTaskIDSuperpodTotal lists every ReplicaSet labeled with the given
// taskid and returns the aggregate total device slots (singlePodRequest *
// Replicas summed across all matching ReplicaSets).
func resolveTaskIDSuperpodTotal(clientset kubernetes.Interface, namespace, taskID string) (int64, error) {
	if clientset == nil || strings.TrimSpace(taskID) == "" {
		return 0, nil
	}
	replicaSets, err := clientset.AppsV1().ReplicaSets(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", util.TaskIDLabel, taskID),
	})
	if err != nil {
		return 0, err
	}
	var total int64
	for i := range replicaSets.Items {
		rs := &replicaSets.Items[i]
		if rs.Spec.Replicas == nil || *rs.Spec.Replicas <= 0 {
			continue
		}
		// Evaluate the device request from the ReplicaSet's pod template so
		// we don't depend on a living instance.
		templatePod := &corev1.Pod{Spec: rs.Spec.Template.Spec}
		podReq, err := singleSuperpodPodRequest(templatePod)
		if err != nil {
			return 0, fmt.Errorf("failed to parse ReplicaSet %q pod template request: %w", rs.Name, err)
		}
		if podReq <= 0 {
			continue
		}
		total += podReq * int64(*rs.Spec.Replicas)
	}
	return total, nil
}

// superpodOwnerReplicas returns the total device slots declared by the pod's
// owning controller (singlePodRequest * Replicas/Parallelism) and true when
// such an owner is resolvable. A missing or zero count yields (0, false).
func (s *Scheduler) superpodOwnerReplicas(task *corev1.Pod) (int64, bool, error) {
	if len(task.OwnerReferences) == 0 || s.kubeClient == nil {
		return 0, false, nil
	}
	owner := task.OwnerReferences[0]
	klog.V(4).InfoS("Superpod total request: resolving from owner reference",
		"ownerKind", owner.Kind, "ownerName", owner.Name)
	switch owner.Kind {
	case "ReplicaSet":
		if rs, err := s.kubeClient.AppsV1().ReplicaSets(task.Namespace).Get(context.TODO(), owner.Name, metav1.GetOptions{}); err == nil && rs != nil && rs.Spec.Replicas != nil {
			request, err := singleSuperpodPodRequest(task)
			if err != nil {
				return 0, false, err
			}
			return request * int64(*rs.Spec.Replicas), true, nil
		}
	case "StatefulSet":
		if sts, err := s.kubeClient.AppsV1().StatefulSets(task.Namespace).Get(context.TODO(), owner.Name, metav1.GetOptions{}); err == nil && sts != nil && sts.Spec.Replicas != nil {
			request, err := singleSuperpodPodRequest(task)
			if err != nil {
				return 0, false, err
			}
			return request * int64(*sts.Spec.Replicas), true, nil
		}
	case "Job":
		if job, err := s.kubeClient.BatchV1().Jobs(task.Namespace).Get(context.TODO(), owner.Name, metav1.GetOptions{}); err == nil && job != nil && job.Spec.Parallelism != nil {
			request, err := singleSuperpodPodRequest(task)
			if err != nil {
				return 0, false, err
			}
			return request * int64(*job.Spec.Parallelism), true, nil
		}
	}
	return 0, false, nil
}

// resolveSuperpodTotalRequest derives the total device slots the whole task
// requests, in priority order:
//  1. Explicit hami.io/superpod-total-request label (for CRD-owned workloads).
//  2. Aggregation across same-taskid ReplicaSets.
//  3. Pod's own owner controller replicas.
//  4. Fallback to the single-pod request.
//
// single is the task's single-pod device request, passed in to avoid
// recomputing.
func (s *Scheduler) resolveSuperpodTotalRequest(task *corev1.Pod, single int64) (int64, error) {
	if task == nil || single <= 0 {
		return 0, nil
	}
	// Priority 1: explicit label.
	if raw, ok := task.Labels[util.SuperpodTotalRequestLabel]; ok {
		declared, parseErr := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if parseErr != nil || declared <= 0 {
			klog.V(4).InfoS("Superpod total request: invalid or non-positive label, ignoring",
				"labelValue", raw, "err", parseErr)
		} else {
			klog.V(4).InfoS("Superpod total request resolved from explicit label",
				"totalRequest", declared, "singlePodRequest", single)
			return declared, nil
		}
	}
	// Priority 2: same-taskid ReplicaSets.
	if taskid := task.Labels[util.TaskIDLabel]; taskid != "" && s.kubeClient != nil {
		if total, resolveErr := resolveTaskIDSuperpodTotal(s.kubeClient, task.Namespace, taskid); resolveErr == nil && total > 0 {
			klog.V(4).InfoS("Superpod total request resolved from task's ReplicaSets",
				"taskid", taskid, "totalRequest", total)
			return total, nil
		} else if resolveErr != nil {
			var invalidRequest *device.ErrInvalidDeviceRequest
			if errors.As(resolveErr, &invalidRequest) {
				return 0, resolveErr
			}
			klog.V(4).ErrorS(resolveErr, "Superpod total request: failed to resolve task's ReplicaSets",
				"taskid", taskid)
		} else {
			klog.V(4).InfoS("Superpod total request: taskid aggregation returned zero, falling back",
				"taskid", taskid, "namespace", task.Namespace,
				"singlePodRequest", single)
		}
	}
	// Priority 3: owner controller replicas.
	if r, ok, err := s.superpodOwnerReplicas(task); err != nil {
		return 0, err
	} else if ok && r > 0 {
		klog.V(4).InfoS("Superpod total request resolved from owner controller replicas",
			"pod", klog.KObj(task), "totalRequest", r)
		return r, nil
	}
	// Priority 4: single-pod request.
	klog.V(4).InfoS("Superpod total request fell back to single pod request",
		"totalRequest", single)
	return single, nil
}

// collectSuperpodCapacities computes the aggregate usable device capacity per
// requested superpod from a full node snapshot. A full snapshot (not just the
// candidate set) is used so allocations on the superpod's other nodes are
// counted as consumed. Only healthy devices are counted. The returned map is
// always non-nil.
func (s *Scheduler) collectSuperpodCapacities(spids map[string]struct{}) map[string]int64 {
	capacities := make(map[string]int64, len(spids))
	if len(spids) == 0 {
		return capacities
	}
	// getNodesUsage with a nil node list returns the full snapshot; the pod
	// argument only drives the scoring policy, so an empty task is safe.
	_, overall, _, err := s.getNodesUsage(nil, &corev1.Pod{})
	if err != nil {
		klog.ErrorS(err, "Superpod capacity collection failed to collect node usage")
		return capacities
	}
	for _, usage := range *overall {
		if usage == nil || usage.Node == nil {
			continue
		}
		spid := usage.Node.Labels[util.SuperPodIDLabel]
		if _, wanted := spids[spid]; !wanted {
			continue
		}
		capacities[spid] += nodeUsableSlots(usage)
	}
	klog.V(4).InfoS("Superpod capacities computed",
		"superpods", len(capacities),
		"capacities", capacities)
	return capacities
}

// superpodRackBoost is the score bonus same-rack nodes receive. It is
// chip-dependent, so it is overridable via the HAMI_SUPERPOD_RACK_BOOST
// environment variable. Defaults to util.Weight.
var superpodRackBoost = resolveSuperpodRackBoost()

// resolveSuperpodRackBoost reads the HAMI_SUPERPOD_RACK_BOOST environment
// variable and falls back to util.Weight when unset or invalid.
func resolveSuperpodRackBoost() float32 {
	raw := strings.TrimSpace(os.Getenv("HAMI_SUPERPOD_RACK_BOOST"))
	if raw != "" {
		if v, err := strconv.ParseFloat(raw, 32); err == nil && v > 0 {
			klog.V(2).InfoS("Superpod rack boost overridden",
				"value", float32(v), "source", "HAMI_SUPERPOD_RACK_BOOST")
			return float32(v)
		}
		klog.ErrorS(nil, "Invalid HAMI_SUPERPOD_RACK_BOOST, falling back to default",
			"value", raw, "default", float32(util.Weight))
	}
	return float32(util.Weight)
}

// applySuperpodRackBoost applies a rack preference to a node's base score.
//
//   - Anchored: nodes in racks where the task's pods already run get the full
//     bonus, added under binpack and subtracted under spread.
//   - Not anchored (first pod): nodes get a proportional bonus of
//     superpodRackBoost * rackSlots / maxRackSlots, where maxRackSlots is the
//     largest per-rack remaining slots across all candidate superpods,
//     derived from ctx.RackSlotCounts on the fly. The cross-superpod
//     denominator keeps the boost comparable between superpods. Racks whose
//     remaining slots are at most the single-pod request get no bonus.
//
// Nodes without a rackid label keep their base score.
func applySuperpodRackBoost(ctx *superpodContext, node *corev1.Node, nodeID string, baseScore float32, policy string) float32 {
	if ctx == nil {
		return baseScore
	}
	rack, ok := node.Labels[util.RackIDLabel]
	if !ok || rack == "" {
		return baseScore
	}

	if ctx.TaskAnchored {
		if _, ok := ctx.PreferredRacks[rack]; !ok {
			return baseScore
		}
		finalScore := baseScore + superpodRackBoost
		if util.PolicyContains(policy, util.NodeSchedulerPolicySpread) {
			finalScore = baseScore - superpodRackBoost
		}
		klog.V(5).InfoS("Superpod rack boost applied (anchored)",
			"node", nodeID,
			"rack", rack,
			"baseScore", baseScore,
			"finalScore", finalScore)
		return finalScore
	}

	// First-pod mode: proportional bonus based on the rack's remaining
	// device slots relative to the largest rack across all candidate
	// superpods.
	spid, ok := node.Labels[util.SuperPodIDLabel]
	if !ok || spid == "" {
		return baseScore
	}
	rackSlotsMap, ok := ctx.RackSlotCounts[spid]
	if !ok {
		klog.V(5).InfoS("Superpod rack boost skipped: no rack counts for superpod",
			"node", nodeID, "superpod", spid, "rack", rack)
		return baseScore
	}
	rackSlots := rackSlotsMap[rack]
	if rackSlots <= ctx.podRequest {
		// A rack whose remaining slots cannot fit even one more pod offers no
		// room for convergence.
		klog.V(5).InfoS("Superpod rack boost skipped: rack has insufficient remaining slots",
			"node", nodeID, "superpod", spid, "rack", rack, "rackSlots", rackSlots,
			"podRequest", ctx.podRequest)
		return baseScore
	}
	maxRackSlots := int64(0)
	for _, racks := range ctx.RackSlotCounts {
		for _, slots := range racks {
			if slots > maxRackSlots {
				maxRackSlots = slots
			}
		}
	}
	if maxRackSlots <= 0 {
		return baseScore
	}
	boost := superpodRackBoost * float32(rackSlots) / float32(maxRackSlots)
	finalScore := baseScore + boost
	if util.PolicyContains(policy, util.NodeSchedulerPolicySpread) {
		finalScore = baseScore - boost
	}
	klog.V(5).InfoS("Superpod rack boost applied (first pod)",
		"node", nodeID,
		"superpod", spid,
		"rack", rack,
		"rackSlots", rackSlots,
		"maxRackSlots", maxRackSlots,
		"boost", boost,
		"baseScore", baseScore,
		"finalScore", finalScore)
	return finalScore
}