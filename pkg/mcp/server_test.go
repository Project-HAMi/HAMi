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

package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Project-HAMi/HAMi/pkg/mcp/client"
)

func newPromStub(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
}

func TestNewServerWithClients_NilArgs(t *testing.T) {
	prom := newPromStub(t)
	defer prom.Close()
	pc, _ := client.NewPrometheusClient(prom.URL)
	k8s := client.NewK8sClientFromInterface(fake.NewClientset())

	if _, err := NewServerWithClients(&ServerConfig{}, nil, pc); err == nil {
		t.Errorf("expected error for nil k8s client")
	}
	if _, err := NewServerWithClients(&ServerConfig{}, k8s, nil); err == nil {
		t.Errorf("expected error for nil prom client")
	}
}

func TestServer_RegistersAllToolNames(t *testing.T) {
	prom := newPromStub(t)
	defer prom.Close()
	pc, _ := client.NewPrometheusClient(prom.URL)
	k8s := client.NewK8sClientFromInterface(fake.NewClientset())

	srv, err := NewServerWithClients(&ServerConfig{}, k8s, pc)
	if err != nil {
		t.Fatalf("NewServerWithClients: %v", err)
	}

	got := srv.ToolNames()
	sort.Strings(got)
	want := []string{
		"describe_node",
		"get_gpu_metrics",
		"get_quota_usage",
		"list_gpu_nodes",
		"list_gpu_pods",
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d tools, got %d (%v)", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tool[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestServer_ListToolsViaInMemoryTransport(t *testing.T) {
	prom := newPromStub(t)
	defer prom.Close()
	pc, _ := client.NewPrometheusClient(prom.URL)
	k8s := client.NewK8sClientFromInterface(fake.NewClientset())

	srv, err := NewServerWithClients(&ServerConfig{}, k8s, pc)
	if err != nil {
		t.Fatalf("NewServerWithClients: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clientTr, serverTr := mcpsdk.NewInMemoryTransports()

	if _, err := srv.Connect(ctx, serverTr); err != nil {
		t.Fatalf("server connect: %v", err)
	}

	c := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "v0"}, nil)
	cs, err := c.Connect(ctx, clientTr, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	resp, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(resp.Tools) != 5 {
		t.Errorf("expected 5 tools advertised, got %d", len(resp.Tools))
	}

	names := make(map[string]bool, len(resp.Tools))
	for _, tool := range resp.Tools {
		names[tool.Name] = true
	}
	for _, expected := range []string{"list_gpu_nodes", "list_gpu_pods", "get_quota_usage", "get_gpu_metrics", "describe_node"} {
		if !names[expected] {
			t.Errorf("expected tool %q to be advertised", expected)
		}
	}
}
