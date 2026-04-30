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
	"encoding/json"
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
	nums   int
	memreq int32
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
		for _, k := range resourceReqs[i] {
			nums += int(k.Nums)
			mem += k.Memreq
		}
		if nums > maxReq.nums || (nums == maxReq.nums && mem > maxReq.memreq) {
			maxReq.nums = nums
			maxReq.memreq = mem
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

	// Pre-compute container counts and request totals once, outside the
	// per-node goroutines, since they are identical for every node.
	numInitContainers := len(task.Spec.InitContainers)
	maxInitReq := podInitContainerMaxRequest(resourceReqs, numInitContainers)
	appReqTotal := podAppContainerTotalRequest(resourceReqs, numInitContainers)

	// Maintainer optimization (@Shouren):
	// Init containers run sequentially and exit before app containers start.
	// The node only ever needs to hold max(maxInitReq, appReqTotal) free at
	// any moment. We compare both GPU count AND memory to correctly detect
	// when init containers need more resources than app containers.
	// If needsInitClone=false, we skip fitInDevices for init containers
	// entirely — the app allocation implicitly covers them.
	needsInitClone := numInitContainers > 0 &&
		(maxInitReq.nums > appReqTotal.nums || maxInitReq.memreq > appReqTotal.memreq)

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

			ctrfit := true
			deviceType := ""

			// appContainersNode is the NodeUsage that regular containers
			// will deduct from.
			var appContainersNode *NodeUsage

			// initialNodeBytes holds the JSON snapshot of the node's full
			// capacity. It is only populated when needsInitClone is true,
			// so that each init container can be evaluated against a fresh
			// copy of the node without draining the pool for app containers.
			var initialNodeBytes []byte

			if needsInitClone {
				// Init containers request MORE than app containers (by count
				// or memory), so we must evaluate them against full node
				// capacity. Marshal once; each init container gets its own
				// Unmarshal.
				initialNodeBytes, err = json.Marshal(node)
				if err != nil {
					klog.ErrorS(err, "Failed to marshal node state for cloning", "nodeID", nodeID)
					errCh <- err
					return
				}

				// App containers share a separate copy that starts at full
				// capacity and is drained as each app container is scheduled.
				appContainersNode = &NodeUsage{}
				if err := json.Unmarshal(initialNodeBytes, appContainersNode); err != nil {
					klog.ErrorS(err, "Failed to unmarshal node state for app containers", "nodeID", nodeID)
					errCh <- err
					return
				}
			} else {
				// No init containers, or init requests are already covered
				// by app container requests — use the node directly with no
				// clone overhead.
				appContainersNode = node
			}

			// This loop iterates over every container's device requests.
			// resourceReqs is ordered: [initContainer_0, ..., initContainer_N-1,
			//                           appContainer_0, ..., appContainer_M-1]
			for ctrid, n := range resourceReqs {
				sums := 0
				for _, k := range n {
					sums += int(k.Nums)
				}

				// Maintainer optimization (@Shouren):
				// When init container requests are already covered by app
				// container requests (!needsInitClone), skip fitInDevices
				// for init containers entirely. Their device usage is
				// implicitly satisfied by the app container allocation since
				// init and app containers never run simultaneously — the node
				// only needs max(init, app) capacity at any moment.
				if ctrid < numInitContainers && !needsInitClone {
					if deviceType != "" {
						score.Devices[deviceType] = append(score.Devices[deviceType], device.ContainerDevices{})
					}
					continue
				}

				// Container needs no device but a deviceType has already been
				// identified — record an empty allocation and continue.
				if sums == 0 && deviceType != "" {
					score.Devices[deviceType] = append(score.Devices[deviceType], device.ContainerDevices{})
					continue
				}

				klog.V(5).InfoS("fitInDevices", "pod", klog.KObj(task), "node", nodeID)

				// Decide which NodeUsage view to pass to fitInDevices.
				var workingNode *NodeUsage
				if needsInitClone && ctrid < numInitContainers {
					// This is an init container that needs more resources than
					// the app containers. Give it a fresh snapshot of the
					// node's full capacity so it doesn't interfere with the
					// app container pool.
					workingNode = &NodeUsage{}
					if err := json.Unmarshal(initialNodeBytes, workingNode); err != nil {
						klog.ErrorS(err, "Failed to unmarshal node state for init container",
							"nodeID", nodeID, "ctrid", ctrid)
						errCh <- err
						return
					}
				} else {
					// Regular app container (or an init container whose
					// request is already covered by the app allocation).
					// Both deduct from the shared accumulated state.
					workingNode = appContainersNode
				}

				fit, reason := fitInDevices(workingNode, n, task, nodeInfo, &score.Devices)

				// Fill any missing empty-allocation slots for containers
				// that were processed before the first device-using container.
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

	// Only record a filter failure event when no node could fit the pod.
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
