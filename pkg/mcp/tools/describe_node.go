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
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"
	klog "k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/mcp/client"
	"github.com/Project-HAMi/HAMi/pkg/mcp/redact"
)

// DescribeNodeTool implements the describe_node MCP tool.
type DescribeNodeTool struct {
	k8sClient *client.K8sClient
}

// NewDescribeNodeTool creates a new DescribeNodeTool.
func NewDescribeNodeTool(k8sClient *client.K8sClient) *DescribeNodeTool {
	return &DescribeNodeTool{
		k8sClient: k8sClient,
	}
}

// Tool returns the MCP tool definition.
func (t *DescribeNodeTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "describe_node",
		Description: "Describe a Kubernetes node with GPU details. Returns HAMi annotations, GPU device list, and per-GPU usage.",
		InputSchema: &jsonschema.Schema{
			Type:     "object",
			Required: []string{"node"},
			Properties: map[string]*jsonschema.Schema{
				"node": {
					Type:        "string",
					Description: "The name of the node to describe.",
				},
			},
		},
	}
}

// NodeDescription represents detailed node information.
type NodeDescription struct {
	Name        string            `json:"name"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	GPUDevices  []GPUDeviceInfo   `json:"gpuDevices"`
	Capacity    map[string]string `json:"capacity"`
	Allocatable map[string]string `json:"allocatable"`
}

// GPUDeviceInfo represents a GPU device on a node.
type GPUDeviceInfo struct {
	ID     string `json:"id"`
	Index  int    `json:"index"`
	Type   string `json:"type"`
	Memory int    `json:"memoryMB"`
	Cores  int    `json:"cores"`
	Numa   int    `json:"numa"`
	Health bool   `json:"health"`
	Mode   string `json:"mode"`
}

// Handler returns the tool handler function.
func (t *DescribeNodeTool) Handler() func(ctx context.Context, req *mcp.CallToolRequest, args struct {
	Node string `json:"node"`
}) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args struct {
		Node string `json:"node"`
	}) (*mcp.CallToolResult, any, error) {
		klog.V(2).InfoS("describe_node called", "node", args.Node)

		// Validate node name
		if args.Node == "" {
			return errorResult("node name is required"), nil, nil
		}

		// Get node
		node, err := t.k8sClient.GetNode(ctx, args.Node)
		if err != nil {
			return errorResult(fmt.Sprintf("failed to get node %s: %v", args.Node, err)), nil, nil
		}

		// Extract node description
		description := t.extractNodeDescription(node)

		// Marshal to JSON
		data, err := json.Marshal(description)
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

// extractNodeDescription extracts detailed node information.
func (t *DescribeNodeTool) extractNodeDescription(node *corev1.Node) NodeDescription {
	description := NodeDescription{
		Name:        node.Name,
		Labels:      node.Labels,
		Annotations: node.Annotations,
		Capacity:    make(map[string]string),
		Allocatable: make(map[string]string),
	}

	// Extract capacity
	for name, qty := range node.Status.Capacity {
		description.Capacity[string(name)] = qty.String()
	}

	// Extract allocatable
	for name, qty := range node.Status.Allocatable {
		description.Allocatable[string(name)] = qty.String()
	}

	// Extract GPU devices from annotations
	description.GPUDevices = t.extractGPUDevices(node.Annotations)

	return description
}

// extractGPUDevices extracts GPU device information from node annotations.
func (t *DescribeNodeTool) extractGPUDevices(annotations map[string]string) []GPUDeviceInfo {
	// HAMi writes its register-set annotation in this exact key. We don't
	// fall back to nvidia.com/gpu-devices: that annotation is set by the
	// NVIDIA device plugin with a different schema and would silently parse
	// to garbage with parseDeviceString.
	if deviceStr, ok := annotations["hami.io/node-devices-to-register"]; ok && deviceStr != "" {
		return parseDeviceString(deviceStr)
	}
	return nil
}

// parseDeviceString parses a device annotation string.
// Format per device (','-separated): "UUID,Index,Memory,Cores,Type,Numa,Health,Count,Mode"
// Devices are joined by ":". A trailing ":" is allowed.
func parseDeviceString(deviceStr string) []GPUDeviceInfo {
	var devices []GPUDeviceInfo

	for _, part := range strings.Split(deviceStr, ":") {
		if part == "" {
			continue
		}

		fields := strings.Split(part, ",")
		if len(fields) < 7 {
			continue
		}

		device := GPUDeviceInfo{
			ID:   fields[0],
			Type: fields[4],
		}

		// Parse numeric fields. Surface parse errors via klog so an operator
		// can spot a malformed annotation; we still emit the device with
		// whatever fields parsed cleanly rather than dropping it silently.
		if v, err := strconv.Atoi(fields[1]); err == nil {
			device.Index = v
		} else {
			klog.V(4).InfoS("invalid device Index", "raw", fields[1], "err", err)
		}
		if v, err := strconv.Atoi(fields[2]); err == nil {
			device.Memory = v
		} else {
			klog.V(4).InfoS("invalid device Memory", "raw", fields[2], "err", err)
		}
		if v, err := strconv.Atoi(fields[3]); err == nil {
			device.Cores = v
		} else {
			klog.V(4).InfoS("invalid device Cores", "raw", fields[3], "err", err)
		}
		if v, err := strconv.Atoi(fields[5]); err == nil {
			device.Numa = v
		} else {
			klog.V(4).InfoS("invalid device Numa", "raw", fields[5], "err", err)
		}

		device.Health = fields[6] == "true"

		// Mode is the 9th field (index 8). The 8th (index 7) is Count.
		if len(fields) > 8 {
			device.Mode = fields[8]
		}

		devices = append(devices, device)
	}

	return devices
}
