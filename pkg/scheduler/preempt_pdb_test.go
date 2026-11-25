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
	"context"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	policylisters "k8s.io/client-go/listers/policy/v1"
	"k8s.io/client-go/tools/cache"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

// budgetFor returns a PodDisruptionBudget for the pods labeled app in the
// default namespace, which allows that many more evictions.
func budgetFor(app string, allowed int32) *policyv1.PodDisruptionBudget {
	return &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Name: app, Namespace: "default"},
		Spec: policyv1.PodDisruptionBudgetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": app}},
		},
		Status: policyv1.PodDisruptionBudgetStatus{DisruptionsAllowed: allowed},
	}
}

// withBudgets gives s the PodDisruptionBudgets its informer would hold.
func withBudgets(t *testing.T, s *Scheduler, pdbs ...*policyv1.PodDisruptionBudget) {
	t.Helper()
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	for _, pdb := range pdbs {
		assert.NilError(t, indexer.Add(pdb))
	}
	s.pdbLister = policylisters.NewPodDisruptionBudgetLister(indexer)
	s.pdbSynced = func() bool { return true }
}

// withPods gives s the index of pods by UID that its informer would hold,
// with the tracked pods and others.
func withPods(t *testing.T, s *Scheduler, tracked []*device.PodInfo, others ...*corev1.Pod) {
	t.Helper()
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{podUIDIndex: indexPodByUID})
	for _, pi := range tracked {
		assert.NilError(t, indexer.Add(pi.Pod))
	}
	for _, pod := range others {
		assert.NilError(t, indexer.Add(pod))
	}
	s.podIndexer = indexer
}

// labeled gives a pod of trackedOn the label a budget of budgetFor selects.
func labeled(pi *device.PodInfo, app string) *device.PodInfo {
	pi.Labels = map[string]string{"app": app}
	return pi
}

// Preempt spares the pods a PodDisruptionBudget protects first, and adds the
// ones it cannot spare to NumPDBViolations.
func Test_Preempt_CountsDisruptionBudgets(t *testing.T) {
	victims := func(violations int64, uids ...string) *extenderv1.MetaVictims {
		meta := &extenderv1.MetaVictims{Pods: []*extenderv1.MetaPod{}, NumPDBViolations: violations}
		for _, uid := range uids {
			meta.Pods = append(meta.Pods, &extenderv1.MetaPod{UID: uid})
		}
		return meta
	}
	otherNamespace := budgetFor("db", 0)
	otherNamespace.Namespace = "another"
	alreadyCounted := budgetFor("db", 0)
	alreadyCounted.Status.DisruptedPods = map[string]metav1.Time{"protected": metav1.Now()}
	invalidSelector := budgetFor("db", 0)
	invalidSelector.Spec.Selector = &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
		{Key: "app", Operator: "Unknown", Values: []string{"db"}},
	}}
	everyPod := budgetFor("all", 0)
	everyPod.Spec.Selector = &metav1.LabelSelector{}

	tests := []struct {
		name    string
		budgets []*policyv1.PodDisruptionBudget
		tracked []*device.PodInfo
		// others are pods that hold no HAMi device.
		others []*corev1.Pod
		// unlisted are tracked pods the informer has dropped already.
		unlisted []*device.PodInfo
		request  *extenderv1.MetaVictims
		want     *extenderv1.MetaVictims
	}{
		{
			// By importance alone the pod of priority 500 would be spared.
			name:    "a protected pod is spared before a more important pod",
			budgets: []*policyv1.PodDisruptionBudget{budgetFor("db", 0)},
			tracked: []*device.PodInfo{
				labeled(holding(trackedOn(moveNodeX, "protected", 10), 4000), "db"),
				holding(trackedOn(moveNodeX, "plain", 500), 4000),
			},
			request: victims(0),
			want:    victims(0, "plain"),
		},
		{
			name:    "a protected pod that has to go is added to the count",
			budgets: []*policyv1.PodDisruptionBudget{budgetFor("db", 0)},
			tracked: []*device.PodInfo{
				labeled(holding(trackedOn(moveNodeX, "protected", 10), 8000), "db"),
			},
			request: victims(2, "without-devices"),
			want:    victims(3, "without-devices", "protected"),
		},
		{
			name:    "a budget that allows the eviction is not broken",
			budgets: []*policyv1.PodDisruptionBudget{budgetFor("db", 1)},
			tracked: []*device.PodInfo{
				labeled(holding(trackedOn(moveNodeX, "protected", 10), 8000), "db"),
			},
			request: victims(0),
			want:    victims(0, "protected"),
		},
		{
			// The budget allows one eviction, and db-a is first in line for
			// it. The answer lists the victims most important first.
			name:    "a budget is used up in the order of the candidates",
			budgets: []*policyv1.PodDisruptionBudget{budgetFor("db", 1)},
			tracked: []*device.PodInfo{
				holding(trackedOn(moveNodeX, "higher", 2000), 7000),
				labeled(holding(trackedOn(moveNodeX, "db-b", 10), 500), "db"),
				labeled(holding(trackedOn(moveNodeX, "db-a", 10), 600), "db"),
			},
			request: victims(0),
			want:    victims(1, "db-a", "db-b"),
		},
		{
			name:    "a proposed victim uses the budget first",
			budgets: []*policyv1.PodDisruptionBudget{budgetFor("db", 1)},
			tracked: []*device.PodInfo{
				labeled(holding(trackedOn(moveNodeX, "proposed", 10), 100), "db"),
				labeled(holding(trackedOn(moveNodeX, "other", 10), 8000), "db"),
			},
			request: victims(0, "proposed"),
			want:    victims(1, "proposed", "other"),
		},
		{
			// kube-scheduler proposed it for CPU or memory. HAMi does not
			// track it, but it uses the budget all the same.
			name:    "a proposed victim without devices uses the budget too",
			budgets: []*policyv1.PodDisruptionBudget{budgetFor("db", 1)},
			tracked: []*device.PodInfo{
				labeled(holding(trackedOn(moveNodeX, "gpu", 10), 8000), "db"),
			},
			others: []*corev1.Pod{{ObjectMeta: metav1.ObjectMeta{
				Name: "cpu-only", Namespace: "default", UID: "cpu-only", Labels: map[string]string{"app": "db"},
			}}},
			request: victims(0, "cpu-only"),
			want:    victims(1, "cpu-only", "gpu"),
		},
		{
			name:    "a proposed victim only the pod cache knows uses the budget too",
			budgets: []*policyv1.PodDisruptionBudget{budgetFor("db", 1)},
			tracked: []*device.PodInfo{
				labeled(holding(trackedOn(moveNodeX, "other", 10), 8000), "db"),
			},
			unlisted: []*device.PodInfo{
				labeled(holding(trackedOn(moveNodeX, "proposed", 10), 100), "db"),
			},
			request: victims(0, "proposed"),
			want:    victims(1, "proposed", "other"),
		},
		{
			name:    "a proposed victim nobody knows uses no budget",
			budgets: []*policyv1.PodDisruptionBudget{budgetFor("db", 1)},
			tracked: []*device.PodInfo{
				labeled(holding(trackedOn(moveNodeX, "protected", 10), 8000), "db"),
			},
			request: victims(0, "unknown"),
			want:    victims(0, "unknown", "protected"),
		},
		{
			name:    "a budget of another namespace does not cover the pod",
			budgets: []*policyv1.PodDisruptionBudget{otherNamespace},
			tracked: []*device.PodInfo{
				labeled(holding(trackedOn(moveNodeX, "protected", 10), 8000), "db"),
			},
			request: victims(0),
			want:    victims(0, "protected"),
		},
		{
			name:    "a pod in DisruptedPods is not counted again",
			budgets: []*policyv1.PodDisruptionBudget{alreadyCounted},
			tracked: []*device.PodInfo{
				labeled(holding(trackedOn(moveNodeX, "protected", 10), 8000), "db"),
			},
			request: victims(0),
			want:    victims(0, "protected"),
		},
		{
			name:    "a budget with an invalid selector covers no pod",
			budgets: []*policyv1.PodDisruptionBudget{invalidSelector},
			tracked: []*device.PodInfo{
				labeled(holding(trackedOn(moveNodeX, "protected", 10), 8000), "db"),
			},
			request: victims(0),
			want:    victims(0, "protected"),
		},
		{
			name:    "a budget with an empty selector covers every pod of its namespace",
			budgets: []*policyv1.PodDisruptionBudget{everyPod},
			tracked: []*device.PodInfo{
				holding(trackedOn(moveNodeX, "plain", 10), 8000),
			},
			request: victims(0),
			want:    victims(1, "plain"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, pod := devicePreemptor(t)
			withBudgets(t, s, tt.budgets...)
			track(t, s, tt.tracked...)
			track(t, s, tt.unlisted...)
			withPods(t, s, tt.tracked, tt.others...)

			res, err := s.Preempt(extenderv1.ExtenderPreemptionArgs{
				Pod:                   pod,
				NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{moveNodeX: tt.request},
			})
			assert.NilError(t, err)
			assert.DeepEqual(t, res.NodeNameToMetaVictims, map[string]*extenderv1.MetaVictims{moveNodeX: tt.want})
		})
	}
}

// A pod uses every budget that covers it, so the order of the budgets does
// not change the result. The proposed pod breaks the strict budget and uses
// up the shared one, which the candidate then breaks.
func Test_disruptionBudgets_split_CountsEveryBudget(t *testing.T) {
	strict := budgetFor("db", 0)
	shared := budgetFor("shared", 1)
	shared.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"tier": "data"}}

	proposed := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "proposed", Namespace: "default", UID: "proposed",
		Labels: map[string]string{"app": "db", "tier": "data"},
	}}
	candidate := trackedOn(moveNodeX, "candidate", 10)
	candidate.Labels = map[string]string{"tier": "data"}

	for name, pdbs := range map[string][]*policyv1.PodDisruptionBudget{
		"strict budget first": {strict, shared},
		"shared budget first": {shared, strict},
	} {
		t.Run(name, func(t *testing.T) {
			budgets := newDisruptionBudgets(pdbs)
			budgets.proposed = map[k8stypes.UID]*corev1.Pod{proposed.UID: proposed}

			protected, others := budgets.split(nil, []*extenderv1.MetaPod{{UID: "proposed"}}, []*device.PodInfo{candidate})
			assert.Equal(t, len(protected), 1)
			assert.Assert(t, protected[0] == candidate)
			assert.Equal(t, len(others), 0)
		})
	}
}

// Proposed victims are looked up in the index of pods by UID, which also holds
// the pods without a HAMi device.
func Test_proposedVictims(t *testing.T) {
	request := map[string]*extenderv1.MetaVictims{
		moveNodeX: {Pods: []*extenderv1.MetaPod{{UID: "cpu-only"}, {UID: "gone"}}},
	}
	s := NewScheduler()
	found, err := s.proposedVictims(request)
	assert.NilError(t, err)
	assert.Equal(t, len(found), 0)

	cpuOnly := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "cpu-only", Namespace: "default", UID: "cpu-only"}}
	other := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "default", UID: "other"}}
	withPods(t, s, nil, cpuOnly, other)
	found, err = s.proposedVictims(request)
	assert.NilError(t, err)
	assert.Equal(t, len(found), 1)
	assert.Assert(t, found["cpu-only"] == cpuOnly)

	// Without the index the lookup fails, and Preempt with it.
	s.podIndexer = cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	_, err = s.proposedVictims(request)
	assert.Assert(t, err != nil)
}

// Preempt does not wait for PodDisruptionBudgets. While their cache is not
// synced it picks no victim and fails at once. A pod that asks for no HAMi
// device needs none, and is still answered.
func Test_Preempt_NeedsDisruptionBudgets(t *testing.T) {
	s, pod := devicePreemptor(t)
	track(t, s, holding(trackedOn(moveNodeX, "low", 10), 8000))
	request := extenderv1.ExtenderPreemptionArgs{
		Pod:                   pod,
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{moveNodeX: {}},
	}

	s.pdbSynced = func() bool { return false }
	res, err := s.Preempt(request)
	assert.ErrorContains(t, err, "not synced")
	assert.Assert(t, res == nil)

	// A scheduler that was not started has no informer at all.
	s.pdbLister, s.pdbSynced = nil, nil
	res, err = s.Preempt(request)
	assert.ErrorContains(t, err, "not synced")
	assert.Assert(t, res == nil)

	other := preemptorPod()
	other.Name, other.UID = "no-devices", "no-devices-uid"
	seedPods(t, s, other)
	res, err = s.Preempt(extenderv1.ExtenderPreemptionArgs{
		Pod:                   other,
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{moveNodeX: {}},
	})
	assert.NilError(t, err)
	assert.Equal(t, len(res.NodeNameToMetaVictims), 1)
}

// Start prepares what Preempt reads: the PodDisruptionBudget informer, which
// it does not wait for, and the index of pods by UID.
func Test_Start_PreparesPreemptionCaches(t *testing.T) {
	victim := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "victim", Namespace: "default", UID: "victim-uid"}}
	previousClient := client.KubeClient
	client.KubeClient = fake.NewClientset(victim, budgetFor("db", 1))
	t.Cleanup(func() { client.KubeClient = previousClient })
	previousLeaderElect := config.LeaderElect
	config.LeaderElect = false
	t.Cleanup(func() { config.LeaderElect = previousLeaderElect })

	s := NewScheduler()
	assert.NilError(t, s.Start())
	t.Cleanup(s.Stop)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	assert.Assert(t, cache.WaitForCacheSync(ctx.Done(), s.pdbSynced), "the PodDisruptionBudget cache did not sync")

	budgets, err := s.podDisruptionBudgets(map[string]*extenderv1.MetaVictims{
		moveNodeX: {Pods: []*extenderv1.MetaPod{{UID: "victim-uid"}}},
	})
	assert.NilError(t, err)
	assert.Equal(t, len(budgets.pdbs), 1)
	found := budgets.proposed["victim-uid"]
	assert.Assert(t, found != nil, "the proposed victim was not found by its UID")
	assert.Equal(t, found.Name, "victim")
}
