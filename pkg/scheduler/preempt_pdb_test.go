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
	"errors"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	policylisters "k8s.io/client-go/listers/policy/v1"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
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
	s.pdbInformerSynced = true
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
		request *extenderv1.MetaVictims
		want    *extenderv1.MetaVictims
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

			res, err := s.Preempt(extenderv1.ExtenderPreemptionArgs{
				Pod:                   pod,
				NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{moveNodeX: tt.request},
			})
			assert.NilError(t, err)
			assert.DeepEqual(t, res.NodeNameToMetaVictims, map[string]*extenderv1.MetaVictims{moveNodeX: tt.want})
		})
	}
}

// Without PodDisruptionBudgets Preempt picks no victim. A pod that asks for
// no HAMi device needs none, and is still answered.
func Test_Preempt_NeedsDisruptionBudgets(t *testing.T) {
	s, pod := devicePreemptor(t)
	s.pdbLister, s.pdbInformerSynced = nil, false
	track(t, s, holding(trackedOn(moveNodeX, "low", 10), 8000))

	res, err := s.Preempt(extenderv1.ExtenderPreemptionArgs{
		Pod:                   pod,
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{moveNodeX: {}},
	})
	assert.ErrorContains(t, err, "informer factory is unavailable")
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

// The informer starts on first use and lists what the API server has.
func Test_ensurePDBInformer(t *testing.T) {
	stopCh := make(chan struct{})
	defer close(stopCh)

	s := &Scheduler{
		informerFactory: informers.NewSharedInformerFactory(fake.NewClientset(budgetFor("db", 1)), 0),
		stopCh:          stopCh,
	}
	assert.Assert(t, s.pdbLister == nil, "the lister must not exist before Preempt needs it")

	assert.NilError(t, s.ensurePDBInformer())
	pdbs, err := s.pdbLister.List(labels.Everything())
	assert.NilError(t, err)
	assert.Equal(t, len(pdbs), 1)
	assert.Equal(t, pdbs[0].Name, "db")
	assert.NilError(t, s.ensurePDBInformer())
}

// A cache that cannot sync, for example without the RBAC rule, fails the call
// after the timeout and does not block it for good.
func Test_ensurePDBInformer_BoundsSyncWait(t *testing.T) {
	stopCh := make(chan struct{})
	defer close(stopCh)

	clientset := fake.NewClientset()
	clientset.PrependReactor("list", "*", func(k8stesting.Action) (bool, runtime.Object, error) {
		<-stopCh
		return true, nil, errors.New("informer stopped")
	})
	s := &Scheduler{
		informerFactory:    informers.NewSharedInformerFactory(clientset, 0),
		stopCh:             stopCh,
		pdbInformerTimeout: 30 * time.Millisecond,
	}

	err := s.ensurePDBInformer()
	assert.ErrorContains(t, err, "within 30ms")
	assert.Assert(t, !s.pdbInformerSynced)
}
