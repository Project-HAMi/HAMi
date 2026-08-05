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
	klog "k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/mcp/client"
	"github.com/Project-HAMi/HAMi/pkg/mcp/redact"
)

// GPUDeviceInfo describes one physical/virtual GPU device found on a node.
type GPUDeviceInfo struct {
	ID       string `json:"id"`
	Index    uint   `json:"index"`
	Vendor   string `json:"vendor"`
	Type     string `json:"type"`
	Count    int32  `json:"count"`
	DevmemMB int32  `json:"devmemMB"`
	Devcore  int32  `json:"devcorePct"`
	Numa     int    `json:"numa"`
	Mode     string `json:"mode"` // hami-core | mig | mps
	Healthy  bool   `json:"healthy"`
}

// NodeDescription is the describe_node tool's output.
type NodeDescription struct {
	Name        string            `json:"name"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"` // redacted
	GPUDevices  []GPUDeviceInfo   `json:"gpuDevices,omitempty"`
	Capacity    map[string]string `json:"capacity,omitempty"`
	Allocatable map[string]string `json:"allocatable,omitempty"`
}

type describeNodeInput struct {
	Node string `json:"node" jsonschema:"name of the node to describe"`
}

// RegisterDescribeNode registers the describe_node tool.
func RegisterDescribeNode(s *mcp.Server, k8sClient *client.K8sClient) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "describe_node",
		Description: "Describe a Kubernetes node's GPU devices (vendor, type, memory, sharing mode) " +
			"plus its labels, redacted annotations, and resource capacity. Read-only.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in describeNodeInput) (*mcp.CallToolResult, any, error) {
		if in.Node == "" {
			return errorResult("describe_node: node is required"), nil, nil
		}

		node, err := k8sClient.GetNode(ctx, in.Node)
		if err != nil {
			return errorResult("describe_node: %v", err), nil, nil
		}

		desc := NodeDescription{
			Name:        node.Name,
			Labels:      node.Labels,
			Annotations: redact.RedactAnnotations(node.Annotations),
			GPUDevices:  nodeGPUDevices(node),
			Capacity:    resourceListToStrings(node.Status.Capacity),
			Allocatable: resourceListToStrings(node.Status.Allocatable),
		}

		data, err := json.Marshal(desc)
		if err != nil {
			return errorResult("describe_node: marshal result: %v", err), nil, nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
		}, nil, nil
	})
}

// nodeGPUDevices asks every registered device backend to decode its own
// node-registration annotation (e.g. hami.io/node-nvidia-register for
// NVIDIA, hami.io/node-register-Ascend910B for Ascend). A backend returns an
// error when the node has no devices of that vendor, which is the normal
// case for every vendor but the one actually installed.
func nodeGPUDevices(node *corev1.Node) []GPUDeviceInfo {
	var out []GPUDeviceInfo
	for _, dev := range device.GetDevices() {
		infos, err := dev.GetNodeDevices(*node)
		if err != nil {
			klog.V(5).InfoS("backend reported no devices for node",
				"node", node.Name, "vendor", dev.CommonWord(), "reason", err)
			continue
		}
		for _, i := range infos {
			out = append(out, GPUDeviceInfo{
				ID:       i.ID,
				Index:    i.Index,
				Vendor:   i.DeviceVendor,
				Type:     i.Type,
				Count:    i.Count,
				DevmemMB: i.Devmem,
				Devcore:  i.Devcore,
				Numa:     i.Numa,
				Mode:     i.Mode,
				Healthy:  i.Health,
			})
		}
	}
	return out
}

func resourceListToStrings(rl corev1.ResourceList) map[string]string {
	if len(rl) == 0 {
		return nil
	}
	out := make(map[string]string, len(rl))
	for name, qty := range rl {
		out[string(name)] = qty.String()
	}
	return out
}
