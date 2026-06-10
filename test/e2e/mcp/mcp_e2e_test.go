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

// Package mcp_e2e exercises the full HAMi MCP server stack end-to-end via the
// in-memory MCP transport. It uses a fake Kubernetes clientset and a stubbed
// Prometheus HTTP server so the suite runs hermetically (no real cluster
// required), while still going through the real MCP protocol and JSON-RPC
// machinery the production binary uses.
package mcp_e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	hamimcp "github.com/Project-HAMi/HAMi/pkg/mcp"
	"github.com/Project-HAMi/HAMi/pkg/mcp/client"
	"github.com/Project-HAMi/HAMi/pkg/mcp/tools"
)

// e2eFixture builds a fully-wired MCP server connected to an MCP client via
// in-memory transports. It returns the client session and a cleanup func.
type e2eFixture struct {
	clientSession *mcpsdk.ClientSession
	cleanup       func()
}

func setupFixture(t *testing.T) *e2eFixture {
	t.Helper()

	gpuNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gpu-node-1",
			Labels: map[string]string{
				"gpu": "on",
			},
			Annotations: map[string]string{
				"nvidia.com/gpu.memory":            "16384",
				"nvidia.com/gpu.cores":             "100",
				"hami.io/node-devices-to-register": "GPU-aaa,0,16384,100,A100,0,true,1,exclusive:",
				// Sensitive — should be redacted out of describe_node output
				"my-api-token": "super-secret",
			},
		},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("4"),
				corev1.ResourceCPU:                    resource.MustParse("32"),
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("4"),
			},
		},
	}
	cpuNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "cpu-only"},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("8"),
			},
		},
	}

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ai-team"}}
	gpuPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "training-job",
			Namespace: "ai-team",
			Annotations: map[string]string{
				"hami.io/gpu-devices-to-use": "GPU-aaa",
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "gpu-node-1",
			Containers: []corev1.Container{{
				Name: "trainer",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceName("nvidia.com/gpu"):      resource.MustParse("1"),
						corev1.ResourceName("nvidia.com/gpumem"):   resource.MustParse("4096"),
						corev1.ResourceName("nvidia.com/gpucores"): resource.MustParse("80"),
					},
				},
			}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "hami-scheduler-config",
			Namespace: "hami-system",
		},
		Data: map[string]string{
			"scheduler-config.yaml": "policy: binpack",
			"api-token":             "should-be-redacted-by-mcp",
		},
	}

	cs := fake.NewClientset(gpuNode, cpuNode, ns, gpuPod, configMap)

	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"status":"success",
			"data":{"resultType":"vector","result":[
				{"metric":{"node":"gpu-node-1","gpu":"0"},"value":[1700000000,"42"]}
			]}
		}`))
	}))

	pc, err := client.NewPrometheusClient(prom.URL)
	if err != nil {
		t.Fatalf("prom client: %v", err)
	}
	k8s := client.NewK8sClientFromInterface(cs)

	srv, err := hamimcp.NewServerWithClients(&hamimcp.ServerConfig{
		PrometheusURL: prom.URL,
	}, k8s, pc)
	if err != nil {
		t.Fatalf("server init: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	clientTr, serverTr := mcpsdk.NewInMemoryTransports()

	if _, err := srv.Connect(ctx, serverTr); err != nil {
		cancel()
		prom.Close()
		t.Fatalf("server connect: %v", err)
	}

	mcpClient := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "hami-e2e-client", Version: "v0"}, nil)
	clientSession, err := mcpClient.Connect(ctx, clientTr, nil)
	if err != nil {
		cancel()
		prom.Close()
		t.Fatalf("client connect: %v", err)
	}

	return &e2eFixture{
		clientSession: clientSession,
		cleanup: func() {
			_ = clientSession.Close()
			cancel()
			prom.Close()
		},
	}
}

func extractToolText(t *testing.T, res *mcpsdk.CallToolResult) string {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatalf("empty tool result")
	}
	tc, ok := res.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("expected text content, got %T", res.Content[0])
	}
	return tc.Text
}

func callTool(t *testing.T, cs *mcpsdk.ClientSession, name string, args map[string]any) *mcpsdk.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	return res
}

func TestMCPServer_ListTools(t *testing.T) {
	f := setupFixture(t)
	defer f.cleanup()

	resp, err := f.clientSession.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(resp.Tools) != 5 {
		t.Fatalf("expected 5 tools, got %d", len(resp.Tools))
	}
}

func TestMCPServer_ListGPUNodes(t *testing.T) {
	f := setupFixture(t)
	defer f.cleanup()

	res := callTool(t, f.clientSession, "list_gpu_nodes", nil)
	if res.IsError {
		t.Fatalf("tool returned error: %s", extractToolText(t, res))
	}

	var nodes []tools.GPUNodeInfo
	if err := json.Unmarshal([]byte(extractToolText(t, res)), &nodes); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Name != "gpu-node-1" {
		t.Fatalf("expected 1 node 'gpu-node-1', got %+v", nodes)
	}
	if nodes[0].GPUVendor != "NVIDIA" || nodes[0].GPUCount != 4 {
		t.Errorf("unexpected GPU info: %+v", nodes[0])
	}
}

func TestMCPServer_ListGPUPods(t *testing.T) {
	f := setupFixture(t)
	defer f.cleanup()

	res := callTool(t, f.clientSession, "list_gpu_pods", map[string]any{"namespace": "ai-team"})
	if res.IsError {
		t.Fatalf("tool returned error: %s", extractToolText(t, res))
	}

	var pods []tools.GPUPodInfo
	if err := json.Unmarshal([]byte(extractToolText(t, res)), &pods); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(pods) != 1 {
		t.Fatalf("expected 1 pod, got %d", len(pods))
	}
	if pods[0].Name != "training-job" || pods[0].Node != "gpu-node-1" {
		t.Errorf("unexpected pod info: %+v", pods[0])
	}
	if pods[0].RequestedGPU != 1 {
		t.Errorf("expected RequestedGPU=1, got %d", pods[0].RequestedGPU)
	}
	if len(pods[0].AllocatedDeviceUUIDs) != 1 || pods[0].AllocatedDeviceUUIDs[0] != "GPU-aaa" {
		t.Errorf("expected allocated UUID GPU-aaa, got %v", pods[0].AllocatedDeviceUUIDs)
	}
}

func TestMCPServer_DescribeNode(t *testing.T) {
	f := setupFixture(t)
	defer f.cleanup()

	res := callTool(t, f.clientSession, "describe_node", map[string]any{"node": "gpu-node-1"})
	if res.IsError {
		t.Fatalf("tool returned error: %s", extractToolText(t, res))
	}

	body := extractToolText(t, res)
	var desc tools.NodeDescription
	if err := json.Unmarshal([]byte(body), &desc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if desc.Name != "gpu-node-1" {
		t.Errorf("expected node name gpu-node-1, got %s", desc.Name)
	}
	if len(desc.GPUDevices) != 1 || desc.GPUDevices[0].Type != "A100" {
		t.Errorf("expected one A100 device, got %+v", desc.GPUDevices)
	}

	// The node has annotation "my-api-token"; redact pass should mask its value.
	if !strings.Contains(body, "REDACTED") {
		t.Errorf("expected sensitive annotation to be redacted, body=%s", body)
	}
}

func TestMCPServer_DescribeNode_Missing(t *testing.T) {
	f := setupFixture(t)
	defer f.cleanup()

	res := callTool(t, f.clientSession, "describe_node", map[string]any{"node": "does-not-exist"})
	if !res.IsError {
		t.Fatalf("expected error result for missing node, got %s", extractToolText(t, res))
	}
}

func TestMCPServer_GetQuotaUsage(t *testing.T) {
	f := setupFixture(t)
	defer f.cleanup()

	res := callTool(t, f.clientSession, "get_quota_usage", map[string]any{"namespace": "ai-team"})
	if res.IsError {
		t.Fatalf("tool returned error: %s", extractToolText(t, res))
	}

	var usage tools.QuotaUsage
	if err := json.Unmarshal([]byte(extractToolText(t, res)), &usage); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if usage.Namespace != "ai-team" {
		t.Errorf("expected namespace ai-team, got %s", usage.Namespace)
	}
	if usage.GPUMemoryUsedGiB != 4 {
		t.Errorf("expected 4 GiB used (4096 MiB), got %v", usage.GPUMemoryUsedGiB)
	}
	if usage.GPUCoreUsed != 80 {
		t.Errorf("expected 80 cores, got %v", usage.GPUCoreUsed)
	}
}

func TestMCPServer_GetGPUMetrics(t *testing.T) {
	f := setupFixture(t)
	defer f.cleanup()

	res := callTool(t, f.clientSession, "get_gpu_metrics", map[string]any{
		"metric": "hami_gpu_device_count",
		"node":   "gpu-node-1",
	})
	if res.IsError {
		t.Fatalf("tool returned error: %s", extractToolText(t, res))
	}

	var metrics []tools.GPUMetric
	if err := json.Unmarshal([]byte(extractToolText(t, res)), &metrics); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric value, got %d", len(metrics))
	}
	if metrics[0].Value != 42 {
		t.Errorf("expected value 42, got %v", metrics[0].Value)
	}
}

func TestMCPServer_ReadConfigResource(t *testing.T) {
	f := setupFixture(t)
	defer f.cleanup()

	res, err := f.clientSession.ReadResource(context.Background(), &mcpsdk.ReadResourceParams{
		URI: "hami://config/scheduler",
	})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(res.Contents))
	}
	body := res.Contents[0].Text
	if !strings.Contains(body, "binpack") {
		t.Errorf("expected scheduler config to contain 'binpack', got %s", body)
	}
	if !strings.Contains(body, "REDACTED") {
		t.Errorf("expected api-token to be redacted, got %s", body)
	}
}

func TestMCPServer_GetGPUMetrics_RequiresMetric(t *testing.T) {
	f := setupFixture(t)
	defer f.cleanup()

	res := callTool(t, f.clientSession, "get_gpu_metrics", map[string]any{})
	if !res.IsError {
		t.Fatalf("expected error for missing metric, got %s", extractToolText(t, res))
	}
}
