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
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"
	klog "k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/mcp/client"
	"github.com/Project-HAMi/HAMi/pkg/mcp/redact"
)

// ListGPUPodsTool implements the list_gpu_pods MCP tool.
type ListGPUPodsTool struct {
	k8sClient *client.K8sClient
}

// NewListGPUPodsTool creates a new ListGPUPodsTool.
func NewListGPUPodsTool(k8sClient *client.K8sClient) *ListGPUPodsTool {
	return &ListGPUPodsTool{
		k8sClient: k8sClient,
	}
}

// Tool returns the MCP tool definition.
func (t *ListGPUPodsTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "list_gpu_pods",
		Description: "List pods that have GPU resource requests. Returns pod namespace, name, node, requested GPU, allocated device UUIDs, and status.",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"namespace": {
					Type:        "string",
					Description: "Optional namespace to filter pods. If empty, lists pods across all namespaces.",
				},
				"phase": {
					Type:        "string",
					Description: "Optional phase filter (Running, Pending, Succeeded, Failed, Unknown).",
					Enum:        []any{"Running", "Pending", "Succeeded", "Failed", "Unknown"},
				},
			},
		},
	}
}

// GPUPodInfo represents GPU information for a pod.
type GPUPodInfo struct {
	Namespace            string   `json:"namespace"`
	Name                 string   `json:"name"`
	Node                 string   `json:"node"`
	RequestedGPU         int      `json:"requestedGPU"`
	AllocatedDeviceUUIDs []string `json:"allocatedDeviceUUIDs"`
	Status               string   `json:"status"`
}

// Handler returns the tool handler function.
func (t *ListGPUPodsTool) Handler() func(ctx context.Context, req *mcp.CallToolRequest, args struct {
	Namespace string `json:"namespace,omitempty"`
	Phase     string `json:"phase,omitempty"`
}) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args struct {
		Namespace string `json:"namespace,omitempty"`
		Phase     string `json:"phase,omitempty"`
	}) (*mcp.CallToolResult, any, error) {
		klog.V(2).InfoS("list_gpu_pods called", "namespace", args.Namespace, "phase", args.Phase)

		// List GPU pods
		pods, err := t.k8sClient.ListGPUPods(ctx, args.Namespace, args.Phase)
		if err != nil {
			return errorResult(fmt.Sprintf("failed to list GPU pods: %v", err)), nil, nil
		}

		// Convert to response format
		var podInfos []GPUPodInfo
		for _, pod := range pods {
			info := t.extractGPUPodInfo(pod)
			podInfos = append(podInfos, info)
		}

		// Marshal to JSON
		data, err := json.Marshal(podInfos)
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

// extractGPUPodInfo extracts GPU information from a pod.
func (t *ListGPUPodsTool) extractGPUPodInfo(pod *corev1.Pod) GPUPodInfo {
	info := GPUPodInfo{
		Namespace: pod.Namespace,
		Name:      pod.Name,
		Node:      pod.Spec.NodeName,
		Status:    string(pod.Status.Phase),
	}

	// Extract GPU requests from containers
	gpuResourceNames := []string{
		"nvidia.com/gpu",
		"cambricon.com/vmlu",
		"hygon.com/dcunum",
		"metax-tech.com/sgpu",
		"enflame.com/drs-gcu",
		"kunlunxin.com/xpu",
		"vastaitech.com/va",
	}

	for _, container := range pod.Spec.Containers {
		for _, resourceName := range gpuResourceNames {
			if container.Resources.Requests != nil {
				if qty, ok := container.Resources.Requests[corev1.ResourceName(resourceName)]; ok {
					info.RequestedGPU += int(qty.Value())
				}
			}
		}
	}

	// Extract allocated device UUIDs from annotations
	if uuids, ok := pod.Annotations["nvidia.com/gpu-devices-to-use"]; ok && uuids != "" {
		info.AllocatedDeviceUUIDs = splitAndClean(uuids, ",")
	} else if uuids, ok := pod.Annotations["hami.io/gpu-devices-to-use"]; ok && uuids != "" {
		info.AllocatedDeviceUUIDs = splitAndClean(uuids, ",")
	}

	return info
}

// splitAndClean splits s by sep, trims whitespace, and drops empty entries.
func splitAndClean(s, sep string) []string {
	if s == "" {
		return nil
	}
	var result []string
	for part := range strings.SplitSeq(s, sep) {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
