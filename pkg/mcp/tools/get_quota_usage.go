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

// GetQuotaUsageTool implements the get_quota_usage MCP tool.
type GetQuotaUsageTool struct {
	k8sClient *client.K8sClient
}

// NewGetQuotaUsageTool creates a new GetQuotaUsageTool.
func NewGetQuotaUsageTool(k8sClient *client.K8sClient) *GetQuotaUsageTool {
	return &GetQuotaUsageTool{
		k8sClient: k8sClient,
	}
}

// Tool returns the MCP tool definition.
func (t *GetQuotaUsageTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "get_quota_usage",
		Description: "Get GPU quota usage for a namespace. Returns memory and core usage compared to quota limits.",
		InputSchema: &jsonschema.Schema{
			Type:     "object",
			Required: []string{"namespace"},
			Properties: map[string]*jsonschema.Schema{
				"namespace": {
					Type:        "string",
					Description: "The namespace to check quota usage for.",
				},
			},
		},
	}
}

// QuotaUsage represents GPU quota usage for a namespace. Memory values are
// expressed in GiB (1 GiB = 1024 MiB). Cores are integer counts.
type QuotaUsage struct {
	Namespace         string  `json:"namespace"`
	GPUMemoryUsedGiB  float64 `json:"gpuMemoryUsedGiB"`
	GPUMemoryQuotaGiB float64 `json:"gpuMemoryQuotaGiB"`
	GPUCoreUsed       float64 `json:"gpuCoreUsed"`
	GPUCoreQuota      float64 `json:"gpuCoreQuota"`
}

// Handler returns the tool handler function.
func (t *GetQuotaUsageTool) Handler() func(ctx context.Context, req *mcp.CallToolRequest, args struct {
	Namespace string `json:"namespace"`
}) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args struct {
		Namespace string `json:"namespace"`
	}) (*mcp.CallToolResult, any, error) {
		klog.V(2).InfoS("get_quota_usage called", "namespace", args.Namespace)

		// Validate namespace
		if args.Namespace == "" {
			return errorResult("namespace is required"), nil, nil
		}

		// Check if namespace exists
		_, err := t.k8sClient.GetNamespace(ctx, args.Namespace)
		if err != nil {
			return errorResult(fmt.Sprintf("namespace %s not found: %v", args.Namespace, err)), nil, nil
		}

		// Get pods in namespace
		pods, err := t.k8sClient.ListGPUPods(ctx, args.Namespace, "")
		if err != nil {
			return errorResult(fmt.Sprintf("failed to list pods in namespace %s: %v", args.Namespace, err)), nil, nil
		}

		// Calculate usage
		usage := t.calculateQuotaUsage(args.Namespace, pods)

		// Marshal to JSON
		data, err := json.Marshal(usage)
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

// calculateQuotaUsage calculates GPU quota usage for a namespace.
func (t *GetQuotaUsageTool) calculateQuotaUsage(namespace string, pods []*corev1.Pod) QuotaUsage {
	usage := QuotaUsage{
		Namespace: namespace,
	}

	// Calculate usage from pods
	for _, pod := range pods {
		if pod.Status.Phase != corev1.PodRunning && pod.Status.Phase != corev1.PodPending {
			continue
		}

		// Sum GPU requests
		for _, container := range pod.Spec.Containers {
			if container.Resources.Requests != nil {
				// nvidia.com/gpumem is reported in MiB by the device plugin;
				// convert to GiB for the response (binary, not decimal).
				if memQty, ok := container.Resources.Requests[corev1.ResourceName("nvidia.com/gpumem")]; ok {
					usage.GPUMemoryUsedGiB += float64(memQty.Value()) / 1024
				}

				// Cores are reported as a percentage of a single GPU; sum directly.
				if coreQty, ok := container.Resources.Requests[corev1.ResourceName("nvidia.com/gpucores")]; ok {
					usage.GPUCoreUsed += float64(coreQty.Value())
				}
			}
		}
	}

	// TODO: Get quota from namespace annotations or ResourceQuota
	// For now, return usage without quota limits
	// In a real implementation, you would check for ResourceQuota objects
	// or namespace annotations like "hami.io/gpu-memory-quota"

	return usage
}
