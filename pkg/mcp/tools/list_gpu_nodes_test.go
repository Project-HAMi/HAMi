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
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Project-HAMi/HAMi/pkg/mcp/client"
)

// extractText extracts the text content from an MCP CallToolResult.
func extractText(r *mcp.CallToolResult) string {
	if r == nil || len(r.Content) == 0 {
		return ""
	}
	if tc, ok := r.Content[0].(*mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

func TestListGPUNodesTool_Handler(t *testing.T) {
	gpuNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gpu-node-1",
			Annotations: map[string]string{
				"nvidia.com/gpu.memory": "16384",
				"nvidia.com/gpu.cores":  "100",
			},
		},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("4"),
			},
		},
	}
	cpuNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "cpu-node"},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("8"),
			},
		},
	}

	cs := fake.NewClientset(gpuNode, cpuNode)
	k8s := client.NewK8sClientFromInterface(cs)
	tool := NewListGPUNodesTool(k8s)

	if tool.Tool().Name != "list_gpu_nodes" {
		t.Errorf("unexpected tool name: %s", tool.Tool().Name)
	}

	res, _, err := tool.Handler()(context.Background(), nil, struct {
		LabelSelector string `json:"labelSelector,omitempty"`
	}{})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", extractText(res))
	}

	var nodes []GPUNodeInfo
	if err := json.Unmarshal([]byte(extractText(res)), &nodes); err != nil {
		t.Fatalf("failed to unmarshal response: %v\nbody=%s", err, extractText(res))
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 GPU node, got %d", len(nodes))
	}
	if nodes[0].Name != "gpu-node-1" {
		t.Errorf("expected gpu-node-1, got %s", nodes[0].Name)
	}
	if nodes[0].GPUVendor != "NVIDIA" {
		t.Errorf("expected vendor NVIDIA, got %s", nodes[0].GPUVendor)
	}
	if nodes[0].GPUCount != 4 {
		t.Errorf("expected 4 GPUs, got %d", nodes[0].GPUCount)
	}
	if nodes[0].TotalMemoryGB != 16.0 {
		t.Errorf("expected 16 GB total memory, got %v", nodes[0].TotalMemoryGB)
	}
	if nodes[0].AllocatedCoresPct != 100 {
		t.Errorf("expected 100 cores, got %v", nodes[0].AllocatedCoresPct)
	}
}

func TestListGPUNodesTool_VendorDetection(t *testing.T) {
	cases := []struct {
		resource string
		vendor   string
	}{
		{"nvidia.com/gpu", "NVIDIA"},
		{"cambricon.com/vmlu", "Cambricon"},
		{"hygon.com/dcunum", "Hygon"},
		{"metax-tech.com/sgpu", "Metax"},
		{"enflame.com/drs-gcu", "Enflame"},
		{"kunlunxin.com/xpu", "Kunlun"},
		{"vastaitech.com/va", "Vastai"},
	}
	for _, tc := range cases {
		t.Run(tc.vendor, func(t *testing.T) {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "n"},
				Status: corev1.NodeStatus{
					Capacity: corev1.ResourceList{
						corev1.ResourceName(tc.resource): resource.MustParse("2"),
					},
				},
			}
			tool := NewListGPUNodesTool(client.NewK8sClientFromInterface(fake.NewClientset()))
			info := tool.extractGPUNodeInfo(node)
			if info.GPUVendor != tc.vendor {
				t.Errorf("vendor for %s = %s, want %s", tc.resource, info.GPUVendor, tc.vendor)
			}
			if info.GPUCount != 2 {
				t.Errorf("count for %s = %d, want 2", tc.resource, info.GPUCount)
			}
		})
	}
}

func TestErrorResult(t *testing.T) {
	r := errorResult("oops")
	if !r.IsError {
		t.Fatalf("expected IsError=true")
	}
	if extractText(r) != "oops" {
		t.Errorf("expected text 'oops', got %q", extractText(r))
	}
}
