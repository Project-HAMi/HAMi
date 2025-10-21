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

var scheduleFailureReasons = []string{
	cardTypeMismatch,
	cardUUIDMismatch,
	cardTimeSlicingExhausted,
	cardComputeUnitsExhausted,
	cardInsufficientMemory,
	cardInsufficientCore,
	numaNotFit,
	exclusiveDeviceAllocateConflict,
	cardNotFoundCustomFilterRule,
	nodeInsufficientDevice,
	allocatedCardsInsufficientRequest,
}

func getNodeResources(list NodeUsage, t string) []*device.DeviceUsage {
	l := []*device.DeviceUsage{}
	for _, val := range list.Devices.DeviceLists {
		if strings.Contains(val.Device.Type, t) {
			l = append(l, val.Device)
		}
	}
	return l
}

func fitInDevices(node *NodeUsage, requests device.ContainerDeviceRequests, pod *corev1.Pod, nodeInfo *device.NodeInfo, devinput *device.PodDevices) (bool, string) {
	//devmap := make(map[string]device.ContainerDevices)
	devs := device.ContainerDevices{}
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
		fit, tmpDevs, devreason := device.GetDevices()[k.Type].Fit(getNodeResources(*node, k.Type), k, pod, nodeInfo, devinput)
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

func (s *Scheduler) calcScore(nodes *map[string]*NodeUsage, resourceReqs device.PodDeviceRequests, task *corev1.Pod, failedNodes map[string]string) (*policy.NodeScoreList, error) {
	userNodePolicy := config.NodeSchedulerPolicy
	if task.GetAnnotations() != nil {
		if value, ok := task.GetAnnotations()[policy.NodeSchedulerPolicyAnnotationKey]; ok {
			userNodePolicy = value
		}
	}
	res := policy.NodeScoreList{
		Policy:   userNodePolicy,
		NodeList: make([]*policy.NodeScore, 0),
	}

	wg := sync.WaitGroup{}
	fitNodesMutex := sync.Mutex{}
	failedNodesMutex := sync.Mutex{}
	failureReason := make(map[string][]string)
	errCh := make(chan error, len(*nodes))
	for nodeID, node := range *nodes {
		wg.Add(1)
		go func(nodeID string, node *NodeUsage) {
			defer wg.Done()

			viewStatus(*node)
			score := policy.NodeScore{NodeID: nodeID, Node: node.Node, Devices: make(device.PodDevices), Score: 0}
			score.ComputeDefaultScore(node.Devices)
			snapshot := score.SnapshotDevice(node.Devices)

			nodeInfo, err := s.GetNode(nodeID)
			if err != nil {
				klog.ErrorS(err, "Failed to get node", "nodeID", nodeID)
				errCh <- err
				return
			}

			//This loop is for different container request
			ctrfit := false
			deviceType := ""
			for ctrid, n := range resourceReqs {
				sums := 0
				for _, k := range n {
					sums += int(k.Nums)
				}

				// container need no device and we have got certain deviceType
				if sums == 0 && deviceType != "" {
					score.Devices[deviceType] = append(score.Devices[deviceType], device.ContainerDevices{})
					continue
				}
				klog.V(5).InfoS("fitInDevices", "pod", klog.KObj(task), "node", nodeID)
				fit, reason := fitInDevices(node, n, task, nodeInfo, &score.Devices)
				// found certain deviceType, fill missing empty allocation for containers before this
				for idx := range score.Devices {
					deviceType = idx
					for len(score.Devices[idx]) <= ctrid {
						emptyContainerDevices := device.ContainerDevices{}
						emptyPodSingleDevice := device.PodSingleDevice{}
						emptyPodSingleDevice = append(emptyPodSingleDevice, emptyContainerDevices)
						score.Devices[idx] = append(emptyPodSingleDevice, score.Devices[idx]...)
					}
				}
				ctrfit = fit
				if !fit {
					klog.V(4).InfoS(nodeUnfitPod, "pod", klog.KObj(task), "node", nodeID, "reason", reason)
					failedNodesMutex.Lock()
					failedNodes[nodeID] = nodeUnfitPod
					for _, reasonType := range parseNodeReason(reason) {
						failureReason[reasonType] = append(failureReason[reasonType], nodeID)
					}
					failedNodesMutex.Unlock()
					break
				}
			}

			if ctrfit {
				fitNodesMutex.Lock()
				res.NodeList = append(res.NodeList, &score)
				fitNodesMutex.Unlock()
				score.OverrideScore(snapshot, userNodePolicy)
				klog.V(4).InfoS(nodeFitPod, "pod", klog.KObj(task), "node", nodeID, "score", score.Score)
			}
		}(nodeID, node)
	}
	wg.Wait()
	close(errCh)

	// only pod scheduler failure will record failure event
	if len(res.NodeList) == 0 {
		for reasonType, failureNodes := range failureReason {
			sort.Strings(failureNodes)
			reason := fmt.Errorf("%d nodes %s(%s)", len(failureNodes), reasonType, strings.Join(failureNodes, ","))
			s.recordScheduleFilterResultEvent(task, EventReasonFilteringFailed, "", reason)
		}
	}

	var errorsSlice []error
	for e := range errCh {
		errorsSlice = append(errorsSlice, e)
	}
	return &res, utilerrors.NewAggregate(errorsSlice)
}

func parseNodeReason(nodeReason string) []string {
	var res []string
	for _, reason := range scheduleFailureReasons {
		if strings.Contains(nodeReason, reason) {
			res = append(res, reason)
		}
	}
	return res
}
