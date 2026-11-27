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

package device

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

const (
	scopeTestNS  = "team"
	scopeTestMem = "nvidia.com/gpumem"
)

func newScopeTestManager() *QuotaManager {
	return &QuotaManager{
		Quotas:       make(map[string]*DeviceQuota),
		objectLimits: make(map[string]map[string]map[string]int64),
	}
}

func scopePod(name, priorityClass string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: scopeTestNS, UID: k8stypes.UID("uid-" + name)},
		Spec: corev1.PodSpec{
			PriorityClassName: priorityClass,
			Containers:        []corev1.Container{{Name: "main"}},
		},
	}
}

func gpuMem(mem int64) PodDevices {
	return PodDevices{"NVIDIA": PodSingleDevice{{{UUID: "GPU-0", Type: "NVIDIA", Usedmem: mem}}}}
}

func scopedMemQuota(name, limit string, scopes []corev1.ResourceQuotaScope, exprs ...corev1.ScopedResourceSelectorRequirement) *corev1.ResourceQuota {
	q := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: scopeTestNS},
		Spec: corev1.ResourceQuotaSpec{
			Hard:   corev1.ResourceList{"limits." + scopeTestMem: resource.MustParse(limit)},
			Scopes: scopes,
		},
	}
	if len(exprs) > 0 {
		q.Spec.ScopeSelector = &corev1.ScopeSelector{MatchExpressions: exprs}
	}
	return q
}

func priorityIn(values ...string) corev1.ScopedResourceSelectorRequirement {
	return corev1.ScopedResourceSelectorRequirement{
		ScopeName: corev1.ResourceQuotaScopePriorityClass, Operator: corev1.ScopeSelectorOpIn, Values: values,
	}
}

func TestQuotaScopeMatching(t *testing.T) {
	initTest()
	withCPU := scopePod("burstable", "")
	withCPU.Spec.Containers[0].Resources.Requests = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")}
	withPodCPU := scopePod("pod-level", "")
	withPodCPU.Spec.Resources = &corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")}}
	withQOS := scopePod("qos-status", "")
	withQOS.Status.QOSClass = corev1.PodQOSGuaranteed
	gpuOnly := scopePod("gpu-only", "")
	gpuOnly.Spec.Containers[0].Resources.Limits = corev1.ResourceList{"nvidia.com/gpu": resource.MustParse("1")}
	terminating := scopePod("job", "")
	terminating.Spec.ActiveDeadlineSeconds = new(int64(600))
	crossNS := scopePod("cross", "")
	crossNS.Spec.Affinity = &corev1.Affinity{PodAntiAffinity: &corev1.PodAntiAffinity{
		PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{{
			Weight:          1,
			PodAffinityTerm: corev1.PodAffinityTerm{TopologyKey: "kubernetes.io/hostname", Namespaces: []string{"other"}},
		}},
	}}
	sameNS := scopePod("same", "")
	sameNS.Spec.Affinity = &corev1.Affinity{PodAffinity: &corev1.PodAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{{TopologyKey: "kubernetes.io/hostname"}},
	}}

	op := func(o corev1.ScopeSelectorOperator, values ...string) corev1.ScopedResourceSelectorRequirement {
		return corev1.ScopedResourceSelectorRequirement{ScopeName: corev1.ResourceQuotaScopePriorityClass, Operator: o, Values: values}
	}
	cases := []struct {
		name   string
		quota  *corev1.ResourceQuota
		pod    *corev1.Pod
		expect bool
	}{
		{"PriorityClass In matches", scopedMemQuota("q", "1", nil, priorityIn("high")), scopePod("p", "high"), true},
		{"PriorityClass In skips other", scopedMemQuota("q", "1", nil, priorityIn("high")), scopePod("p", "low"), false},
		{"PriorityClass NotIn matches empty", scopedMemQuota("q", "1", nil, op(corev1.ScopeSelectorOpNotIn, "high")), scopePod("p", ""), true},
		{"PriorityClass NotIn skips listed", scopedMemQuota("q", "1", nil, op(corev1.ScopeSelectorOpNotIn, "high")), scopePod("p", "high"), false},
		{"PriorityClass Exists needs a class", scopedMemQuota("q", "1", nil, op(corev1.ScopeSelectorOpExists)), scopePod("p", ""), false},
		{"PriorityClass Exists matches a class", scopedMemQuota("q", "1", nil, op(corev1.ScopeSelectorOpExists)), scopePod("p", "low"), true},
		{"PriorityClass DoesNotExist matches no class", scopedMemQuota("q", "1", nil, op(corev1.ScopeSelectorOpDoesNotExist)), scopePod("p", ""), true},
		{"PriorityClass DoesNotExist skips a class", scopedMemQuota("q", "1", nil, op(corev1.ScopeSelectorOpDoesNotExist)), scopePod("p", "low"), false},
		{"PriorityClass in scopes means Exists", scopedMemQuota("q", "1", []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopePriorityClass}), scopePod("p", "low"), true},
		{"invalid operator matches nothing", scopedMemQuota("q", "1", nil, op("Gt", "1")), scopePod("p", "1"), false},
		{"BestEffort without requests", scopedMemQuota("q", "1", []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeBestEffort}), gpuOnly, true},
		{"BestEffort skips cpu request", scopedMemQuota("q", "1", []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeBestEffort}), withCPU, false},
		{"BestEffort skips pod-level limits", scopedMemQuota("q", "1", []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeBestEffort}), withPodCPU, false},
		{"NotBestEffort uses status QOS", scopedMemQuota("q", "1", []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeNotBestEffort}), withQOS, true},
		{"Terminating", scopedMemQuota("q", "1", []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeTerminating}), terminating, true},
		{"NotTerminating", scopedMemQuota("q", "1", []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeNotTerminating}), terminating, false},
		{"CrossNamespacePodAffinity", scopedMemQuota("q", "1", []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeCrossNamespacePodAffinity}), crossNS, true},
		{"same namespace affinity", scopedMemQuota("q", "1", []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeCrossNamespacePodAffinity}), sameNS, false},
		{"VolumeAttributesClass never matches pods", scopedMemQuota("q", "1", []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeVolumeAttributesClass}), gpuOnly, false},
		{"all scopes must match", scopedMemQuota("q", "1", []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeTerminating}, priorityIn("high")), scopePod("p", "high"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sq := &scopedQuota{matchers: quotaScopeMatchers(tc.quota)}
			if got := sq.matches(scopesOf(tc.pod)); got != tc.expect {
				t.Errorf("matches() = %v, want %v", got, tc.expect)
			}
		})
	}
}

// A quota scoped to one PriorityClass used to be skipped, so it limited no pod.
func TestScopedQuotaLimitsMatchingPods(t *testing.T) {
	initTest()
	qm := newScopeTestManager()
	qm.AddQuota(scopedMemQuota("high", "2000", nil, priorityIn("high")))

	qm.AddUsage(scopePod("running", "high"), gpuMem(1500))
	if qm.FitQuotaForPod(scopePod("new", "high"), 1000, 0, "NVIDIA") {
		t.Error("a high pod fits the high quota with 1500 + 1000 > 2000")
	}
	if !qm.FitQuotaForPod(scopePod("new", "high"), 500, 0, "NVIDIA") {
		t.Error("a high pod does not fit the high quota with 1500 + 500 = 2000")
	}
	if !qm.FitQuotaForPod(scopePod("new", "low"), 8000, 0, "NVIDIA") {
		t.Error("the high quota limits a low pod")
	}
	if !qm.FitQuota(scopeTestNS, 8000, 0, "NVIDIA") {
		t.Error("a scoped quota became a namespace limit")
	}

	// A low pod's usage does not count against the high quota.
	qm.AddUsage(scopePod("other", "low"), gpuMem(4000))
	if !qm.FitQuotaForPod(scopePod("new", "high"), 500, 0, "NVIDIA") {
		t.Error("a low pod's usage was counted against the high quota")
	}
}

func TestScopedQuotaCountsPodsAddedBefore(t *testing.T) {
	initTest()
	qm := newScopeTestManager()
	qm.AddUsage(scopePod("a", "high"), gpuMem(1200))
	qm.AddUsage(scopePod("b", "high"), gpuMem(600))
	qm.AddUsage(scopePod("c", "low"), gpuMem(5000))
	qm.AddQuota(scopedMemQuota("high", "2000", nil, priorityIn("high")))

	if got := qm.scoped[scopeTestNS]["high"].used[scopeTestMem]; got != 1800 {
		t.Fatalf("used = %d, want 1800", got)
	}
	if qm.FitQuotaForPod(scopePod("new", "high"), 300, 0, "NVIDIA") {
		t.Error("the quota ignored the pods that were running before it")
	}
}

func TestScopedQuotaReleaseAndReplace(t *testing.T) {
	initTest()
	qm := newScopeTestManager()
	qm.AddQuota(scopedMemQuota("high", "2000", nil, priorityIn("high")))
	pod := scopePod("a", "high")
	qm.AddUsage(pod, gpuMem(1500))

	qm.ReplaceUsage(pod, gpuMem(1500), gpuMem(500))
	if got := qm.scoped[scopeTestNS]["high"].used[scopeTestMem]; got != 500 {
		t.Fatalf("used after replace = %d, want 500", got)
	}
	qm.RmUsage(pod, gpuMem(500))
	if got := qm.scoped[scopeTestNS]["high"].used[scopeTestMem]; got != 0 {
		t.Fatalf("used after release = %d, want 0", got)
	}
	if len(qm.podUsages) != 0 {
		t.Errorf("a released pod is still tracked: %v", qm.podUsages)
	}
	// Releasing a pod that was never counted changes nothing.
	qm.RmUsage(scopePod("unknown", "high"), gpuMem(700))
	if got := qm.scoped[scopeTestNS]["high"].used[scopeTestMem]; got != 0 {
		t.Fatalf("used after an unknown release = %d, want 0", got)
	}
}

// A pod that gets activeDeadlineSeconds moves from NotTerminating to
// Terminating, and its usage moves with it.
func TestScopedQuotaFollowsPodScopes(t *testing.T) {
	initTest()
	qm := newScopeTestManager()
	qm.AddQuota(scopedMemQuota("long", "10000", []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeNotTerminating}))
	qm.AddQuota(scopedMemQuota("short", "10000", []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeTerminating}))
	pod := scopePod("a", "")
	qm.AddUsage(pod, gpuMem(1000))

	updated := pod.DeepCopy()
	updated.Spec.ActiveDeadlineSeconds = new(int64(60))
	qm.ReplaceUsage(updated, gpuMem(1000), gpuMem(1000))
	if got := qm.scoped[scopeTestNS]["long"].used[scopeTestMem]; got != 0 {
		t.Errorf("NotTerminating used = %d, want 0", got)
	}
	if got := qm.scoped[scopeTestNS]["short"].used[scopeTestMem]; got != 1000 {
		t.Errorf("Terminating used = %d, want 1000", got)
	}
}

func TestScopedQuotaUpdateAndDelete(t *testing.T) {
	initTest()
	qm := newScopeTestManager()
	qm.AddUsage(scopePod("a", "high"), gpuMem(1500))
	plain := scopedMemQuota("gpu", "2000", nil)
	qm.AddQuota(plain)
	if qm.FitQuotaForPod(scopePod("new", "low"), 1000, 0, "NVIDIA") {
		t.Fatal("the namespace quota is not enforced")
	}

	scoped := scopedMemQuota("gpu", "2000", nil, priorityIn("high"))
	qm.UpdateQuota(plain, scoped)
	if !qm.FitQuotaForPod(scopePod("new", "low"), 1000, 0, "NVIDIA") {
		t.Error("the namespace limit stayed after the quota got a scope")
	}
	if qm.FitQuotaForPod(scopePod("new", "high"), 1000, 0, "NVIDIA") {
		t.Error("the scoped quota is not enforced after the update")
	}

	qm.UpdateQuota(scoped, plain)
	if len(qm.scoped) != 0 {
		t.Errorf("the scoped quota stayed after its scope was removed: %v", qm.scoped)
	}
	if qm.FitQuotaForPod(scopePod("new", "low"), 1000, 0, "NVIDIA") {
		t.Error("the namespace quota is not enforced after the scope was removed")
	}

	qm.UpdateQuota(plain, scoped)
	qm.DelQuota(scoped)
	if len(qm.scoped) != 0 || !qm.FitQuotaForPod(scopePod("new", "high"), 8000, 0, "NVIDIA") {
		t.Error("a deleted scoped quota still limits pods")
	}
}

// The namespace quota and the scoped quota both apply to a matching pod.
func TestScopedAndNamespaceQuota(t *testing.T) {
	initTest()
	qm := newScopeTestManager()
	qm.AddQuota(scopedMemQuota("all", "3000", nil))
	qm.AddQuota(scopedMemQuota("high", "1000", nil, priorityIn("high")))
	qm.AddUsage(scopePod("low", "low"), gpuMem(2500))

	if qm.FitQuotaForPod(scopePod("new", "high"), 800, 0, "NVIDIA") {
		t.Error("the namespace quota was not checked for a scoped pod")
	}
	if !qm.FitQuotaForPod(scopePod("new", "high"), 500, 0, "NVIDIA") {
		t.Error("a pod within both quotas does not fit")
	}
}

func TestScopedQuotaCores(t *testing.T) {
	initTest()
	qm := newScopeTestManager()
	q := scopedMemQuota("high", "100000", nil, priorityIn("high"))
	q.Spec.Hard["limits.nvidia.com/gpucore"] = resource.MustParse("50")
	qm.AddQuota(q)
	pod := scopePod("a", "high")
	qm.AddUsage(pod, PodDevices{"NVIDIA": PodSingleDevice{{{UUID: "GPU-0", Type: "NVIDIA", Usedmem: 100, Usedcores: 30}}}})

	if qm.FitQuotaForPod(scopePod("new", "high"), 100, 30, "NVIDIA") {
		t.Error("a core request over the scoped limit fits")
	}
	if !qm.FitQuotaForPod(scopePod("new", "high"), 100, 20, "NVIDIA") {
		t.Error("a core request within the scoped limit does not fit")
	}
}

// Scoped limits are scaled by the backend's memoryFactor, like namespace limits.
func TestScopedQuotaMemoryFactor(t *testing.T) {
	initScaledTest()
	qm := newScopeTestManager()
	q := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "high", Namespace: scopeTestNS},
		Spec: corev1.ResourceQuotaSpec{
			Hard:          corev1.ResourceList{"limits.cambricon.com/mlu.smlu.vmemory": resource.MustParse("4")},
			ScopeSelector: &corev1.ScopeSelector{MatchExpressions: []corev1.ScopedResourceSelectorRequirement{priorityIn("high")}},
		},
	}
	qm.AddQuota(q)
	if !qm.FitQuotaForPod(scopePod("new", "high"), 1024, 0, "MLU") {
		t.Error("1024 MiB does not fit 4 units of 256 MiB")
	}
	if qm.FitQuotaForPod(scopePod("new", "high"), 1025, 0, "MLU") {
		t.Error("1025 MiB fits 4 units of 256 MiB")
	}
}

func TestRefreshPodScopes(t *testing.T) {
	initTest()
	qm := newScopeTestManager()
	qm.AddQuota(scopedMemQuota("long", "10000", []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeNotTerminating}))
	qm.AddQuota(scopedMemQuota("short", "2000", []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeTerminating}))
	pod := scopePod("a", "")
	qm.AddUsage(pod, gpuMem(1000))

	// Same devices, new deadline: the usage moves once and is not added twice.
	updated := pod.DeepCopy()
	updated.Spec.ActiveDeadlineSeconds = new(int64(60))
	qm.RefreshPodScopes(updated)
	qm.RefreshPodScopes(updated)
	if got := qm.scoped[scopeTestNS]["long"].used[scopeTestMem]; got != 0 {
		t.Errorf("NotTerminating used = %d, want 0", got)
	}
	if got := qm.scoped[scopeTestNS]["short"].used[scopeTestMem]; got != 1000 {
		t.Errorf("Terminating used = %d, want 1000", got)
	}
	short := scopePod("new", "")
	short.Spec.ActiveDeadlineSeconds = new(int64(60))
	if qm.FitQuotaForPod(short, 1500, 0, "NVIDIA") {
		t.Error("a terminating pod fits the Terminating quota with 1000 + 1500 > 2000")
	}

	// A pod with no tracked usage is ignored.
	qm.RefreshPodScopes(scopePod("untracked", "high"))
	if len(qm.podUsages[scopeTestNS]) != 1 {
		t.Errorf("tracked pods = %d, want 1", len(qm.podUsages[scopeTestNS]))
	}
}
