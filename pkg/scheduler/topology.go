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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/topology"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

// unlimitedTier is the highest tier a pod can ask for when it wants topology
// aware ranking without a cap. It is resolved to the depth of the configured
// hierarchy once the hierarchy is known.
const unlimitedTier = -1

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

// topologyView is one immutable snapshot of the cluster's NPU interconnect: the
// hierarchy built from the node labels and annotations of the moment, plus the
// tier specification it was built from.
type topologyView struct {
	tree  *topology.Tree
	tiers []topology.Tier
}

// topologyPlan is what one Filter call needs to apply the topology rules to one
// pod: the hierarchy, the group the pod belongs to, and where that group is.
type topologyPlan struct {
	view    *topologyView
	group   string
	placed  []string
	mode    string
	highest int
	// gangSize is the number of Pods that must be placed together. It is 1
	// unless the Pod opts in with the gang size annotation, in which case the
	// hard filter proves that one allowed domain can hold the whole gang.
	gangSize int
	// placedMembers counts the members of the group already bound or reserved
	// by this scheduler. A count rather than a node count matters when a node
	// hosts more than one member of the group.
	placedMembers int
}

// rebuildTopology replaces the cached hierarchy with one built from nodes. It is
// called from register, which already holds the scheduler lock and has already
// listed the nodes, so a relabeled fabric is picked up on the next register tick
// without any additional watch.
func (s *Scheduler) rebuildTopology(nodes []*corev1.Node) {
	tiers, err := topology.ParseTiers(config.TopologyTierLabels)
	if err != nil {
		klog.ErrorS(err, "Ignoring invalid NPU topology tier labels", "tierLabels", config.TopologyTierLabels)
		s.topology.Store(nil)
		return
	}
	if len(tiers) == 0 {
		s.topology.Store(nil)
		return
	}
	s.topology.Store(&topologyView{tree: topology.Build(nodes, tiers), tiers: tiers})
}

// onUpdateNode refreshes the cached hierarchy when a node was moved in the
// fabric. A node reaches the scheduler before the device plugin has read the
// super-pod membership of its NPUs, so its place in the hierarchy is only known
// after the annotation has been written: without this the update would have to
// wait for the next register tick.
//
// The notification is filtered on the tier values. The kubelet rewrites the
// status of every node every few seconds, and the device plugin rewrites its own
// annotations on its own schedule, so notifying on any update at all would turn
// the coalesced register tick into a loop that re-reads the cluster for changes
// that cannot affect the hierarchy.
func (s *Scheduler) onUpdateNode(oldObj, newObj any) {
	oldNode, ok := oldObj.(*corev1.Node)
	if !ok {
		return
	}
	newNode, ok := newObj.(*corev1.Node)
	if !ok {
		return
	}

	view := s.topologySnapshot()
	if view == nil {
		return
	}
	for _, tier := range view.tiers {
		if tier.Value(oldNode) == tier.Value(newNode) {
			continue
		}
		klog.InfoS("Node NPU topology changed, refreshing the hierarchy",
			"node", newNode.Name, "tier", tier.String(),
			"from", tier.Value(oldNode), "to", tier.Value(newNode))
		s.doNodeNotify()
		return
	}
}

// topologySnapshot returns the cached hierarchy, nil when the feature is off.
func (s *Scheduler) topologySnapshot() *topologyView {
	return s.topology.Load()
}

// topologyRequirement reads the per-pod topology requirement. The feature is
// opt-in per pod: a pod that carries neither annotation keeps the placement
// behavior it had before, so enabling the tier labels flag on a shared cluster
// does not change how unrelated workloads are scheduled.
func topologyRequirement(pod *corev1.Pod) (highest int, mode string, ok bool) {
	if pod == nil {
		return 0, "", false
	}

	mode = strings.ToLower(strings.TrimSpace(pod.Annotations[util.TopologyModeAnnotationKey]))
	if mode != "" && mode != util.TopologyModeHard && mode != util.TopologyModeSoft {
		klog.ErrorS(nil, "Ignoring invalid NPU topology mode", "pod", klog.KObj(pod), "mode", mode)
		mode = ""
	}

	raw := strings.TrimSpace(pod.Annotations[util.TopologyHighestTierAnnotationKey])
	if raw == "" {
		// A mode without a tier asks for ranking only: nothing is rejected.
		return unlimitedTier, mode, mode != ""
	}

	tier, err := strconv.Atoi(raw)
	if err != nil || tier < 0 {
		klog.ErrorS(nil, "Ignoring invalid NPU topology tier", "pod", klog.KObj(pod), "tier", raw)
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

// topologyGroupPlacement collects the nodes and the number of Pods the other
// members of group already occupy. Reservations made by this scheduler count
// too: a Pod of a gang placed in this same scheduling round has to constrain
// its siblings before the API server has observed it as bound. UIDs deduplicate
// the informer and the reservation cache, which both know about a Pod after it
// has been observed.
func (s *Scheduler) topologyGroupPlacement(task *corev1.Pod, group string) ([]string, int) {
	placed := make(map[string]struct{})
	active := make(map[types.UID]string)
	if s.podLister != nil {
		pods, err := s.podLister.Pods(task.Namespace).List(labels.Everything())
		if err != nil {
			klog.ErrorS(err, "Failed to list pods for NPU topology grouping", "namespace", task.Namespace)
		}
		for _, pod := range pods {
			if pod.UID == task.UID || pod.Spec.NodeName == "" || pod.DeletionTimestamp != nil {
				continue
			}
			if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
				continue
			}
			if topologyGroupKey(pod) != group {
				continue
			}
			active[pod.UID] = pod.Spec.NodeName
			placed[pod.Spec.NodeName] = struct{}{}
		}
	}
	for _, info := range s.podManager.ListPodsInfo() {
		if info.Pod == nil || info.Pod.UID == task.UID || info.NodeID == "" || info.Namespace != task.Namespace {
			continue
		}
		if topologyGroupKey(info.Pod) != group {
			continue
		}
		active[info.Pod.UID] = info.NodeID
		placed[info.NodeID] = struct{}{}
	}
	if len(placed) == 0 {
		return nil, 0
	}
	return slices.Sorted(maps.Keys(placed)), len(active)
}

// topologyPlacedNodes is the node-only view retained for callers that do not
// need the gang member count.
func (s *Scheduler) topologyPlacedNodes(task *corev1.Pod, group string) []string {
	nodes, _ := s.topologyGroupPlacement(task, group)
	return nodes
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
	placed, placedMembers := s.topologyGroupPlacement(task, group)
	return &topologyPlan{
		view:          view,
		group:         group,
		placed:        placed,
		mode:          mode,
		highest:       highest,
		gangSize:      topologyGangSize(task),
		placedMembers: placedMembers,
	}
}

// topologyGangSize reads the optional gang size. A missing or malformed value
// keeps the previous incremental behavior instead of turning a typo into a
// cluster-wide scheduling outage.
func topologyGangSize(task *corev1.Pod) int {
	if task == nil {
		return 1
	}
	raw := strings.TrimSpace(task.Annotations[util.TopologyGangSizeAnnotationKey])
	if raw == "" {
		return 1
	}
	size, err := strconv.Atoi(raw)
	if err != nil || size < 1 {
		klog.ErrorS(nil, "Ignoring invalid NPU topology gang size", "pod", klog.KObj(task), "gangSize", raw)
		return 1
	}
	return size
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
		klog.V(4).InfoS("NPU topology rejects node", "pod", klog.KObj(task), "node", nodeID,
			"highestTierAllowed", plan.highest, "detail", violation.String())
	}
	if len(rejected) == 0 {
		return
	}

	klog.InfoS("NPU topology constraint rejected nodes", "pod", klog.KObj(task), "group", plan.group,
		"highestTierAllowed", plan.highest, "rejected", len(rejected), "remaining", len(*nodes))
	// Only a rejection that leaves nothing to schedule is worth an event: a
	// partial one is reported through the failed node reasons and the log, and
	// the pod is scheduled anyway.
	if len(*nodes) == 0 {
		s.recordFilteringFailures(task, map[string][]string{common.HyperNodeTierNotFit: rejected})
	}
}

// topologyGangNeedsDistinctNodes reports whether the pod's required pod
// anti-affinity makes every member of the gang need a node of its own. The
// capacity check has to know this: scoring a node more than once would
// otherwise claim that two gang members fit on one node even though Kubernetes
// will reject the second one.
func topologyGangNeedsDistinctNodes(task *corev1.Pod) bool {
	if task == nil || task.Spec.Affinity == nil || task.Spec.Affinity.PodAntiAffinity == nil {
		return false
	}
	own := labels.Set(task.Labels)
	for _, term := range task.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution {
		if term.TopologyKey != corev1.LabelHostname || term.LabelSelector == nil {
			continue
		}
		selector, err := metav1.LabelSelectorAsSelector(term.LabelSelector)
		if err != nil {
			continue
		}
		if selector.Matches(own) {
			return true
		}
	}
	return false
}

// topologyGangDomainContainsPlaced reports whether domain is the domain that
// holds every member already placed. A placed member whose chain stops below
// level cannot belong to that domain, so the domain is not a candidate.
func topologyGangDomainContainsPlaced(tree *topology.Tree, domain *topology.HyperNode, level int, placed []string) bool {
	for _, placedNode := range placed {
		chain := tree.Chain(placedNode)
		if level >= len(chain) || chain[level] != domain {
			return false
		}
	}
	return true
}

// topologyConsumeAllocation replays the device allocation scoreNode computed
// for one simulated pod on the copied NodeUsage. AddResourceUsage is the same
// hook the real allocation path uses, so vendor-specific accounting (for
// example Ascend's per-card Used counter) stays consistent with Filter.
func topologyConsumeAllocation(usage *NodeUsage, task *corev1.Pod, allocations device.PodDevices) bool {
	changed := false
	for devType, containers := range allocations {
		plugin, ok := device.GetDevices()[devType]
		if !ok {
			continue
		}
		for _, containerDevices := range containers {
			for i := range containerDevices {
				allocated := containerDevices[i]
				if allocated.UUID == "" {
					continue
				}
				var target *device.DeviceUsage
				for _, deviceList := range usage.Devices.DeviceLists {
					if deviceList.Device != nil && deviceList.Device.ID == allocated.UUID {
						target = deviceList.Device
						break
					}
				}
				if target == nil {
					continue
				}
				beforeUsed, beforeCores, beforeMem := target.Used, target.Usedcores, target.Usedmem
				if err := plugin.AddResourceUsage(task, target, &allocated); err != nil {
					return changed
				}
				if target.Used != beforeUsed || target.Usedcores != beforeCores || target.Usedmem != beforeMem {
					changed = true
				}
			}
		}
	}
	return changed
}

// topologyGangNodeCapacity returns how many additional members of the gang one
// node can hold. It simulates the same scoring path Filter uses on a private
// deep copy, so device geometry, NUMA rules and vendor accounting are resolved
// exactly as they will be at bind time, and the scheduler's live node cache is
// never modified.
func (s *Scheduler) topologyGangNodeCapacity(nodeID string, usage *NodeUsage, resourceReqs device.PodDeviceRequests, task *corev1.Pod, userNodePolicy string, weights util.DeviceScoringWeights, limit int, distinct bool, placed []string) int {
	if limit <= 0 || usage == nil {
		return 0
	}
	if distinct {
		if slices.Contains(placed, nodeID) {
			return 0
		}
		limit = 1
	}
	simulated := usage.DeepCopy()
	capacity := 0
	for capacity < limit {
		result := s.scoreNode(nodeID, simulated, resourceReqs, task, userNodePolicy, weights)
		if result.err != nil || result.score == nil {
			break
		}
		capacity++
		if !topologyConsumeAllocation(simulated, task, result.score.Devices) {
			break
		}
	}
	return capacity
}

// applyTopologyGangFilter enforces all-or-nothing placement for a gang whose
// members must share one allowed topology domain. The incremental hard filter
// only looks at members that are already placed, so without this check the
// first Pod of a gang binds before anyone knows whether the rest can follow it.
// When no domain of tier <= highest can hold every remaining member, every
// candidate is rejected, which leaves the whole gang Pending instead of
// splitting it into Running and Pending members.
//
// A node is kept only when it belongs to at least one domain that contains all
// placed members and has enough remaining capacity for the whole gang. That is
// what stops the first Pod from landing in a domain that looks locally free but
// cannot hold the gang, for example a lone node in another rack when only one
// rack has room for both members.
func (s *Scheduler) applyTopologyGangFilter(plan *topologyPlan, task *corev1.Pod, resourceReqs device.PodDeviceRequests, nodes *map[string]*NodeUsage, failedNodes map[string]string) {
	if plan == nil || plan.mode != util.TopologyModeHard || plan.gangSize <= 1 || nodes == nil || len(*nodes) == 0 {
		return
	}
	remaining := plan.gangSize - plan.placedMembers
	if remaining <= 1 {
		// The incremental filter already checks the last member against the
		// placed ones; a single member has no group-wide choice left to make.
		return
	}
	tree := plan.view.tree
	if tree == nil {
		return
	}
	for _, placedNode := range plan.placed {
		if !tree.InModel(placedNode) {
			// The model deliberately leaves unlabeled nodes unconstrained. A
			// gang that already has a member there cannot be proven feasible,
			// so keep the legacy behavior instead of guessing a domain.
			klog.V(4).InfoS("NPU topology gang check skipped: placed member is outside the topology model",
				"pod", klog.KObj(task), "group", plan.group, "node", placedNode)
			return
		}
	}

	userNodePolicy := resolveNodeSchedulerPolicy(task)
	weights, err := util.GetDeviceScoringWeightsByPod(task)
	if err != nil {
		klog.ErrorS(err, "NPU topology gang check skipped: invalid device scoring weights", "pod", klog.KObj(task))
		return
	}
	distinctNodes := topologyGangNeedsDistinctNodes(task)

	capacity := make(map[string]int, len(*nodes))
	for nodeID, usage := range *nodes {
		capacity[nodeID] = s.topologyGangNodeCapacity(nodeID, usage, resourceReqs, task, userNodePolicy, weights, remaining, distinctNodes, plan.placed)
	}

	feasible, fittingDomains := topologyGangFeasibleNodes(tree, *nodes, capacity, plan.placed, plan.highest, remaining)
	for _, domain := range fittingDomains {
		klog.V(4).InfoS("NPU topology gang domain fits", "pod", klog.KObj(task), "group", plan.group,
			"domain", domain, "remaining", remaining)
	}

	rejected := make([]string, 0, len(*nodes))
	for nodeID := range *nodes {
		if _, ok := feasible[nodeID]; ok {
			continue
		}
		delete(*nodes, nodeID)
		failedNodes[nodeID] = common.GenReason(map[string]int{common.GangTopologyNotFit: 1}, 1)
		rejected = append(rejected, nodeID)
		klog.V(4).InfoS("NPU topology gang rejects node", "pod", klog.KObj(task), "node", nodeID,
			"highestTierAllowed", plan.highest, "gangSize", plan.gangSize, "remaining", remaining)
	}
	if len(rejected) == 0 {
		return
	}
	klog.InfoS("NPU topology gang constraint rejected nodes", "pod", klog.KObj(task), "group", plan.group,
		"highestTierAllowed", plan.highest, "gangSize", plan.gangSize, "remaining", remaining,
		"rejected", len(rejected), "remainingNodes", len(*nodes))
	// Only a rejection that leaves nothing to schedule is worth an event. The
	// all-or-nothing guarantee itself comes from the empty candidate set: the
	// caller then returns FailedNodes and reserves nothing.
	if len(*nodes) == 0 {
		s.recordFilteringFailures(task, map[string][]string{common.GangTopologyNotFit: rejected})
	}
}

// topologyGangFeasibleNodes returns the candidate nodes that belong to at
// least one domain which contains every placed member and whose summed
// remaining capacity holds the rest of the gang. It is the pure core of the
// all-or-nothing check: capacity is supplied by the caller so that the
// domain arithmetic can be tested without a device inventory.
//
// Levels are visited from the node upwards, but a domain of a deeper level is
// not automatically preferred: the result is a union, and a node survives when
// any allowed domain can hold the gang. The hard tier filter that runs next
// still rejects a node that is too far from the members already placed.
func topologyGangFeasibleNodes(tree *topology.Tree, candidates map[string]*NodeUsage, capacity map[string]int, placed []string, highest, remaining int) (map[string]struct{}, []string) {
	feasible := make(map[string]struct{})
	var fittingDomains []string
	if tree == nil || remaining <= 0 {
		return feasible, nil
	}
	highest = min(highest, tree.MaxTier())
	for level := 0; level <= highest; level++ {
		domains := make(map[*topology.HyperNode][]string)
		for nodeID := range candidates {
			chain := tree.Chain(nodeID)
			if level >= len(chain) || chain[level] == nil {
				continue
			}
			domains[chain[level]] = append(domains[chain[level]], nodeID)
		}
		for domain, members := range domains {
			if !topologyGangDomainContainsPlaced(tree, domain, level, placed) {
				continue
			}
			total := 0
			for _, nodeID := range members {
				total += capacity[nodeID]
			}
			if total < remaining {
				continue
			}
			fittingDomains = append(fittingDomains, domain.String())
			for _, nodeID := range members {
				feasible[nodeID] = struct{}{}
			}
		}
	}
	return feasible, fittingDomains
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
		if nodeScores.Policy == util.NodeSchedulerPolicySpread.String() {
			bonus = -bonus
		}
		nodeScore.Score += bonus
		klog.V(4).InfoS("NPU topology score", "pod", klog.KObj(task), "node", nodeScore.NodeID,
			"group", plan.group, "locality", locality, "bonus", bonus)
	}
}
