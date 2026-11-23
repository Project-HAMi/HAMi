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
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	appslisters "k8s.io/client-go/listers/apps/v1"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	schedulerpolicy "github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
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
	// RackFittingPods maps each superpod to the number of copies of the
	// current pod that fit in each rack, for first-pod rack preference.
	RackFittingPods map[string]map[string]int64

	// podRequest is the current pod's single-pod device request, resolved
	// during the filter stage.
	podRequest int64
	// totalPods is the whole task's replica count, resolved during the filter
	// stage for unanchored capacity validation.
	totalPods int64
	// superpodCapacities maps each candidate superpod to its number of fitting
	// copies of the current pod, populated during the filter stage.
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
		TaskID:          taskid,
		Superpods:       make(map[string]struct{}),
		PreferredRacks:  make(map[string]struct{}),
		RackFittingPods: make(map[string]map[string]int64),
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
		// Filter records an allocation in PodManager before patching the
		// assigned-node annotation. Prefer that reservation to close the window
		// where neither the informer nor the pod object shows the assignment.
		nodeName := ""
		if s.podManager != nil {
			if podInfo, ok := s.podManager.GetPod(p); ok {
				nodeName = podInfo.NodeID
			}
		}
		if nodeName == "" {
			nodeName = p.Annotations[util.AssignedNodeAnnotations]
		}
		if nodeName == "" {
			nodeName = p.Spec.NodeName
		}
		if nodeName == "" || p.UID == task.UID || util.IsPodTerminating(p) || util.IsPodInTerminatedState(p) {
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
//   - Not anchored: reject superpods whose total fitting-pod capacity cannot
//     hold all replicas of the task, so the first pod never anchors the
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
		ctx.totalPods, err = s.resolveSuperpodTotalRequest(task, ctx.podRequest)
		if err != nil {
			return fmt.Errorf("failed to resolve superpod total request for pod %s: %w", task.Name, err)
		}
		candidateSuperpods := make(map[string]struct{})
		for nodeID, usage := range *nodes {
			node := s.currentNode(nodeID, usage.Node)
			if spid, ok := node.Labels[util.SuperPodIDLabel]; ok && spid != "" {
				candidateSuperpods[spid] = struct{}{}
			}
		}
		if ctx.totalPods > 0 {
			ctx.superpodCapacities, err = s.collectSuperpodCapacities(candidateSuperpods, task)
			if err != nil {
				return fmt.Errorf("failed to collect superpod capacities: %w", err)
			}
		}
		klog.V(4).InfoS("Superpod capacity filter",
			"taskid", ctx.TaskID,
			"taskAnchored", taskAnchored,
			"totalPods", ctx.totalPods,
			"podRequest", ctx.podRequest)
	}

	rejected := s.rejectNodesBySuperpod(ctx, task, nodes, failedNodes)

	// Rack counting runs after rejectNodesBySuperpod so only surviving nodes
	// are counted, avoiding bias from nodes that fail the capacity check.
	if !taskAnchored {
		if err := s.collectRackFittingPods(ctx, task, nodes); err != nil {
			return fmt.Errorf("failed to count fitting pods per rack: %w", err)
		}
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
// The derived filtering state (podRequest, totalPods, superpodCapacities)
// is carried by ctx, populated by applySuperpodFilter, so it does not need to
// be passed as loose parameters.
//
// Returns the number of nodes rejected.
func (s *Scheduler) rejectNodesBySuperpod(ctx *superpodContext, task *corev1.Pod, nodes *map[string]*NodeUsage, failedNodes map[string]string) int {
	rejected := 0
	for nodeID, usage := range *nodes {
		node := s.currentNode(nodeID, usage.Node)
		spid, ok := node.Labels[util.SuperPodIDLabel]
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
			if ctx.totalPods > 0 && ctx.superpodCapacities[spid] < ctx.totalPods {
				failedNodes[nodeID] = common.SuperpodInsufficientCapacity
				klog.V(5).InfoS(common.SuperpodInsufficientCapacity,
					"pod", klog.KObj(task), "node", nodeID,
					"taskid", ctx.TaskID,
					"superpod", spid,
					"capacity", ctx.superpodCapacities[spid],
					"podRequest", ctx.podRequest,
					"totalPods", ctx.totalPods)
				delete(*nodes, nodeID)
				rejected++
			}
		}
	}
	return rejected
}

// collectRackFittingPods counts, per superpod and rack, how many copies of the
// current pod fit using the same device Fit path as scheduling.
func (s *Scheduler) collectRackFittingPods(ctx *superpodContext, task *corev1.Pod, nodes *map[string]*NodeUsage) error {
	resourceRequests, err := device.Resourcereqs(task)
	if err != nil {
		return fmt.Errorf("failed to calculate per-pod device requests: %w", err)
	}
	weights, err := util.GetDeviceScoringWeightsByPod(task)
	if err != nil {
		return err
	}
	for nodeID, usage := range *nodes {
		if usage == nil || usage.Node == nil {
			continue
		}
		node := s.currentNode(nodeID, usage.Node)
		spid, ok := node.Labels[util.SuperPodIDLabel]
		if !ok || spid == "" {
			continue
		}
		rack, ok := node.Labels[util.RackIDLabel]
		if !ok || rack == "" {
			continue
		}
		fittingPods, err := s.countFittingPods(nodeID, usage, task, resourceRequests, resolveNodeSchedulerPolicy(task), weights)
		if err != nil {
			return fmt.Errorf("failed to count fitting pods on node %q: %w", nodeID, err)
		}
		if fittingPods == 0 {
			continue
		}
		if ctx.RackFittingPods[spid] == nil {
			ctx.RackFittingPods[spid] = make(map[string]int64)
		}
		ctx.RackFittingPods[spid][rack] += fittingPods
	}
	klog.V(5).InfoS("Superpod rack fitting pod counts",
		"taskid", ctx.TaskID,
		"rackNodeCounts", ctx.RackFittingPods)
	return nil
}

// countFittingPods repeatedly simulates scheduling the same pod on a node copy
// and counts complete successful allocations. scoreNode mutates only the
// supplied node state after a successful Fit, so later attempts see the prior
// simulated allocations without changing the live snapshot.
func (s *Scheduler) countFittingPods(
	nodeID string,
	node *NodeUsage,
	pod *corev1.Pod,
	resourceRequests device.PodDeviceRequests,
	policy string,
	weights util.DeviceScoringWeights,
) (int64, error) {
	if node == nil || node.Node == nil || pod == nil {
		return 0, nil
	}
	hasRequest := false
	for _, requests := range resourceRequests {
		for _, request := range requests {
			if request.Nums > 0 {
				hasRequest = true
				break
			}
		}
		if hasRequest {
			break
		}
	}
	if !hasRequest {
		return 0, nil
	}

	nodeCopy := node.DeepCopy()
	var fittingPods int64
	for {
		before := snapshotPeakUsage(nodeCopy.Devices)
		result := s.scoreNode(nodeID, nodeCopy, resourceRequests, pod, policy, nil, weights)
		if result.err != nil {
			return 0, result.err
		}
		if result.score == nil {
			return fittingPods, nil
		}
		consumed := false
		for _, ds := range nodeCopy.Devices.DeviceLists {
			if ds == nil || ds.Device == nil {
				continue
			}
			previous := before[ds.Device.ID]
			if ds.Device.Used != previous.used ||
				ds.Device.Usedcores != previous.usedcores ||
				ds.Device.Usedmem != previous.usedmem {
				consumed = true
				break
			}
		}
		if !consumed {
			return 0, fmt.Errorf("device Fit succeeded without changing node usage")
		}
		fittingPods++
	}
}

// singleSuperpodPodRequest counts the device shares one pod requests across
// all HAMi backends. App container requests are summed, init container
// requests take the running-max, and the larger of the two wins.
func singleSuperpodPodRequest(pod *corev1.Pod) (int64, error) {
	if pod == nil {
		return 0, nil
	}
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
				containerReq += r.Nums
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
// taskid and returns the aggregate replica count of matching workloads that
// request HAMi devices.
func resolveTaskIDSuperpodTotal(replicaSetLister appslisters.ReplicaSetLister, namespace, taskID string) (int64, error) {
	if replicaSetLister == nil || strings.TrimSpace(taskID) == "" {
		return 0, nil
	}
	replicaSets, err := replicaSetLister.ReplicaSets(namespace).List(labels.Set{util.TaskIDLabel: taskID}.AsSelector())
	if err != nil {
		return 0, err
	}
	var total int64
	for _, rs := range replicaSets {
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
		total += int64(*rs.Spec.Replicas)
	}
	return total, nil
}

// superpodOwnerReplicas returns the desired replica count declared by the pod's
// owning controller and true when such an owner is resolvable. A missing,
// zero-count, or device-less owner yields (0, false).
func (s *Scheduler) superpodOwnerReplicas(task *corev1.Pod) (int64, bool, error) {
	if len(task.OwnerReferences) == 0 {
		return 0, false, nil
	}
	request, err := singleSuperpodPodRequest(task)
	if err != nil {
		return 0, false, err
	}
	if request <= 0 {
		return 0, false, nil
	}
	owner := task.OwnerReferences[0]
	klog.V(4).InfoS("Superpod total request: resolving from owner reference",
		"ownerKind", owner.Kind, "ownerName", owner.Name)
	switch owner.Kind {
	case "ReplicaSet":
		if s.replicaSetLister == nil {
			return 0, false, nil
		}
		rs, err := s.replicaSetLister.ReplicaSets(task.Namespace).Get(owner.Name)
		if err != nil && !apierrors.IsNotFound(err) {
			klog.V(4).ErrorS(err, "Superpod total request: failed to get owner",
				"ownerKind", owner.Kind, "ownerName", owner.Name)
			return 0, false, fmt.Errorf("failed to get owner %s %s: %w", owner.Kind, owner.Name, err)
		}
		if err == nil && rs != nil && rs.Spec.Replicas != nil && *rs.Spec.Replicas > 0 {
			return int64(*rs.Spec.Replicas), true, nil
		}
	case "StatefulSet":
		if s.statefulSetLister == nil {
			return 0, false, nil
		}
		sts, err := s.statefulSetLister.StatefulSets(task.Namespace).Get(owner.Name)
		if err != nil && !apierrors.IsNotFound(err) {
			klog.V(4).ErrorS(err, "Superpod total request: failed to get owner",
				"ownerKind", owner.Kind, "ownerName", owner.Name)
			return 0, false, fmt.Errorf("failed to get owner %s %s: %w", owner.Kind, owner.Name, err)
		}
		if err == nil && sts != nil && sts.Spec.Replicas != nil && *sts.Spec.Replicas > 0 {
			return int64(*sts.Spec.Replicas), true, nil
		}
	case "Job":
		if s.jobLister == nil {
			return 0, false, nil
		}
		job, err := s.jobLister.Jobs(task.Namespace).Get(owner.Name)
		if err != nil && !apierrors.IsNotFound(err) {
			klog.V(4).ErrorS(err, "Superpod total request: failed to get owner",
				"ownerKind", owner.Kind, "ownerName", owner.Name)
			return 0, false, fmt.Errorf("failed to get owner %s %s: %w", owner.Kind, owner.Name, err)
		}
		if err == nil && job != nil && job.Spec.Parallelism != nil && *job.Spec.Parallelism > 0 {
			return int64(*job.Spec.Parallelism), true, nil
		}
	}
	return 0, false, nil
}

// resolveSuperpodTotalRequest derives the number of pods the whole task
// requests, in priority order:
//  1. Explicit hami.io/superpod-total-request label, expressed in device shares.
//  2. Aggregation across same-taskid ReplicaSets.
//  3. Pod's own owner controller replicas.
//  4. Fallback to one pod.
//
// single is the task's single-pod device-share request, used to convert the
// legacy explicit label value into a pod count.
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
			pods := declared / single
			if declared%single != 0 {
				pods++
			}
			klog.V(4).InfoS("Superpod total request resolved from explicit label",
				"declaredDeviceShares", declared, "singlePodRequest", single, "totalPods", pods)
			return pods, nil
		}
	}
	// Priority 2: same-taskid ReplicaSets.
	if taskid := task.Labels[util.TaskIDLabel]; taskid != "" && s.replicaSetLister != nil {
		if total, resolveErr := resolveTaskIDSuperpodTotal(s.replicaSetLister, task.Namespace, taskid); resolveErr == nil && total > 0 {
			klog.V(4).InfoS("Superpod total request resolved from task's ReplicaSets",
				"taskid", taskid, "totalPods", total)
			return total, nil
		} else if resolveErr != nil {
			if _, ok := errors.AsType[*device.ErrInvalidDeviceRequest](resolveErr); ok {
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
		klog.V(4).InfoS("Superpod total pods resolved from owner controller",
			"pod", klog.KObj(task), "totalPods", r)
		return r, nil
	}
	// Priority 4: single-pod request.
	klog.V(4).InfoS("Superpod total request fell back to one pod")
	return 1, nil
}

// collectSuperpodCapacities computes how many copies of the current pod fit in
// each requested superpod using a full node snapshot, then reserves the
// outstanding replica counts of other tasks already anchored there.
func (s *Scheduler) collectSuperpodCapacities(spids map[string]struct{}, currentTask *corev1.Pod) (map[string]int64, error) {
	capacities := make(map[string]int64, len(spids))
	if len(spids) == 0 {
		return capacities, nil
	}
	if currentTask == nil {
		return capacities, nil
	}
	resourceRequests, err := device.Resourcereqs(currentTask)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate per-pod device requests: %w", err)
	}
	weights, err := util.GetDeviceScoringWeightsByPod(currentTask)
	if err != nil {
		return nil, err
	}
	policy := resolveNodeSchedulerPolicy(currentTask)
	// getNodesUsage with a nil node list returns the full snapshot; the pod
	// argument only drives the scoring policy, so an empty task is safe.
	_, overall, _, err := s.getNodesUsage(nil, &corev1.Pod{})
	if err != nil {
		return nil, fmt.Errorf("failed to collect node usage: %w", err)
	}
	for nodeID, usage := range *overall {
		if usage == nil || usage.Node == nil || usage.Node.Spec.Unschedulable {
			continue
		}
		node := s.currentNode(nodeID, usage.Node)
		spid := node.Labels[util.SuperPodIDLabel]
		if _, wanted := spids[spid]; !wanted {
			continue
		}
		fittingPods, err := s.countFittingPods(nodeID, usage, currentTask, resourceRequests, policy, weights)
		if err != nil {
			return nil, fmt.Errorf("failed to count fitting pods on node %q: %w", nodeID, err)
		}
		capacities[spid] += fittingPods
	}
	reservations, err := s.collectAnchoredTaskReservations(spids, *overall, currentTask)
	if err != nil {
		return nil, err
	}
	for spid, reserved := range reservations {
		capacities[spid] -= reserved
		if capacities[spid] < 0 {
			capacities[spid] = 0
		}
	}
	klog.V(4).InfoS("Superpod capacities computed",
		"superpods", len(capacities),
		"capacities", capacities,
		"reservations", reservations)
	return capacities, nil
}

type superpodTaskKey struct {
	namespace string
	taskID    string
}

// collectAnchoredTaskReservations returns the remaining pod count to reserve
// in each requested superpod. When a task is anchored to multiple superpods,
// its outstanding demand is reserved in each one so a new task cannot consume
// capacity that the anchored task may still need.
func (s *Scheduler) collectAnchoredTaskReservations(spids map[string]struct{}, overall map[string]*NodeUsage, currentTask *corev1.Pod) (map[string]int64, error) {
	reservations := make(map[string]int64, len(spids))
	if s.podLister == nil {
		return nil, fmt.Errorf("pod lister is unavailable")
	}
	podInfos := s.podManager.ListPodsInfo()
	pods, err := s.podLister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("failed to list pods for anchored task reservations: %w", err)
	}

	taskPods := make(map[superpodTaskKey]map[string]*corev1.Pod)
	addTaskPod := func(pod *corev1.Pod) {
		if pod == nil || util.IsPodInTerminatedState(pod) || util.IsPodTerminating(pod) {
			return
		}
		taskID := pod.Labels[util.TaskIDLabel]
		if taskID == "" {
			return
		}
		key := superpodTaskKey{namespace: pod.Namespace, taskID: taskID}
		if taskPods[key] == nil {
			taskPods[key] = make(map[string]*corev1.Pod)
		}
		taskPods[key][pod.Namespace+"/"+pod.Name] = pod
	}
	for _, pod := range pods {
		addTaskPod(pod)
	}
	// A recently scheduled pod can already be in PodManager before the pod
	// informer reflects its assignment or labels.
	for _, podInfo := range podInfos {
		if podInfo != nil {
			addTaskPod(podInfo.Pod)
		}
	}

	anchoredSuperpods := make(map[superpodTaskKey]map[string]struct{})
	for key, group := range taskPods {
		if currentTask != nil &&
			key.namespace == currentTask.Namespace &&
			key.taskID == currentTask.Labels[util.TaskIDLabel] {
			continue
		}
		for _, pod := range group {
			nodeName := ""
			if podInfo, ok := s.podManager.GetPod(pod); ok {
				nodeName = podInfo.NodeID
			}
			if nodeName == "" && pod.Annotations != nil {
				nodeName = pod.Annotations[util.AssignedNodeAnnotations]
			}
			if nodeName == "" {
				nodeName = pod.Spec.NodeName
			}
			usage := overall[nodeName]
			if usage == nil || usage.Node == nil {
				continue
			}
			node := s.currentNode(nodeName, usage.Node)
			spid := node.Labels[util.SuperPodIDLabel]
			if _, requested := spids[spid]; !requested {
				continue
			}
			if anchoredSuperpods[key] == nil {
				anchoredSuperpods[key] = make(map[string]struct{})
			}
			anchoredSuperpods[key][spid] = struct{}{}
		}
	}

	accountedPodsByTask := make(map[superpodTaskKey]map[string]struct{})
	for _, usage := range overall {
		if usage == nil {
			continue
		}
		for _, nodeDevice := range usage.Devices.DeviceLists {
			if nodeDevice == nil || nodeDevice.Device == nil {
				continue
			}
			seenPods := make(map[string]struct{})
			for _, podInfo := range nodeDevice.Device.PodInfos {
				if podInfo == nil || podInfo.Pod == nil {
					continue
				}
				podKey := podInfo.Namespace + "/" + podInfo.Name
				if _, seen := seenPods[podKey]; seen {
					continue
				}
				seenPods[podKey] = struct{}{}
				key := superpodTaskKey{
					namespace: podInfo.Namespace,
					taskID:    podInfo.Labels[util.TaskIDLabel],
				}
				if key.taskID == "" || len(anchoredSuperpods[key]) == 0 {
					continue
				}
				hasAllocation := false
				for _, containers := range podInfo.Devices {
					for _, devices := range containers {
						for _, allocated := range devices {
							if allocated.UUID == nodeDevice.Device.ID {
								hasAllocation = true
								break
							}
						}
						if hasAllocation {
							break
						}
					}
					if hasAllocation {
						break
					}
				}
				if hasAllocation {
					if accountedPodsByTask[key] == nil {
						accountedPodsByTask[key] = make(map[string]struct{})
					}
					accountedPodsByTask[key][podKey] = struct{}{}
				}
			}
		}
	}

	for key, superpods := range anchoredSuperpods {
		var representative *corev1.Pod
		for _, pod := range taskPods[key] {
			representative = pod
			break
		}
		singleRequest, err := singleSuperpodPodRequest(representative)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate request for anchored task %q in namespace %q: %w", key.taskID, key.namespace, err)
		}
		totalPods, err := s.resolveSuperpodTotalRequest(representative, singleRequest)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve total request for anchored task %q in namespace %q: %w", key.taskID, key.namespace, err)
		}
		remaining := totalPods - int64(len(accountedPodsByTask[key]))
		if remaining <= 0 {
			continue
		}
		for spid := range superpods {
			reservations[spid] += remaining
		}
	}
	return reservations, nil
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
//     bonus, added under binpack and capped under spread.
//   - Not anchored (first pod): nodes get a proportional bonus of
//     superpodRackBoost * rackFittingPods / maxRackFittingPods, where the
//     denominator is the largest fitting-pod count across all candidate
//     superpods. Under
//     spread, the boost is capped so positive scores stay positive and retain
//     their relative order; zero and negative scores still receive a bounded
//     rack preference.
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
		finalScore := applyRackBoostToScore(baseScore, superpodRackBoost, schedulerpolicy.NodePolicyUsesSpread(policy))
		klog.V(5).InfoS("Superpod rack boost applied (anchored)",
			"node", nodeID,
			"rack", rack,
			"baseScore", baseScore,
			"finalScore", finalScore)
		return finalScore
	}

	// First-pod mode: proportional bonus based on complete pods that fit in
	// the rack relative to the largest rack across all candidate superpods.
	spid, ok := node.Labels[util.SuperPodIDLabel]
	if !ok || spid == "" {
		return baseScore
	}
	rackPodsByRack, ok := ctx.RackFittingPods[spid]
	if !ok {
		klog.V(5).InfoS("Superpod rack boost skipped: no rack counts for superpod",
			"node", nodeID, "superpod", spid, "rack", rack)
		return baseScore
	}
	rackFittingPods := rackPodsByRack[rack]
	maxRackFittingPods := int64(0)
	for _, racks := range ctx.RackFittingPods {
		for _, fittingPods := range racks {
			if fittingPods > maxRackFittingPods {
				maxRackFittingPods = fittingPods
			}
		}
	}
	if maxRackFittingPods <= 0 {
		return baseScore
	}
	boost := superpodRackBoost * float32(rackFittingPods) / float32(maxRackFittingPods)
	finalScore := applyRackBoostToScore(baseScore, boost, schedulerpolicy.NodePolicyUsesSpread(policy))
	klog.V(5).InfoS("Superpod rack boost applied (first pod)",
		"node", nodeID,
		"superpod", spid,
		"rack", rack,
		"rackFittingPods", rackFittingPods,
		"maxRackFittingPods", maxRackFittingPods,
		"boost", boost,
		"baseScore", baseScore,
		"finalScore", finalScore)
	return finalScore
}

func applyRackBoostToScore(baseScore, boost float32, spread bool) float32 {
	if !spread {
		return baseScore + boost
	}
	if baseScore > 0 && boost > baseScore/2 {
		boost = baseScore / 2
	}
	return baseScore - boost
}
