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

package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newPromTestServer(t *testing.T, body string, status int) (*httptest.Server, *[]string) {
	t.Helper()
	queries := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/api/v1/query") {
			http.NotFound(w, r)
			return
		}
		queries = append(queries, r.URL.Query().Get("query"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	return srv, &queries
}

func TestNewPrometheusClient(t *testing.T) {
	if _, err := NewPrometheusClient(""); err == nil {
		t.Errorf("expected error for empty URL")
	}
	c, err := NewPrometheusClient("http://localhost:9090")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.baseURL != "http://localhost:9090" {
		t.Errorf("baseURL not stored")
	}
}

func TestQueryInstant_Success(t *testing.T) {
	body := `{
		"status":"success",
		"data":{
			"resultType":"vector",
			"result":[
				{"metric":{"node":"n1"},"value":[1700000000,"42.5"]},
				{"metric":{"node":"n2"},"value":[1700000000,"7"]}
			]
		}
	}`
	srv, queries := newPromTestServer(t, body, http.StatusOK)
	defer srv.Close()

	c, _ := NewPrometheusClient(srv.URL)
	results, err := c.QueryInstant(context.Background(), "up")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Value != 42.5 {
		t.Errorf("expected 42.5, got %v", results[0].Value)
	}
	if results[0].Metric["node"] != "n1" {
		t.Errorf("expected node label n1, got %s", results[0].Metric["node"])
	}
	if (*queries)[0] != "up" {
		t.Errorf("expected query 'up', got %q", (*queries)[0])
	}
}

func TestQueryInstant_HTTPError(t *testing.T) {
	srv, _ := newPromTestServer(t, "boom", http.StatusInternalServerError)
	defer srv.Close()

	c, _ := NewPrometheusClient(srv.URL)
	if _, err := c.QueryInstant(context.Background(), "up"); err == nil {
		t.Errorf("expected error from non-200 response")
	}
}

func TestQueryInstant_PromError(t *testing.T) {
	body := `{"status":"error","error":"bad syntax","errorType":"parse"}`
	srv, _ := newPromTestServer(t, body, http.StatusOK)
	defer srv.Close()

	c, _ := NewPrometheusClient(srv.URL)
	if _, err := c.QueryInstant(context.Background(), "bad{"); err == nil {
		t.Errorf("expected error when prom returns status=error")
	}
}

func TestQueryInstant_BadJSON(t *testing.T) {
	srv, _ := newPromTestServer(t, "not json", http.StatusOK)
	defer srv.Close()

	c, _ := NewPrometheusClient(srv.URL)
	if _, err := c.QueryInstant(context.Background(), "x"); err == nil {
		t.Errorf("expected error for malformed JSON")
	}
}

func TestQueryInstant_SkipsBadEntries(t *testing.T) {
	body := `{
		"status":"success",
		"data":{"resultType":"vector","result":[
			{"metric":{},"value":[1700000000]},
			{"metric":{},"value":["bad","1"]},
			{"metric":{},"value":[1700000000,1]},
			{"metric":{"a":"b"},"value":[1700000000,"3.14"]}
		]}
	}`
	srv, _ := newPromTestServer(t, body, http.StatusOK)
	defer srv.Close()

	c, _ := NewPrometheusClient(srv.URL)
	results, err := c.QueryInstant(context.Background(), "x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 valid result, got %d", len(results))
	}
	if results[0].Value != 3.14 {
		t.Errorf("expected 3.14, got %v", results[0].Value)
	}
}

func TestQueryHelpers(t *testing.T) {
	body := `{"status":"success","data":{"resultType":"vector","result":[]}}`
	srv, queries := newPromTestServer(t, body, http.StatusOK)
	defer srv.Close()

	c, _ := NewPrometheusClient(srv.URL)
	ctx := context.Background()

	if _, err := c.QueryGPUMemoryAllocated(ctx, ""); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if _, err := c.QueryGPUMemoryAllocated(ctx, "node-a"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if _, err := c.QueryGPUCoreAllocated(ctx, "node-a"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if _, err := c.QueryGPUDeviceCount(ctx, ""); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if _, err := c.QueryCustomMetric(ctx, "hami_gpu_device_count", "node-a"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}

	wantContains := []string{
		"hami_gpu_memory_allocated_bytes",
		`hami_gpu_memory_allocated_bytes{node="node-a"}`,
		`hami_gpu_core_allocated_percent{node="node-a"}`,
		"hami_gpu_device_count",
		`hami_gpu_device_count{node="node-a"}`,
	}
	if len(*queries) != len(wantContains) {
		t.Fatalf("expected %d queries, got %d (%v)", len(wantContains), len(*queries), *queries)
	}
	for i, want := range wantContains {
		if (*queries)[i] != want {
			t.Errorf("query[%d] = %q, want %q", i, (*queries)[i], want)
		}
	}
}

func TestQueryCustomMetric_RejectsInjection(t *testing.T) {
	body := `{"status":"success","data":{"resultType":"vector","result":[]}}`
	srv, queries := newPromTestServer(t, body, http.StatusOK)
	defer srv.Close()

	c, _ := NewPrometheusClient(srv.URL)
	ctx := context.Background()

	cases := []struct {
		name   string
		metric string
		node   string
	}{
		{"empty metric", "", ""},
		{"unknown prefix", "process_cpu_seconds", ""},
		{"metric with quote", `hami_x"+up{}+"`, ""},
		{"metric with brace", "hami_x{a=1}", ""},
		{"metric with comma", "hami_x,b", ""},
		{"node with quote", "hami_gpu_device_count", `n"+up{}+"`},
		{"node with brace", "hami_gpu_device_count", "n{a=1}"},
		{"node with backslash", "hami_gpu_device_count", `n\x`},
		{"node uppercase invalid", "hami_gpu_device_count", "Node-A"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.QueryCustomMetric(ctx, tc.metric, tc.node)
			if err == nil {
				t.Errorf("expected error for metric=%q node=%q, got nil", tc.metric, tc.node)
			}
		})
	}
	if len(*queries) != 0 {
		t.Errorf("expected 0 queries to reach server (all rejected by validator), got %d: %v", len(*queries), *queries)
	}
}

func TestValidateMetricName(t *testing.T) {
	if err := validateMetricName("hami_gpu_device_count"); err != nil {
		t.Errorf("expected hami_ to be allowed: %v", err)
	}
	if err := validateMetricName("dcgm_gpu_temp"); err != nil {
		t.Errorf("expected dcgm_ to be allowed: %v", err)
	}
	if err := validateMetricName(""); err == nil {
		t.Error("expected empty metric to be rejected")
	}
	if err := validateMetricName("foo_bar"); err == nil {
		t.Error("expected non-allowlisted prefix to be rejected")
	}
	if err := validateMetricName("hami_x{a=1}"); err == nil {
		t.Error("expected metric with brace to be rejected")
	}
}

func TestValidateNodeLabel(t *testing.T) {
	if err := validateNodeLabel(""); err != nil {
		t.Errorf("empty node should pass: %v", err)
	}
	if err := validateNodeLabel("node-a.example.com"); err != nil {
		t.Errorf("DNS-1123 subdomain should pass: %v", err)
	}
	if err := validateNodeLabel("Node-A"); err == nil {
		t.Error("uppercase should be rejected")
	}
	if err := validateNodeLabel(`n"`); err == nil {
		t.Error("double quote should be rejected")
	}
}
