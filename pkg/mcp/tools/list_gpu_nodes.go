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
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"
	klog "k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/mcp/client"
	"github.com/Project-HAMi/HAMi/pkg/mcp/redact"
)

// ListGPUNodesTool implements the list_gpu_nodes MCP tool.
type ListGPUNodesTool struct {
	k8sClient *client.K8sClient
}

// NewListGPUNodesTool creates a new ListGPUNodesTool.
func NewListGPUNodesTool(k8sClient *client.K8sClient) *ListGPUNodesTool {
	return &ListGPUNodesTool{
		k8sClient: k8sClient,
	}
}

// Tool returns the MCP tool definition.
func (t *ListGPUNodesTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "list_gpu_nodes",
		Description: "List Kubernetes nodes with GPU resources. Returns node names, GPU vendor, GPU count, memory, and core information.",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"labelSelector": {
					Type:        "string",
					Description: "Optional label selector to filter nodes (e.g., 'gpu=on').",
				},
			},
		},
	}
}

// GPUNodeInfo represents GPU information for a node.
type GPUNodeInfo struct {
	Name              string  `json:"name"`
	GPUVendor         string  `json:"gpuVendor"`
	GPUCount          int     `json:"gpuCount"`
	AllocatedMemoryGB float64 `json:"allocatedMemoryGB"`
	TotalMemoryGB     float64 `json:"totalMemoryGB"`
	AllocatedCoresPct float64 `json:"allocatedCoresPct"`
}

// Handler returns the tool handler function.
func (t *ListGPUNodesTool) Handler() func(ctx context.Context, req *mcp.CallToolRequest, args struct {
	LabelSelector string `json:"labelSelector,omitempty"`
}) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args struct {
		LabelSelector string `json:"labelSelector,omitempty"`
	}) (*mcp.CallToolResult, any, error) {
		klog.V(2).InfoS("list_gpu_nodes called", "labelSelector", args.LabelSelector)

		// List GPU nodes
		nodes, err := t.k8sClient.ListGPUNodes(ctx, args.LabelSelector)
		if err != nil {
			return errorResult(fmt.Sprintf("failed to list GPU nodes: %v", err)), nil, nil
		}

		// Convert to response format
		var nodeInfos []GPUNodeInfo
		for _, node := range nodes {
			info := t.extractGPUNodeInfo(node)
			nodeInfos = append(nodeInfos, info)
		}

		// Marshal to JSON
		data, err := json.Marshal(nodeInfos)
		if err != nil {
			return errorResult(fmt.Sprintf("failed to marshal response: %v", err)), nil, nil
		}

		// Apply redaction
		redactedData := redact.Redact(string(data))

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: redactedData,
				},
			},
		}, nil, nil
	}
}

// extractGPUNodeInfo extracts GPU information from a node.
func (t *ListGPUNodesTool) extractGPUNodeInfo(node *corev1.Node) GPUNodeInfo {
	info := GPUNodeInfo{
		Name: node.Name,
	}

	// Determine GPU vendor and count from capacity
	gpuResourceNames := map[string]string{
		"nvidia.com/gpu":      "NVIDIA",
		"cambricon.com/vmlu":  "Cambricon",
		"hygon.com/dcunum":    "Hygon",
		"metax-tech.com/sgpu": "Metax",
		"enflame.com/drs-gcu": "Enflame",
		"kunlunxin.com/xpu":   "Kunlun",
		"vastaitech.com/va":   "Vastai",
	}

	for resourceName, vendor := range gpuResourceNames {
		if qty, ok := node.Status.Capacity[corev1.ResourceName(resourceName)]; ok {
			info.GPUVendor = vendor
			info.GPUCount = int(qty.Value())
			break
		}
	}

	// Extract memory information from annotations
	if memStr, ok := node.Annotations["nvidia.com/gpu.memory"]; ok {
		var totalMem float64
		if _, err := fmt.Sscanf(memStr, "%f", &totalMem); err == nil {
			info.TotalMemoryGB = totalMem / 1024 // Convert MB to GB
		}
	}

	// Extract core information from annotations
	if coreStr, ok := node.Annotations["nvidia.com/gpu.cores"]; ok {
		var totalCores float64
		if _, err := fmt.Sscanf(coreStr, "%f", &totalCores); err == nil {
			info.AllocatedCoresPct = totalCores
		}
	}

	return info
}

// errorResult creates an error MCP result.
func errorResult(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: message,
			},
		},
	}
}
