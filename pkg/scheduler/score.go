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

// containerResourceSummary holds both the GPU count and total memory for a group of containers, so we can compare them on both dimensions.
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
		if val.Device == nil {
			continue
		}
		if getDeviceBaseType(val.Device.Type) == t {
			l = append(l, val.Device)
		}
	}
	return l
}

// getDeviceBaseType maps a device model string (e.g., "NVIDIA A100-SXM4-40GB") to the base type key used in the device registry (e.g., "NVIDIA").
func getDeviceBaseType(model string) string {
	bestName := ""
	bestWordLen := -1
	for name, dev := range device.GetDevices() {
		word := dev.CommonWord()
		if !strings.HasPrefix(model, word) {
			continue
		}
		if len(word) > bestWordLen || (len(word) == bestWordLen && name < bestName) {
			bestName = name
			bestWordLen = len(word)
		}
	}
	if bestWordLen == -1 {
		return model
	}
	return bestName
}

// nodeDeviceBaseTypes returns the set of base device types physically present on the node, computed once so that callers don't repeatedly re-derive it (which, combined with any residual
// ambiguity in getDeviceBaseType, previously risked producing different results across calls within the same scheduling attempt).
func nodeDeviceBaseTypes(list policy.DeviceUsageList) map[string]struct{} {
	types := make(map[string]struct{})
	for _, dl := range list.DeviceLists {
		if dl.Device != nil {
			types[getDeviceBaseType(dl.Device.Type)] = struct{}{}
		}
	}
	return types
}

// fitInDevices tries to allocate a single container's device requests on the given node.
func fitInDevices(node *NodeUsage, requests device.ContainerDeviceRequests, pod *corev1.Pod, nodeInfo *device.NodeInfo, devinput *device.PodDevices) (bool, string) {
	// Snapshot the entire node state before any modifications.
	type devSnapshot struct {
		idx       int
		used      int32
		usedcores int32
		usedmem   int32
	}
	saved := make([]devSnapshot, len(node.Devices.DeviceLists))
	for i := range node.Devices.DeviceLists {
		d := node.Devices.DeviceLists[i].Device
		saved[i] = devSnapshot{
			idx:       i,
			used:      d.Used,
			usedcores: d.Usedcores,
			usedmem:   d.Usedmem,
		}
	}

	// Global rollback function restores the node to its original state.
	rollbackAll := func() {
		for i := range saved {
			dev := node.Devices.DeviceLists[i].Device
			dev.Used = saved[i].used
			dev.Usedcores = saved[i].usedcores
			dev.Usedmem = saved[i].usedmem
		}
	}

	for index := range node.Devices.DeviceLists {
		node.Devices.DeviceLists[index].ComputeScore(requests)
	}

	// Process each device type in the request.
	for _, k := range requests {
		// Sort devices by score (best fit first).
		sort.Sort(node.Devices)

		devPlugin, ok := device.GetDevices()[k.Type]
		if !ok {
			rollbackAll()
			errMsg := "Device type not found"
			klog.ErrorS(nil, errMsg, "pod", klog.KObj(pod), "type", k.Type, "node", node.Node.Name)
			return false, errMsg
		}

		typeDevices := getNodeResources(*node, k.Type)
		if int(k.Nums) > len(typeDevices) {
			klog.V(5).InfoS(common.NodeInsufficientDevice, "pod", klog.KObj(pod),
				"request devices nums", k.Nums, "node device nums (type)", len(typeDevices), "type", k.Type)
			rollbackAll()
			return false, common.NodeInsufficientDevice
		}

		fit, tmpDevs, reason := devPlugin.Fit(typeDevices, k, pod, nodeInfo, devinput)
		if !fit {
			rollbackAll()
			return false, reason
		}

		type usageSnapshot struct {
			idx       int
			used      int32
			usedcores int32
			usedmem   int32
		}
		modified := make([]usageSnapshot, 0, len(tmpDevs[k.Type]))

		for idx, val := range tmpDevs[k.Type] {
			targetIdx := -1
			for nidx, v := range node.Devices.DeviceLists {
				if v.Device.ID == val.UUID {
					targetIdx = nidx
					break
				}
			}
			if targetIdx == -1 {
				rollbackAll()
				errMsg := fmt.Sprintf("Device with UUID %q not found on node %s after Fit", val.UUID, node.Node.Name)
				klog.ErrorS(nil, errMsg, "pod", klog.KObj(pod))
				return false, errMsg
			}
			d := node.Devices.DeviceLists[targetIdx].Device

			snap := usageSnapshot{
				idx:       targetIdx,
				used:      d.Used,
				usedcores: d.Usedcores,
				usedmem:   d.Usedmem,
			}

			err := devPlugin.AddResourceUsage(pod, d, &tmpDevs[k.Type][idx])
			if err != nil {
				klog.Errorf("AddResourceUsage failed for device %s: %v, rolling back all changes", d.ID, err)
				for _, m := range modified {
					dev := node.Devices.DeviceLists[m.idx].Device
					dev.Used = m.used
					dev.Usedcores = m.usedcores
					dev.Usedmem = m.usedmem
				}
				rollbackAll()
				errMsg := fmt.Sprintf("AddResourceUsage failed for device %s: %v", d.ID, err)
				return false, errMsg
			}
			modified = append(modified, snap)
			klog.V(5).Infof("Allocated device %s: used=%d, cores=%d, mem=%d",
				d.ID, d.Used, d.Usedcores, d.Usedmem)
		}

		(*devinput)[k.Type] = append((*devinput)[k.Type], tmpDevs[k.Type])
	}

	return true, ""
}

// podInitContainerMaxRequest returns a map of max requirements per device type.
func podInitContainerMaxRequest(resourceReqs device.PodDeviceRequests, numInitContainers int) map[string]containerResourceSummary {
	maxReqs := make(map[string]containerResourceSummary)
	for i := range numInitContainers {
		if i >= len(resourceReqs) {
			break
		}
		for devType, req := range resourceReqs[i] {
			existing := maxReqs[devType]
			existing.nums = max(existing.nums, int(req.Nums))
			existing.memreq = max(existing.memreq, req.Memreq)
			existing.coresreq = max(existing.coresreq, req.Coresreq)
			maxReqs[devType] = existing
		}
	}
	return maxReqs
}

// podAppContainerTotalRequest returns the sum of device requests per device type.
func podAppContainerTotalRequest(resourceReqs device.PodDeviceRequests, numInitContainers int) map[string]containerResourceSummary {
	totals := make(map[string]containerResourceSummary)
	for i := numInitContainers; i < len(resourceReqs); i++ {
		for devType, req := range resourceReqs[i] {
			current := totals[devType]
			current.nums += int(req.Nums)
			current.memreq += req.Memreq
			current.coresreq += req.Coresreq
			totals[devType] = current
		}
	}
	return totals
}

// stripInitContainerAliasSlots removes the device allocations of init containers from the
// PodDevices structure, keeping only regular (non-init) container allocations.
func stripInitContainerAliasSlots(pod *corev1.Pod, resourceReqs device.PodDeviceRequests, devices device.PodDevices) device.PodDevices {
	numInitContainers := len(pod.Spec.InitContainers)
	if numInitContainers == 0 || len(devices) == 0 {
		return devices
	}

	expectedTotal := -1
	if resourceReqs != nil {
		expectedTotal = len(resourceReqs)
	}

	result := make(device.PodDevices, len(devices))
	for devType, containerList := range devices {
		if expectedTotal >= 0 && len(containerList) != expectedTotal {
			klog.ErrorS(nil, "device slot count does not match pod container count, skipping alias-slot strip for this device type to avoid misaligned data",
				"pod", klog.KObj(pod), "deviceType", devType, "slots", len(containerList), "expectedContainers", expectedTotal)
			result[devType] = containerList
			continue
		}
		if len(containerList) <= numInitContainers {
			result[devType] = device.PodSingleDevice{}
		} else {
			result[devType] = containerList[numInitContainers:]
		}
	}
	return result
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

	for nodeID, node := range *nodes {
		wg.Add(1)
		go func(nodeID string, node *NodeUsage) {
			defer wg.Done()

			viewStatus(*node)
			nodeInfo, err := s.GetNode(nodeID)
			if err != nil {
				klog.ErrorS(err, "Failed to get node", "nodeID", nodeID)
				failedNodesMutex.Lock()
				failedNodes[nodeID] = fmt.Sprintf("failed to fetch node info: %v", err)
				failedNodesMutex.Unlock()
				errCh <- err
				return
			}

			baseTypes := nodeDeviceBaseTypes(node.Devices)

			peakUsage := make([]struct {
				used      int32
				usedcores int32
				usedmem   int32
			}, len(node.Devices.DeviceLists))
			for i, dl := range node.Devices.DeviceLists {
				peakUsage[i].used = dl.Device.Used
				peakUsage[i].usedcores = dl.Device.Usedcores
				peakUsage[i].usedmem = dl.Device.Usedmem
			}

			//Check init containers (they run sequentially, each on a fresh copy)
			var initAllocs device.PodDevices
			if numInitContainers > 0 {
				initAllocs = make(device.PodDevices)

				initFit := true
				for i, req := range resourceReqs {
					if i >= numInitContainers {
						break
					}
					// Pad previous slots for all types to maintain index alignment
					for typ := range baseTypes {
						for len(initAllocs[typ]) < i {
							initAllocs[typ] = append(initAllocs[typ], device.ContainerDevices{})
						}
					}
					if len(req) == 0 {
						for typ := range baseTypes {
							initAllocs[typ] = append(initAllocs[typ], device.ContainerDevices{})
						}
						continue
					}
					nodeCopy := node.DeepCopy()
					fit, reason := fitInDevices(nodeCopy, req, task, nodeInfo, &initAllocs)
					if !fit {
						klog.V(4).InfoS("Init container does not fit",
							"pod", klog.KObj(task), "node", nodeID, "containerIndex", i, "reason", reason)
						failedNodesMutex.Lock()
						failedNodes[nodeID] = reason
						for reasonType := range common.ParseReason(reason) {
							failureReason[reasonType] = append(failureReason[reasonType], nodeID)
						}
						failedNodesMutex.Unlock()
						initFit = false
						break
					}
					// Record how much this init container used, at its peak, per device.
					for pi, dl := range nodeCopy.Devices.DeviceLists {
						if dl.Device.Used > peakUsage[pi].used {
							peakUsage[pi].used = dl.Device.Used
						}
						if dl.Device.Usedcores > peakUsage[pi].usedcores {
							peakUsage[pi].usedcores = dl.Device.Usedcores
						}
						if dl.Device.Usedmem > peakUsage[pi].usedmem {
							peakUsage[pi].usedmem = dl.Device.Usedmem
						}
					}
					// Ensure every type has an entry for this container (even if empty)
					for typ := range baseTypes {
						if len(initAllocs[typ]) == i {
							initAllocs[typ] = append(initAllocs[typ], device.ContainerDevices{})
						}
					}
				}
				if !initFit {
					return
				}
			}

			// Allocate app containers (they run concurrently, cumulative)
			appNodeCopy := node.DeepCopy()
			score := policy.NodeScore{
				NodeID:  nodeID,
				Node:    node.Node,
				Devices: make(device.PodDevices),
				Score:   0,
			}
			score.ComputeDefaultScore(appNodeCopy.Devices)
			snapshot := score.SnapshotDevice(appNodeCopy.Devices)

			appIndex := 0
			ctrfit := true

			for ctrid, n := range resourceReqs {
				if ctrid < numInitContainers {
					continue
				}
				for typ := range baseTypes {
					for len(score.Devices[typ]) < appIndex {
						score.Devices[typ] = append(score.Devices[typ], device.ContainerDevices{})
					}
				}
				sums := 0
				for _, k := range n {
					sums += int(k.Nums)
				}
				if sums == 0 {
					for typ := range baseTypes {
						score.Devices[typ] = append(score.Devices[typ], device.ContainerDevices{})
					}
					appIndex++
					continue
				}
				fit, reason := fitInDevices(appNodeCopy, n, task, nodeInfo, &score.Devices)
				ctrfit = fit
				if !fit {
					klog.V(4).InfoS(common.NodeUnfitPod, "pod", klog.KObj(task), "node", nodeID, "reason", reason)
					failedNodesMutex.Lock()
					failedNodes[nodeID] = reason
					for reasonType := range common.ParseReason(reason) {
						failureReason[reasonType] = append(failureReason[reasonType], nodeID)
					}
					failedNodesMutex.Unlock()
					break
				}
				// Ensure every type has an entry for this container
				for typ := range baseTypes {
					if len(score.Devices[typ]) == appIndex {
						score.Devices[typ] = append(score.Devices[typ], device.ContainerDevices{})
					}
				}
				appIndex++
			}

			if !ctrfit {
				return
			}

			//  Prepend init container allocations
			if numInitContainers > 0 && initAllocs != nil {
				for devType, initConList := range initAllocs {
					score.Devices[devType] = append(initConList, score.Devices[devType]...)
				}
			}

			// Commit the successful allocations to the original node.
			for i := range node.Devices.DeviceLists {
				finalUsed := appNodeCopy.Devices.DeviceLists[i].Device.Used
				finalCores := appNodeCopy.Devices.DeviceLists[i].Device.Usedcores
				finalMem := appNodeCopy.Devices.DeviceLists[i].Device.Usedmem
				if peakUsage[i].used > finalUsed {
					finalUsed = peakUsage[i].used
				}
				if peakUsage[i].usedcores > finalCores {
					finalCores = peakUsage[i].usedcores
				}
				if peakUsage[i].usedmem > finalMem {
					finalMem = peakUsage[i].usedmem
				}
				node.Devices.DeviceLists[i].Device.Used = finalUsed
				node.Devices.DeviceLists[i].Device.Usedcores = finalCores
				node.Devices.DeviceLists[i].Device.Usedmem = finalMem
			}

			score.OverrideScore(snapshot, userNodePolicy)
			fitNodesMutex.Lock()
			res.NodeList = append(res.NodeList, &score)
			fitNodesMutex.Unlock()

			klog.V(4).InfoS(common.NodeFitPod, "pod", klog.KObj(task), "node", nodeID, "score", score.Score)
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
