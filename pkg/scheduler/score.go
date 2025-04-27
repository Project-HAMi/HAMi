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
	"fmt"
	"sort"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

func viewStatus(usage NodeUsage) {
	klog.V(5).Info("devices status")
	for _, val := range usage.Devices.DeviceLists {
		klog.V(5).InfoS("device status", "device id", val.Device.ID, "device detail", val)
	}
}

func checkType(annos map[string]string, d util.DeviceUsage, n util.ContainerDeviceRequest) (bool, bool) {
	//General type check, NVIDIA->NVIDIA MLU->MLU
	klog.V(3).InfoS("Type check", "device", d.Type, "req", n.Type)
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

const (
	cardTypeMismatch                  = "CardTypeMismatch"
	cardUUIDMismatch                  = "CardUuidMismatch"
	cardTimeSlicingExhausted          = "CardTimeSlicingExhausted"
	cardComputeUnitsExhausted         = "CardComputeUnitsExhausted"
	cardInsufficientMemory            = "CardInsufficientMemory"
	cardInsufficientCore              = "CardInsufficientCore"
	numaNotFit                        = "NumaNotFit"
	exclusiveDeviceAllocateConflict   = "ExclusiveDeviceAllocateConflict"
	cardNotFoundCustomFilterRule      = "CardNotFoundCustomFilterRule"
	nodeInsufficientDevice            = "NodeInsufficientDevice"
	allocatedCardsInsufficientRequest = "AllocatedCardsInsufficientRequest"
	nodeUnfitPod                      = "NodeUnfitPod"
	nodeFitPod                        = "NodeFitPod"
)

func fitInCertainDevice(node *NodeUsage, request util.ContainerDeviceRequest, annos map[string]string, pod *corev1.Pod, allocated *util.PodDevices) (bool, map[string]util.ContainerDevices, string) {
	k := request
	originReq := k.Nums
	prevnuma := -1
	nodeName := node.Node.Name
	klog.InfoS("Allocating device for container request", "pod", klog.KObj(pod), "card request", k)
	var tmpDevs map[string]util.ContainerDevices
	tmpDevs = make(map[string]util.ContainerDevices)
	reason := make(map[string]int)
	for i := len(node.Devices.DeviceLists) - 1; i >= 0; i-- {
		dev := node.Devices.DeviceLists[i].Device
		klog.V(4).InfoS("scoring pod", "pod", klog.KObj(pod), "node", nodeName, "device", dev.ID, "Memreq", k.Memreq, "MemPercentagereq", k.MemPercentagereq, "Coresreq", k.Coresreq, "Nums", k.Nums, "device index", i)
		found, numa := checkType(annos, *dev, k)
		if !found {
			reason[cardTypeMismatch]++
			klog.V(5).InfoS(cardTypeMismatch, "pod", klog.KObj(pod), "node", nodeName, "device", dev.ID, dev.Type, k.Type)
			continue
		}
		if numa && prevnuma != dev.Numa {
			if k.Nums != originReq {
				reason[numaNotFit] += len(tmpDevs)
				klog.V(5).InfoS(numaNotFit, "pod", klog.KObj(pod), "node", nodeName, "device", dev.ID, "k.nums", k.Nums, "numa", numa, "prevnuma", prevnuma, "device numa", dev.Numa)
			}
			k.Nums = originReq
			prevnuma = dev.Numa
			tmpDevs = make(map[string]util.ContainerDevices)
		}
		if !checkUUID(annos, *dev, k) {
			reason[cardUUIDMismatch]++
			klog.V(5).InfoS(cardUUIDMismatch, "pod", klog.KObj(pod), "node", nodeName, "device", dev.ID, "current device info is:", *dev)
			continue
		}

		memreq := int32(0)
		if dev.Count <= dev.Used {
			reason[cardTimeSlicingExhausted]++
			klog.V(5).InfoS(cardTimeSlicingExhausted, "pod", klog.KObj(pod), "node", nodeName, "device", dev.ID, "count", dev.Count, "used", dev.Used)
			continue
		}
		if k.Coresreq > 100 {
			klog.ErrorS(nil, "core limit can't exceed 100", "pod", klog.KObj(pod), "node", nodeName, "device", dev.ID)
			k.Coresreq = 100
			//return false, tmpDevs
		}
		if k.Memreq > 0 {
			memreq = k.Memreq
		}
		if k.MemPercentagereq != 101 && k.Memreq == 0 {
			//This incurs an issue
			memreq = dev.Totalmem * k.MemPercentagereq / 100
		}
		if dev.Totalmem-dev.Usedmem < memreq {
			reason[cardInsufficientMemory]++
			klog.V(5).InfoS(cardInsufficientMemory, "pod", klog.KObj(pod), "node", nodeName, "device", dev.ID, "device index", i, "device total memory", dev.Totalmem, "device used memory", dev.Usedmem, "request memory", memreq)
			continue
		}
		if dev.Totalcore-dev.Usedcores < k.Coresreq {
			reason[cardInsufficientCore]++
			klog.V(5).InfoS(cardInsufficientCore, "pod", klog.KObj(pod), "node", nodeName, "device", dev.ID, "device index", i, "device total core", dev.Totalcore, "device used core", dev.Usedcores, "request cores", k.Coresreq)
			continue
		}
		// Coresreq=100 indicates it want this card exclusively
		if dev.Totalcore == 100 && k.Coresreq == 100 && dev.Used > 0 {
			reason[exclusiveDeviceAllocateConflict]++
			klog.V(5).InfoS(exclusiveDeviceAllocateConflict, "pod", klog.KObj(pod), "node", nodeName, "device", dev.ID, "device index", i, "used", dev.Used)
			continue
		}
		// You can't allocate core=0 job to an already full GPU
		if dev.Totalcore != 0 && dev.Usedcores == dev.Totalcore && k.Coresreq == 0 {
			reason[cardComputeUnitsExhausted]++
			klog.V(5).InfoS(cardComputeUnitsExhausted, "pod", klog.KObj(pod), "node", nodeName, "device", dev.ID, "device index", i)
			continue
		}
		if !device.GetDevices()[k.Type].CustomFilterRule(allocated, request, tmpDevs[k.Type], dev) {
			reason[cardNotFoundCustomFilterRule]++
			klog.V(5).InfoS(cardNotFoundCustomFilterRule, "pod", klog.KObj(pod), "node", nodeName, "device", dev.ID, "device index", i)
			continue
		}
		if k.Nums > 0 {
			klog.V(5).InfoS("find fit device", "pod", klog.KObj(pod), "node", nodeName, "device", dev.ID)
			k.Nums--
			tmpDevs[k.Type] = append(tmpDevs[k.Type], util.ContainerDevice{
				Idx:       int(dev.Index),
				UUID:      dev.ID,
				Type:      k.Type,
				Usedmem:   memreq,
				Usedcores: k.Coresreq,
			})
		}
		if k.Nums == 0 {
			klog.V(4).InfoS("device allocate success", "pod", klog.KObj(pod), "node", nodeName, "allocate device", tmpDevs)
			return true, tmpDevs, ""
		}
		if dev.Mode == "mig" {
			i++
		}
	}
	if len(tmpDevs) > 0 {
		reason[allocatedCardsInsufficientRequest] = len(tmpDevs)
		klog.V(5).InfoS(allocatedCardsInsufficientRequest, "pod", klog.KObj(pod), "node", nodeName, "request", originReq, "allocated", len(tmpDevs))
	}
	return false, tmpDevs, genReason(reason, len(node.Devices.DeviceLists))
}

func genReason(reasons map[string]int, cards int) string {
	var reason []string
	for r, cnt := range reasons {
		reason = append(reason, fmt.Sprintf("%d/%d %s", cnt, cards, r))
	}
	return strings.Join(reason, ", ")
}

func fitInDevices(node *NodeUsage, requests util.ContainerDeviceRequests, annos map[string]string, pod *corev1.Pod, devinput *util.PodDevices) (bool, string) {
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
			klog.V(5).InfoS(nodeInsufficientDevice, "pod", klog.KObj(pod), "request devices nums", k.Nums, "node device nums", len(node.Devices.DeviceLists))
			return false, nodeInsufficientDevice
		}
		sort.Sort(node.Devices)
		fit, tmpDevs, reason := fitInCertainDevice(node, k, annos, pod, devinput)
		if fit {
			for idx, val := range tmpDevs[k.Type] {
				for nidx, v := range node.Devices.DeviceLists {
					//bc node.Devices has been sorted, so we should find out the correct device
					if v.Device.ID != val.UUID {
						continue
					}
					total += v.Device.Count
					totalCore += v.Device.Totalcore
					totalMem += v.Device.Totalmem
					free += v.Device.Count - v.Device.Used
					freeCore += v.Device.Totalcore - v.Device.Usedcores
					freeMem += v.Device.Totalmem - v.Device.Usedmem
					err := device.GetDevices()[k.Type].AddResourceUsage(node.Devices.DeviceLists[nidx].Device, &tmpDevs[k.Type][idx])
					if err != nil {
						klog.Errorf("AddResourceUsage failed:%s", err.Error())
						return false, "AddResourceUsage failed"
					}
					klog.Infoln("After AddResourceUsage:", node.Devices.DeviceLists[nidx].Device)
				}
			}
			devs = append(devs, tmpDevs[k.Type]...)
		} else {
			return false, reason
		}
		(*devinput)[k.Type] = append((*devinput)[k.Type], devs)
	}
	return true, ""
}

func (s *Scheduler) calcScore(nodes *map[string]*NodeUsage, nums util.PodDeviceRequests, annos map[string]string, task *corev1.Pod, failedNodes map[string]string) (*policy.NodeScoreList, error) {
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

	wg := sync.WaitGroup{}
	mutex := sync.Mutex{}
	errCh := make(chan error, len(*nodes))
	for nodeID, node := range *nodes {
		wg.Add(1)
		go func(nodeID string, node *NodeUsage) {
			defer wg.Done()

			viewStatus(*node)
			score := policy.NodeScore{NodeID: nodeID, Node: node.Node, Devices: make(util.PodDevices), Score: 0}
			score.ComputeDefaultScore(node.Devices)

			//This loop is for different container request
			ctrfit := false
			for ctrid, n := range nums {
				sums := 0
				for _, k := range n {
					sums += int(k.Nums)
				}

				if sums == 0 {
					for idx := range score.Devices {
						for len(score.Devices[idx]) < ctrid {
							defaultContainerDevices := util.ContainerDevices{}
							defaultPodSingleDevice := util.PodSingleDevice{}
							defaultPodSingleDevice = append(defaultPodSingleDevice, defaultContainerDevices)
							score.Devices[idx] = append(defaultPodSingleDevice, score.Devices[idx]...)
						}
						defaultContainerDevices := util.ContainerDevices{}
						score.Devices[idx] = append(score.Devices[idx], defaultContainerDevices)
					}
				}
				klog.V(5).InfoS("fitInDevices", "pod", klog.KObj(task), "node", nodeID)
				fit, reason := fitInDevices(node, n, annos, task, &score.Devices)
				ctrfit = fit
				if !fit {
					klog.V(4).InfoS(nodeUnfitPod, "pod", klog.KObj(task), "node", nodeID, "reason", reason)
					failedNodes[nodeID] = nodeUnfitPod
					break
				}
			}

			if ctrfit {
				mutex.Lock()
				res.NodeList = append(res.NodeList, &score)
				mutex.Unlock()
				score.OverrideScore(node.Devices, userNodePolicy)
				klog.V(4).InfoS(nodeFitPod, "pod", klog.KObj(task), "node", nodeID, "score", score.Score)
			}
		}(nodeID, node)
	}
	wg.Wait()
	close(errCh)

	var errorsSlice []error
	for e := range errCh {
		errorsSlice = append(errorsSlice, e)
	}
	return &res, utilerrors.NewAggregate(errorsSlice)
}
