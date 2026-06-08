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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/mcp/client"
)

func newPromHandlerServer(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
}

func TestGetGPUMetricsTool_Handler(t *testing.T) {
	body := `{
		"status":"success",
		"data":{"resultType":"vector","result":[
			{"metric":{"node":"n1"},"value":[1700000000,"5"]}
		]}
	}`
	srv := newPromHandlerServer(t, body, http.StatusOK)
	defer srv.Close()

	pc, err := client.NewPrometheusClient(srv.URL)
	if err != nil {
		t.Fatalf("client init: %v", err)
	}
	tool := NewGetGPUMetricsTool(pc)

	if tool.Tool().Name != "get_gpu_metrics" {
		t.Errorf("unexpected tool name: %s", tool.Tool().Name)
	}

	t.Run("empty metric returns error", func(t *testing.T) {
		res, _, _ := tool.Handler()(context.Background(), nil, struct {
			Metric string `json:"metric"`
			Node   string `json:"node,omitempty"`
		}{})
		if !res.IsError {
			t.Errorf("expected error result for empty metric")
		}
	})

	t.Run("returns metric values", func(t *testing.T) {
		res, _, err := tool.Handler()(context.Background(), nil, struct {
			Metric string `json:"metric"`
			Node   string `json:"node,omitempty"`
		}{Metric: "hami_gpu_device_count", Node: "n1"})
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if res.IsError {
			t.Fatalf("unexpected error: %s", extractText(res))
		}
		var metrics []GPUMetric
		if err := json.Unmarshal([]byte(extractText(res)), &metrics); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if len(metrics) != 1 {
			t.Fatalf("expected 1 metric, got %d", len(metrics))
		}
		if metrics[0].Value != 5 {
			t.Errorf("expected value 5, got %v", metrics[0].Value)
		}
		if metrics[0].Metric["node"] != "n1" {
			t.Errorf("expected node label n1, got %v", metrics[0].Metric)
		}
	})
}

func TestGetGPUMetricsTool_PrometheusError(t *testing.T) {
	srv := newPromHandlerServer(t, "boom", http.StatusInternalServerError)
	defer srv.Close()

	pc, _ := client.NewPrometheusClient(srv.URL)
	tool := NewGetGPUMetricsTool(pc)

	res, _, _ := tool.Handler()(context.Background(), nil, struct {
		Metric string `json:"metric"`
		Node   string `json:"node,omitempty"`
	}{Metric: "x"})
	if !res.IsError {
		t.Errorf("expected error result when prom returns 500")
	}
}
