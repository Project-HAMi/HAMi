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

package tools

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/mcp/client"
)

// GPUNodeInfo summarizes one GPU-bearing node for list_gpu_nodes.
type GPUNodeInfo struct {
	Name               string  `json:"name"`
	GPUVendor          string  `json:"gpuVendor"`
	GPUCount           int32   `json:"gpuCount"`
	AllocatedMemoryMiB float64 `json:"allocatedMemoryMiB"`
	TotalMemoryMiB     float64 `json:"totalMemoryMiB"`
	// AllocatedCoresPct is the average core allocation across the node's
	// GPUs, 0-100. It is the sum of each device's Usedcores percentage
	// divided by GPUCount, not a per-device value.
	AllocatedCoresPct float64 `json:"allocatedCoresPct,omitempty"`
}

type listGPUNodesInput struct {
	LabelSelector string `json:"labelSelector,omitempty" jsonschema:"optional Kubernetes label selector to filter nodes, e.g. gpu=on"`
}

// RegisterListGPUNodes registers the list_gpu_nodes tool.
func RegisterListGPUNodes(s *mcp.Server, k8sClient *client.K8sClient) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "list_gpu_nodes",
		Description: "List Kubernetes nodes that have GPU resources registered by any HAMi-supported " +
			"vendor (NVIDIA, Cambricon, Hygon, Ascend, and others). Read-only.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in listGPUNodesInput) (*mcp.CallToolResult, any, error) {
		nodes, err := k8sClient.ListGPUNodes(ctx, in.LabelSelector)
		if err != nil {
			return errorResult("list_gpu_nodes: %v", err), nil, nil
		}

		// Allocated memory/cores can't be read off node Capacity/Allocatable
		// for device-plugin resources — plugins advertise the same value in
		// both, so that subtraction is always zero. Derive it from live pod
		// allocations instead, aggregated per node.
		pods, err := k8sClient.ListGPUPods(ctx, "", "")
		if err != nil {
			return errorResult("list_gpu_nodes: listing pods: %v", err), nil, nil
		}
		allocByNode := aggregateAllocationsByNode(pods)

		infos := make([]GPUNodeInfo, 0, len(nodes))
		for _, n := range nodes {
			info := extractGPUNodeInfo(n)
			if alloc, ok := allocByNode[n.Name]; ok {
				info.AllocatedMemoryMiB = alloc.memMiB
				// alloc.coresPct is a sum of per-device core percentages
				// (e.g. two GPUs at 80% each sums to 160). Normalize by GPU
				// count so this field represents average node-level core
				// utilization (0-100), not a device-count-dependent total.
				if info.GPUCount > 0 {
					info.AllocatedCoresPct = alloc.coresPct / float64(info.GPUCount)
				}
			}
			infos = append(infos, info)
		}

		data, err := json.Marshal(infos)
		if err != nil {
			return errorResult("list_gpu_nodes: marshal result: %v", err), nil, nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
		}, nil, nil
	})
}

// nodeAllocation is the summed live GPU usage for one node.
type nodeAllocation struct {
	memMiB   float64
	coresPct float64
}

// aggregateAllocationsByNode sums each pod's allocated devices (from
// podAllocations, see list_gpu_pods.go) onto the node it's bound to.
// Terminal-phase pods are excluded, matching the accounting used by
// get_quota_usage.
func aggregateAllocationsByNode(pods []*corev1.Pod) map[string]nodeAllocation {
	out := make(map[string]nodeAllocation)
	for _, pod := range pods {
		if isTerminalPhase(pod.Status.Phase) {
			continue
		}
		// Only pod.Spec.NodeName is authoritative for "this pod is running on
		// this node" — it's set by the API server on bind. The
		// hami.io/vgpu-node annotation can be written by the extender before
		// the actual bind completes (or left stale from a prior attempt), so
		// it must not be trusted for usage totals.
		nodeName := pod.Spec.NodeName
		if nodeName == "" {
			continue
		}
		agg := out[nodeName]
		for _, ad := range podAllocations(pod) {
			agg.memMiB += float64(ad.UsedMemMiB)
			agg.coresPct += float64(ad.UsedCoresPct)
		}
		out[nodeName] = agg
	}
	return out
}

// extractGPUNodeInfo asks every registered device backend to decode this
// node's registration annotation. A backend that has not registered this
// node simply contributes nothing — that's the normal case for a
// multi-vendor cluster.
func extractGPUNodeInfo(node *corev1.Node) GPUNodeInfo {
	info := GPUNodeInfo{Name: node.Name}

	for _, dev := range device.GetDevices() {
		devInfos, err := dev.GetNodeDevices(*node)
		if err != nil || len(devInfos) == 0 {
			continue
		}
		info.GPUVendor = dev.CommonWord()
		for _, d := range devInfos {
			info.GPUCount += d.Count
			info.TotalMemoryMiB += float64(d.Devmem) * float64(d.Count)
		}
		// Only one vendor is expected to have registered this node; stop at
		// the first that did.
		break
	}
	return info
}
