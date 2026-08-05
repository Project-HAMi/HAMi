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
	AllocatedCoresPct  float64 `json:"allocatedCoresPct,omitempty"`
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

		infos := make([]GPUNodeInfo, 0, len(nodes))
		for _, n := range nodes {
			infos = append(infos, extractGPUNodeInfo(n))
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

	// Allocated memory/cores come from the resource-name pair the matching
	// backend was configured with, read off node capacity vs. allocatable.
	for _, dev := range device.GetDevices() {
		if dev.CommonWord() != info.GPUVendor {
			continue
		}
		rn := dev.GetResourceNames()
		if rn.ResourceMemoryName != "" {
			cap := node.Status.Capacity[corev1.ResourceName(rn.ResourceMemoryName)]
			alloc := node.Status.Allocatable[corev1.ResourceName(rn.ResourceMemoryName)]
			info.AllocatedMemoryMiB = float64(cap.Value() - alloc.Value())
		}
		break
	}

	return info
}
