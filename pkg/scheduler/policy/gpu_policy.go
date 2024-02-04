package policy

import (
	"github.com/Project-HAMi/HAMi/pkg/util"

	"k8s.io/klog/v2"
)

type DeviceListsScore struct {
	Device *util.DeviceUsage
	// Score recode every device user/allocate score
	Score float32
}

type DeviceUsageList struct {
	DeviceLists []*DeviceListsScore
	Policy      string
}

func (l DeviceUsageList) Len() int {
	return len(l.DeviceLists)
}

func (l DeviceUsageList) Swap(i, j int) {
	l.DeviceLists[i], l.DeviceLists[j] = l.DeviceLists[j], l.DeviceLists[i]
}

func (l DeviceUsageList) Less(i, j int) bool {
	if l.Policy == GPUSchedulerPolicyBinpack.String() {
		if l.DeviceLists[i].Device.Numa == l.DeviceLists[j].Device.Numa {
			return l.DeviceLists[i].Score < l.DeviceLists[j].Score
		}
		return l.DeviceLists[i].Device.Numa > l.DeviceLists[j].Device.Numa
	}
	// default policy is spread
	if l.DeviceLists[i].Device.Numa == l.DeviceLists[j].Device.Numa {
		return l.DeviceLists[i].Score > l.DeviceLists[j].Score
	}
	return l.DeviceLists[i].Device.Numa < l.DeviceLists[j].Device.Numa
}

func (ds *DeviceListsScore) ComputeScore(requests util.ContainerDeviceRequests) {
	request, core, mem := int32(0), int32(0), int32(0)
	// Here we are required to use the same type device
	for _, container := range requests {
		request += container.Nums
		core += container.Coresreq
		if container.MemPercentagereq != 0 && container.MemPercentagereq != 101 {
			mem += ds.Device.Totalmem * (container.MemPercentagereq / 100.0)
			continue
		}
		mem += container.Memreq
	}
	useScore := float32(request+ds.Device.Used) / float32(ds.Device.Count)
	coreScore := float32(core+ds.Device.Usedcores) / float32(ds.Device.Totalcore)
	memScore := float32(mem+ds.Device.Usedmem) / float32(ds.Device.Totalmem)
	ds.Score = float32(Weight) * (useScore + coreScore + memScore)
	klog.V(2).Infof("device %s computer score is %f", ds.Device.ID, ds.Score)
}
