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

package device

import (
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

type Quota struct {
	Used  int64
	Limit int64
	// LimitSet distinguishes an explicitly configured limit (including an
	// explicit 0, which blocks all usage) from an entry auto-created by usage
	// tracking, which carries Limit 0 but means "no limit configured". FitQuota
	// gates on this rather than Limit != 0 so that a ResourceQuota of "0" is
	// honored as a hard block instead of being read as unlimited.
	LimitSet bool
}

type DeviceQuota map[string]*Quota

type QuotaManager struct {
	Quotas map[string]*DeviceQuota
	// objectLimits records what each ResourceQuota object contributes:
	// namespace -> object name -> resource -> limit. Kubernetes enforces every
	// ResourceQuota object in a namespace independently, so the effective limit
	// for a resource is the minimum across all objects. Storing per object (not
	// a single slot) keeps the derived state a pure function of the current set
	// of objects, so the result no longer depends on informer event order or on
	// which object happened to be added/removed last.
	objectLimits map[string]map[string]map[string]int64
	mutex        sync.RWMutex
}

var localCache QuotaManager

func GetLocalCache() *QuotaManager {
	return &localCache
}

var once sync.Once

func NewQuotaManager() *QuotaManager {
	once.Do(func() {
		localCache = QuotaManager{
			Quotas:       make(map[string]*DeviceQuota),
			objectLimits: make(map[string]map[string]map[string]int64),
		}
	})
	return &localCache
}

func (q *QuotaManager) FitQuota(ns string, memreq int64, memoryFactor int32, coresreq int64, deviceName string) bool {
	devs, ok := GetDevices()[deviceName]
	if !ok {
		return true
	}
	resourceNames := devs.GetResourceNames()
	memResourceName := resourceNames.ResourceMemoryName
	coreResourceName := resourceNames.ResourceCoreName

	q.mutex.RLock()
	defer q.mutex.RUnlock()
	dq := q.Quotas[ns]
	if dq == nil {
		return true
	}
	memQuota, ok := (*dq)[memResourceName]
	if ok {
		klog.V(4).InfoS("resourceMem quota judging", "quota limit", memQuota.Limit, "used", memQuota.Used, "alloc", memreq, "memoryFactor", memoryFactor)
		limit := memQuota.Limit
		if memoryFactor > 1 {
			limit = limit * int64(memoryFactor)
		}
		if memQuota.LimitSet && memQuota.Used+memreq > limit {
			klog.V(4).InfoS("resourceMem quota not fitted", "limit", limit, "used", memQuota.Used, "alloc", memreq)
			return false
		}
	}
	coreQuota, ok := (*dq)[coreResourceName]
	if ok && coreQuota.LimitSet && coreQuota.Used+coresreq > coreQuota.Limit {
		klog.V(4).InfoS("resourceCores quota not fitted", "limit", coreQuota.Limit, "used", coreQuota.Used, "alloc", coresreq)
		return false
	}
	return true
}

func countPodDevices(podDev PodDevices) map[string]int64 {
	res := make(map[string]int64)
	for deviceName, podSingle := range podDev {
		devs, ok := GetDevices()[deviceName]
		if !ok {
			continue
		}
		resourceNames := devs.GetResourceNames()
		for _, ctrdevices := range podSingle {
			for _, ctrdevice := range ctrdevices {
				if len(resourceNames.ResourceMemoryName) > 0 {
					res[resourceNames.ResourceMemoryName] += int64(ctrdevice.Usedmem)
				}
				if len(resourceNames.ResourceCoreName) > 0 {
					res[resourceNames.ResourceCoreName] += int64(ctrdevice.Usedcores)
				}
			}
		}
	}
	return res
}

func (q *QuotaManager) AddUsage(pod *corev1.Pod, podDev PodDevices) {
	usage := countPodDevices(podDev)
	if len(usage) == 0 {
		return
	}
	q.mutex.Lock()
	defer q.mutex.Unlock()
	q.addUsageLocked(pod.Namespace, usage)
	if klog.V(4).Enabled() {
		for _, val := range q.Quotas {
			for idx, val1 := range *val {
				klog.V(4).Infoln("add usage val=", idx, ":", val1)
			}
		}
	}
}

func (q *QuotaManager) addUsageLocked(namespace string, usage map[string]int64) {
	if q.Quotas[namespace] == nil {
		q.Quotas[namespace] = &DeviceQuota{}
	}
	dp := q.Quotas[namespace]
	for idx, val := range usage {
		if _, ok := (*dp)[idx]; !ok {
			(*dp)[idx] = &Quota{
				Used:  0,
				Limit: 0,
			}
		}
		(*dp)[idx].Used += val
	}
}

func (q *QuotaManager) RmUsage(pod *corev1.Pod, podDev PodDevices) {
	usage := countPodDevices(podDev)
	if len(usage) == 0 {
		return
	}
	q.mutex.Lock()
	defer q.mutex.Unlock()
	q.rmUsageLocked(pod.Namespace, usage)
	if klog.V(4).Enabled() {
		for _, val := range q.Quotas {
			for idx, val1 := range *val {
				klog.V(4).Infoln("after val=", idx, ":", val1)
			}
		}
	}
}

func (q *QuotaManager) rmUsageLocked(namespace string, usage map[string]int64) {
	dp, ok := q.Quotas[namespace]
	if !ok {
		return
	}
	for idx, val := range usage {
		if qInfo, ok := (*dp)[idx]; ok && qInfo != nil {
			qInfo.Used -= val
			if qInfo.Used < 0 {
				klog.V(4).InfoS("RmUsage: clamping negative Used to zero", "quota", idx, "val", val)
				qInfo.Used = 0
			}
		}
	}
}
func (q *QuotaManager) ReplaceUsage(pod *corev1.Pod, oldDevices, newDevices PodDevices) {
	oldUsage := countPodDevices(oldDevices)
	newUsage := countPodDevices(newDevices)
	if len(oldUsage) == 0 && len(newUsage) == 0 {
		return
	}
	q.mutex.Lock()
	defer q.mutex.Unlock()
	q.rmUsageLocked(pod.Namespace, oldUsage)
	q.addUsageLocked(pod.Namespace, newUsage)
}

func IsManagedQuota(quotaName string) bool {
	for _, val := range GetDevices() {
		names := val.GetResourceNames()
		if len(names.ResourceMemoryName) > 0 && names.ResourceMemoryName == quotaName {
			return true
		}
		if len(names.ResourceCoreName) > 0 && names.ResourceCoreName == quotaName {
			return true
		}
	}
	return false
}

func (q *QuotaManager) AddQuota(quota *corev1.ResourceQuota) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	q.addQuotaLocked(quota)
	q.logQuotasLocked()
}

func (q *QuotaManager) DelQuota(quota *corev1.ResourceQuota) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	q.delQuotaLocked(quota)
	q.logQuotasLocked()
}

// UpdateQuota swaps oldQuota's limits for newQuota's without releasing the lock
// in between. Doing it as DelQuota followed by AddQuota would leave the limits
// zeroed for the gap between the two, and FitQuota reads a zero limit as no
// limit, so a check landing in that gap is waved through. The kube quota
// controller rewrites status.used on every pod event, so updates arrive
// constantly while pods are being scheduled.
func (q *QuotaManager) UpdateQuota(oldQuota, newQuota *corev1.ResourceQuota) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if oldQuota != nil {
		q.delQuotaLocked(oldQuota)
	}
	if newQuota != nil {
		q.addQuotaLocked(newQuota)
	}
	q.logQuotasLocked()
}

// addQuotaLocked requires q.mutex to be held. It records the object's limits
// under its name and recomputes the effective limit for every resource the
// object (previously or now) carries.
func (q *QuotaManager) addQuotaLocked(quota *corev1.ResourceQuota) {
	newLimits := make(map[string]int64)
	for idx, val := range quota.Spec.Hard {
		value, ok := val.AsInt64()
		if !ok {
			continue
		}
		dn, ok := managedQuotaName(idx)
		if !ok {
			continue
		}
		newLimits[dn] = value
		klog.V(4).InfoS("quota set:", "idx=", idx, "val", value)
	}
	if q.objectLimits == nil {
		q.objectLimits = make(map[string]map[string]map[string]int64)
	}
	if q.objectLimits[quota.Namespace] == nil {
		q.objectLimits[quota.Namespace] = make(map[string]map[string]int64)
	}
	// Replace the object's entry wholesale so resources dropped from its spec
	// are released rather than left at their old values.
	recomputed := make(map[string]struct{})
	for dn := range q.objectLimits[quota.Namespace][quota.Name] {
		recomputed[dn] = struct{}{}
	}
	q.objectLimits[quota.Namespace][quota.Name] = newLimits
	for dn := range newLimits {
		recomputed[dn] = struct{}{}
	}
	for dn := range recomputed {
		q.recalcLimitLocked(quota.Namespace, dn)
	}
}

// delQuotaLocked requires q.mutex to be held. It removes the object's recorded
// limits for the resources its spec mentions and recomputes the effective
// limit from the remaining objects. Deletion is driven by the spec so an
// object that was never added (or a stale event) cannot wipe another object's
// recorded limits.
func (q *QuotaManager) delQuotaLocked(quota *corev1.ResourceQuota) {
	obj, ok := q.objectLimits[quota.Namespace][quota.Name]
	if !ok {
		return
	}
	for idx := range quota.Spec.Hard {
		dn, ok := managedQuotaName(idx)
		if !ok {
			continue
		}
		if _, recorded := obj[dn]; !recorded {
			continue
		}
		delete(obj, dn)
		klog.V(4).InfoS("quota remove:", "resource", dn, "obj", quota.Name)
		q.recalcLimitLocked(quota.Namespace, dn)
	}
	if len(obj) == 0 {
		delete(q.objectLimits[quota.Namespace], quota.Name)
		if len(q.objectLimits[quota.Namespace]) == 0 {
			delete(q.objectLimits, quota.Namespace)
		}
	}
}

// recalcLimitLocked requires q.mutex to be held. It derives the effective limit
// for a resource as the minimum across all ResourceQuota objects that set it —
// a pod must satisfy every object, so the tightest one wins. With no objects
// left the resource reads as unset (Limit 0, LimitSet false).
func (q *QuotaManager) recalcLimitLocked(namespace, deviceResource string) {
	limit := int64(0)
	found := false
	for _, res := range q.objectLimits[namespace] {
		if v, ok := res[deviceResource]; ok && (!found || v < limit) {
			limit = v
			found = true
		}
	}
	if q.Quotas[namespace] == nil {
		q.Quotas[namespace] = &DeviceQuota{}
	}
	dp := q.Quotas[namespace]
	quotaInfo, ok := (*dp)[deviceResource]
	if !ok {
		quotaInfo = &Quota{
			Used:  0,
			Limit: 0,
		}
		(*dp)[deviceResource] = quotaInfo
	}
	quotaInfo.Limit = limit
	quotaInfo.LimitSet = found
}

// managedQuotaName maps a ResourceQuota key to the device resource it limits,
// reporting false for keys this manager does not track.
func managedQuotaName(idx corev1.ResourceName) (string, bool) {
	if !strings.HasPrefix(idx.String(), "limits.") {
		return "", false
	}
	dn := strings.TrimPrefix(idx.String(), "limits.")
	if !IsManagedQuota(dn) {
		return "", false
	}
	return dn, true
}

// logQuotasLocked requires q.mutex to be held.
func (q *QuotaManager) logQuotasLocked() {
	if !klog.V(4).Enabled() {
		return
	}
	for _, val := range q.Quotas {
		for idx, val1 := range *val {
			klog.V(4).Infoln("after val=", idx, ":", val1)
		}
	}
}

func (q *QuotaManager) GetResourceQuota() map[string]*DeviceQuota {
	quotasCopy := make(map[string]*DeviceQuota)
	q.mutex.RLock()
	defer q.mutex.RUnlock()
	for ns, dq := range q.Quotas {
		curDQ := &DeviceQuota{}
		for name, quota := range *dq {
			(*curDQ)[name] = &Quota{
				Used:     quota.Used,
				Limit:    quota.Limit,
				LimitSet: quota.LimitSet,
			}
		}
		quotasCopy[ns] = curDQ
	}
	return quotasCopy
}
