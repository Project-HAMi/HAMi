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
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
	"github.com/Project-HAMi/HAMi/pkg/util"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

func viewStatus(usage NodeUsage) {
	klog.Info("devices status")
	for _, val := range usage.Devices.DeviceLists {
		klog.InfoS("device status", "device id", val.Device.ID, "device detail", val)
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
	for i := len(node.Devices.DeviceLists) - 1; i >= 0; i-- {
		klog.InfoS("scoring pod", "pod", klog.KObj(pod), "Memreq", k.Memreq, "MemPercentagereq", k.MemPercentagereq, "Coresreq", k.Coresreq, "Nums", k.Nums, "device index", i, "device", node.Devices.DeviceLists[i].Device.ID)
		found, numa := checkType(annos, *node.Devices.DeviceLists[i].Device, k)
		if !found {
			klog.InfoS("card type mismatch,continuing...", "pod", klog.KObj(pod), (node.Devices.DeviceLists[i].Device).Type, k.Type)
			continue
		}
		if numa && prevnuma != node.Devices.DeviceLists[i].Device.Numa {
			klog.InfoS("Numa not fit, resotoreing", "pod", klog.KObj(pod), "k.nums", k.Nums, "numa", numa, "prevnuma", prevnuma, "device numa", node.Devices.DeviceLists[i].Device.Numa)
			k.Nums = originReq
			prevnuma = node.Devices.DeviceLists[i].Device.Numa
			tmpDevs = make(map[string]util.ContainerDevices)
		}
		if !checkUUID(annos, *node.Devices.DeviceLists[i].Device, k) {
			klog.InfoS("card uuid mismatch,", "pod", klog.KObj(pod), "current device info is:", *node.Devices.DeviceLists[i].Device)
			continue
		}

		memreq := int32(0)
		if node.Devices.DeviceLists[i].Device.Count <= node.Devices.DeviceLists[i].Device.Used {
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
			memreq = node.Devices.DeviceLists[i].Device.Totalmem * k.MemPercentagereq / 100
		}
		if node.Devices.DeviceLists[i].Device.Totalmem-node.Devices.DeviceLists[i].Device.Usedmem < memreq {
			klog.V(5).InfoS("card Insufficient remaining memory", "pod", klog.KObj(pod), "device index", i, "device", node.Devices.DeviceLists[i].Device.ID, "device total memory", node.Devices.DeviceLists[i].Device.Totalmem, "device used memory", node.Devices.DeviceLists[i].Device.Usedmem, "request memory", memreq)
			continue
		}
		if node.Devices.DeviceLists[i].Device.Totalcore-node.Devices.DeviceLists[i].Device.Usedcores < k.Coresreq {
			klog.V(5).InfoS("card Insufficient remaining cores", "pod", klog.KObj(pod), "device index", i, "device", node.Devices.DeviceLists[i].Device.ID, "device total core", node.Devices.DeviceLists[i].Device.Totalcore, "device used core", node.Devices.DeviceLists[i].Device.Usedcores, "request cores", k.Coresreq)
			continue
		}
		// Coresreq=100 indicates it want this card exclusively
		if node.Devices.DeviceLists[i].Device.Totalcore == 100 && k.Coresreq == 100 && node.Devices.DeviceLists[i].Device.Used > 0 {
			klog.V(5).InfoS("the container wants exclusive access to an entire card, but the card is already in use", "pod", klog.KObj(pod), "device index", i, "device", node.Devices.DeviceLists[i].Device.ID, "used", node.Devices.DeviceLists[i].Device.Used)
			continue
		}
		// You can't allocate core=0 job to an already full GPU
		if node.Devices.DeviceLists[i].Device.Totalcore != 0 && node.Devices.DeviceLists[i].Device.Usedcores == node.Devices.DeviceLists[i].Device.Totalcore && k.Coresreq == 0 {
			klog.V(5).InfoS("can't allocate core=0 job to an already full GPU", "pod", klog.KObj(pod), "device index", i, "device", node.Devices.DeviceLists[i].Device.ID)
			continue
		}
		if k.Nums > 0 {
			klog.InfoS("first fitted", "pod", klog.KObj(pod), "device", node.Devices.DeviceLists[i].Device.ID)
			k.Nums--
			tmpDevs[k.Type] = append(tmpDevs[k.Type], util.ContainerDevice{
				Idx:       int(node.Devices.DeviceLists[i].Device.Index),
				UUID:      node.Devices.DeviceLists[i].Device.ID,
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
	total, totalCore, totalMem := int32(0), int32(0), int32(0)
	free, freeCore, freeMem := int32(0), int32(0), int32(0)
	sums := 0
	// computer all device score for one node
	for index := range node.Devices.DeviceLists {
		node.Devices.DeviceLists[index].ComputeScore(requests)
	}
	//This loop is for requests for different devices
	for _, k := range requests {
		sums += int(k.Nums)
		if int(k.Nums) > len(node.Devices.DeviceLists) {
			klog.InfoS("request devices nums cannot exceed the total number of devices on the node.", "pod", klog.KObj(pod), "request devices nums", k.Nums, "node device nums", len(node.Devices.DeviceLists))
			return false, 0
		}
		sort.Sort(node.Devices)
		fit, tmpDevs := fitInCertainDevice(node, k, annos, pod)
		if fit {
			for _, val := range tmpDevs[k.Type] {
				total += node.Devices.DeviceLists[val.Idx].Device.Count
				totalCore += node.Devices.DeviceLists[val.Idx].Device.Totalcore
				totalMem += node.Devices.DeviceLists[val.Idx].Device.Totalmem
				free += node.Devices.DeviceLists[val.Idx].Device.Count - node.Devices.DeviceLists[val.Idx].Device.Used
				freeCore += node.Devices.DeviceLists[val.Idx].Device.Totalcore - node.Devices.DeviceLists[val.Idx].Device.Usedcores
				freeMem += node.Devices.DeviceLists[val.Idx].Device.Totalmem - node.Devices.DeviceLists[val.Idx].Device.Usedmem

				node.Devices.DeviceLists[val.Idx].Device.Used++
				node.Devices.DeviceLists[val.Idx].Device.Usedcores += val.Usedcores
				node.Devices.DeviceLists[val.Idx].Device.Usedmem += val.Usedmem
			}
			devs = append(devs, tmpDevs[k.Type]...)
		} else {
			return false, 0
		}
		(*devinput)[k.Type] = append((*devinput)[k.Type], devs)
	}
	return true, 0
}

func (s *Scheduler) calcScore(nodes *map[string]*NodeUsage, nums util.PodDeviceRequests, annos map[string]string, task *corev1.Pod) (*policy.NodeScoreList, error) {
	userNodePolicy := config.NodeSchedulerPolicy
	if annos != nil {
		if value, ok := annos[policy.NodeSchedulerPolicyAnnotationKey]; ok {
			userNodePolicy = value
		}
	}
	res := policy.NodeScoreList{
		Policy:   userNodePolicy,
		NodeList: make([]*policy.NodeScore, 0),
	}

	//func calcScore(nodes *map[string]*NodeUsage, errMap *map[string]string, nums util.PodDeviceRequests, annos map[string]string, task *corev1.Pod) (*NodeScoreList, error) {
	//	res := make(NodeScoreList, 0, len(*nodes))
	for nodeID, node := range *nodes {
		viewStatus(*node)
		score := policy.NodeScore{NodeID: nodeID, Devices: make(util.PodDevices), Score: 0}

		//This loop is for different container request
		ctrfit := false
		for ctrid, n := range nums {
			sums := 0
			for _, k := range n {
				sums += int(k.Nums)
			}

			if sums == 0 {
				for idx := range score.Devices {
					if len(score.Devices[idx]) <= ctrid {
						score.Devices[idx] = append(score.Devices[idx], util.ContainerDevices{})
					}
					score.Devices[idx][ctrid] = append(score.Devices[idx][ctrid], util.ContainerDevice{})
					continue
				}
			}
			klog.V(5).InfoS("fitInDevices", "pod", klog.KObj(task), "node", nodeID)
			fit, _ := fitInDevices(node, n, annos, task, &score.Devices)
			ctrfit = fit
			if !fit {
				klog.InfoS("calcScore:node not fit pod", "pod", klog.KObj(task), "node", nodeID)
				break
			}
		}

		if ctrfit {
			score.ComputeScore(node.Devices)
			res.NodeList = append(res.NodeList, &score)
		}
	}
	return &res, nil
}
