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

func nodeDeviceBaseTypes(list policy.DeviceUsageList) map[string]struct{} {
	types := make(map[string]struct{})
	for _, dl := range list.DeviceLists {
		if dl.Device != nil {
			types[dl.Device.Type] = struct{}{}
		}
	}
	return types
}

func fitInDevices(node *NodeUsage, requests device.ContainerDeviceRequests, pod *corev1.Pod, nodeInfo *device.NodeInfo, devinput *device.PodDevices) (bool, string) {
	// Compute scores for all devices based on the request.
	for index := range node.Devices.DeviceLists {
		node.Devices.DeviceLists[index].ComputeScore(requests)
	}

	// Process each device type in the request.
	for _, k := range requests {
		sort.Sort(node.Devices)

		devPlugin, ok := device.GetDevices()[k.Type]
		if !ok {
			errMsg := "Device type not found"
			klog.ErrorS(nil, errMsg, "pod", klog.KObj(pod), "type", k.Type, "node", node.Node.Name)
			return false, errMsg
		}

		typeDevices := getNodeResources(*node, k.Type)
		if int(k.Nums) > len(typeDevices) {
			klog.V(5).InfoS(common.NodeInsufficientDevice, "pod", klog.KObj(pod),
				"request devices nums", k.Nums, "node device nums (type)", len(typeDevices), "type", k.Type)
			return false, common.NodeInsufficientDevice
		}

		fit, tmpDevs, reason := devPlugin.Fit(typeDevices, k, pod, nodeInfo, devinput)
		if !fit {
			return false, reason
		}

		// Note: need idx to pass &tmpDevs[k.Type][idx] to AddResourceUsage
		for idx, val := range tmpDevs[k.Type] {
			targetIdx := -1
			for nidx, v := range node.Devices.DeviceLists {
				if v.Device.ID == val.UUID {
					targetIdx = nidx
					break
				}
			}
			if targetIdx == -1 {
				errMsg := fmt.Sprintf("Device with UUID %q not found on node %s after Fit", val.UUID, node.Node.Name)
				klog.ErrorS(nil, errMsg, "pod", klog.KObj(pod))
				return false, errMsg
			}
			d := node.Devices.DeviceLists[targetIdx].Device

			if err := devPlugin.AddResourceUsage(pod, d, &tmpDevs[k.Type][idx]); err != nil {
				klog.Errorf("AddResourceUsage failed for device %s: %v", d.ID, err)
				return false, fmt.Sprintf("AddResourceUsage failed for device %s: %v", d.ID, err)
			}
			klog.V(5).Infof("Allocated device %s: used=%d, cores=%d, mem=%d",
				d.ID, d.Used, d.Usedcores, d.Usedmem)
		}

		(*devinput)[k.Type] = append((*devinput)[k.Type], tmpDevs[k.Type])
	}
	return true, ""
}

func resolveNodeSchedulerPolicy(task *corev1.Pod) string {
	if task.GetAnnotations() != nil {
		if value, ok := task.GetAnnotations()[util.NodeSchedulerPolicyAnnotationKey]; ok {
			return value
		}
	}
	return config.NodeSchedulerPolicy
}

type peakUsageSnapshot struct {
	used      int32
	usedcores int32
	usedmem   int32
}

func snapshotPeakUsage(devices policy.DeviceUsageList) map[string]peakUsageSnapshot {
	peakUsage := make(map[string]peakUsageSnapshot)
	for _, dl := range devices.DeviceLists {
		peakUsage[dl.Device.ID] = peakUsageSnapshot{
			used:      dl.Device.Used,
			usedcores: dl.Device.Usedcores,
			usedmem:   dl.Device.Usedmem,
		}
	}
	return peakUsage
}

func updatePeakUsage(peakUsage map[string]peakUsageSnapshot, nodeCopy *NodeUsage) {
	for _, dl := range nodeCopy.Devices.DeviceLists {
		id := dl.Device.ID
		p, ok := peakUsage[id]
		if !ok {
			peakUsage[id] = peakUsageSnapshot{
				used:      dl.Device.Used,
				usedcores: dl.Device.Usedcores,
				usedmem:   dl.Device.Usedmem,
			}
			continue
		}
		if dl.Device.Used > p.used {
			p.used = dl.Device.Used
		}
		if dl.Device.Usedcores > p.usedcores {
			p.usedcores = dl.Device.Usedcores
		}
		if dl.Device.Usedmem > p.usedmem {
			p.usedmem = dl.Device.Usedmem
		}
		peakUsage[id] = p
	}
}

func applyPeakUsage(node *NodeUsage, appNodeCopy *NodeUsage, peakUsage map[string]peakUsageSnapshot) {
	for _, dl := range node.Devices.DeviceLists {
		id := dl.Device.ID
		var appDev *device.DeviceUsage
		for _, adl := range appNodeCopy.Devices.DeviceLists {
			if adl.Device.ID == id {
				appDev = adl.Device
				break
			}
		}
		finalUsed := int32(0)
		finalCores := int32(0)
		finalMem := int32(0)
		if appDev != nil {
			finalUsed = appDev.Used
			finalCores = appDev.Usedcores
			finalMem = appDev.Usedmem
		}
		if p, ok := peakUsage[id]; ok {
			if p.used > finalUsed {
				finalUsed = p.used
			}
			if p.usedcores > finalCores {
				finalCores = p.usedcores
			}
			if p.usedmem > finalMem {
				finalMem = p.usedmem
			}
		}
		dl.Device.Used = finalUsed
		dl.Device.Usedcores = finalCores
		dl.Device.Usedmem = finalMem
	}
}

func allocateInitContainers(node *NodeUsage, nodeID string, resourceReqs device.PodDeviceRequests, task *corev1.Pod, nodeInfo *device.NodeInfo, baseTypes map[string]struct{}, numInitContainers int, peakUsage map[string]peakUsageSnapshot) (device.PodDevices, bool, string) {
	initAllocs := make(device.PodDevices)

	for i, req := range resourceReqs {
		if i >= numInitContainers {
			break
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
			return nil, false, reason
		}
		updatePeakUsage(peakUsage, nodeCopy)
		for typ := range baseTypes {
			if len(initAllocs[typ]) == i {
				initAllocs[typ] = append(initAllocs[typ], device.ContainerDevices{})
			}
		}
	}
	return initAllocs, true, ""
}

func allocateAppContainers(score *policy.NodeScore, appNodeCopy *NodeUsage, resourceReqs device.PodDeviceRequests, task *corev1.Pod, nodeInfo *device.NodeInfo, baseTypes map[string]struct{}, numInitContainers int, nodeID string) (string, bool) {
	appIndex := 0
	for ctrid, n := range resourceReqs {
		if ctrid < numInitContainers {
			continue
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
		if !fit {
			klog.V(4).InfoS(common.NodeUnfitPod, "pod", klog.KObj(task), "node", nodeID, "reason", reason)
			return reason, false
		}
		for typ := range baseTypes {
			if len(score.Devices[typ]) == appIndex {
				score.Devices[typ] = append(score.Devices[typ], device.ContainerDevices{})
			}
		}
		appIndex++
	}
	return "", true
}

type nodeScoreResult struct {
	score  *policy.NodeScore
	reason string
	err    error
}

func (s *Scheduler) scoreNode(nodeID string, node *NodeUsage, resourceReqs device.PodDeviceRequests, task *corev1.Pod, userNodePolicy string) nodeScoreResult {
	viewStatus(*node)

	nodeInfo := node.NodeInfo
	if nodeInfo == nil {
		var err error
		nodeInfo, err = s.GetNode(nodeID)
		if err != nil {
			klog.ErrorS(err, "Failed to get node", "nodeID", nodeID)
			return nodeScoreResult{reason: fmt.Sprintf("failed to fetch node info: %v", err), err: err}
		}
	}

	baseTypes := nodeDeviceBaseTypes(node.Devices)
	peakUsage := snapshotPeakUsage(node.Devices)
	numInitContainers := len(task.Spec.InitContainers)

	var initAllocs device.PodDevices
	if numInitContainers > 0 {
		allocs, fit, reason := allocateInitContainers(node, nodeID, resourceReqs, task, nodeInfo, baseTypes, numInitContainers, peakUsage)
		if !fit {
			return nodeScoreResult{reason: reason}
		}
		initAllocs = allocs
	}

	appNodeCopy := node.DeepCopy()
	score := policy.NodeScore{
		NodeID:  nodeID,
		Node:    node.Node,
		Devices: make(device.PodDevices),
		Score:   0,
	}
	score.ComputeDefaultScore(appNodeCopy.Devices)
	snapshot := score.SnapshotDevice(appNodeCopy.Devices)

	if reason, fit := allocateAppContainers(&score, appNodeCopy, resourceReqs, task, nodeInfo, baseTypes, numInitContainers, nodeID); !fit {
		return nodeScoreResult{reason: reason}
	}

	if numInitContainers > 0 && initAllocs != nil {
		for devType, initConList := range initAllocs {
			score.Devices[devType] = append(initConList, score.Devices[devType]...)
		}
	}

	applyPeakUsage(node, appNodeCopy, peakUsage)

	score.OverrideScore(snapshot, userNodePolicy)
	klog.V(4).InfoS(common.NodeFitPod, "pod", klog.KObj(task), "node", nodeID, "score", score.Score)

	return nodeScoreResult{score: &score}
}

func (s *Scheduler) recordFilteringFailures(task *corev1.Pod, failureReason map[string][]string) {
	for reasonType, failureNodes := range failureReason {
		sort.Strings(failureNodes)
		reason := fmt.Errorf("%d nodes %s(%s)", len(failureNodes), reasonType, strings.Join(failureNodes, ","))
		s.recordScheduleFilterResultEvent(task, EventReasonFilteringFailed, "", reason)
	}
}

func (s *Scheduler) calcScore(nodes *map[string]*NodeUsage, resourceReqs device.PodDeviceRequests, task *corev1.Pod, failedNodes map[string]string) (*policy.NodeScoreList, error) {
	return s.calcScoreWithOptions(nodes, resourceReqs, task, failedNodes, true, false)
}

func (s *Scheduler) calcScoreWithOptions(nodes *map[string]*NodeUsage, resourceReqs device.PodDeviceRequests, task *corev1.Pod, failedNodes map[string]string, recordEvents bool, detailedFailureReason bool) (*policy.NodeScoreList, error) {
	userNodePolicy := resolveNodeSchedulerPolicy(task)
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

			result := s.scoreNode(nodeID, node, resourceReqs, task, userNodePolicy)

			switch {
			case result.err != nil:
				failedNodesMutex.Lock()
				failedNodes[nodeID] = result.reason
				failedNodesMutex.Unlock()
				errCh <- result.err

			case result.score == nil:
				failedNodesMutex.Lock()
				failedNodes[nodeID] = result.reason
				for reasonType := range common.ParseReason(result.reason) {
					failureReason[reasonType] = append(failureReason[reasonType], nodeID)
				}
				failedNodesMutex.Unlock()

			default:
				fitNodesMutex.Lock()
				res.NodeList = append(res.NodeList, result.score)
				fitNodesMutex.Unlock()
			}
		}(nodeID, node)
	}
	wg.Wait()
	close(errCh)

	if recordEvents && len(res.NodeList) == 0 {
		s.recordFilteringFailures(task, failureReason)
	}

	var errorsSlice []error
	for e := range errCh {
		errorsSlice = append(errorsSlice, e)
	}
	return &res, utilerrors.NewAggregate(errorsSlice)
}
