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
	"strings"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/util"

	"k8s.io/klog/v2"
)

type DeviceListsScore struct {
	Device *device.DeviceUsage
	// Score recode every device user/allocate score
	Score float32
}

type DeviceUsageList struct {
	DeviceLists []*DeviceListsScore
	Policy      string
	// NumaBind groups devices by NUMA so Fit can accumulate a same-NUMA run.
	// When false, Score is the primary key and NUMA only breaks ties (#1806).
	NumaBind bool
}

func (l DeviceUsageList) Len() int {
	return len(l.DeviceLists)
}

func (l DeviceUsageList) Swap(i, j int) {
	l.DeviceLists[i], l.DeviceLists[j] = l.DeviceLists[j], l.DeviceLists[i]
}

// gpuSortKeyOrder lists the recognized sort-key policies, in the priority
// used when a caller-supplied chain doesn't disambiguate them further.
var gpuSortKeyOrder = []util.SchedulerPolicyName{
	util.GPUSchedulerPolicyBinpack,
	util.GPUSchedulerPolicySpread,
	util.GPUSchedulerPolicyNuma,
}

// gpuSortKeyChain parses policy as a comma-separated ordered list and returns
// the sort-key policies (binpack/spread/numa) it names, in the order written,
// deduplicated. mutex and topology-aware are filters consumed via
// util.PolicyContains in Fit(), not sort keys, so they're dropped here.
func gpuSortKeyChain(policy string) []util.SchedulerPolicyName {
	seen := make(map[util.SchedulerPolicyName]bool, len(gpuSortKeyOrder))
	var chain []util.SchedulerPolicyName
	for p := range strings.SplitSeq(policy, ",") {
		name := util.SchedulerPolicyName(strings.TrimSpace(p))
		for _, key := range gpuSortKeyOrder {
			if name == key && !seen[key] {
				chain = append(chain, key)
				seen[key] = true
			}
		}
	}
	return chain
}

func (l DeviceUsageList) Less(i, j int) bool {
	// Comma-separated policy: chain binpack/spread/numa as sort keys in the
	// order the caller wrote them. mutex/topology-aware are filters applied
	// in each device backend's Fit(), not sort keys, so they don't appear here.
	// Bare "numa" also routes here: it's a chain token, not a legacy value.
	if strings.Contains(l.Policy, ",") || l.Policy == util.GPUSchedulerPolicyNuma.String() {
		return l.lessByChain(i, j)
	}

	// Single policy value: unchanged behavior.
	si, sj := l.DeviceLists[i].Score, l.DeviceLists[j].Score
	ni, nj := l.DeviceLists[i].Device.Numa, l.DeviceLists[j].Device.Numa
	binpack := l.Policy == util.GPUSchedulerPolicyBinpack.String()

	// mutex: busy GPUs first, idle GPUs at tail so Fit picks idle ones.
	if l.Policy == util.GPUSchedulerPolicyMutex.String() {
		ui, uj := l.DeviceLists[i].Device.Used, l.DeviceLists[j].Device.Used
		if ui != uj {
			return ui > uj
		}
		return ni < nj
	}

	// numa-bind: keep NUMA groups contiguous for Fit's same-NUMA accumulation.
	if l.NumaBind {
		if binpack {
			if ni == nj {
				return si < sj
			}
			return ni > nj
		}
		// default policy is spread
		if ni == nj {
			return si > sj
		}
		return ni < nj
	}

	// score primary, NUMA tiebreaker (#1806).
	if binpack {
		if si != sj {
			return si < sj
		}
		return ni < nj
	}
	// default policy is spread
	if si != sj {
		return si > sj
	}
	return ni < nj
}

// lessByChain compares devices i and j by the ordered sort-key chain parsed
// from l.Policy, falling back to spread (today's default policy) when the
// comma list names no recognized sort key (e.g. "mutex,topology-aware").
func (l DeviceUsageList) lessByChain(i, j int) bool {
	chain := gpuSortKeyChain(l.Policy)
	if len(chain) == 0 {
		chain = []util.SchedulerPolicyName{util.GPUSchedulerPolicySpread}
	}
	// numa-bind requires NUMA groups to stay contiguous for Fit's same-NUMA
	// accumulation, so force numa as the primary key if the chain omits it.
	if l.NumaBind && chain[0] != util.GPUSchedulerPolicyNuma {
		withNuma := []util.SchedulerPolicyName{util.GPUSchedulerPolicyNuma}
		for _, key := range chain {
			if key != util.GPUSchedulerPolicyNuma {
				withNuma = append(withNuma, key)
			}
		}
		chain = withNuma
	}
	a, b := l.DeviceLists[i], l.DeviceLists[j]
	for _, key := range chain {
		switch key {
		case util.GPUSchedulerPolicyBinpack:
			if a.Score != b.Score {
				return a.Score < b.Score
			}
		case util.GPUSchedulerPolicySpread:
			if a.Score != b.Score {
				return a.Score > b.Score
			}
		case util.GPUSchedulerPolicyNuma:
			if a.Device.Numa != b.Device.Numa {
				return a.Device.Numa < b.Device.Numa
			}
		}
	}
	// Deterministic tiebreak when every chained key is equal.
	return a.Device.Index < b.Device.Index
}

func (l DeviceUsageList) DeepCopy() DeviceUsageList {
	var deviceLists []*DeviceListsScore
	if l.DeviceLists != nil {
		deviceLists = make([]*DeviceListsScore, len(l.DeviceLists))
		for i, ds := range l.DeviceLists {
			deviceLists[i] = ds.DeepCopy()
		}
	}
	return DeviceUsageList{
		DeviceLists: deviceLists,
		Policy:      l.Policy,
		NumaBind:    l.NumaBind,
	}
}

func (ds *DeviceListsScore) DeepCopy() *DeviceListsScore {
	if ds == nil {
		return nil
	}
	return &DeviceListsScore{
		Device: ds.Device.DeepCopy(),
		Score:  ds.Score,
	}
}

// mostSpecificMatchingType returns the request type in requests that applies to
// a device of deviceType, and whether any does.
//
// A device is served by exactly one backend, so at most one request *type* may
// be scored against it. Comparing types for equality gave that for free. A
// substring match does not, because registered types nest: the chart ships both
// "Ascend910B4" and "Ascend910B4-1" as Ascend common words, and the longer type
// contains the shorter one, so a pod requesting both would otherwise have such a
// device scored twice. The longest matching type is the most specific one; ties
// break on the string itself so the choice is deterministic.
//
// This selects the type only. ComputeScore still sums every request carrying it,
// which is the existing behaviour for a pod whose containers request the same
// device type more than once.
func mostSpecificMatchingType(deviceType string, requests device.ContainerDeviceRequests) (string, bool) {
	best := ""
	found := false
	for _, container := range requests {
		if !device.MatchesRequestType(deviceType, container.Type) {
			continue
		}
		if !found || len(container.Type) > len(best) ||
			(len(container.Type) == len(best) && container.Type < best) {
			best = container.Type
			found = true
		}
	}
	return best, found
}

func (ds *DeviceListsScore) ComputeScore(requests device.ContainerDeviceRequests, weights util.DeviceScoringWeights) {
	if ds.Device == nil || ds.Device.Count == 0 || ds.Device.Totalcore == 0 || ds.Device.Totalmem == 0 {
		ds.Score = 0
		return
	}
	request, core, mem := int32(0), int32(0), int32(0)
	// Here we are required to use the same type device.
	// A node registers model-specific device types ("NVIDIA A100-SXM4-40GB",
	// "MLU370-X8") while a request carries the vendor common word ("NVIDIA",
	// "MLU"), so match the way getNodeResources selects candidate devices.
	// Comparing for equality dropped the request from the score entirely on
	// those backends, leaving devices ranked by current usage instead of usage
	// after placement.
	//
	// Only one request type may be scored against a device, which equality
	// gave for free and a substring match does not: registered types nest, and
	// the shipped Ascend config carries both "Ascend910B4" and "Ascend910B4-1".
	// Requests sharing that one type still accumulate, as they did before.
	matchType, matched := mostSpecificMatchingType(ds.Device.Type, requests)
	for _, container := range requests {
		if !matched || container.Type != matchType {
			continue
		}

		request += 1
		core += container.Coresreq
		if container.MemPercentagereq != 0 && container.MemPercentagereq != 101 {
			mem += int32((int64(ds.Device.Totalmem) * int64(container.MemPercentagereq)) / 100)
			continue
		}
		mem += container.Memreq
	}
	klog.V(2).Infof("device %s user %d, userCore %d, userMem %d,", ds.Device.ID, ds.Device.Used, ds.Device.Usedcores, ds.Device.Usedmem)

	usedScore := float32(request+ds.Device.Used) / float32(ds.Device.Count)
	coreScore := float32(core+ds.Device.Usedcores) / float32(ds.Device.Totalcore)
	memScore := float32(mem+ds.Device.Usedmem) / float32(ds.Device.Totalmem)
	ds.Score = float32(util.Weight) * (float32(weights.Slot)*usedScore + float32(weights.Core)*coreScore + float32(weights.Memory)*memScore)
	klog.V(2).Infof("device %s computer score is %f", ds.Device.ID, ds.Score)
}
