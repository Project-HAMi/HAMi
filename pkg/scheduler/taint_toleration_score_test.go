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
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
)

// Fixture for issue #3336 (honor PreferNoSchedule taints in the node score).
//
// It reuses the two nodes from the nodeAffinity tests:
//   - idleNode: GPU completely idle -> GPU score 0
//   - busyNode: GPU partly used     -> GPU score 9
//
// Binpack picks busyNode and spread picks idleNode on GPU score alone, so each
// test taints the node the policy would otherwise choose.
const idleNode = preferredNode

func trainingTaint() corev1.Taint {
	return corev1.Taint{Key: "dedicated", Value: "training", Effect: corev1.TaintEffectPreferNoSchedule}
}

func taintNode(nodes *map[string]*NodeUsage, nodeID string, taints ...corev1.Taint) *map[string]*NodeUsage {
	(*nodes)[nodeID].Node.Spec.Taints = taints
	return nodes
}

func newTaintTestPod(annotations map[string]string, tolerations ...corev1.Toleration) *corev1.Pod {
	pod := newNodeAffinityTestPod(0, annotations)
	pod.Spec.Tolerations = tolerations
	return pod
}

// withTaintWeight sets the --taint-toleration-score-weight flag for one test.
// It also pins the nodeAffinity weight to 0 so the rack label on idleNode
// cannot leak into the scores.
func withTaintWeight(t *testing.T, weight float32) {
	t.Helper()
	withAffinityWeight(t, 0)
	prev := config.TaintTolerationScoreWeight
	config.TaintTolerationScoreWeight = weight
	t.Cleanup(func() { config.TaintTolerationScoreWeight = prev })
}

// --- feature off (default weight 0): nothing changes -----------------------

func TestTaintTolerationIgnoredWhenWeightIsZero(t *testing.T) {
	withTaintWeight(t, 0)
	nodes := taintNode(newNodeAffinityTestNodes(), busyNode, trainingTaint())
	picked, scores := pickNode(t, nodes, newTaintTestPod(nil))
	assert.Equal(t, picked, busyNode)
	assertScoreNear(t, scores[busyNode], busyNodeGPUScore)
}

// --- feature on --------------------------------------------------------------

func TestTaintTolerationPenaltyAvoidsTaintedNodeBinpack(t *testing.T) {
	withTaintWeight(t, 100)
	nodes := taintNode(newNodeAffinityTestNodes(), busyNode, trainingTaint())
	picked, scores := pickNode(t, nodes, newTaintTestPod(nil))
	assert.Equal(t, picked, idleNode)
	assertScoreNear(t, scores[busyNode], busyNodeGPUScore-100)
	assertScoreNear(t, scores[idleNode], 0)
}

func TestTaintTolerationPenaltyAvoidsTaintedNodeSpread(t *testing.T) {
	withTaintWeight(t, 100)
	nodes := taintNode(newNodeAffinityTestNodes(), idleNode, trainingTaint())
	picked, scores := pickNode(t, nodes, newTaintTestPod(spreadAnnotation()))
	assert.Equal(t, picked, busyNode)
	assertScoreNear(t, scores[idleNode], 100)
	assertScoreNear(t, scores[busyNode], busyNodeGPUScore)
}

func TestTaintTolerationWeightBelowGPUScoreDoesNotFlip(t *testing.T) {
	withTaintWeight(t, 5)
	nodes := taintNode(newNodeAffinityTestNodes(), busyNode, trainingTaint())
	picked, scores := pickNode(t, nodes, newTaintTestPod(nil))
	assert.Equal(t, picked, busyNode)
	assertScoreNear(t, scores[busyNode], busyNodeGPUScore-5)
}

func TestTaintTolerationToleratedTaintIsNotPenalized(t *testing.T) {
	withTaintWeight(t, 100)
	nodes := taintNode(newNodeAffinityTestNodes(), busyNode, trainingTaint())
	pod := newTaintTestPod(nil, corev1.Toleration{
		Key: "dedicated", Operator: corev1.TolerationOpEqual, Value: "training",
		Effect: corev1.TaintEffectPreferNoSchedule,
	})
	picked, scores := pickNode(t, nodes, pod)
	assert.Equal(t, picked, busyNode)
	assertScoreNear(t, scores[busyNode], busyNodeGPUScore)
}

func TestTaintTolerationNoScheduleTolerationDoesNotCover(t *testing.T) {
	withTaintWeight(t, 100)
	nodes := taintNode(newNodeAffinityTestNodes(), busyNode, trainingTaint())
	pod := newTaintTestPod(nil, corev1.Toleration{
		Key: "dedicated", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule,
	})
	picked, _ := pickNode(t, nodes, pod)
	assert.Equal(t, picked, idleNode)
}

func TestTaintTolerationNoScheduleTaintIsIgnored(t *testing.T) {
	withTaintWeight(t, 100)
	// kube-scheduler filters NoSchedule itself; the extender must not double count it.
	nodes := taintNode(newNodeAffinityTestNodes(), busyNode,
		corev1.Taint{Key: "dedicated", Value: "training", Effect: corev1.TaintEffectNoSchedule})
	picked, scores := pickNode(t, nodes, newTaintTestPod(nil))
	assert.Equal(t, picked, busyNode)
	assertScoreNear(t, scores[busyNode], busyNodeGPUScore)
}

// The penalty is count / maxCount * weight over the nodes that fit, so a node
// with fewer untolerated taints is penalized proportionally less.
func TestTaintTolerationPenaltyScalesWithTaintCount(t *testing.T) {
	withTaintWeight(t, 20)
	nodes := newNodeAffinityTestNodes()
	taintNode(nodes, busyNode, trainingTaint(),
		corev1.Taint{Key: "zone", Value: "slow", Effect: corev1.TaintEffectPreferNoSchedule})
	taintNode(nodes, idleNode, trainingTaint())

	picked, scores := pickNode(t, nodes, newTaintTestPod(nil))
	assert.Equal(t, picked, idleNode)
	assertScoreNear(t, scores[busyNode], busyNodeGPUScore-20) // 2/2 of the weight
	assertScoreNear(t, scores[idleNode], -10)                 // 1/2 of the weight
}

// Equal taint counts cancel out, leaving the GPU score to decide.
func TestTaintTolerationEqualTaintsFallBackToGPUScore(t *testing.T) {
	withTaintWeight(t, 100)
	nodes := newNodeAffinityTestNodes()
	taintNode(nodes, busyNode, trainingTaint())
	taintNode(nodes, idleNode, trainingTaint())

	picked, scores := pickNode(t, nodes, newTaintTestPod(nil))
	assert.Equal(t, picked, busyNode)
	assertScoreNear(t, scores[busyNode], busyNodeGPUScore-100)
	assertScoreNear(t, scores[idleNode], -100)
}

// PreferNoSchedule is a preference: when the tainted node is the only one that
// fits, the pod still lands there.
func TestTaintTolerationOnlyTaintedNodeFitsStillPicked(t *testing.T) {
	withTaintWeight(t, 100)
	nodes := taintNode(newNodeAffinityTestNodes(), busyNode, trainingTaint())
	delete(*nodes, idleNode)
	picked, _ := pickNode(t, nodes, newTaintTestPod(nil))
	assert.Equal(t, picked, busyNode)
}

// The affinity bonus and the taint penalty are independent terms added to the
// same score, so the larger weight wins on a node that both prefers and taints.
func TestTaintTolerationCombinesWithNodeAffinity(t *testing.T) {
	tests := []struct {
		name           string
		affinityWeight float32
		taintWeight    float32
		wantPicked     string
		wantIdleScore  float32
	}{
		{name: "affinity outweighs the taint", affinityWeight: 100, taintWeight: 50, wantPicked: idleNode, wantIdleScore: 50},
		{name: "taint outweighs the affinity", affinityWeight: 50, taintWeight: 100, wantPicked: busyNode, wantIdleScore: -50},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withTaintWeight(t, tc.taintWeight)
			withAffinityWeight(t, tc.affinityWeight)
			// idleNode carries rack=r1 (preferred) and the training taint.
			nodes := taintNode(newNodeAffinityTestNodes(), idleNode, trainingTaint())
			picked, scores := pickNode(t, nodes, newNodeAffinityTestPod(100, nil))
			assert.Equal(t, picked, tc.wantPicked)
			assertScoreNear(t, scores[idleNode], tc.wantIdleScore)
			assertScoreNear(t, scores[busyNode], busyNodeGPUScore)
		})
	}
}

// Like labels, a taint-only update (kubectl taint) does not refresh the
// scheduler's cached Node object, so the penalty must read the informer's copy.
func TestTaintTolerationUsesInformerTaints(t *testing.T) {
	withTaintWeight(t, 100)

	t.Run("taint added after caching is penalized", func(t *testing.T) {
		nodes := newNodeAffinityTestNodes() // cache has no taint
		live := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: busyNode},
			Spec:       corev1.NodeSpec{Taints: []corev1.Taint{trainingTaint()}},
		}
		s := &Scheduler{nodeLister: newTestNodeLister(t, live)}

		picked, scores := pickNodeWith(t, s, nodes, newTaintTestPod(nil))
		assert.Equal(t, picked, idleNode)
		assertScoreNear(t, scores[busyNode], busyNodeGPUScore-100)
	})

	t.Run("taint removed after caching is not penalized", func(t *testing.T) {
		nodes := taintNode(newNodeAffinityTestNodes(), busyNode, trainingTaint())
		live := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: busyNode}}
		s := &Scheduler{nodeLister: newTestNodeLister(t, live)}

		picked, scores := pickNodeWith(t, s, nodes, newTaintTestPod(nil))
		assert.Equal(t, picked, busyNode)
		assertScoreNear(t, scores[busyNode], busyNodeGPUScore)
	})
}
