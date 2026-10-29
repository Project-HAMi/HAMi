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

package policy

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/component-helpers/scheduling/corev1/nodeaffinity"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

type NodeScore struct {
	NodeID  string
	Node    *corev1.Node
	Devices device.PodDevices
	// Score recode every node all device user/allocate score
	Score float32
}

type NodeScoreList struct {
	NodeList []*NodeScore
	Policy   string
}

func (l NodeScoreList) Len() int {
	return len(l.NodeList)
}

func (l NodeScoreList) Swap(i, j int) {
	l.NodeList[i], l.NodeList[j] = l.NodeList[j], l.NodeList[i]
}

func (l NodeScoreList) Less(i, j int) bool {
	if l.Policy == util.NodeSchedulerPolicySpread.String() {
		return l.NodeList[i].Score > l.NodeList[j].Score
	}
	// default policy is Binpack
	return l.NodeList[i].Score < l.NodeList[j].Score
}

// policyNeutralScorer is an optional interface a device backend may implement to
// declare that its ScoreNode result is policy-independent and follows a "higher
// score is a better node" convention. Backends that implement it must not
// inspect the scheduler policy inside ScoreNode; instead OverrideScore weights
// the returned score and adapts it to the active scheduling policy (for example,
// inverting it under the Spread policy). This keeps policy handling in one place
// so new policies do not require backend-specific changes.
type policyNeutralScorer interface {
	PolicyNeutralScore()
}

func (ns *NodeScore) OverrideScore(previous []*device.DeviceUsage, policy string) {
	devScore := float32(0)
	for idx, val := range ns.Devices {
		dev, ok := device.GetDevices()[idx]
		if !ok || dev == nil {
			klog.V(4).Infof("Skip scoring for device type %s: not registered", idx)
			continue
		}

		score := dev.ScoreNode(ns.Node, val, previous, policy)

		// Backends implementing policyNeutralScorer return policy-independent
		// "higher is better" scores. For the Spread policy (which selects the
		// lowest score), we invert the score to preserve the ranking.
		// We also apply a weight of 10000 to ensure device scores dominate
		// the base node score.
		if _, ok := dev.(policyNeutralScorer); ok {
			weight := float32(10000)
			if policy == util.NodeSchedulerPolicySpread.String() {
				weight = -10000
			}
			score = weight * score
		}

		devScore += score
	}
	ns.Score += devScore
	klog.V(2).Infof("node %s default score is %f, computer override score is %f", ns.NodeID, ns.Score-devScore, ns.Score)
}

func (ns *NodeScore) SnapshotDevice(devices DeviceUsageList) []*device.DeviceUsage {
	snapshot := []*device.DeviceUsage{}
	for _, val := range devices.DeviceLists {
		tmp := *val.Device
		snapshot = append(snapshot, &tmp)
	}
	return snapshot
}

func (ns *NodeScore) ComputeDefaultScore(devices DeviceUsageList) {
	used, usedCore, usedMem := int64(0), int64(0), int64(0)
	for _, device := range devices.DeviceLists {
		used += device.Device.Used
		usedCore += device.Device.Usedcores
		usedMem += device.Device.Usedmem
	}
	klog.V(2).Infof("node %s used %d, usedCore %d, usedMem %d,", ns.NodeID, used, usedCore, usedMem)

	total, totalCore, totalMem := int64(0), int64(0), int64(0)
	for _, deviceLists := range devices.DeviceLists {
		total += deviceLists.Device.Count
		totalCore += deviceLists.Device.Totalcore
		totalMem += deviceLists.Device.Totalmem
	}
	if total == 0 || totalCore == 0 || totalMem == 0 {
		ns.Score = 0
		return
	}
	useScore := float32(used) / float32(total)
	coreScore := float32(usedCore) / float32(totalCore)
	memScore := float32(usedMem) / float32(totalMem)
	ns.Score = float32(util.Weight) * (useScore + coreScore + memScore)
	klog.V(2).Infof("node %s computer default score is %f", ns.NodeID, ns.Score)
}

// PreferredNodeAffinity is a pod's preferred nodeAffinity parsed once per
// scheduling request so every node can be scored without re-parsing the terms.
// terms holds the matcher built by component-helpers (the same code the
// kube-scheduler NodeAffinity plugin uses); totalWeight is the sum of the
// weights of the terms that matcher can actually score, and serves as the
// denominator when normalizing the matched weight to the 0..1 range.
type PreferredNodeAffinity struct {
	terms       *nodeaffinity.PreferredSchedulingTerms
	totalWeight int64
}

// NewPreferredNodeAffinity parses the pod's preferredDuringSchedulingIgnoredDuringExecution
// terms one by one, so a single malformed term does not discard the valid ones.
//
// A term is left out of both the matcher and the denominator when:
//   - its weight is 0 or its preference is empty: component-helpers skips such
//     terms, so they can never match and must not dilute the ratio;
//   - it fails to parse, for example a Gt/Lt value that is not an integer,
//     which the API server admits but the matcher rejects.
//
// Parse failures are returned as an aggregated error alongside the usable
// result, so the caller can log them and still score with the valid terms.
// The returned value is nil when no term is usable.
func NewPreferredNodeAffinity(terms []corev1.PreferredSchedulingTerm) (*PreferredNodeAffinity, error) {
	var (
		usable []corev1.PreferredSchedulingTerm
		total  int64
		errs   []error
	)
	for i, term := range terms {
		if term.Weight == 0 || isEmptyNodeSelectorTerm(&term.Preference) {
			continue
		}
		if _, err := nodeaffinity.NewPreferredSchedulingTerms([]corev1.PreferredSchedulingTerm{term}); err != nil {
			errs = append(errs, fmt.Errorf("preferred term %d: %w", i, err))
			continue
		}
		usable = append(usable, term)
		total += int64(term.Weight)
	}
	aggErr := utilerrors.NewAggregate(errs)
	if len(usable) == 0 {
		return nil, aggErr
	}
	// Every term in usable parsed on its own above, so this cannot fail.
	parsed, err := nodeaffinity.NewPreferredSchedulingTerms(usable)
	if err != nil {
		return nil, err
	}
	return &PreferredNodeAffinity{terms: parsed, totalWeight: total}, aggErr
}

// isEmptyNodeSelectorTerm mirrors the unexported check in component-helpers
// that decides which preferred terms it skips.
func isEmptyNodeSelectorTerm(term *corev1.NodeSelectorTerm) bool {
	return len(term.MatchExpressions) == 0 && len(term.MatchFields) == 0
}

// AddNodeAffinityScore adds a bonus of weight * (matched weight / total weight)
// to the node score, so a node matching every preferred term gains exactly
// weight and a node matching none is unchanged. Binpack picks the highest
// score and spread the lowest, so the bonus is negated under spread to keep
// favouring the preferred node. A nil pref (no preference, or the feature
// disabled with weight 0) and a missing Node object leave the score untouched.
func (ns *NodeScore) AddNodeAffinityScore(pref *PreferredNodeAffinity, policy string, weight float32) {
	if pref == nil || ns.Node == nil || pref.totalWeight == 0 {
		return
	}
	matched := pref.terms.Score(ns.Node)
	r := float32(matched) / float32(pref.totalWeight)
	bonus := weight * r
	if policy == util.NodeSchedulerPolicySpread.String() {
		bonus = -bonus
	}
	ns.Score += bonus
	klog.V(2).Infof("node %s affinity matched %d/%d, bonus %f, score %f", ns.NodeID, matched, pref.totalWeight, bonus, ns.Score)
}
