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
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

// containerResourceSummary holds both the GPU count and total memory
// for a group of containers, so we can compare them on both dimensions.
type containerResourceSummary struct {
	nums     int
	memreq   int32
	coresreq int32
}

func viewStatus(usage NodeUsage) {
	klog.V(5).Info("devices status")
	for _, val := range usage.Devices.DeviceLists {
		klog.V(5).InfoS("device status", "device id", val.Device.ID, "device detail", val)
	}
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
	devs := device.ContainerDevices{}
	total, totalCore, totalMem := int32(0), int32(0), int32(0)
	free, freeCore, freeMem := int32(0), int32(0), int32(0)
	sums := 0
	// compute all device scores for one node
	for index := range node.Devices.DeviceLists {
		node.Devices.DeviceLists[index].ComputeScore(requests)
	}
	// This loop is for requests for different devices
	for _, k := range requests {
		sums += int(k.Nums)
		if int(k.Nums) > len(node.Devices.DeviceLists) {
			klog.V(5).InfoS(common.NodeInsufficientDevice, "pod", klog.KObj(pod), "request devices nums", k.Nums, "node device nums", len(node.Devices.DeviceLists))
			return false, common.NodeInsufficientDevice
		}
		sort.Sort(node.Devices)
		_, ok := device.GetDevices()[k.Type]
		if !ok {
			return false, "Device type not found"
		}
		fit, tmpDevs, reason := device.GetDevices()[k.Type].Fit(getNodeResources(*node, k.Type), k, pod, nodeInfo, devinput)
		if fit {
			for idx, val := range tmpDevs[k.Type] {
				for nidx, v := range node.Devices.DeviceLists {
					// bc node.Devices has been sorted, so we should find out the correct device
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
					klog.V(5).Infoln("After AddResourceUsage:", node.Devices.DeviceLists[nidx].Device)
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

// podInitContainerMaxRequest returns the maximum single-container device
// request across all init containers, considering both GPU count AND memory.
// Per Kubernetes semantics, init containers run sequentially, so the node
// only needs to reserve capacity for the largest one at any moment.
func podInitContainerMaxRequest(resourceReqs device.PodDeviceRequests, numInitContainers int) containerResourceSummary {
	maxReq := containerResourceSummary{}
	for i := range numInitContainers {
		var nums int
		var mem int32
		var cores int32
		for _, k := range resourceReqs[i] {
			nums += int(k.Nums)
			mem += k.Memreq
			cores += k.Coresreq
		}
		if nums > maxReq.nums || (nums == maxReq.nums && mem > maxReq.memreq) {
			maxReq.nums = nums
			maxReq.memreq = mem
			maxReq.coresreq = cores
		}
	}
	return maxReq
}

// podAppContainerTotalRequest returns the sum of device requests across all
// regular (non-init) containers, considering both GPU count AND memory.
func podAppContainerTotalRequest(resourceReqs device.PodDeviceRequests, numInitContainers int) containerResourceSummary {
	total := containerResourceSummary{}
	for i := numInitContainers; i < len(resourceReqs); i++ {
		for _, k := range resourceReqs[i] {
			total.nums += int(k.Nums)
			total.memreq += k.Memreq
		}
	}
	return total
}

// canFitInitContainer checks if the node has at least one device that can
// individually satisfy the init container's resource requirements.
// This prevents allocation failures when total capacity appears sufficient
// but no single device has enough memory/cores.
func (node *NodeUsage) canFitInitContainer(maxInitReq containerResourceSummary) bool {
	if node == nil {
		return false // nil node cannot satisfy any request
	}

	for _, deviceList := range node.Devices.DeviceLists {
		device := deviceList.Device
		if device == nil {
			continue
		}

		availableMem := device.Totalmem - device.Usedmem
		availableCores := device.Totalcore - device.Usedcores

		// Check if this device individually satisfies init requirements
		if availableMem >= maxInitReq.memreq && availableCores >= maxInitReq.coresreq {
			return true
		}
	}
	return false
}

func (s *Scheduler) calcScore(nodes *map[string]*NodeUsage, resourceReqs device.PodDeviceRequests, task *corev1.Pod, failedNodes map[string]string) (*policy.NodeScoreList, error) {
	userNodePolicy := config.NodeSchedulerPolicy
	if task.GetAnnotations() != nil {
		if value, ok := task.GetAnnotations()[util.NodeSchedulerPolicyAnnotationKey]; ok {
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

	numInitContainers := len(task.Spec.InitContainers)
	maxInitReq := podInitContainerMaxRequest(resourceReqs, numInitContainers)
	appReqTotal := podAppContainerTotalRequest(resourceReqs, numInitContainers)

	needsInitClone := numInitContainers > 0 &&
		(maxInitReq.nums > appReqTotal.nums || maxInitReq.memreq > appReqTotal.memreq)

	for nodeID, node := range *nodes {
		// Early exit: skip nodes that can't fit init containers on any single device
		if numInitContainers > 0 && !node.canFitInitContainer(maxInitReq) {
			failedNodesMutex.Lock()
			failedNodes[nodeID] = fmt.Sprintf(
				"no single device has sufficient memory (%dMiB) for init container",
				maxInitReq.memreq)
			failureReason["InsufficientInitDeviceMemory"] = append(failureReason["InsufficientInitDeviceMemory"], nodeID)
			failedNodesMutex.Unlock()
			klog.V(4).InfoS("Node filtered: insufficient per-device capacity for init container",
				"pod", klog.KObj(task), "node", nodeID, "required_mem", maxInitReq.memreq)
			continue
		}
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

			var appContainersNode *NodeUsage
			if needsInitClone {
				appContainersNode = node.DeepCopy()
			} else {
				appContainersNode = node
			}

			ctrfit := true
			for ctrid, n := range resourceReqs {
				sums := 0
				for _, k := range n {
					sums += int(k.Nums)
				}

				// Capture the set of device types that existed BEFORE processing this container
				existingBefore := make(map[string]bool)
				for typ := range score.Devices {
					existingBefore[typ] = true
				}

				// Skip device allocation for init containers if they don't need cloning.
				if ctrid < numInitContainers && !needsInitClone {
					// Do nothing. Let it fall through to normalization.
				} else if sums > 0 {
					klog.V(5).InfoS("fitInDevices", "pod", klog.KObj(task), "node", nodeID)

					var workingNode *NodeUsage
					if needsInitClone && ctrid < numInitContainers {
						workingNode = node.DeepCopy()
					} else {
						workingNode = appContainersNode
					}

					fit, reason := fitInDevices(workingNode, n, task, nodeInfo, &score.Devices)
					ctrfit = fit
					if !fit {
						klog.V(4).InfoS(common.NodeUnfitPod, "pod", klog.KObj(task), "node", nodeID, "reason", reason)
						failedNodesMutex.Lock()
						failedNodes[nodeID] = common.NodeUnfitPod
						for reasonType := range common.ParseReason(reason) {
							failureReason[reasonType] = append(failureReason[reasonType], nodeID)
						}
						failedNodesMutex.Unlock()
						break
					}
				}

				// NORMALIZATION: Ensure every device slice has exactly ctrid+1 entries,
				// keeping previous allocations at their original indices.
				for typ, devSlice := range score.Devices {
					if len(devSlice) < ctrid+1 {
						if !existingBefore[typ] {
							// Brand new type for this container: allocation is at devSlice[0],
							// shift it to index ctrid by prepending non‑nil empty slices.
							pad := make(device.PodSingleDevice, 0, ctrid)
							for i := 0; i < ctrid; i++ {
								pad = append(pad, device.ContainerDevices{})
							}
							score.Devices[typ] = append(pad, devSlice...)
						} else {
							// Type existed before: just append empty (non‑nil) slices to reach the needed length.
							for len(score.Devices[typ]) < ctrid+1 {
								score.Devices[typ] = append(score.Devices[typ], device.ContainerDevices{})
							}
						}
					}
				}
			}

			// When needsInitClone=false, copy the first app container's allocation
			// into the init container slots so the device plugin knows which devices to use.
			if numInitContainers > 0 && !needsInitClone {
				for deviceType := range score.Devices {
					containerList := score.Devices[deviceType]
					if len(containerList) > numInitContainers {
						firstAppAllocation := containerList[numInitContainers]

						for initIdx := range numInitContainers {
							if initIdx < len(containerList) {
								containerList[initIdx] = firstAppAllocation
								klog.V(4).InfoS(
									"Copied app container device allocation to init container slot",
									"pod", klog.KObj(task),
									"init_idx", initIdx,
									"device_type", deviceType,
									"allocation", firstAppAllocation,
								)
							}
						}
					}
				}
			}

			if ctrfit {
				fitNodesMutex.Lock()
				res.NodeList = append(res.NodeList, &score)
				fitNodesMutex.Unlock()
				score.OverrideScore(snapshot, userNodePolicy)
				klog.V(4).InfoS(common.NodeFitPod, "pod", klog.KObj(task), "node", nodeID, "score", score.Score)
			}
		}(nodeID, node)
	}
	wg.Wait()
	close(errCh)

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
