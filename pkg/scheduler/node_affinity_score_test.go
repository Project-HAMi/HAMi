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
	"sort"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	listerscorev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

// Fixture for issue #3062 (honor preferred nodeAffinity in the node score).
//
// Two nodes pass Filter:
//   - preferredNode: labelled rack=r1, GPU completely idle -> GPU score 0
//   - busyNode:      no label,          GPU partly used    -> GPU score 9
//
// The pod prefers rack=r1 with weight 100. Under binpack HAMi picks the
// highest GPU score, so with the bonus disabled the pod lands on busyNode.
const (
	preferredNode = "node-preferred"
	busyNode      = "node-busy"
	rackLabel     = "rack"
	rackValue     = "r1"
	diskLabel     = "disk"
	diskValue     = "nvme"

	busyNodeGPUScore float32 = 9
)

func newNodeAffinityTestNodes() *map[string]*NodeUsage {
	idle := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:   preferredNode,
		Labels: map[string]string{rackLabel: rackValue},
	}}
	busy := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: busyNode}}
	return &map[string]*NodeUsage{
		preferredNode: {
			Node:     idle,
			NodeInfo: &device.NodeInfo{ID: idle.Name, Node: idle},
			Devices: policy.DeviceUsageList{
				Policy: util.GPUSchedulerPolicySpread.String(),
				DeviceLists: []*policy.DeviceListsScore{
					{Device: &device.DeviceUsage{
						ID: "gpu-idle", Index: 0, Type: nvidia.NvidiaGPUDevice, Health: true,
						Count: 10, Used: 0, Totalcore: 100, Usedcores: 0, Totalmem: 40000, Usedmem: 0,
					}},
				},
			},
		},
		busyNode: {
			Node:     busy,
			NodeInfo: &device.NodeInfo{ID: busy.Name, Node: busy},
			Devices: policy.DeviceUsageList{
				Policy: util.GPUSchedulerPolicySpread.String(),
				DeviceLists: []*policy.DeviceListsScore{
					{Device: &device.DeviceUsage{
						// 3/10 slots + 30/100 cores + 12000/40000 mem = 0.9 -> score 9
						ID: "gpu-busy", Index: 0, Type: nvidia.NvidiaGPUDevice, Health: true,
						Count: 10, Used: 3, Totalcore: 100, Usedcores: 30, Totalmem: 40000, Usedmem: 12000,
					}},
				},
			},
		},
	}
}

// preferBusyNode moves the rack=r1 label from the idle node to the busy one.
// Under spread the idle node already wins on GPU score alone, so to prove the
// bonus does anything the pod must prefer the node that spread would reject.
func preferBusyNode(nodes *map[string]*NodeUsage) *map[string]*NodeUsage {
	(*nodes)[preferredNode].Node.Labels = nil
	(*nodes)[busyNode].Node.Labels = map[string]string{rackLabel: rackValue}
	return nodes
}

func preferredTerm(weight int32, key, value string) corev1.PreferredSchedulingTerm {
	return corev1.PreferredSchedulingTerm{
		Weight: weight,
		Preference: corev1.NodeSelectorTerm{
			MatchExpressions: []corev1.NodeSelectorRequirement{{
				Key: key, Operator: corev1.NodeSelectorOpIn, Values: []string{value},
			}},
		},
	}
}

func newNodeAffinityTestPodWithTerms(annotations map[string]string, terms ...corev1.PreferredSchedulingTerm) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "affinity-pod", Namespace: "default", Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app"}},
		},
	}
	if len(terms) > 0 {
		pod.Spec.Affinity = &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: terms,
			},
		}
	}
	return pod
}

// newNodeAffinityTestPod prefers rack=r1 with the given weight. A zero weight
// returns a pod without any affinity, which must behave exactly as today.
func newNodeAffinityTestPod(preferWeight int32, annotations map[string]string) *corev1.Pod {
	if preferWeight == 0 {
		return newNodeAffinityTestPodWithTerms(annotations)
	}
	return newNodeAffinityTestPodWithTerms(annotations, preferredTerm(preferWeight, rackLabel, rackValue))
}

// assertScoreNear compares float32 scores with a tolerance, since the bonus is
// a float32 product that may carry rounding noise (30.000002 for 100 * 0.3).
func assertScoreNear(t *testing.T, got, want float32) {
	t.Helper()
	if diff := got - want; diff > 1e-3 || diff < -1e-3 {
		t.Fatalf("score = %f, want %f", got, want)
	}
}

func spreadAnnotation() map[string]string {
	return map[string]string{
		util.NodeSchedulerPolicyAnnotationKey: util.NodeSchedulerPolicySpread.String(),
	}
}

// withAffinityWeight sets the --node-affinity-score-weight flag for one test.
func withAffinityWeight(t *testing.T, weight float32) {
	t.Helper()
	prev := config.NodeAffinityScoreWeight
	config.NodeAffinityScoreWeight = weight
	t.Cleanup(func() { config.NodeAffinityScoreWeight = prev })
}

// pickNode mirrors Scheduler.Filter: score every node, sort with the pod's
// node policy, and take the last entry. It returns the chosen node and the
// scores keyed by node so a test can assert on the raw numbers.
func pickNode(t *testing.T, nodes *map[string]*NodeUsage, pod *corev1.Pod) (string, map[string]float32) {
	t.Helper()
	return pickNodeWith(t, &Scheduler{}, nodes, pod)
}

// pickNodeWith is pickNode with a caller-supplied Scheduler, so a test can
// wire a node lister into it.
func pickNodeWith(t *testing.T, s *Scheduler, nodes *map[string]*NodeUsage, pod *corev1.Pod) (string, map[string]float32) {
	t.Helper()
	requests := device.PodDeviceRequests{{
		"hami.io/vgpu-devices-to-allocate": {
			Nums: 1, Type: nvidia.NvidiaGPUDevice, Memreq: 10000, Coresreq: 10,
		},
	}}
	scores, err := s.calcScoreWithOptions(nodes, requests, pod, map[string]string{}, false, false)
	assert.NilError(t, err)
	if len(scores.NodeList) == 0 {
		t.Fatal("no node fit the pod")
	}
	sort.Sort(scores)
	byNode := make(map[string]float32, len(scores.NodeList))
	for _, ns := range scores.NodeList {
		byNode[ns.NodeID] = ns.Score
		t.Logf("policy=%s node=%s score=%.2f", scores.Policy, ns.NodeID, ns.Score)
	}
	return scores.NodeList[len(scores.NodeList)-1].NodeID, byNode
}

// --- feature off (default weight 0): nothing changes -----------------------

func TestNodeAffinityIgnoredWhenWeightIsZero(t *testing.T) {
	withAffinityWeight(t, 0)
	picked, scores := pickNode(t, newNodeAffinityTestNodes(), newNodeAffinityTestPod(100, nil))
	assert.Equal(t, picked, busyNode)
	assertScoreNear(t, scores[preferredNode], float32(0))
	assertScoreNear(t, scores[busyNode], busyNodeGPUScore)
}

func TestNodeAffinitySpreadIgnoredWhenWeightIsZero(t *testing.T) {
	withAffinityWeight(t, 0)
	picked, _ := pickNode(t, preferBusyNode(newNodeAffinityTestNodes()), newNodeAffinityTestPod(100, spreadAnnotation()))
	assert.Equal(t, picked, preferredNode)
}

// --- feature on, pod without preference: nothing changes -------------------

func TestNodeAffinityNoPreferenceUnchanged(t *testing.T) {
	withAffinityWeight(t, 100)
	picked, scores := pickNode(t, newNodeAffinityTestNodes(), newNodeAffinityTestPod(0, nil))
	assert.Equal(t, picked, busyNode)
	assertScoreNear(t, scores[preferredNode], float32(0))
	assertScoreNear(t, scores[busyNode], busyNodeGPUScore)
}

// --- feature on: the preferred node wins under both policies ---------------

func TestNodeAffinityBonusPicksPreferredNodeBinpack(t *testing.T) {
	withAffinityWeight(t, 100)
	picked, scores := pickNode(t, newNodeAffinityTestNodes(), newNodeAffinityTestPod(100, nil))
	assert.Equal(t, picked, preferredNode)
	assertScoreNear(t, scores[preferredNode], float32(100)) // 0 + 100
	assertScoreNear(t, scores[busyNode], busyNodeGPUScore)  // no label, no bonus
}

func TestNodeAffinityBonusPicksPreferredNodeSpread(t *testing.T) {
	withAffinityWeight(t, 100)
	// spread picks the lowest score: busy 9 - 100 = -91 beats idle 0.
	picked, scores := pickNode(t, preferBusyNode(newNodeAffinityTestNodes()), newNodeAffinityTestPod(100, spreadAnnotation()))
	assert.Equal(t, picked, busyNode)
	assertScoreNear(t, scores[busyNode], busyNodeGPUScore-100)
	assertScoreNear(t, scores[preferredNode], float32(0))
}

// A small weight keeps GPU usage in charge: 0 + 5 < 9.
func TestNodeAffinityWeightBelowGPUScoreDoesNotFlip(t *testing.T) {
	withAffinityWeight(t, 5)
	picked, scores := pickNode(t, newNodeAffinityTestNodes(), newNodeAffinityTestPod(100, nil))
	assert.Equal(t, picked, busyNode)
	assertScoreNear(t, scores[preferredNode], float32(5))
}

// --- several terms: the bonus follows the matched weight ratio ------------

func TestNodeAffinityBonusFollowsTermWeightRatio(t *testing.T) {
	withAffinityWeight(t, 100)
	nodes := newNodeAffinityTestNodes()
	(*nodes)[busyNode].Node.Labels = map[string]string{diskLabel: diskValue}
	pod := newNodeAffinityTestPodWithTerms(nil,
		preferredTerm(70, rackLabel, rackValue), // only the idle node matches
		preferredTerm(30, diskLabel, diskValue), // only the busy node matches
	)

	picked, scores := pickNode(t, nodes, pod)
	assert.Equal(t, picked, preferredNode)
	assertScoreNear(t, scores[preferredNode], float32(70))             // 0 + 100 * 70/100
	assertScoreNear(t, scores[busyNode], busyNodeGPUScore+float32(30)) // 9 + 100 * 30/100
}

// Both nodes match the same terms, so the bonus cancels out and GPU usage
// decides again.
func TestNodeAffinityEqualMatchFallsBackToGPUScore(t *testing.T) {
	withAffinityWeight(t, 100)
	nodes := newNodeAffinityTestNodes()
	(*nodes)[busyNode].Node.Labels = map[string]string{rackLabel: rackValue}

	picked, scores := pickNode(t, nodes, newNodeAffinityTestPod(100, nil))
	assert.Equal(t, picked, busyNode)
	assertScoreNear(t, scores[preferredNode], float32(100))
	assertScoreNear(t, scores[busyNode], busyNodeGPUScore+100)
}

// --- robustness --------------------------------------------------------------

// The preferred node is dropped by Fit (its GPU is too small), so the pod
// must still land on the other node instead of staying Pending.
func TestNodeAffinityPreferredNodeUnfitFallsBack(t *testing.T) {
	withAffinityWeight(t, 100)
	nodes := newNodeAffinityTestNodes()
	(*nodes)[preferredNode].Devices.DeviceLists[0].Device.Totalmem = 5000 // request needs 10000

	picked, scores := pickNode(t, nodes, newNodeAffinityTestPod(100, nil))
	assert.Equal(t, picked, busyNode)
	assert.Equal(t, len(scores), 1)
}

// A pod whose only preference is malformed is scheduled as if it had none:
// the term is logged and ignored rather than failing the pod.
func TestNodeAffinityInvalidTermIsIgnored(t *testing.T) {
	withAffinityWeight(t, 100)
	bad := preferredTerm(100, rackLabel, rackValue)
	bad.Preference.MatchExpressions[0].Operator = "Bogus"

	picked, scores := pickNode(t, newNodeAffinityTestNodes(), newNodeAffinityTestPodWithTerms(nil, bad))
	assert.Equal(t, picked, busyNode)
	assertScoreNear(t, scores[preferredNode], float32(0))
	assertScoreNear(t, scores[busyNode], busyNodeGPUScore)
}

// A malformed term next to a valid one: the valid term still decides, and
// the rejected term is not counted in the ratio (bonus 100, not 50).
func TestNodeAffinityInvalidTermKeepsValidTerms(t *testing.T) {
	withAffinityWeight(t, 100)
	bad := preferredTerm(100, "gpu-count", "abc")
	bad.Preference.MatchExpressions[0].Operator = corev1.NodeSelectorOpGt

	pod := newNodeAffinityTestPodWithTerms(nil, bad, preferredTerm(100, rackLabel, rackValue))
	picked, scores := pickNode(t, newNodeAffinityTestNodes(), pod)
	assert.Equal(t, picked, preferredNode)
	assertScoreNear(t, scores[preferredNode], float32(100))
}

// An empty preferred term (allowed by the API, skipped by the matcher) must not
// dilute the bonus of the term that does match.
func TestNodeAffinityEmptyTermDoesNotDiluteBonus(t *testing.T) {
	withAffinityWeight(t, 100)
	pod := newNodeAffinityTestPodWithTerms(nil,
		corev1.PreferredSchedulingTerm{Weight: 100},
		preferredTerm(100, rackLabel, rackValue),
	)
	picked, scores := pickNode(t, newNodeAffinityTestNodes(), pod)
	assert.Equal(t, picked, preferredNode)
	assertScoreNear(t, scores[preferredNode], float32(100))
}

// A node whose Node object is missing from the usage snapshot cannot be
// label-matched and simply receives no bonus.
func TestNodeAffinityNilNodeObjectGetsNoBonus(t *testing.T) {
	withAffinityWeight(t, 100)
	nodes := newNodeAffinityTestNodes()
	(*nodes)[preferredNode].Node = nil

	picked, scores := pickNode(t, nodes, newNodeAffinityTestPod(100, nil))
	assert.Equal(t, picked, busyNode)
	assertScoreNear(t, scores[preferredNode], float32(0))
}

// resolvePreferredSchedulingTerm must tolerate every missing level of the
// affinity tree.
func TestResolvePreferredSchedulingTerm(t *testing.T) {
	assert.Equal(t, len(resolvePreferredSchedulingTerm(&corev1.Pod{})), 0)
	assert.Equal(t, len(resolvePreferredSchedulingTerm(&corev1.Pod{Spec: corev1.PodSpec{
		Affinity: &corev1.Affinity{},
	}})), 0)
	assert.Equal(t, len(resolvePreferredSchedulingTerm(&corev1.Pod{Spec: corev1.PodSpec{
		Affinity: &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{}},
	}})), 0)
	terms := resolvePreferredSchedulingTerm(newNodeAffinityTestPod(42, nil))
	assert.Equal(t, len(terms), 1)
	assert.Equal(t, terms[0].Weight, int32(42))
}

// newTestNodeLister returns a NodeLister backed by an in-memory indexer, the
// same store the shared informer fills, holding the given nodes.
func newTestNodeLister(t *testing.T, nodes ...*corev1.Node) listerscorev1.NodeLister {
	t.Helper()
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	for _, n := range nodes {
		assert.NilError(t, indexer.Add(n))
	}
	return listerscorev1.NewNodeLister(indexer)
}

// The scheduler's node cache only refreshes a Node object when its device
// handshake changes, so a label-only update (kubectl label) leaves the cached
// labels stale. Affinity scoring must read the informer's copy instead.
func TestNodeAffinityUsesInformerLabels(t *testing.T) {
	withAffinityWeight(t, 100)

	t.Run("label added after caching earns the bonus", func(t *testing.T) {
		nodes := newNodeAffinityTestNodes()
		(*nodes)[preferredNode].Node.Labels = nil // cache predates the label
		live := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
			Name: preferredNode, Labels: map[string]string{rackLabel: rackValue},
		}}
		s := &Scheduler{nodeLister: newTestNodeLister(t, live)}

		picked, scores := pickNodeWith(t, s, nodes, newNodeAffinityTestPod(100, nil))
		assert.Equal(t, picked, preferredNode)
		assertScoreNear(t, scores[preferredNode], float32(100))
	})

	t.Run("label removed after caching loses the bonus", func(t *testing.T) {
		nodes := newNodeAffinityTestNodes() // cache still says rack=r1
		live := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: preferredNode}}
		s := &Scheduler{nodeLister: newTestNodeLister(t, live)}

		picked, scores := pickNodeWith(t, s, nodes, newNodeAffinityTestPod(100, nil))
		assert.Equal(t, picked, busyNode)
		assertScoreNear(t, scores[preferredNode], float32(0))
	})

	t.Run("node missing from the informer falls back to the cache", func(t *testing.T) {
		nodes := newNodeAffinityTestNodes() // cache says rack=r1
		s := &Scheduler{nodeLister: newTestNodeLister(t)}

		picked, scores := pickNodeWith(t, s, nodes, newNodeAffinityTestPod(100, nil))
		assert.Equal(t, picked, preferredNode)
		assertScoreNear(t, scores[preferredNode], float32(100))
	})
}

func TestCurrentNode(t *testing.T) {
	cached := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n", ResourceVersion: "1"}}
	live := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n", ResourceVersion: "2"}}

	assert.Equal(t, (&Scheduler{}).currentNode("n", cached), cached)
	assert.Equal(t, (&Scheduler{nodeLister: newTestNodeLister(t)}).currentNode("n", cached), cached)
	assert.Equal(t, (&Scheduler{nodeLister: newTestNodeLister(t, live)}).currentNode("n", cached), live)
}
