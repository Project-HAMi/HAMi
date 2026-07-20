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
	// NumaBind is kept for backward compatibility with the nvidia.com/numa-bind
	// annotation; NUMA grouping is now the default (see NumaIgnore) so this no
	// longer changes Less's behavior.
	NumaBind bool
	// NumaIgnore sorts purely by Score, ignoring NUMA locality. Set via the
	// hami.io/topology-aware-scoring: "false" annotation (#1806).
	NumaIgnore bool
}

func (l DeviceUsageList) Len() int {
	return len(l.DeviceLists)
}

func (l DeviceUsageList) Swap(i, j int) {
	l.DeviceLists[i], l.DeviceLists[j] = l.DeviceLists[j], l.DeviceLists[i]
}

func (l DeviceUsageList) Less(i, j int) bool {
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

	// numa-ignore: pure Score ordering, no NUMA involved at all (#1806).
	if l.NumaIgnore {
		if binpack {
			return si < sj
		}
		return si > sj
	}

	// default: NUMA groups first, Score orders devices within a NUMA node.
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
		NumaIgnore:  l.NumaIgnore,
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

func (ds *DeviceListsScore) ComputeScore(requests device.ContainerDeviceRequests) {
	if ds.Device == nil || ds.Device.Count == 0 || ds.Device.Totalcore == 0 || ds.Device.Totalmem == 0 {
		ds.Score = 0
		return
	}
	request, core, mem := int32(0), int32(0), int32(0)
	// Here we are required to use the same type device
	for _, container := range requests {

		if container.Type != ds.Device.Type {
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
	ds.Score = float32(util.Weight) * (usedScore + coreScore + memScore)
	klog.V(2).Infof("device %s computer score is %f", ds.Device.ID, ds.Score)
}
