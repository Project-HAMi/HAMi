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
}

type DeviceQuota map[string]*Quota

type QuotaManager struct {
	Quotas map[string]*DeviceQuota
	mutex  sync.RWMutex
}

var localCache QuotaManager

func GetLocalCache() *QuotaManager {
	return &localCache
}

var once sync.Once

func NewQuotaManager() *QuotaManager {
	once.Do(func() {
		localCache = QuotaManager{
			Quotas: make(map[string]*DeviceQuota),
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
		if limit != 0 && memQuota.Used+memreq > limit {
			klog.V(4).InfoS("resourceMem quota not fitted", "limit", limit, "used", memQuota.Used, "alloc", memreq)
			return false
		}
	}
	coreQuota, ok := (*dq)[coreResourceName]
	if ok && coreQuota.Limit != 0 && coreQuota.Used+coresreq > coreQuota.Limit {
		klog.V(4).InfoS("resourceCores quota not fitted", "limit", coreQuota.Limit, "used", coreQuota.Used, "alloc", coresreq)
		return false
	}
	return true
}

func containerResourceRequest(ctr *corev1.Container, resName corev1.ResourceName) (int64, bool) {
	v, ok := ctr.Resources.Limits[resName]
	if !ok {
		v, ok = ctr.Resources.Requests[resName]
	}
	if ok {
		if n, ok := v.AsInt64(); ok {
			return n, true
		}
	}
	return 0, false
}

func PodQuotaRequests(pod *corev1.Pod, deviceName string) (memoryReq int64, coresReq int64) {
	devs, ok := GetDevices()[deviceName]
	if !ok {
		return 0, 0
	}
	resourceNames := devs.GetResourceNames()
	if resourceNames.ResourceCountName == "" {
		return 0, 0
	}
	resourceName := corev1.ResourceName(resourceNames.ResourceCountName)
	memResourceName := corev1.ResourceName(resourceNames.ResourceMemoryName)
	coreResourceName := corev1.ResourceName(resourceNames.ResourceCoreName)

	for _, ctr := range pod.Spec.Containers {
		req, ok := containerResourceRequest(&ctr, resourceName)
		if !ok {
			continue
		}
		if memReq, ok := containerResourceRequest(&ctr, memResourceName); ok {
			memoryReq += memReq * req
		}
		if coreReq, ok := containerResourceRequest(&ctr, coreResourceName); ok {
			coresReq += coreReq * req
		}
	}
	return memoryReq, coresReq
}

func FitPodQuota(pod *corev1.Pod, deviceName string, memoryFactor int32) bool {
	memoryReq, coresReq := PodQuotaRequests(pod, deviceName)
	if memoryReq == 0 && coresReq == 0 {
		return true
	}
	if memoryFactor > 1 {
		memoryReq *= int64(memoryFactor)
	}
	return GetLocalCache().FitQuota(pod.Namespace, memoryReq, memoryFactor, coresReq, deviceName)
}

func FitAllocationQuota(ns, deviceType string, memoryFactor int32, memreq, coresreq int64, tmpDevs map[string]ContainerDevices, allocated *PodDevices) bool {
	mem := memreq
	core := coresreq
	for _, val := range tmpDevs[deviceType] {
		mem += int64(val.Usedmem)
		core += int64(val.Usedcores)
	}
	if allocated != nil {
		if podSingleDevice, exists := (*allocated)[deviceType]; exists {
			for _, containerDevices := range podSingleDevice {
				for _, val := range containerDevices {
					mem += int64(val.Usedmem)
					core += int64(val.Usedcores)
				}
			}
		}
	}
	klog.V(4).InfoS("Allocating quota", "device", deviceType, "mem", mem, "cores", core)
	return GetLocalCache().FitQuota(ns, mem, memoryFactor, core, deviceType)
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
	if q.Quotas[pod.Namespace] == nil {
		q.Quotas[pod.Namespace] = &DeviceQuota{}
	}
	dp, ok := q.Quotas[pod.Namespace]
	if !ok {
		return
	}
	for idx, val := range usage {
		_, ok := (*dp)[idx]
		if !ok {
			(*dp)[idx] = &Quota{
				Used:  0,
				Limit: 0,
			}
		}
		(*dp)[idx].Used += val
	}
	if klog.V(4).Enabled() {
		for _, val := range q.Quotas {
			for idx, val1 := range *val {
				klog.V(4).Infoln("add usage val=", idx, ":", val1)
			}
		}
	}
}

func (q *QuotaManager) RmUsage(pod *corev1.Pod, podDev PodDevices) {
	usage := countPodDevices(podDev)
	if len(usage) == 0 {
		return
	}
	q.mutex.Lock()
	defer q.mutex.Unlock()
	dp, ok := q.Quotas[pod.Namespace]
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
	if klog.V(4).Enabled() {
		for _, val := range q.Quotas {
			for idx, val1 := range *val {
				klog.V(4).Infoln("after val=", idx, ":", val1)
			}
		}
	}
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
	for idx, val := range quota.Spec.Hard {
		value, ok := val.AsInt64()
		if ok {
			if !strings.HasPrefix(idx.String(), "limits.") {
				continue
			}
			dn := strings.TrimPrefix(idx.String(), "limits.")
			if !IsManagedQuota(dn) {
				continue
			}
			if q.Quotas[quota.Namespace] == nil {
				q.Quotas[quota.Namespace] = &DeviceQuota{}
			}
			dp := q.Quotas[quota.Namespace]
			_, ok := (*dp)[dn]
			if !ok {
				(*dp)[dn] = &Quota{
					Used:  0,
					Limit: value,
				}
			}
			(*dp)[dn].Limit = value
			klog.V(4).InfoS("quota set:", "idx=", idx, "val", value)
		}
	}
	if klog.V(4).Enabled() {
		for _, val := range q.Quotas {
			for idx, val1 := range *val {
				klog.V(4).Infoln("after val=", idx, ":", val1)
			}
		}
	}
}

func (q *QuotaManager) DelQuota(quota *corev1.ResourceQuota) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	for idx, val := range quota.Spec.Hard {
		value, ok := val.AsInt64()
		if ok {
			if len(idx.String()) <= len("limits.") {
				continue
			}
			dn := idx.String()[len("limits."):]
			if !IsManagedQuota(dn) {
				continue
			}
			klog.V(4).InfoS("quota remove:", "idx=", idx, "val", value)
			if dq, ok := q.Quotas[quota.Namespace]; ok {
				if quotaInfo, ok := (*dq)[dn]; ok {
					quotaInfo.Limit = 0
				}
			}
		}
	}

	if klog.V(4).Enabled() {
		for _, val := range q.Quotas {
			for idx, val1 := range *val {
				klog.V(4).Infoln("after val=", idx, ":", val1)
			}
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
				Used:  quota.Used,
				Limit: quota.Limit,
			}
		}
		quotasCopy[ns] = curDQ
	}
	return quotasCopy
}
