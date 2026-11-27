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
	"maps"
	"slices"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

const (
	// unlimitedTier is the highest tier a pod can ask for when it wants
	// topology-aware ranking without a cap. It is resolved to the depth of the
	// configured hierarchy once the hierarchy is known.
	unlimitedTier = -1
)

// topologyGroupLabels identify the distributed job a pod belongs to. They cover
// the usual training operators: Kubeflow and most job controllers set job-name,
// Volcano sets volcano.sh/job-name, and the scheduler-plugins PodGroup label
// names the gang.
var topologyGroupLabels = []string{
	"job-name",
	"training.kubeflow.org/job-name",
	"volcano.sh/job-name",
	util.PodGroupLabel,
}

// topologyPlan is what one Filter call needs to apply the topology rules to one
// pod: the hierarchy, the group the pod belongs to, and where that group is.
type topologyPlan struct {
	view    *topologyView
	group   string
	placed  []string
	mode    string
	highest int
}

// topologyRequirement reads the per-pod topology requirement. The feature is
// opt-in per pod: a pod that carries neither annotation keeps the placement
// behavior it had before, so enabling the tier labels flag on a shared cluster
// does not change how unrelated workloads are scheduled.
func topologyRequirement(pod *corev1.Pod) (highest int, mode string, ok bool) {
	if pod == nil {
		return 0, "", false
	}

	rawMode := strings.ToLower(strings.TrimSpace(pod.Annotations[util.TopologyModeAnnotationKey]))
	mode = rawMode
	if mode != "" && mode != util.TopologyModeHard && mode != util.TopologyModeSoft {
		klog.ErrorS(nil, "Ignoring invalid topology mode", "pod", klog.KObj(pod), "mode", mode)
		mode = ""
	}

	raw := strings.TrimSpace(pod.Annotations[util.TopologyHighestTierAnnotationKey])
	if raw == "" {
		// A mode without a tier asks for ranking only: nothing is rejected.
		// An unrecognized mode still opted in, it only falls back to the
		// configured default, so it must not turn the feature off.
		if rawMode != "" {
			return unlimitedTier, mode, true
		}
		return 0, "", false
	}

	tier, err := strconv.Atoi(raw)
	if err != nil || tier < 0 {
		klog.ErrorS(nil, "Ignoring invalid topology highest tier", "pod", klog.KObj(pod), "tier", raw)
		return 0, "", false
	}
	return tier, mode, true
}

// topologyGroupKey identifies the group of pods that has to stay close to each
// other. It is empty for a pod that belongs to no group, which is a pod nothing
// needs to be co-located with.
func topologyGroupKey(pod *corev1.Pod) string {
	if pod == nil {
		return ""
	}
	if name := strings.TrimSpace(pod.Annotations[util.TopologyGroupAnnotationKey]); name != "" {
		return pod.Namespace + "/" + name
	}
	for _, label := range topologyGroupLabels {
		if name := strings.TrimSpace(pod.Labels[label]); name != "" {
			return pod.Namespace + "/" + label + "/" + name
		}
	}
	for _, owner := range pod.OwnerReferences {
		if owner.Controller != nil && *owner.Controller {
			return pod.Namespace + "/" + owner.Kind + "/" + string(owner.UID)
		}
	}
	return ""
}

// topologyGroupPlacement collects the nodes the other members of group already
// occupy. Reservations made by this scheduler count too: a Pod of a group placed
// in this same scheduling round has to constrain its siblings before the API
// server has observed it as bound. UIDs deduplicate the informer and the
// reservation cache.
func (s *Scheduler) topologyGroupPlacement(task *corev1.Pod, group string) []string {
	placed := make(map[string]struct{})
	active := make(map[types.UID]string)
	if s.podLister != nil {
		pods, err := s.podLister.Pods(task.Namespace).List(labels.Everything())
		if err != nil {
			klog.ErrorS(err, "Failed to list pods for topology grouping", "namespace", task.Namespace)
		}
		for _, pod := range pods {
			if pod.UID == task.UID || pod.DeletionTimestamp != nil {
				continue
			}
			if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
				continue
			}
			if topologyGroupKey(pod) != group {
				continue
			}
			if pod.Spec.NodeName == "" {
				continue
			}
			active[pod.UID] = pod.Spec.NodeName
			placed[pod.Spec.NodeName] = struct{}{}
		}
	}
	for _, info := range s.podManager.ListPodsInfo() {
		if info.Pod == nil || info.UID == task.UID || info.Namespace != task.Namespace {
			continue
		}
		if topologyGroupKey(info.Pod) != group {
			continue
		}
		if info.NodeID == "" {
			continue
		}
		active[info.UID] = info.NodeID
		placed[info.NodeID] = struct{}{}
	}
	if len(placed) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(placed))
}

// topologyPlacedNodes returns the nodes the group already occupies.
func (s *Scheduler) topologyPlacedNodes(task *corev1.Pod, group string) []string {
	return s.topologyGroupPlacement(task, group)
}

// topologyPlanFor resolves the placement rules that apply to task, or nil when
// the feature is off, the pod did not opt in, or the pod is part of no group.
func (s *Scheduler) topologyPlanFor(task *corev1.Pod) *topologyPlan {
	view := s.topologySnapshot()
	if view == nil {
		return nil
	}

	highest, mode, ok := topologyRequirement(task)
	if !ok {
		return nil
	}
	if mode == "" {
		mode = config.TopologyMode
	}
	if highest == unlimitedTier {
		highest = view.tree.MaxTier()
	}

	group := topologyGroupKey(task)
	if group == "" {
		return nil
	}
	return &topologyPlan{
		view:    view,
		group:   group,
		placed:  s.topologyGroupPlacement(task, group),
		mode:    mode,
		highest: highest,
	}
}

// applyTopologyFilter drops the candidate nodes that would place this pod
// beyond the tier its group allows, recording the reason on each of them. The
// first pod of a group is never rejected: with nothing placed yet there is no
// tier to violate, so a job lands somewhere and the rest of its pods follow it.
func (s *Scheduler) applyTopologyFilter(plan *topologyPlan, task *corev1.Pod, nodes *map[string]*NodeUsage, failedNodes map[string]string) {
	if plan == nil || plan.mode != util.TopologyModeHard || len(plan.placed) == 0 || nodes == nil {
		return
	}

	rejected := make([]string, 0, len(*nodes))
	for nodeID := range *nodes {
		violation := plan.view.tree.CheckCandidate(nodeID, plan.placed, plan.highest)
		if violation == nil {
			continue
		}
		delete(*nodes, nodeID)
		failedNodes[nodeID] = common.GenReason(map[string]int{common.HyperNodeTierNotFit: 1}, 1)
		rejected = append(rejected, nodeID)
		klog.V(4).InfoS("Topology rejects node", "pod", klog.KObj(task), "node", nodeID,
			"highestTierAllowed", plan.highest, "detail", violation.String())
	}
	if len(rejected) == 0 {
		return
	}

	klog.InfoS("Topology constraint rejected nodes", "pod", klog.KObj(task), "group", plan.group,
		"highestTierAllowed", plan.highest, "rejected", len(rejected), "remaining", len(*nodes))
	// Only a rejection that leaves nothing to schedule is worth an event: a
	// partial one is reported through the failed node reasons and the log, and
	// the pod is scheduled anyway.
	if len(*nodes) == 0 {
		s.recordFilteringFailures(task, map[string][]string{common.HyperNodeTierNotFit: rejected})
	}
}

// applyTopologyScore ranks the fitting nodes by how close they are to the nodes
// the group already occupies, so that a job stays inside the smallest domain
// that has room for it. The bonus follows the node policy's direction: binpack
// picks the highest score and spread the lowest, and both have to prefer the
// closer node for the group to stay together.
func (s *Scheduler) applyTopologyScore(plan *topologyPlan, task *corev1.Pod, nodeScores *policy.NodeScoreList) {
	if plan == nil || nodeScores == nil || len(plan.placed) == 0 {
		return
	}

	for _, nodeScore := range nodeScores.NodeList {
		locality := plan.view.tree.Locality(nodeScore.NodeID, plan.placed)
		if locality == 0 {
			continue
		}
		bonus := config.TopologyScoreWeight * float32(locality)
		if policy.NodePolicyUsesSpread(nodeScores.Policy) {
			bonus = -bonus
		}
		nodeScore.Score += bonus
		klog.V(4).InfoS("Topology score", "pod", klog.KObj(task), "node", nodeScore.NodeID,
			"group", plan.group, "locality", locality, "bonus", bonus)
	}
}
