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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

// cceTierSpec is the tier label scheme of the CCE multi-level topology example,
// smallest domain first.
const cceTierSpec = "test-tier-1,test-tier-2,test-tier-3"

// cceTopologyNodes is the eight node, three level fabric of the same example:
// two nodes per tier 1 domain, two tier 1 domains per tier 2 domain, and both
// tier 2 domains under a single tier 3 domain.
func cceTopologyNodes() []*corev1.Node {
	fabric := [][4]string{
		{"node0", "HyperNode0", "HyperNode4", "HyperNode6"},
		{"node1", "HyperNode0", "HyperNode4", "HyperNode6"},
		{"node2", "HyperNode1", "HyperNode4", "HyperNode6"},
		{"node3", "HyperNode1", "HyperNode4", "HyperNode6"},
		{"node4", "HyperNode2", "HyperNode5", "HyperNode6"},
		{"node5", "HyperNode2", "HyperNode5", "HyperNode6"},
		{"node6", "HyperNode3", "HyperNode5", "HyperNode6"},
		{"node7", "HyperNode3", "HyperNode5", "HyperNode6"},
	}
	nodes := make([]*corev1.Node, 0, len(fabric))
	for _, entry := range fabric {
		nodes = append(nodes, &corev1.Node{ObjectMeta: metav1.ObjectMeta{
			Name: entry[0],
			Labels: map[string]string{
				"test-tier-1": entry[1],
				"test-tier-2": entry[2],
				"test-tier-3": entry[3],
			},
		}})
	}
	return nodes
}

// topologyTestScheduler returns a scheduler whose cached hierarchy is the CCE
// fabric, with the tier labels flag pointed at it. It restores the configuration
// when the test ends, so the tests stay independent of each other.
func topologyTestScheduler(t *testing.T, nodes []*corev1.Node) *Scheduler {
	t.Helper()

	originalLabels := config.TopologyTierLabels
	originalMode := config.TopologyMode
	originalWeight := config.TopologyScoreWeight
	t.Cleanup(func() {
		config.TopologyTierLabels = originalLabels
		config.TopologyMode = originalMode
		config.TopologyScoreWeight = originalWeight
	})

	config.TopologyTierLabels = cceTierSpec
	config.TopologyMode = util.TopologyModeSoft
	config.TopologyScoreWeight = config.DefaultTopologyScoreWeight

	scheduler := NewScheduler()
	scheduler.rebuildTopology(nodes)
	require.NotNil(t, scheduler.topologySnapshot())
	return scheduler
}

func topologyPod(annotations map[string]string, labels map[string]string) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name:        "topology-pod",
		Namespace:   "default",
		UID:         "topology-pod-uid",
		Annotations: annotations,
		Labels:      labels,
	}}
}

// TestTopologyRequirement covers the per-Pod opt-in. The feature must stay off
// for a Pod that does not ask for it, and a Pod that asks for it without naming
// a tier must only be ranked, never rejected, which is what the unlimited
// sentinel encodes.
func TestTopologyRequirement(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		wantTier    int
		wantMode    string
		wantOK      bool
	}{
		{"no annotation keeps the feature off", nil, 0, "", false},
		{"mode alone asks for ranking only", map[string]string{util.TopologyModeAnnotationKey: "soft"}, unlimitedTier, "soft", true},
		{"mode and tier", map[string]string{util.TopologyModeAnnotationKey: "hard", util.TopologyHighestTierAnnotationKey: "1"}, 1, "hard", true},
		{"mode is case insensitive and trimmed", map[string]string{util.TopologyModeAnnotationKey: " HARD "}, unlimitedTier, "hard", true},
		{"unrecognized mode falls back to the configured one", map[string]string{util.TopologyModeAnnotationKey: "strict"}, unlimitedTier, "", true},
		{"tier alone opts the Pod in", map[string]string{util.TopologyHighestTierAnnotationKey: "2"}, 2, "", true},
		{"tier zero is the node itself", map[string]string{util.TopologyHighestTierAnnotationKey: "0"}, 0, "", true},
		{"non numeric tier is ignored", map[string]string{util.TopologyModeAnnotationKey: "hard", util.TopologyHighestTierAnnotationKey: "edge"}, 0, "", false},
		{"negative tier is ignored", map[string]string{util.TopologyHighestTierAnnotationKey: "-1"}, 0, "", false},
		{"unrelated annotation is ignored", map[string]string{"hami.io/unrelated": "hard"}, 0, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tier, mode, ok := topologyRequirement(topologyPod(tt.annotations, nil))
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantMode, mode)
			assert.Equal(t, tt.wantTier, tier)
		})
	}
}

// TestTopologyGroupKey covers how the set of Pods that has to stay together is
// identified. An explicit annotation wins over the job labels, a job label wins
// over the controller reference, and a Pod in no group reports none.
func TestTopologyGroupKey(t *testing.T) {
	controller := func(kind string, uid string) []metav1.OwnerReference {
		owned := true
		return []metav1.OwnerReference{{Kind: kind, UID: types.UID(uid), Controller: &owned}}
	}
	tests := []struct {
		name  string
		pod   *corev1.Pod
		group string
	}{
		{"no group at all", topologyPod(nil, nil), ""},
		{"job-name label", topologyPod(nil, map[string]string{"job-name": "train"}), "default/job-name/train"},
		{"kubeflow label", topologyPod(nil, map[string]string{"training.kubeflow.org/job-name": "train"}), "default/training.kubeflow.org/job-name/train"},
		{"volcano label", topologyPod(nil, map[string]string{"volcano.sh/job-name": "train"}), "default/volcano.sh/job-name/train"},
		{"pod group label", topologyPod(nil, map[string]string{util.PodGroupLabel: "train"}), "default/" + util.PodGroupLabel + "/train"},
		{"controller reference", topologyPod(nil, nil), "default/Job/job-uid"},
		{"annotation overrides the labels", topologyPod(map[string]string{util.TopologyGroupAnnotationKey: "explicit"}, map[string]string{"job-name": "train"}), "default/explicit"},
		{"job label overrides the controller reference", topologyPod(nil, map[string]string{"job-name": "train"}), "default/job-name/train"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := tt.pod
			if tt.name == "controller reference" || tt.name == "job label overrides the controller reference" {
				pod = pod.DeepCopy()
				pod.OwnerReferences = controller("Job", "job-uid")
			}
			assert.Equal(t, tt.group, topologyGroupKey(pod))
		})
	}
	assert.Equal(t, "", topologyGroupKey(nil))
}

// TestRebuildTopologyFromTierLabels covers the flag that turns the feature on.
// An unset flag, and a flag that does not parse, both leave the scheduler
// without a hierarchy, so the placement path is exactly the one it had before.
func TestRebuildTopologyFromTierLabels(t *testing.T) {
	nodes := cceTopologyNodes()
	originalLabels := config.TopologyTierLabels
	t.Cleanup(func() { config.TopologyTierLabels = originalLabels })

	tests := []struct {
		name       string
		tierLabels string
		loaded     bool
		maxTier    int
	}{
		{"unset flag keeps the feature off", "", false, 0},
		{"invalid flag keeps the feature off", "npu.io/rack=1,npu.io/rack=2", false, 0},
		{"valid flag loads the hierarchy", cceTierSpec, true, 3},
		{"explicit levels load the hierarchy", "test-tier-2=2,test-tier-1=1", true, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config.TopologyTierLabels = tt.tierLabels
			scheduler := NewScheduler()
			scheduler.rebuildTopology(nodes)

			snapshot := scheduler.topologySnapshot()
			if !tt.loaded {
				assert.Nil(t, snapshot)
				return
			}
			require.NotNil(t, snapshot)
			assert.Equal(t, tt.maxTier, snapshot.tree.MaxTier())
			assert.NotEmpty(t, snapshot.tiers)
		})
	}
}

// topologyPlanForTest builds the plan the way Filter does, from an explicit
// placement instead of from the Pod lists, so the filter and the scorer can be
// exercised on their own.
func topologyPlanForTest(scheduler *Scheduler, mode string, highest int, placed ...string) *topologyPlan {
	return &topologyPlan{
		view:    scheduler.topologySnapshot(),
		group:   "default/job-name/train",
		placed:  placed,
		mode:    mode,
		highest: highest,
	}
}

func candidateNodes(names ...string) map[string]*NodeUsage {
	candidates := make(map[string]*NodeUsage, len(names))
	for _, name := range names {
		candidates[name] = &NodeUsage{Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}}}
	}
	return candidates
}

// TestApplyTopologyFilter covers the hard half: a candidate that would stretch
// the group past the allowed tier is dropped and reported, a candidate the
// hierarchy says nothing about is left alone, and a soft request rejects
// nothing at all.
func TestApplyTopologyFilter(t *testing.T) {
	allNodes := cceTopologyNodes()
	scheduler := topologyTestScheduler(t, allNodes)
	pod := topologyPod(nil, nil)

	t.Run("hard mode keeps only the domains within the allowed tier", func(t *testing.T) {
		candidates := candidateNodes("node0", "node1", "node2", "node3", "node4", "node5", "node6", "node7")
		failedNodes := map[string]string{}
		scheduler.applyTopologyFilter(topologyPlanForTest(scheduler, util.TopologyModeHard, 1, "node0"), pod, &candidates, failedNodes)

		assert.Len(t, candidates, 2, "only the two nodes of the tier 1 domain of the group survive")
		assert.Contains(t, candidates, "node0")
		assert.Contains(t, candidates, "node1")
		assert.Len(t, failedNodes, 6)
		for node := range candidates {
			assert.NotContains(t, failedNodes, node)
		}
		for _, node := range []string{"node2", "node3", "node4", "node5", "node6", "node7"} {
			assert.Equal(t, 1, common.ParseReason(failedNodes[node])[common.HyperNodeTierNotFit],
				"rejected node %s must report the topology reason", node)
		}
	})

	t.Run("a wider tier keeps the nodes inside it", func(t *testing.T) {
		candidates := candidateNodes("node0", "node1", "node2", "node3", "node4", "node5", "node6", "node7")
		failedNodes := map[string]string{}
		scheduler.applyTopologyFilter(topologyPlanForTest(scheduler, util.TopologyModeHard, 3, "node0"), pod, &candidates, failedNodes)

		assert.Len(t, candidates, 8)
		assert.Empty(t, failedNodes)
	})

	t.Run("soft mode rejects nothing", func(t *testing.T) {
		candidates := candidateNodes("node0", "node1", "node2", "node3", "node4", "node5", "node6", "node7")
		failedNodes := map[string]string{}
		scheduler.applyTopologyFilter(topologyPlanForTest(scheduler, util.TopologyModeSoft, 1, "node0"), pod, &candidates, failedNodes)

		assert.Len(t, candidates, 8)
		assert.Empty(t, failedNodes)
	})

	t.Run("the first pod of a group is never rejected", func(t *testing.T) {
		candidates := candidateNodes("node0", "node1", "node2")
		failedNodes := map[string]string{}
		scheduler.applyTopologyFilter(topologyPlanForTest(scheduler, util.TopologyModeHard, 1), pod, &candidates, failedNodes)

		assert.Len(t, candidates, 3)
		assert.Empty(t, failedNodes)
	})

	t.Run("nodes outside the described fabric are not constrained", func(t *testing.T) {
		unlabeled := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-unlabeled"}}
		schedulerWithUnlabeled := topologyTestScheduler(t, append(allNodes, unlabeled))
		candidates := candidateNodes("node0", "node2", "node-unlabeled")
		failedNodes := map[string]string{}
		schedulerWithUnlabeled.applyTopologyFilter(topologyPlanForTest(schedulerWithUnlabeled, util.TopologyModeHard, 1, "node0"), pod, &candidates, failedNodes)

		assert.Len(t, candidates, 2)
		assert.Contains(t, candidates, "node-unlabeled")
		assert.Contains(t, candidates, "node0")
	})

	t.Run("a pod group already placed outside the fabric keeps scheduling", func(t *testing.T) {
		unlabeled := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-unlabeled"}}
		schedulerWithUnlabeled := topologyTestScheduler(t, append(allNodes, unlabeled))
		candidates := candidateNodes("node0", "node2", "node-unlabeled")
		failedNodes := map[string]string{}
		schedulerWithUnlabeled.applyTopologyFilter(topologyPlanForTest(schedulerWithUnlabeled, util.TopologyModeHard, 1, "node-unlabeled"), pod, &candidates, failedNodes)

		assert.Len(t, candidates, 3)
		assert.Empty(t, failedNodes)
	})

	t.Run("a rejection that leaves nothing reports an event", func(t *testing.T) {
		recorder := record.NewFakeRecorder(4)
		scheduler := topologyTestScheduler(t, allNodes)
		scheduler.eventRecorder = recorder
		candidates := candidateNodes("node4")
		failedNodes := map[string]string{}
		scheduler.applyTopologyFilter(topologyPlanForTest(scheduler, util.TopologyModeHard, 1, "node0"), pod, &candidates, failedNodes)

		assert.Empty(t, candidates)
		assert.Len(t, failedNodes, 1)
		require.Len(t, recorder.Events, 1)
		assert.Contains(t, <-recorder.Events, common.HyperNodeTierNotFit)
	})

	t.Run("a partial rejection reports no event", func(t *testing.T) {
		recorder := record.NewFakeRecorder(4)
		scheduler := topologyTestScheduler(t, allNodes)
		scheduler.eventRecorder = recorder
		candidates := candidateNodes("node0", "node4")
		failedNodes := map[string]string{}
		scheduler.applyTopologyFilter(topologyPlanForTest(scheduler, util.TopologyModeHard, 1, "node0"), pod, &candidates, failedNodes)

		assert.Len(t, candidates, 1)
		assert.Empty(t, recorder.Events, "the pod is still schedulable, so no warning event")
	})

	t.Run("a nil plan and a nil candidate map are no-ops", func(t *testing.T) {
		candidates := candidateNodes("node4")
		failedNodes := map[string]string{}
		scheduler.applyTopologyFilter(nil, pod, &candidates, failedNodes)
		scheduler.applyTopologyFilter(topologyPlanForTest(scheduler, util.TopologyModeHard, 1, "node0"), pod, nil, failedNodes)

		assert.Len(t, candidates, 1)
		assert.Empty(t, failedNodes)
	})
}

// TestApplyTopologyScore covers the soft half: the bonus points at the node
// closest to the group and follows the direction of the node policy, so binpack
// and spread both end up preferring it.
func TestApplyTopologyScore(t *testing.T) {
	scheduler := topologyTestScheduler(t, cceTopologyNodes())
	pod := topologyPod(nil, nil)
	// The nodes of the CCE fabric, one per distinct distance from node0: the
	// same tier 1 domain, the same tier 2 domain, and only the tier 3 domain.
	scored := func() *policy.NodeScoreList {
		return &policy.NodeScoreList{NodeList: []*policy.NodeScore{
			{NodeID: "node1", Score: 100},
			{NodeID: "node2", Score: 100},
			{NodeID: "node4", Score: 100},
			{NodeID: "node-unlabeled", Score: 100},
		}}
	}

	t.Run("binpack rewards the closer node", func(t *testing.T) {
		scores := scored()
		scores.Policy = util.NodeSchedulerPolicyBinpack.String()
		plan := topologyPlanForTest(scheduler, util.TopologyModeHard, 1, "node0")
		scheduler.applyTopologyScore(plan, pod, scores)

		assert.InDelta(t, 100+float32(config.DefaultTopologyScoreWeight)*0.75, scores.NodeList[0].Score, 0.001)
		assert.InDelta(t, 100+float32(config.DefaultTopologyScoreWeight)*0.5, scores.NodeList[1].Score, 0.001)
		assert.InDelta(t, 100+float32(config.DefaultTopologyScoreWeight)*0.25, scores.NodeList[2].Score, 0.001)
		assert.InDelta(t, 100, scores.NodeList[3].Score, 0.001, "a node outside the fabric is left alone")
	})

	t.Run("spread rewards the closer node too", func(t *testing.T) {
		scores := scored()
		scores.Policy = util.NodeSchedulerPolicySpread.String()
		plan := topologyPlanForTest(scheduler, util.TopologyModeHard, 1, "node0")
		scheduler.applyTopologyScore(plan, pod, scores)

		assert.InDelta(t, 100-float32(config.DefaultTopologyScoreWeight)*0.75, scores.NodeList[0].Score, 0.001)
		assert.InDelta(t, 100-float32(config.DefaultTopologyScoreWeight)*0.5, scores.NodeList[1].Score, 0.001)
		assert.InDelta(t, 100-float32(config.DefaultTopologyScoreWeight)*0.25, scores.NodeList[2].Score, 0.001)
	})

	t.Run("the configured weight scales the bonus", func(t *testing.T) {
		config.TopologyScoreWeight = 40
		scores := scored()
		scores.Policy = util.NodeSchedulerPolicyBinpack.String()
		plan := topologyPlanForTest(scheduler, util.TopologyModeHard, 1, "node0")
		scheduler.applyTopologyScore(plan, pod, scores)

		assert.InDelta(t, 100+float32(40)*0.75, scores.NodeList[0].Score, 0.001)
	})

	t.Run("nothing placed and no plan are no-ops", func(t *testing.T) {
		scores := scored()
		scores.Policy = util.NodeSchedulerPolicyBinpack.String()
		scheduler.applyTopologyScore(topologyPlanForTest(scheduler, util.TopologyModeHard, 1), pod, scores)
		assert.InDelta(t, 100, scores.NodeList[0].Score, 0.001)

		scheduler.applyTopologyScore(nil, pod, scores)
		assert.InDelta(t, 100, scores.NodeList[0].Score, 0.001)
	})
}

// TestTopologyPlacedNodes collects the nodes the group occupies from both the
// Pod lister, which covers the pods the API server has already bound, and the
// scheduler's own reservations, which cover the siblings of this gang placed in
// this same round.
func TestTopologyPlacedNodes(t *testing.T) {
	scheduler := topologyTestScheduler(t, cceTopologyNodes())
	factory := informers.NewSharedInformerFactoryWithOptions(fake.NewSimpleClientset(), time.Hour)
	scheduler.podLister = factory.Core().V1().Pods().Lister()
	indexer := factory.Core().V1().Pods().Informer().GetIndexer()

	task := topologyPod(
		map[string]string{util.TopologyModeAnnotationKey: util.TopologyModeHard, util.TopologyHighestTierAnnotationKey: "1"},
		map[string]string{"job-name": "train"},
	)
	bound := func(name, node string, phase corev1.PodPhase) *corev1.Pod {
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: "default", UID: types.UID(name),
			Labels: map[string]string{"job-name": "train"},
		}}
		pod.Spec.NodeName = node
		pod.Status.Phase = phase
		return pod
	}
	unrelated := bound("other-0", "node5", corev1.PodRunning)
	unrelated.Labels = map[string]string{"job-name": "other"}

	pending := bound("train-2", "", corev1.PodPending)
	pending.Spec.NodeName = ""
	finished := bound("train-3", "node2", corev1.PodSucceeded)
	reserved := bound("train-4", "", corev1.PodPending)

	for _, pod := range []*corev1.Pod{task, bound("train-1", "node1", corev1.PodRunning), pending, finished, unrelated, reserved} {
		require.NoError(t, indexer.Add(pod))
	}
	// The sibling placed by this scheduler in this same round is only known to
	// the reservation manager, which is exactly what makes a gang stay together
	// before the API server sees it as bound.
	require.True(t, scheduler.podManager.AddPod(reserved, "node0", device.PodDevices{}))

	require.Equal(t, []string{"node0", "node1"}, scheduler.topologyPlacedNodes(task, topologyGroupKey(task)))

	plan := scheduler.topologyPlanFor(task)
	require.NotNil(t, plan)
	assert.Equal(t, []string{"node0", "node1"}, plan.placed)
	assert.Equal(t, util.TopologyModeHard, plan.mode)
	assert.Equal(t, 1, plan.highest)
	assert.Equal(t, "default/job-name/train", plan.group)
}

// TestTopologyPlanGating covers the cases where the plan must stay nil, which is
// what keeps the placement of every other workload byte for byte what it was.
func TestTopologyPlanGating(t *testing.T) {
	t.Run("the feature is off when no tier labels are configured", func(t *testing.T) {
		originalLabels := config.TopologyTierLabels
		t.Cleanup(func() { config.TopologyTierLabels = originalLabels })
		config.TopologyTierLabels = ""

		scheduler := NewScheduler()
		scheduler.rebuildTopology(cceTopologyNodes())
		assert.Nil(t, scheduler.topologySnapshot())

		task := topologyPod(map[string]string{util.TopologyHighestTierAnnotationKey: "1"}, map[string]string{"job-name": "train"})
		assert.Nil(t, scheduler.topologyPlanFor(task))
	})

	t.Run("a pod that does not opt in is not constrained", func(t *testing.T) {
		scheduler := topologyTestScheduler(t, cceTopologyNodes())
		task := topologyPod(nil, map[string]string{"job-name": "train"})
		assert.Nil(t, scheduler.topologyPlanFor(task))
	})

	t.Run("a pod in no group is not constrained", func(t *testing.T) {
		scheduler := topologyTestScheduler(t, cceTopologyNodes())
		task := topologyPod(map[string]string{util.TopologyModeAnnotationKey: util.TopologyModeHard}, nil)
		assert.Nil(t, scheduler.topologyPlanFor(task))
		assert.Nil(t, scheduler.topologyPlanFor(nil))
	})

	t.Run("a mode without a tier ranks within the whole hierarchy", func(t *testing.T) {
		scheduler := topologyTestScheduler(t, cceTopologyNodes())
		config.TopologyMode = util.TopologyModeHard
		task := topologyPod(map[string]string{util.TopologyModeAnnotationKey: util.TopologyModeSoft}, map[string]string{"job-name": "train"})

		plan := scheduler.topologyPlanFor(task)
		require.NotNil(t, plan)
		assert.Equal(t, util.TopologyModeSoft, plan.mode, "the Pod names its own mode")
		assert.Equal(t, 3, plan.highest, "unlimited resolves to the depth of the configured hierarchy")
	})

	t.Run("a tier without a mode takes the configured mode", func(t *testing.T) {
		scheduler := topologyTestScheduler(t, cceTopologyNodes())
		config.TopologyMode = util.TopologyModeHard
		task := topologyPod(map[string]string{util.TopologyHighestTierAnnotationKey: "2"}, map[string]string{"job-name": "train"})

		plan := scheduler.topologyPlanFor(task)
		require.NotNil(t, plan)
		assert.Equal(t, util.TopologyModeHard, plan.mode)
		assert.Equal(t, 2, plan.highest)
	})
}

// superPodAnnotation is the node annotation the ascend device plugin writes with
// the super-pod the NPUs of a node are wired to. The plugin knows its own key,
// the scheduler is told about it, so the literal is repeated here on purpose:
// this is the wire contract between the two components.
const superPodAnnotation = "hami.io/npu-super-pod-id"

// superPodTierSpec is the tier list such a cluster is started with. The "@"
// makes the tier an annotation tier, which is what lets the hardware place the
// node in the hierarchy instead of an operator.
const superPodTierSpec = "@" + superPodAnnotation

// superPodNodes is the fabric the plugin reports on a four node cluster: node0
// and node1 sit in super-pod 0, node2 and node3 in super-pod 1, and node4 runs
// a product without a super-pod, for which the plugin publishes nothing.
func superPodNodes() []*corev1.Node {
	fabric := [][2]string{
		{"node0", "0"},
		{"node1", "0"},
		{"node2", "1"},
		{"node3", "1"},
	}
	nodes := make([]*corev1.Node, 0, len(fabric)+1)
	for _, entry := range fabric {
		nodes = append(nodes, &corev1.Node{ObjectMeta: metav1.ObjectMeta{
			Name:        entry[0],
			Annotations: map[string]string{superPodAnnotation: entry[1]},
		}})
	}
	return append(nodes, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node4"}})
}

// superPodTestScheduler returns a scheduler whose cached hierarchy was built
// from the annotations the device plugin publishes, with the tier flag pointed
// at them. It restores the configuration when the test ends.
func superPodTestScheduler(t *testing.T) *Scheduler {
	t.Helper()

	originalLabels := config.TopologyTierLabels
	originalMode := config.TopologyMode
	originalWeight := config.TopologyScoreWeight
	t.Cleanup(func() {
		config.TopologyTierLabels = originalLabels
		config.TopologyMode = originalMode
		config.TopologyScoreWeight = originalWeight
	})

	config.TopologyTierLabels = superPodTierSpec
	config.TopologyMode = util.TopologyModeSoft
	config.TopologyScoreWeight = config.DefaultTopologyScoreWeight

	scheduler := NewScheduler()
	scheduler.rebuildTopology(superPodNodes())
	require.NotNil(t, scheduler.topologySnapshot())
	return scheduler
}

// TestTopologyFromSuperPodAnnotations is the acceptance test of the migration:
// the device plugin publishes where the NPUs of a node are wired, the scheduler
// reads that annotation as its smallest tier, and a training job then stays in
// one super-pod without a single node label written by hand.
func TestTopologyFromSuperPodAnnotations(t *testing.T) {
	scheduler := superPodTestScheduler(t)
	pod := topologyPod(nil, nil)

	t.Run("the published super pod becomes the smallest tier", func(t *testing.T) {
		snapshot := scheduler.topologySnapshot()
		require.NotNil(t, snapshot)
		assert.Equal(t, 1, snapshot.tree.MaxTier())
		assert.Equal(t, []string{"node0", "node1"}, snapshot.tree.Chain("node0")[1].Nodes)
		assert.False(t, snapshot.tree.InModel("node4"), "a node the plugin said nothing about stays outside")
	})

	t.Run("a hard request keeps the job in the super pod of its first pod", func(t *testing.T) {
		candidates := candidateNodes("node0", "node1", "node2", "node3", "node4")
		failedNodes := map[string]string{}
		scheduler.applyTopologyFilter(topologyPlanForTest(scheduler, util.TopologyModeHard, 1, "node1"), pod, &candidates, failedNodes)

		assert.Len(t, candidates, 3, "the two nodes of the super pod and the node outside the hierarchy survive")
		assert.Contains(t, candidates, "node1")
		assert.Contains(t, candidates, "node0")
		assert.Contains(t, candidates, "node4", "a node outside the described fabric is not constrained")
		assert.Len(t, failedNodes, 2)
		assert.Equal(t, 1, common.ParseReason(failedNodes["node2"])[common.HyperNodeTierNotFit],
			"a node of another super pod must report the topology reason")
		assert.Equal(t, 1, common.ParseReason(failedNodes["node3"])[common.HyperNodeTierNotFit])
	})

	t.Run("ranking prefers the nodes of the super pod of the group", func(t *testing.T) {
		scores := &policy.NodeScoreList{NodeList: []*policy.NodeScore{
			{NodeID: "node1", Score: 100},
			{NodeID: "node2", Score: 100},
			{NodeID: "node4", Score: 100},
		}}
		scores.Policy = util.NodeSchedulerPolicyBinpack.String()
		scheduler.applyTopologyScore(topologyPlanForTest(scheduler, util.TopologyModeHard, 1, "node0"), pod, scores)

		// The only tier above the node is the super-pod, so a node sharing it
		// is halfway to being the same place, and a node of another super-pod
		// shares nothing at all. A node outside the described fabric is left
		// exactly where it was.
		closer := 100 + float32(config.DefaultTopologyScoreWeight)*0.5
		assert.InDelta(t, closer, scores.NodeList[0].Score, 0.001)
		assert.InDelta(t, 100, scores.NodeList[1].Score, 0.001)
		assert.InDelta(t, 100, scores.NodeList[2].Score, 0.001)
	})
}

// TestOnUpdateNodeNotifiesTopologyChange covers the notification the node
// UpdateFunc sends. It has to fire when the place of a node in the fabric
// changed, which is how the annotation the device plugin writes seconds after
// the node reached the scheduler is picked up, and must stay silent for the
// updates the kubelet produces on its own every few seconds.
func TestOnUpdateNodeNotifiesTopologyChange(t *testing.T) {
	nodeWith := func(annotations, labels map[string]string) *corev1.Node {
		return &corev1.Node{ObjectMeta: metav1.ObjectMeta{
			Name: "node0", Annotations: annotations, Labels: labels,
		}}
	}
	notified := func(scheduler *Scheduler) bool {
		select {
		case <-scheduler.nodeNotify:
			return true
		default:
			return false
		}
	}

	tests := []struct {
		name       string
		tierLabels string
		oldNode    *corev1.Node
		newNode    *corev1.Node
		want       bool
	}{
		{
			name:       "the device plugin published the super pod of the node",
			tierLabels: superPodTierSpec,
			oldNode:    nodeWith(nil, nil),
			newNode:    nodeWith(map[string]string{superPodAnnotation: "0"}, nil),
			want:       true,
		},
		{
			name:       "the node moved to another super pod",
			tierLabels: superPodTierSpec,
			oldNode:    nodeWith(map[string]string{superPodAnnotation: "0"}, nil),
			newNode:    nodeWith(map[string]string{superPodAnnotation: "1"}, nil),
			want:       true,
		},
		{
			name:       "an annotation the hierarchy does not read changed",
			tierLabels: superPodTierSpec,
			oldNode:    nodeWith(map[string]string{"hami.io/node-handshake-Ascend910B4": "Reported_a"}, nil),
			newNode:    nodeWith(map[string]string{"hami.io/node-handshake-Ascend910B4": "Reported_b"}, nil),
			want:       false,
		},
		{
			name:       "a label tier changed",
			tierLabels: cceTierSpec,
			oldNode:    nodeWith(nil, map[string]string{"test-tier-1": "HyperNode0"}),
			newNode:    nodeWith(nil, map[string]string{"test-tier-1": "HyperNode1"}),
			want:       true,
		},
		{
			name:       "a hierarchy built from labels and annotations watches both",
			tierLabels: "test-tier-1," + superPodTierSpec,
			oldNode:    nodeWith(map[string]string{superPodAnnotation: "0"}, map[string]string{"test-tier-1": "HyperNode0"}),
			newNode:    nodeWith(map[string]string{superPodAnnotation: "2"}, map[string]string{"test-tier-1": "HyperNode0"}),
			want:       true,
		},
		{
			name:       "the hierarchy is not configured",
			tierLabels: "",
			oldNode:    nodeWith(nil, nil),
			newNode:    nodeWith(map[string]string{superPodAnnotation: "0"}, nil),
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalLabels := config.TopologyTierLabels
			t.Cleanup(func() { config.TopologyTierLabels = originalLabels })
			config.TopologyTierLabels = tt.tierLabels

			scheduler := NewScheduler()
			scheduler.rebuildTopology([]*corev1.Node{tt.oldNode})

			scheduler.onUpdateNode(tt.oldNode, tt.newNode)

			assert.Equal(t, tt.want, notified(scheduler))
		})
	}

	t.Run("an update that did not move the node is not worth a refresh", func(t *testing.T) {
		scheduler := superPodTestScheduler(t)
		oldNode := nodeWith(map[string]string{superPodAnnotation: "0"}, nil)
		newNode := nodeWith(map[string]string{superPodAnnotation: "0"}, nil)

		scheduler.onUpdateNode(oldNode, newNode)

		assert.False(t, notified(scheduler))
	})

	t.Run("objects that are not nodes are ignored", func(t *testing.T) {
		scheduler := superPodTestScheduler(t)

		scheduler.onUpdateNode(&corev1.Pod{}, &corev1.Pod{})
		scheduler.onUpdateNode("not a node", 42)

		assert.False(t, notified(scheduler))
	})
}

// gangFabric describes a three-node fabric: A and B share a switch in row-1,
// C sits alone in row-2, and all three share fabric-1. It is the smallest
// fabric that exercises every configured level.
func gangFabric() []*corev1.Node {
	fabric := [][4]string{
		{"gang-a", "switch-1", "row-1", "fabric-1"},
		{"gang-b", "switch-1", "row-1", "fabric-1"},
		{"gang-c", "switch-2", "row-2", "fabric-1"},
	}
	nodes := make([]*corev1.Node, 0, len(fabric))
	for _, entry := range fabric {
		nodes = append(nodes, &corev1.Node{ObjectMeta: metav1.ObjectMeta{
			Name: entry[0],
			Labels: map[string]string{
				"test-tier-1": entry[1],
				"test-tier-2": entry[2],
				"test-tier-3": entry[3],
			},
		}})
	}
	return nodes
}

// TestTopologyGangFeasibleNodes covers the domain arithmetic of the
// all-or-nothing check independently of device scoring.
func TestTopologyGangFeasibleNodes(t *testing.T) {
	scheduler := topologyTestScheduler(t, gangFabric())
	tree := scheduler.topologySnapshot().tree
	candidates := candidateNodes("gang-a", "gang-b", "gang-c")

	t.Run("two separate ToRs cannot hold a gang of two", func(t *testing.T) {
		capacity := map[string]int{"gang-a": 1, "gang-b": 1, "gang-c": 1}
		feasible, _ := topologyGangFeasibleNodes(tree, candidates, capacity, nil, 1, 2)
		assert.Empty(t, feasible, "each tier 1 domain only has room for one member")
	})

	t.Run("the ToR that already holds both nodes is the only fit", func(t *testing.T) {
		capacity := map[string]int{"gang-a": 1, "gang-b": 1, "gang-c": 1}
		feasible, domains := topologyGangFeasibleNodes(tree, candidates, capacity, nil, 1, 2)
		assert.Contains(t, feasible, "gang-a")
		assert.Contains(t, feasible, "gang-b")
		assert.NotContains(t, feasible, "gang-c")
		assert.Len(t, domains, 1, "only switch-1 has two slots")
	})

	t.Run("a middle tier domain can satisfy the gang on its own", func(t *testing.T) {
		capacity := map[string]int{"gang-a": 0, "gang-b": 0, "gang-c": 2}
		feasible, _ := topologyGangFeasibleNodes(tree, candidates, capacity, nil, 2, 2)
		assert.Equal(t, map[string]struct{}{"gang-c": {}}, feasible)
	})

	t.Run("the cluster tier admits every node", func(t *testing.T) {
		capacity := map[string]int{"gang-a": 1, "gang-b": 1, "gang-c": 1}
		feasible, _ := topologyGangFeasibleNodes(tree, candidates, capacity, nil, 3, 2)
		assert.Len(t, feasible, 3)
	})

	t.Run("a placed member pins the gang to its domain", func(t *testing.T) {
		capacity := map[string]int{"gang-a": 0, "gang-b": 1, "gang-c": 1}
		feasible, _ := topologyGangFeasibleNodes(tree, candidates, capacity, []string{"gang-a"}, 1, 1)
		assert.Equal(t, map[string]struct{}{"gang-b": {}}, feasible)
	})

	t.Run("no domain under the cap can hold the last member", func(t *testing.T) {
		capacity := map[string]int{"gang-a": 0, "gang-b": 0, "gang-c": 1}
		feasible, _ := topologyGangFeasibleNodes(tree, candidates, capacity, []string{"gang-a"}, 2, 1)
		assert.Empty(t, feasible, "gang-c is in another rack and the cap is rack level")
	})

	t.Run("a wider cap lets the last member leave the rack", func(t *testing.T) {
		capacity := map[string]int{"gang-a": 0, "gang-b": 0, "gang-c": 1}
		feasible, _ := topologyGangFeasibleNodes(tree, candidates, capacity, []string{"gang-a"}, 3, 1)
		assert.Equal(t, map[string]struct{}{"gang-c": {}}, feasible)
	})
}

// TestTopologyGangNeedsDistinctNodes checks that the capacity simulation knows
// when a hostname anti-affinity term makes one Pod per node the limit.
func TestTopologyGangNeedsDistinctNodes(t *testing.T) {
	affinity := func(selectorLabels map[string]string) *corev1.Pod {
		pod := topologyPod(nil, map[string]string{"job": "train"})
		pod.Spec.Affinity = &corev1.Affinity{PodAntiAffinity: &corev1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{{
				TopologyKey:   corev1.LabelHostname,
				LabelSelector: &metav1.LabelSelector{MatchLabels: selectorLabels},
			}},
		}}
		return pod
	}

	assert.True(t, topologyGangNeedsDistinctNodes(affinity(map[string]string{"job": "train"})))
	assert.False(t, topologyGangNeedsDistinctNodes(affinity(map[string]string{"job": "other"})))
	assert.False(t, topologyGangNeedsDistinctNodes(topologyPod(nil, nil)))
}
