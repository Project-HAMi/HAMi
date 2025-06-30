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

func getNodeResources(list NodeUsage, t string) []*util.DeviceUsage {
	l := []*util.DeviceUsage{}
	for _, val := range list.Devices.DeviceLists {
		if strings.Contains(val.Device.Type, t) {
			l = append(l, val.Device)
		}
	}
	return l
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
		_, ok := device.GetDevices()[k.Type]
		if !ok {
			return false, "Device type not found"
		}
		fit, tmpDevs, devreason := device.GetDevices()[k.Type].Fit(getNodeResources(*node, k.Type), k, annos, pod, devinput)
		reason := "node:" + node.Node.Name + " " + "resaon:" + devreason
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
					err := device.GetDevices()[k.Type].AddResourceUsage(pod, node.Devices.DeviceLists[nidx].Device, &tmpDevs[k.Type][idx])
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
			snapshot := score.SnapshotDevice(node.Devices)

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
				score.OverrideScore(snapshot, userNodePolicy)
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
