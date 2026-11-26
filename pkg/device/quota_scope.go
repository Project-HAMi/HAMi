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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/klog/v2"
)

// A scoped ResourceQuota (spec.scopes or spec.scopeSelector) limits only the
// pods its scopes match, so its usage cannot be the namespace total. Each
// scoped quota keeps its own used amounts, added up from the pods it matches.
// The matching follows the kube-apiserver pod quota evaluator.

// podScopes holds the pod fields that ResourceQuota scopes look at.
type podScopes struct {
	priorityClass  string
	terminating    bool
	bestEffort     bool
	crossNamespace bool
}

// podUsage is the device usage of one pod and the scopes it had when that
// usage was recorded.
type podUsage struct {
	scopes podScopes
	usage  map[string]int64
}

// scopeMatcher is one scope of a quota, with its PriorityClass selector
// parsed once.
type scopeMatcher struct {
	scope       corev1.ResourceQuotaScope
	exists      bool
	requirement *labels.Requirement
	invalid     bool
}

type scopedQuota struct {
	matchers []scopeMatcher
	limits   map[string]int64
	used     map[string]int64
}

func isTerminatingPod(pod *corev1.Pod) bool {
	return pod.Spec.ActiveDeadlineSeconds != nil && *pod.Spec.ActiveDeadlineSeconds >= 0
}

func hasComputeResources(list corev1.ResourceList) bool {
	for _, name := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory} {
		if q, ok := list[name]; ok && q.Sign() > 0 {
			return true
		}
	}
	return false
}

// isBestEffortPod reports whether the pod has the BestEffort QoS class: no
// container and no pod-level resources set a CPU or memory request or limit.
func isBestEffortPod(pod *corev1.Pod) bool {
	if pod.Status.QOSClass != "" {
		return pod.Status.QOSClass == corev1.PodQOSBestEffort
	}
	if r := pod.Spec.Resources; r != nil && (hasComputeResources(r.Requests) || hasComputeResources(r.Limits)) {
		return false
	}
	for _, ctrs := range [][]corev1.Container{pod.Spec.InitContainers, pod.Spec.Containers} {
		for i := range ctrs {
			if hasComputeResources(ctrs[i].Resources.Requests) || hasComputeResources(ctrs[i].Resources.Limits) {
				return false
			}
		}
	}
	return true
}

func crossNamespaceTerm(term *corev1.PodAffinityTerm) bool {
	return len(term.Namespaces) != 0 || term.NamespaceSelector != nil
}

// usesCrossNamespacePodAffinity reports whether any pod affinity or
// anti-affinity term of the pod selects pods in other namespaces.
func usesCrossNamespacePodAffinity(pod *corev1.Pod) bool {
	a := pod.Spec.Affinity
	if a == nil {
		return false
	}
	check := func(required []corev1.PodAffinityTerm, preferred []corev1.WeightedPodAffinityTerm) bool {
		for i := range required {
			if crossNamespaceTerm(&required[i]) {
				return true
			}
		}
		for i := range preferred {
			if crossNamespaceTerm(&preferred[i].PodAffinityTerm) {
				return true
			}
		}
		return false
	}
	if a.PodAffinity != nil && check(a.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution, a.PodAffinity.PreferredDuringSchedulingIgnoredDuringExecution) {
		return true
	}
	return a.PodAntiAffinity != nil && check(a.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution, a.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution)
}

func scopesOf(pod *corev1.Pod) podScopes {
	return podScopes{
		priorityClass:  pod.Spec.PriorityClassName,
		terminating:    isTerminatingPod(pod),
		bestEffort:     isBestEffortPod(pod),
		crossNamespace: usesCrossNamespacePodAffinity(pod),
	}
}

var scopeOperators = map[corev1.ScopeSelectorOperator]selection.Operator{
	corev1.ScopeSelectorOpIn:           selection.In,
	corev1.ScopeSelectorOpNotIn:        selection.NotIn,
	corev1.ScopeSelectorOpExists:       selection.Exists,
	corev1.ScopeSelectorOpDoesNotExist: selection.DoesNotExist,
}

// quotaScopeMatchers turns spec.scopes and spec.scopeSelector into matchers.
// A scope in spec.scopes is the same as an Exists requirement on it.
func quotaScopeMatchers(quota *corev1.ResourceQuota) []scopeMatcher {
	var reqs []corev1.ScopedResourceSelectorRequirement
	for _, s := range quota.Spec.Scopes {
		reqs = append(reqs, corev1.ScopedResourceSelectorRequirement{ScopeName: s, Operator: corev1.ScopeSelectorOpExists})
	}
	if quota.Spec.ScopeSelector != nil {
		reqs = append(reqs, quota.Spec.ScopeSelector.MatchExpressions...)
	}
	matchers := make([]scopeMatcher, 0, len(reqs))
	for _, r := range reqs {
		m := scopeMatcher{scope: r.ScopeName, exists: r.Operator == corev1.ScopeSelectorOpExists}
		if r.ScopeName == corev1.ResourceQuotaScopePriorityClass && !m.exists {
			op, ok := scopeOperators[r.Operator]
			var err error
			if ok {
				m.requirement, err = labels.NewRequirement(string(r.ScopeName), op, r.Values)
			}
			if !ok || err != nil {
				klog.ErrorS(err, "ResourceQuota has an invalid PriorityClass scope, it matches no pod",
					"namespace", quota.Namespace, "quotaName", quota.Name, "operator", r.Operator, "values", r.Values)
				m.invalid = true
			}
		}
		matchers = append(matchers, m)
	}
	return matchers
}

func (m scopeMatcher) matches(s podScopes) bool {
	switch m.scope {
	case corev1.ResourceQuotaScopeTerminating:
		return s.terminating
	case corev1.ResourceQuotaScopeNotTerminating:
		return !s.terminating
	case corev1.ResourceQuotaScopeBestEffort:
		return s.bestEffort
	case corev1.ResourceQuotaScopeNotBestEffort:
		return !s.bestEffort
	case corev1.ResourceQuotaScopeCrossNamespacePodAffinity:
		return s.crossNamespace
	case corev1.ResourceQuotaScopePriorityClass:
		if m.invalid {
			return false
		}
		if m.exists {
			return s.priorityClass != ""
		}
		// The key is always present, possibly empty, as in kube-apiserver.
		return m.requirement.Matches(labels.Set{string(corev1.ResourceQuotaScopePriorityClass): s.priorityClass})
	}
	// Other scopes, such as VolumeAttributesClass, never match a pod.
	return false
}

// matches reports whether every scope of the quota matches the pod.
func (sq *scopedQuota) matches(s podScopes) bool {
	for _, m := range sq.matchers {
		if !m.matches(s) {
			return false
		}
	}
	return true
}

func (sq *scopedQuota) add(usage map[string]int64, sign int64) {
	for res, v := range usage {
		if _, ok := sq.limits[res]; !ok {
			continue
		}
		sq.used[res] = max(sq.used[res]+sign*v, 0)
	}
}

// fits reports whether v more of resource res stays within the quota's limit
// for it, if it has one.
func (sq *scopedQuota) fits(res string, v int64) bool {
	limit, ok := sq.limits[res]
	return !ok || sq.used[res]+v <= limit
}

// podKey identifies a pod across informer events.
func podKey(pod *corev1.Pod) string {
	if pod.UID != "" {
		return string(pod.UID)
	}
	return pod.Namespace + "/" + pod.Name
}

// trackPodUsageLocked records that the pod's device usage went down by sub
// and then up by add, and moves the pod's usage between scoped quotas when
// the pod's scopes changed. It requires q.mutex to be held.
func (q *QuotaManager) trackPodUsageLocked(pod *corev1.Pod, sub, add map[string]int64) {
	ns, key := pod.Namespace, podKey(pod)
	quotas := q.scoped[ns]
	entry := q.podUsages[ns][key]
	if entry != nil {
		for _, sq := range quotas {
			if sq.matches(entry.scopes) {
				sq.add(entry.usage, -1)
			}
		}
	} else {
		entry = &podUsage{usage: map[string]int64{}}
	}
	for res, v := range sub {
		entry.usage[res] -= v
	}
	for res, v := range add {
		entry.usage[res] += v
	}
	for res, v := range entry.usage {
		if v <= 0 {
			delete(entry.usage, res)
		}
	}
	if len(entry.usage) == 0 {
		if pods := q.podUsages[ns]; pods != nil {
			delete(pods, key)
			if len(pods) == 0 {
				delete(q.podUsages, ns)
			}
		}
		return
	}
	entry.scopes = scopesOf(pod)
	if q.podUsages == nil {
		q.podUsages = make(map[string]map[string]*podUsage)
	}
	if q.podUsages[ns] == nil {
		q.podUsages[ns] = make(map[string]*podUsage)
	}
	q.podUsages[ns][key] = entry
	for _, sq := range quotas {
		if sq.matches(entry.scopes) {
			sq.add(entry.usage, 1)
		}
	}
}

// addScopedQuotaLocked stores a scoped quota and counts the usage of the
// pods it matches. It requires q.mutex to be held.
func (q *QuotaManager) addScopedQuotaLocked(quota *corev1.ResourceQuota, limits map[string]int64) {
	if len(limits) == 0 {
		return
	}
	sq := &scopedQuota{matchers: quotaScopeMatchers(quota), limits: limits, used: map[string]int64{}}
	for _, entry := range q.podUsages[quota.Namespace] {
		if sq.matches(entry.scopes) {
			sq.add(entry.usage, 1)
		}
	}
	if q.scoped == nil {
		q.scoped = make(map[string]map[string]*scopedQuota)
	}
	if q.scoped[quota.Namespace] == nil {
		q.scoped[quota.Namespace] = make(map[string]*scopedQuota)
	}
	q.scoped[quota.Namespace][quota.Name] = sq
	klog.V(4).InfoS("scoped quota set", "namespace", quota.Namespace, "quotaName", quota.Name, "limits", limits, "used", sq.used)
}

// delScopedQuotaLocked requires q.mutex to be held.
func (q *QuotaManager) delScopedQuotaLocked(quota *corev1.ResourceQuota) {
	quotas := q.scoped[quota.Namespace]
	if quotas == nil {
		return
	}
	delete(quotas, quota.Name)
	if len(quotas) == 0 {
		delete(q.scoped, quota.Namespace)
	}
}

// fitScopedLocked reports whether the pod, asking for memreq and coresreq of
// a backend's resources, fits every scoped quota of its namespace that
// matches it. It requires q.mutex to be held for reading.
func (q *QuotaManager) fitScopedLocked(pod *corev1.Pod, memName, coreName string, memreq, coresreq int64) bool {
	quotas := q.scoped[pod.Namespace]
	if len(quotas) == 0 {
		return true
	}
	scopes := scopesOf(pod)
	for name, sq := range quotas {
		if !sq.matches(scopes) {
			continue
		}
		if (memName != "" && !sq.fits(memName, memreq)) || (coreName != "" && !sq.fits(coreName, coresreq)) {
			klog.V(4).InfoS("scoped quota not fitted", "namespace", pod.Namespace, "quotaName", name,
				"limits", sq.limits, "used", sq.used, "memreq", memreq, "coresreq", coresreq)
			return false
		}
	}
	return true
}
