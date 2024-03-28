package policy

import (
	"github.com/Project-HAMi/HAMi/pkg/util"

	"k8s.io/klog/v2"
)

type NodeScore struct {
	NodeID  string
	Devices util.PodDevices
	// Score recode every node all device user/allocate score
	Score float32
}

type NodeScoreList struct {
	NodeList []*NodeScore
	Policy   string
}

func (l NodeScoreList) Len() int {
	return len(l.NodeList)
}

func (l NodeScoreList) Swap(i, j int) {
	l.NodeList[i], l.NodeList[j] = l.NodeList[j], l.NodeList[i]
}

func (l NodeScoreList) Less(i, j int) bool {
	if l.Policy == NodeSchedulerPolicySpread.String() {
		return l.NodeList[i].Score > l.NodeList[j].Score
	}
	// default policy is Binpack
	return l.NodeList[i].Score < l.NodeList[j].Score
}

func (ns *NodeScore) ComputeScore(devices DeviceUsageList) {
	request, core, mem := int32(0), int32(0), int32(0)
	// current user having request resource
	user, userCore, userMem := int32(0), int32(0), int32(0)
	for _, device := range devices.DeviceLists {
		user += device.Device.Used
		userCore += device.Device.Usedcores
		userMem += device.Device.Usedmem
	}

	total, totalCore, totalMem := int32(0), int32(0), int32(0)
	for _, deviceLists := range devices.DeviceLists {
		total += deviceLists.Device.Count
		totalCore += deviceLists.Device.Totalcore
		totalMem += deviceLists.Device.Totalmem
	}
	useScore := float32(request+user) / float32(total)
	coreScore := float32(core+userCore) / float32(totalCore)
	memScore := float32(mem+userMem) / float32(totalMem)
	ns.Score = float32(Weight) * (useScore + coreScore + memScore)
	klog.V(2).Infof("node %s computer score is %f", ns.NodeID, ns.Score)
}
