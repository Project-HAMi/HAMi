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
	"k8s.io/component-helpers/scheduling/corev1/nodeaffinity"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/topology"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

const (
	// unlimitedTier is the highest tier a pod can ask for when it wants
	// topology aware ranking without a cap. It is resolved to the depth of the
	// configured hierarchy once the hierarchy is known.
	unlimitedTier = -1

	// maxTopologyGangSize bounds the capacity simulation performed for one
	// gang. Larger values are treated as malformed annotations so a typo cannot
	// turn one Filter call into an unbounded amount of device scoring.
	maxTopologyGangSize = 1024

	// maxTopologyGangSearchStates bounds the fallback search used when the
	// fast capacity match produces a concrete set that the device simulation
	// rejects. The fast path handles the ordinary case; the fallback only
	// explores alternative placements after a heterogeneous conflict.
	maxTopologyGangSearchStates = 65536
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
	// pending holds the group members that still wait for a node, in name
	// order. The gang filter scores each of them with its own device request,
	// so that a heterogeneous gang is not measured with the request of the Pod
	// being scheduled first.
	pending []*corev1.Pod
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

	rawMode := strings.ToLower(strings.TrimSpace(pod.Annotations[util.TopologyModeAnnotationKey]))
	mode = rawMode
	if mode != "" && mode != util.TopologyModeHard && mode != util.TopologyModeSoft {
		klog.ErrorS(nil, "Ignoring invalid NPU topology mode", "pod", klog.KObj(pod), "mode", mode)
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
func (s *Scheduler) topologyGroupPlacement(task *corev1.Pod, group string) ([]string, int, []*corev1.Pod) {
	placed := make(map[string]struct{})
	active := make(map[types.UID]string)
	pending := make(map[types.UID]*corev1.Pod)
	if s.podLister != nil {
		pods, err := s.podLister.Pods(task.Namespace).List(labels.Everything())
		if err != nil {
			klog.ErrorS(err, "Failed to list pods for NPU topology grouping", "namespace", task.Namespace)
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
				pending[pod.UID] = pod
				continue
			}
			active[pod.UID] = pod.Spec.NodeName
			placed[pod.Spec.NodeName] = struct{}{}
		}
	}
	for _, info := range s.podManager.ListPodsInfo() {
		if info.Pod == nil || info.Pod.UID == task.UID || info.Namespace != task.Namespace {
			continue
		}
		if topologyGroupKey(info.Pod) != group {
			continue
		}
		if info.NodeID == "" {
			if _, ok := active[info.Pod.UID]; !ok {
				pending[info.Pod.UID] = info.Pod
			}
			continue
		}
		delete(pending, info.Pod.UID)
		active[info.Pod.UID] = info.NodeID
		placed[info.NodeID] = struct{}{}
	}
	pendingPods := make([]*corev1.Pod, 0, len(pending))
	for _, pod := range pending {
		pendingPods = append(pendingPods, pod)
	}
	slices.SortFunc(pendingPods, func(a, b *corev1.Pod) int {
		return strings.Compare(a.Name, b.Name)
	})
	if len(placed) == 0 {
		return nil, 0, pendingPods
	}
	return slices.Sorted(maps.Keys(placed)), len(active), pendingPods
}

// topologyPlacedNodes is the node-only view retained for callers that do not
// need the gang member count.
func (s *Scheduler) topologyPlacedNodes(task *corev1.Pod, group string) []string {
	nodes, _, _ := s.topologyGroupPlacement(task, group)
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
	placed, placedMembers, pending := s.topologyGroupPlacement(task, group)
	return &topologyPlan{
		view:          view,
		group:         group,
		placed:        placed,
		mode:          mode,
		highest:       highest,
		gangSize:      topologyGangSize(task),
		placedMembers: placedMembers,
		pending:       pending,
	}
}

// topologyGangSize reads the optional gang size. A missing or malformed value,
// including one above maxTopologyGangSize, keeps the previous incremental
// behavior instead of turning a typo into a cluster-wide scheduling outage or
// an unbounded capacity simulation.
func topologyGangSize(task *corev1.Pod) int {
	if task == nil {
		return 1
	}
	raw := strings.TrimSpace(task.Annotations[util.TopologyGangSizeAnnotationKey])
	if raw == "" {
		return 1
	}
	size, err := strconv.Atoi(raw)
	if err != nil || size < 1 || size > maxTopologyGangSize {
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
// holds every member already placed. A placed member outside the model, or one
// whose chain stops below level, cannot belong to that domain, so the domain is
// not a candidate.
func topologyGangDomainContainsPlaced(tree *topology.Tree, domain *topology.HyperNode, level int, placed []string) bool {
	for _, placedNode := range placed {
		// A gang anchored on a node outside the model has no domain that can
		// be proven to hold it. Fail closed instead of treating the node's own
		// tier 0 domain as a fit, which would let an unlabeled member anchor a
		// gang anywhere in the cluster.
		if !tree.InModel(placedNode) {
			return false
		}
		chain := tree.Chain(placedNode)
		if level >= len(chain) || chain[level] != domain {
			return false
		}
	}
	return true
}

// topologyGangMember is one member of a gang that still needs a node, with
// everything the capacity simulation needs to score it exactly as Filter will:
// its own device request, node policy, preferred node affinity and scoring weights.
type topologyGangMember struct {
	pod          *corev1.Pod
	resourceReqs device.PodDeviceRequests
	nodePolicy   string
	nodeAffinity *policy.PreferredNodeAffinity
	weights      util.DeviceScoringWeights
	distinct     bool
}

// topologyGangMembers resolves one member per slot the gang still has to place.
// A gang is not required to be homogeneous, so every member is described by its
// own Pod and device.Resourcereqs result instead of the request of the Pod that
// happens to be scheduled first. Members the job controller has not created yet
// are represented by the current Pod, which keeps the check conservative while
// a gang is still being rolled out.
func (s *Scheduler) topologyGangMembers(task *corev1.Pod, resourceReqs device.PodDeviceRequests, pending []*corev1.Pod, remaining int) ([]topologyGangMember, bool) {
	members := make([]topologyGangMember, 0, remaining)
	add := func(pod *corev1.Pod, reqs device.PodDeviceRequests) bool {
		weights, err := util.GetDeviceScoringWeightsByPod(pod)
		if err != nil {
			klog.ErrorS(err, "NPU topology gang check skipped: invalid device scoring weights", "pod", klog.KObj(pod))
			return false
		}
		members = append(members, topologyGangMember{
			pod:          pod,
			resourceReqs: reqs,
			nodePolicy:   resolveNodeSchedulerPolicy(pod),
			nodeAffinity: resolvePreferredNodeAffinity(pod),
			weights:      weights,
			distinct:     topologyGangNeedsDistinctNodes(pod),
		})
		return true
	}
	if remaining <= 0 || !add(task, resourceReqs) {
		return nil, false
	}
	for _, pod := range pending {
		if len(members) >= remaining {
			break
		}
		if pod == nil || pod.UID == task.UID {
			continue
		}
		reqs, err := device.Resourcereqs(pod)
		if err != nil {
			klog.ErrorS(err, "NPU topology gang check skipped: cannot read a gang member's device request", "pod", klog.KObj(pod))
			return nil, false
		}
		if !add(pod, reqs) {
			return nil, false
		}
	}
	for len(members) < remaining {
		if !add(task, resourceReqs) {
			return nil, false
		}
	}
	return members, true
}

// topologyGangNodeFit describes two different properties of one node:
// admits lists the members that fit on the node on their own, while capacity
// is how many members fit together when their allocations share the node.
// Keeping them separate matters for a heterogeneous gang: a 40-core member and
// an 80-core sibling can each fit on either of two 100-core nodes, but not both
// on one node. Matching must be able to put them on separate nodes even though
// the greedy simulation records only one shared-node slot.
type topologyGangNodeFit struct {
	admits   []int
	capacity int
}

// topologyGangMemberMatchesNode applies a sibling Pod runner's required
// node affinity and node selector to a candidate node. scoreNode does not
// evaluate these fields because the apiserver normally prefilters the Pod
// being scheduled; a pending sibling has not gone through that path yet.
func topologyGangMemberMatchesNode(pod *corev1.Pod, node *corev1.Node) bool {
	if pod == nil {
		return false
	}
	if node == nil {
		required := pod.Spec.Affinity
		return len(pod.Spec.NodeSelector) == 0 &&
			(required == nil || required.NodeAffinity == nil ||
				required.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil)
	}
	match, err := nodeaffinity.GetRequiredNodeAffinity(pod).Match(node)
	return err == nil && match
}

// topologyGangNodeAssignment computes the fit information for one node. Each
// member is scored with its own request and Pod context, exactly as Filter will
// score it, on private deep copies so the scheduler's live node cache is never
// modified. The admission pass scores each member against fresh usage; the
// capacity pass then greedily shares one simulated copy. Failed fits are
// remembered per Pod: slots the job controller has not created yet reuse the
// current Pod, and repeating the same failed placeholder cannot make it fit.
func (s *Scheduler) topologyGangNodeAssignment(nodeID string, usage *NodeUsage, members []topologyGangMember, placed []string) topologyGangNodeFit {
	if len(members) == 0 || usage == nil {
		return topologyGangNodeFit{}
	}
	limit := len(members)
	distinct := false
	for i := range members {
		if members[i].distinct {
			distinct = true
			break
		}
	}
	if distinct {
		if slices.Contains(placed, nodeID) {
			return topologyGangNodeFit{}
		}
		// A hostname anti-affinity keeps every sibling off this node too.
		limit = 1
	}

	fit := topologyGangNodeFit{admits: make([]int, 0, len(members))}
	admitted := make([]bool, len(members))
	fitsAlone := make(map[*corev1.Pod]bool, len(members))
	for i := range members {
		member := &members[i]
		if !topologyGangMemberMatchesNode(member.pod, usage.Node) {
			continue
		}
		if len(member.resourceReqs) == 0 {
			fit.admits = append(fit.admits, i)
			admitted[i] = true
			continue
		}
		canFit, seen := fitsAlone[member.pod]
		if !seen {
			result := s.scoreNode(nodeID, usage.DeepCopy(), member.resourceReqs, member.pod, member.nodePolicy, member.nodeAffinity, member.weights)
			canFit = result.err == nil && result.score != nil
			fitsAlone[member.pod] = canFit
		}
		if canFit {
			fit.admits = append(fit.admits, i)
			admitted[i] = true
		}
	}
	if distinct {
		if len(fit.admits) > 0 {
			fit.capacity = 1
		}
		return fit
	}

	simulated := usage.DeepCopy()
	failed := make(map[*corev1.Pod]struct{}, len(members))
	for i := range members {
		if fit.capacity >= limit {
			break
		}
		member := &members[i]
		if len(member.resourceReqs) == 0 {
			// A member that asks for no HAMi device still takes a slot but
			// cannot make the node smaller.
			fit.capacity++
			continue
		}
		if !admitted[i] {
			failed[member.pod] = struct{}{}
			continue
		}
		if _, ok := failed[member.pod]; ok {
			continue
		}
		result := s.scoreNode(nodeID, simulated, member.resourceReqs, member.pod, member.nodePolicy, member.nodeAffinity, member.weights)
		if result.err != nil || result.score == nil {
			failed[member.pod] = struct{}{}
			continue
		}
		fit.capacity++
		// scoreNode applies the allocation to simulated through applyPeakUsage,
		// so the next member already sees the reduced capacity.
	}
	return fit
}

// topologyGangNodeSetFits re-simulates one concrete assignment on a node. The
// greedy capacity count is useful for pruning, but a heterogeneous match can
// assign a different subset whose combined request does not fit. Verifying the
// actual set keeps hard mode all-or-nothing.
func (s *Scheduler) topologyGangNodeSetFits(nodeID string, usage *NodeUsage, members []topologyGangMember, assigned []int) bool {
	if usage == nil {
		return false
	}
	seen := make(map[int]struct{}, len(assigned))
	for _, index := range assigned {
		if index < 0 || index >= len(members) {
			return false
		}
		if !topologyGangMemberMatchesNode(members[index].pod, usage.Node) {
			return false
		}
		if _, duplicate := seen[index]; duplicate {
			return false
		}
		seen[index] = struct{}{}
		if len(assigned) > 1 && members[index].distinct {
			return false
		}
	}
	simulated := usage.DeepCopy()
	for _, index := range assigned {
		member := &members[index]
		if len(member.resourceReqs) == 0 {
			continue
		}
		result := s.scoreNode(nodeID, simulated, member.resourceReqs, member.pod, member.nodePolicy, member.nodeAffinity, member.weights)
		if result.err != nil || result.score == nil {
			return false
		}
	}
	return true
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
	members, ok := s.topologyGangMembers(task, resourceReqs, plan.pending, remaining)
	if !ok {
		return
	}

	assignments := make(map[string]topologyGangNodeFit, len(*nodes))
	for nodeID, usage := range *nodes {
		assignments[nodeID] = s.topologyGangNodeAssignment(nodeID, usage, members, plan.placed)
	}

	feasible, fittingDomains := topologyGangFeasibleNodes(tree, *nodes, assignments, plan.placed, plan.highest, remaining,
		func(nodeID string, assigned []int) bool {
			return s.topologyGangNodeSetFits(nodeID, (*nodes)[nodeID], members, assigned)
		})
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
// least one domain which contains every placed member and can assign every
// remaining member to a node in that domain. It is the pure core of the
// all-or-nothing check: assignments are supplied by the caller so that the
// domain arithmetic can be tested without a device inventory.
//
// Member 0 is the Pod currently being scheduled, so a candidate is retained
// only when pinning member 0 to that candidate still leaves a complete
// assignment for members 1..remaining-1. Summing per-node counts is not enough:
// two nodes that can each host only member 0 would otherwise appear to have
// room for a two-member gang even though member 1 fits nowhere.
//
// Levels are visited from the node upwards, but a domain of a deeper level is
// not automatically preferred: the result is a union, and a node survives when
// any allowed domain can hold the gang. The hard tier filter that runs next
// still rejects a node that is too far from the members already placed.
func topologyGangFeasibleNodes(tree *topology.Tree, candidates map[string]*NodeUsage, assignments map[string]topologyGangNodeFit, placed []string, highest, remaining int, verifiers ...func(string, []int) bool) (map[string]struct{}, []string) {
	feasible := make(map[string]struct{})
	var fittingDomains []string
	if tree == nil || remaining <= 0 {
		return feasible, nil
	}
	var verify func(string, []int) bool
	if len(verifiers) > 0 {
		inner := verifiers[0]
		cache := make(map[string]bool)
		verify = func(nodeID string, members []int) bool {
			key := topologyGangMemberSetKey(nodeID, members)
			if result, seen := cache[key]; seen {
				return result
			}
			result := inner(nodeID, members)
			cache[key] = result
			return result
		}
	}
	// One budget covers the unpinned check and every anchor in this filter
	// call, so a wide domain cannot multiply the fallback search by its nodes.
	budget := &topologyGangSearchBudget{}
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
		for domain, domainNodes := range domains {
			if !topologyGangDomainContainsPlaced(tree, domain, level, placed) {
				continue
			}
			domainFits := topologyGangBuildDomainFits(assignments, domainNodes, remaining)
			// An unpinned match is a necessary condition: when even the
			// whole domain cannot place every member, no anchor in it can.
			if _, ok := topologyGangDomainCanAssignWithin(domainNodes, domainFits, "", remaining, budget, verify); !ok {
				continue
			}
			hasAnchor := false
			for _, nodeID := range domainNodes {
				if !domainFits[nodeID].admits[0] {
					continue
				}
				if _, ok := topologyGangDomainCanAssignWithin(domainNodes, domainFits, nodeID, remaining, budget, verify); !ok {
					continue
				}
				feasible[nodeID] = struct{}{}
				hasAnchor = true
			}
			if hasAnchor {
				fittingDomains = append(fittingDomains, domain.String())
			}
		}
	}
	return feasible, fittingDomains
}

// topologyGangDomainFit is the per-domain form of topologyGangNodeFit. The
// admission flags are built once per domain so matching does not repeatedly
// scan the original member index slices.
type topologyGangDomainFit struct {
	admits   []bool
	capacity int
}

func topologyGangBuildDomainFits(assignments map[string]topologyGangNodeFit, nodes []string, members int) map[string]topologyGangDomainFit {
	fits := make(map[string]topologyGangDomainFit, len(nodes))
	for _, nodeID := range nodes {
		nodeFit := assignments[nodeID]
		admits := make([]bool, members)
		for _, member := range nodeFit.admits {
			if member >= 0 && member < members {
				admits[member] = true
			}
		}
		fits[nodeID] = topologyGangDomainFit{admits: admits, capacity: nodeFit.capacity}
	}
	return fits
}

// topologyGangAssignmentFits re-simulates every node assignment produced by the
// capacity matching. capacity is only a greedy count, so a heterogeneous match
// must not be accepted until its concrete member set has been checked.
func topologyGangAssignmentFits(assigned map[string][]int, verify func(string, []int) bool) bool {
	for nodeID, members := range assigned {
		if !verify(nodeID, members) {
			return false
		}
	}
	return true
}

// topologyGangDomainCanAssign returns the member assignment for a domain. An
// empty pinnedNode performs the unpinned feasibility check. A non-empty one
// reserves member 0 on that node first, then matches the remaining members.
// The fast matcher is a capacity-only bipartite match. If its concrete
// assignment does not pass the optional device verification, search alternative
// placements for the same anchor rather than rejecting a domain that can still
// hold the gang in a different partition.
func topologyGangDomainCanAssign(nodes []string, fits map[string]topologyGangDomainFit, pinnedNode string, remaining int, verifiers ...func(string, []int) bool) (map[string][]int, bool) {
	return topologyGangDomainCanAssignWithin(nodes, fits, pinnedNode, remaining, nil, verifiers...)
}

// topologyGangDomainCanAssignWithin is topologyGangDomainCanAssign with a
// caller-owned search budget. topologyGangFeasibleNodes shares one budget
// across all domains and anchors of a filter call.
func topologyGangDomainCanAssignWithin(nodes []string, fits map[string]topologyGangDomainFit, pinnedNode string, remaining int, budget *topologyGangSearchBudget, verifiers ...func(string, []int) bool) (map[string][]int, bool) {
	var verify func(string, []int) bool
	if len(verifiers) > 0 {
		verify = verifiers[0]
	}
	if budget == nil {
		budget = &topologyGangSearchBudget{}
	}
	assigned, matched := topologyGangDomainMatch(nodes, fits, pinnedNode, remaining)
	if matched && (verify == nil || topologyGangAssignmentFits(assigned, verify)) {
		return assigned, true
	}
	if verify == nil {
		return nil, false
	}
	// The fast matcher uses a greedy per-node capacity count, which is only
	// a lower bound for heterogeneous members. A capacity miss can therefore
	// still have a valid partition; let the verified search decide.
	return topologyGangDomainSearch(nodes, fits, pinnedNode, remaining, verify, budget)
}

// topologyGangDomainMatch is the fast capacity matcher. Each node may host at
// most the number of members its shared-usage simulation admitted, and an
// augmenting path lets a member displace another member from a node that is
// already full. Member 0 is pinned to its anchor and cannot be displaced.
func topologyGangDomainMatch(nodes []string, fits map[string]topologyGangDomainFit, pinnedNode string, remaining int) (map[string][]int, bool) {
	if remaining <= 0 {
		return nil, false
	}
	assigned := make(map[string][]int, len(nodes))
	capacity := make(map[string]int, len(nodes))
	for _, nodeID := range nodes {
		capacity[nodeID] = fits[nodeID].capacity
	}
	start := 0
	if pinnedNode != "" {
		pinned, ok := fits[pinnedNode]
		if !ok || pinned.capacity == 0 || !pinned.admits[0] {
			return nil, false
		}
		assigned[pinnedNode] = []int{0}
		start = 1
	}
	if remaining == 1 && pinnedNode != "" {
		return assigned, true
	}
	var assign func(member int, visited []bool) bool
	assign = func(member int, visited []bool) bool {
		for index, nodeID := range nodes {
			nodeFit := fits[nodeID]
			if visited[index] || member >= len(nodeFit.admits) || !nodeFit.admits[member] {
				continue
			}
			visited[index] = true
			if len(assigned[nodeID]) < capacity[nodeID] {
				assigned[nodeID] = append(assigned[nodeID], member)
				return true
			}
			for _, displaced := range slices.Clone(assigned[nodeID]) {
				if pinnedNode != "" && displaced == 0 {
					// Member 0 is the current Pod. It must stay on the anchor.
					continue
				}
				if !assign(displaced, visited) {
					continue
				}
				assigned[nodeID] = slices.DeleteFunc(assigned[nodeID], func(existing int) bool {
					return existing == displaced
				})
				assigned[nodeID] = append(assigned[nodeID], member)
				return true
			}
		}
		return false
	}
	for member := start; member < remaining; member++ {
		if !assign(member, make([]bool, len(nodes))) {
			return nil, false
		}
	}
	return assigned, true
}

// topologyGangDomainSearch explores alternative member-to-node assignments
// after concrete device verification rejects the fast match. Members are tried
// fewest-candidates-first to prune early, while each node set is verified in
// member-index order because that is the order the scheduler will place them.
func topologyGangDomainSearch(nodes []string, fits map[string]topologyGangDomainFit, pinnedNode string, remaining int, verify func(string, []int) bool, budget *topologyGangSearchBudget) (map[string][]int, bool) {
	if remaining <= 0 {
		return nil, false
	}
	if budget == nil {
		budget = &topologyGangSearchBudget{}
	}
	assigned := make(map[string][]int, len(nodes))
	start := 0
	if pinnedNode != "" {
		pinned, ok := fits[pinnedNode]
		if !ok || pinned.capacity == 0 || !pinned.admits[0] {
			return nil, false
		}
		assigned[pinnedNode] = []int{0}
		if !verify(pinnedNode, assigned[pinnedNode]) {
			return nil, false
		}
		start = 1
	}
	if start >= remaining {
		return assigned, true
	}

	type memberChoice struct {
		member int
		nodes  []string
	}
	choices := make([]memberChoice, 0, remaining-start)
	for member := start; member < remaining; member++ {
		candidateNodes := make([]string, 0, len(nodes))
		for _, nodeID := range nodes {
			nodeFit := fits[nodeID]
			if nodeFit.capacity > 0 && member < len(nodeFit.admits) && nodeFit.admits[member] {
				candidateNodes = append(candidateNodes, nodeID)
			}
		}
		if len(candidateNodes) == 0 {
			return nil, false
		}
		choices = append(choices, memberChoice{member: member, nodes: candidateNodes})
	}
	slices.SortStableFunc(choices, func(a, b memberChoice) int {
		if len(a.nodes) != len(b.nodes) {
			return len(a.nodes) - len(b.nodes)
		}
		return a.member - b.member
	})

	maxMembers := make(map[string]int, len(nodes))
	for _, nodeID := range nodes {
		maxMembers[nodeID] = topologyGangAdmittedCount(fits[nodeID])
	}

	var search func(index int) bool
	search = func(index int) bool {
		if index == len(choices) {
			return true
		}
		choice := choices[index]
		for _, nodeID := range choice.nodes {
			// capacity is a greedy lower bound, not an upper bound. The
			// verified search must be able to place a larger heterogeneous
			// subset, so bound by the number of members that node admits.
			if len(assigned[nodeID]) >= maxMembers[nodeID] {
				continue
			}
			if !budget.spend() {
				return false
			}
			previous := assigned[nodeID]
			next := append(previous[:len(previous):len(previous)], choice.member)
			slices.Sort(next)
			if !verify(nodeID, next) {
				continue
			}
			assigned[nodeID] = next
			if search(index + 1) {
				return true
			}
			assigned[nodeID] = previous
		}
		return false
	}
	if !search(0) {
		return nil, false
	}
	return assigned, true
}

// topologyGangSearchBudget bounds the fallback searches of one gang filter
// call. Sharing it keeps a wide domain from multiplying the exponential search
// by its number of anchors. Once exhausted the search fails closed.
type topologyGangSearchBudget struct {
	attempts int
}

func (b *topologyGangSearchBudget) spend() bool {
	b.attempts++
	return b.attempts <= maxTopologyGangSearchStates
}

// topologyGangMemberSetKey canonicalizes a node and member set for the verifier
// cache, so repeated partitions reached from another anchor reuse the result.
func topologyGangMemberSetKey(nodeID string, members []int) string {
	sorted := slices.Clone(members)
	slices.Sort(sorted)
	var key strings.Builder
	key.WriteString(nodeID)
	for _, member := range sorted {
		key.WriteByte('|')
		key.WriteString(strconv.Itoa(member))
	}
	return key.String()
}

func topologyGangAdmittedCount(fit topologyGangDomainFit) int {
	count := 0
	for _, admitted := range fit.admits {
		if admitted {
			count++
		}
	}
	return count
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
