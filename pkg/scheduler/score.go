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
package scheduler

import (
	"sort"
	"strings"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/util"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

type NodeScore struct {
	nodeID  string
	devices util.PodDevices
	score   float32
}

type NodeScoreList []*NodeScore

func (l DeviceUsageList) Len() int {
	return len(l)
}

func (l DeviceUsageList) Swap(i, j int) {
	l[i], l[j] = l[j], l[i]
}

func (l DeviceUsageList) Less(i, j int) bool {
	if l[i].Numa == l[j].Numa {
		return l[i].Count-l[i].Used < l[j].Count-l[j].Used
	}
	return l[i].Numa < l[j].Numa
}

func (l NodeScoreList) Len() int {
	return len(l)
}

func (l NodeScoreList) Swap(i, j int) {
	l[i], l[j] = l[j], l[i]
}

func (l NodeScoreList) Less(i, j int) bool {
	return l[i].score < l[j].score
}

func viewStatus(usage NodeUsage) {
	klog.Info("devices status")
	for _, val := range usage.Devices {
		klog.InfoS("device status", "device id", val.ID, "device detail", val)
	}
}

func checkType(annos map[string]string, d util.DeviceUsage, n util.ContainerDeviceRequest) (bool, bool) {
	//General type check, NVIDIA->NVIDIA MLU->MLU
	if !strings.Contains(d.Type, n.Type) {
		return false, false
	}
	for _, val := range device.GetDevices() {
		found, pass, numaAssert := val.CheckType(annos, d, n)
		if found {
			return pass, numaAssert
		}
	}
	klog.Infof("Unrecognized device %s", n.Type)
	return false, false
}

func checkUUID(annos map[string]string, d util.DeviceUsage, n util.ContainerDeviceRequest) bool {
	devices, ok := device.GetDevices()[n.Type]
	if !ok {
		klog.Errorf("can not get device for %s type", n.Type)
		return false
	}
	result := devices.CheckUUID(annos, d)
	klog.V(2).Infof("checkUUID result is %v for %s type", result, n.Type)
	return result
}

func fitInCertainDevice(node *NodeUsage, request util.ContainerDeviceRequest, annos map[string]string, pod *corev1.Pod) (bool, map[string]util.ContainerDevices) {
	k := request
	originReq := k.Nums
	prevnuma := -1
	klog.InfoS("Allocating device for container request", "pod", klog.KObj(pod), "card request", k)
	var tmpDevs map[string]util.ContainerDevices
	tmpDevs = make(map[string]util.ContainerDevices)
	for i := len(node.Devices) - 1; i >= 0; i-- {
		klog.InfoS("scoring pod", "pod", klog.KObj(pod), "Memreq", k.Memreq, "MemPercentagereq", k.MemPercentagereq, "Coresreq", k.Coresreq, "Nums", k.Nums, "device index", i, "device", node.Devices[i].ID)
		found, numa := checkType(annos, *node.Devices[i], k)
		if !found {
			klog.InfoS("card type mismatch,continuing...", "pod", klog.KObj(pod), node.Devices[i].Type, k.Type)
			continue
		}
		if numa && prevnuma != node.Devices[i].Numa {
			klog.InfoS("Numa not fit, resotoreing", "pod", klog.KObj(pod), "k.nums", k.Nums, "numa", numa, "prevnuma", prevnuma, "device numa", node.Devices[i].Numa)
			k.Nums = originReq
			prevnuma = node.Devices[i].Numa
			tmpDevs = make(map[string]util.ContainerDevices)
		}
		if !checkUUID(annos, *node.Devices[i], k) {
			klog.InfoS("card uuid mismatch,", "pod", klog.KObj(pod), "current device info is:", node.Devices[i])
			continue
		}

		memreq := int32(0)
		if node.Devices[i].Count <= node.Devices[i].Used {
			continue
		}
		if k.Coresreq > 100 {
			klog.ErrorS(nil, "core limit can't exceed 100", "pod", klog.KObj(pod))
			k.Coresreq = 100
			//return false, tmpDevs
		}
		if k.Memreq > 0 {
			memreq = k.Memreq
		}
		if k.MemPercentagereq != 101 && k.Memreq == 0 {
			//This incurs an issue
			memreq = node.Devices[i].Totalmem * k.MemPercentagereq / 100
		}
		if node.Devices[i].Totalmem-node.Devices[i].Usedmem < memreq {
			klog.V(5).InfoS("card Insufficient remaining memory", "pod", klog.KObj(pod), "device index", i, "device", node.Devices[i].ID, "device total memory", node.Devices[i].Totalmem, "device used memory", node.Devices[i].Usedmem, "request memory", memreq)
			continue
		}
		if node.Devices[i].Totalcore-node.Devices[i].Usedcores < k.Coresreq {
			klog.V(5).InfoS("card Insufficient remaining cores", "pod", klog.KObj(pod), "device index", i, "device", node.Devices[i].ID, "device total core", node.Devices[i].Totalcore, "device used core", node.Devices[i].Usedcores, "request cores", k.Coresreq)
			continue
		}
		// Coresreq=100 indicates it want this card exclusively
		if node.Devices[i].Totalcore == 100 && k.Coresreq == 100 && node.Devices[i].Used > 0 {
			klog.V(5).InfoS("the container wants exclusive access to an entire card, but the card is already in use", "pod", klog.KObj(pod), "device index", i, "device", node.Devices[i].ID, "used", node.Devices[i].Used)
			continue
		}
		// You can't allocate core=0 job to an already full GPU
		if node.Devices[i].Totalcore != 0 && node.Devices[i].Usedcores == node.Devices[i].Totalcore && k.Coresreq == 0 {
			klog.V(5).InfoS("can't allocate core=0 job to an already full GPU", "pod", klog.KObj(pod), "device index", i, "device", node.Devices[i].ID)
			continue
		}
		if k.Nums > 0 {
			klog.InfoS("first fitted", "pod", klog.KObj(pod), "device", node.Devices[i].ID)
			k.Nums--
			tmpDevs[k.Type] = append(tmpDevs[k.Type], util.ContainerDevice{
				Idx:       int(node.Devices[i].Index),
				UUID:      node.Devices[i].ID,
				Type:      k.Type,
				Usedmem:   memreq,
				Usedcores: k.Coresreq,
			})
		}
		if k.Nums == 0 {
			klog.InfoS("device allocate success", "pod", klog.KObj(pod), "allocate device", tmpDevs)
			return true, tmpDevs
		}
	}
	return false, tmpDevs
}

func fitInDevices(node *NodeUsage, requests util.ContainerDeviceRequests, annos map[string]string, pod *corev1.Pod, devinput *util.PodDevices) (bool, float32) {
	//devmap := make(map[string]util.ContainerDevices)
	devs := util.ContainerDevices{}
	total := int32(0)
	free := int32(0)
	sums := 0
	//This loop is for requests for different devices
	for _, k := range requests {
		sums += int(k.Nums)
		if int(k.Nums) > len(node.Devices) {
			klog.InfoS("request devices nums cannot exceed the total number of devices on the node.", "pod", klog.KObj(pod), "request devices nums", k.Nums, "node device nums", len(node.Devices))
			return false, 0
		}
		sort.Sort(node.Devices)
		fit, tmpDevs := fitInCertainDevice(node, k, annos, pod)
		if fit {
			for _, val := range tmpDevs[k.Type] {
				total += node.Devices[val.Idx].Count
				free += node.Devices[val.Idx].Count - node.Devices[val.Idx].Used
				node.Devices[val.Idx].Used++
				node.Devices[val.Idx].Usedcores += val.Usedcores
				node.Devices[val.Idx].Usedmem += val.Usedmem
			}
			devs = append(devs, tmpDevs[k.Type]...)
		} else {
			return false, 0
		}
		(*devinput)[k.Type] = append((*devinput)[k.Type], devs)
	}
	return true, float32(total)/float32(free) + float32(len(node.Devices)-sums)
}

func calcScore(nodes *map[string]*NodeUsage, errMap *map[string]string, nums util.PodDeviceRequests, annos map[string]string, task *corev1.Pod) (*NodeScoreList, error) {
	res := make(NodeScoreList, 0, len(*nodes))
	for nodeID, node := range *nodes {
		viewStatus(*node)
		score := NodeScore{nodeID: nodeID, devices: make(util.PodDevices), score: 0}

		//This loop is for different container request
		ctrfit := false
		for ctrid, n := range nums {
			sums := 0
			for _, k := range n {
				sums += int(k.Nums)
			}

			if sums == 0 {
				for idx := range score.devices {
					if len(score.devices[idx]) <= ctrid {
						score.devices[idx] = append(score.devices[idx], util.ContainerDevices{})
					}
					score.devices[idx][ctrid] = append(score.devices[idx][ctrid], util.ContainerDevice{})
				}
				continue
			}
			klog.V(5).InfoS("fitInDevices", "pod", klog.KObj(task), "node", nodeID)
			fit, nodescore := fitInDevices(node, n, annos, task, &score.devices)
			ctrfit = fit
			if fit {
				klog.InfoS("calcScore:pod fit node score results", "pod", klog.KObj(task), "node", nodeID, "score", nodescore)
				score.score += nodescore
			} else {
				klog.InfoS("calcScore:node not fit pod", "pod", klog.KObj(task), "node", nodeID)
				break
			}
		}
		if ctrfit {
			res = append(res, &score)
		}
	}
	return &res, nil
}
