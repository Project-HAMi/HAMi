/*
Copyright 2026 The HAMi Authors.

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
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

// AllowedSetUnmatched reports that a restricted fit found no device of the
// requested type in the allowed set, distinguishing an ID mismatch (for
// example replica IDs sent instead of physical UUIDs) from real capacity
// exhaustion.
const AllowedSetUnmatched = "AllowedDeviceSetUnmatched"

// restrictNodeUsage returns a copy of node whose devices of deviceType are
// limited to the physical device IDs in allowedUUIDs. Devices of other types
// are kept, so requests spanning several vendors keep fitting. Policy,
// NumaBind, and per-device usage are preserved, so the policy-chain sort and
// Fit observe the restricted set exactly as they would a smaller node. Node
// and NodeInfo are shared because the fit never mutates them; each surviving
// device is deep-copied so usage accounting cannot touch the original.
// Unknown IDs are ignored. node must be non-nil, as in fitInDevices.
func restrictNodeUsage(node *NodeUsage, deviceType string, allowedUUIDs []string) *NodeUsage {
	allowed := make(map[string]struct{}, len(allowedUUIDs))
	for _, id := range allowedUUIDs {
		allowed[id] = struct{}{}
	}

	restricted := &NodeUsage{
		Node:     node.Node,
		NodeInfo: node.NodeInfo,
		Devices: policy.DeviceUsageList{
			Policy:   node.Devices.Policy,
			NumaBind: node.Devices.NumaBind,
		},
	}
	for _, deviceList := range node.Devices.DeviceLists {
		if deviceList == nil || deviceList.Device == nil {
			continue
		}
		if strings.Contains(strings.ToLower(deviceList.Device.Type), strings.ToLower(deviceType)) {
			if _, ok := allowed[deviceList.Device.ID]; !ok {
				continue
			}
		}
		restricted.Devices.DeviceLists = append(restricted.Devices.DeviceLists,
			&policy.DeviceListsScore{Device: deviceList.Device.DeepCopy(), Score: deviceList.Score})
	}
	return restricted
}

// fitInRestrictedDevices runs the standard policy-chain fit for the
// deviceType requests with candidates limited to the allowed physical
// devices, preserving binpack, spread, mutex, and numa ordering. It backs
// the NUMA alignment refit (issue #2080): when kubelet's Topology Manager
// restricts an allocation to replicas of devices the scheduler did not pick,
// the refit re-runs the same fit over kubelet's set instead of adopting
// kubelet's choice unchecked. Requests of other device types are ignored so
// a single-type refit cannot silently reselect unrelated vendors' devices.
// It fails with AllowedSetUnmatched when no device of deviceType on the node
// is in the allowed set.
//
// Contract for the production caller (the refit endpoint):
//   - node must come fresh from buildNodeUsage/getNodesUsage built with this
//     pod, so Policy and NumaBind reflect the pod's own scheduling
//     annotations; the sort reads them from node.Devices while Fit reads the
//     pod annotations directly, and they must agree.
//   - nodeInfo must be the node's populated NodeInfo when the pod selects
//     the topology-aware policy, which reads GPU pair scores from it.
//   - This helper only computes a placement on a restricted copy; the
//     caller must hold the scheduler's allocation lock across snapshot, fit,
//     annotation patch, and reservation move, and release the pod's own
//     usage from the snapshot before fitting.
//
// The selected devices are appended to devinput.
func fitInRestrictedDevices(node *NodeUsage, requests device.ContainerDeviceRequests, deviceType string, allowedUUIDs []string, pod *corev1.Pod, nodeInfo *device.NodeInfo, devinput *device.PodDevices, weights util.DeviceScoringWeights) (bool, string) {
	typeRequests := device.ContainerDeviceRequests{}
	for key, request := range requests {
		if request.Type == deviceType {
			typeRequests[key] = request
		}
	}
	if len(typeRequests) == 0 {
		return false, "no " + deviceType + " request to refit"
	}

	restricted := restrictNodeUsage(node, deviceType, allowedUUIDs)
	if len(getNodeResources(*restricted, deviceType)) == 0 {
		return false, AllowedSetUnmatched
	}
	return fitInDevices(restricted, typeRequests, pod, nodeInfo, devinput, weights)
}
