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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// PrometheusClient queries a Prometheus (or Prometheus-compatible) HTTP API
// for HAMi's own metrics. It is read-only: only the /api/v1/query endpoint
// is used.
type PrometheusClient struct {
	baseURL *url.URL
	http    *http.Client
}

// NewPrometheusClient validates rawURL and builds a client against it.
// rawURL must be an absolute http(s) URL with a host, e.g.
// "http://hami-vgpu-scheduler.hami-system:9395" — bare host:port values like
// "localhost:9090" are rejected because they silently fail later instead.
func NewPrometheusClient(rawURL string) (*PrometheusClient, error) {
	if rawURL == "" {
		return nil, fmt.Errorf("prometheus URL must not be empty")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid prometheus URL %q: %w", rawURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("prometheus URL must use http or https, got %q", rawURL)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("prometheus URL must include a host, got %q", rawURL)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("prometheus URL must not include a query or fragment, got %q", rawURL)
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	return &PrometheusClient{
		baseURL: u,
		http:    &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// MetricValue is one Prometheus instant-vector sample.
type MetricValue struct {
	Metric map[string]string
	Value  float64
	Time   time.Time
}

type promQueryResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  [2]any            `json:"value"` // [unixTimestamp(float64), value(string)]
		} `json:"result"`
	} `json:"data"`
}

// Query runs a raw PromQL instant query. Callers should validate the query
// against an allowlist before calling this (see tools.allowedMetrics) —
// this client does not restrict what it will run.
func (c *PrometheusClient) Query(ctx context.Context, promQL string) ([]MetricValue, error) {
	reqURL := *c.baseURL
	reqURL.Path = reqURL.Path + "/api/v1/query"
	reqURL.RawQuery = url.Values{"query": []string{promQL}}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query prometheus: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10 MiB cap
	if err != nil {
		return nil, fmt.Errorf("read prometheus response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus returned %s: %s", resp.Status, string(body))
	}

	var parsed promQueryResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode prometheus response: %w", err)
	}
	if parsed.Status != "success" {
		return nil, fmt.Errorf("prometheus query failed: %s", parsed.Error)
	}

	values := make([]MetricValue, 0, len(parsed.Data.Result))
	for _, r := range parsed.Data.Result {
		ts, ok := r.Value[0].(float64)
		if !ok {
			continue
		}
		valStr, ok := r.Value[1].(string)
		if !ok {
			continue
		}
		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			continue
		}
		values = append(values, MetricValue{
			Metric: r.Metric,
			Value:  val,
			Time:   time.Unix(int64(ts), 0).UTC(),
		})
	}
	return values, nil
}

// queryByMetric builds `metric{node="node"}` (or bare `metric` if node is
// empty) and runs it. All callers pass metric names verified against
// cmd/scheduler/metrics.go / cmd/vGPUmonitor/metrics.go — see
// pkg/mcp/tools/get_gpu_metrics.go for the allowlist and label mapping.
func (c *PrometheusClient) queryByMetric(ctx context.Context, metric, node string) ([]MetricValue, error) {
	q := metric
	if node != "" {
		q = fmt.Sprintf(`%s{node=%q}`, metric, node)
	}
	return c.Query(ctx, q)
}

// QueryGPUMemoryAllocated queries hami_gpu_memory_allocated_bytes, emitted
// by the scheduler extender (cmd/scheduler/metrics.go).
func (c *PrometheusClient) QueryGPUMemoryAllocated(ctx context.Context, node string) ([]MetricValue, error) {
	return c.queryByMetric(ctx, "hami_gpu_memory_allocated_bytes", node)
}

// QueryGPUCoreAllocated queries hami_gpu_core_allocated_ratio, emitted by
// the scheduler extender.
func (c *PrometheusClient) QueryGPUCoreAllocated(ctx context.Context, node string) ([]MetricValue, error) {
	return c.queryByMetric(ctx, "hami_gpu_core_allocated_ratio", node)
}

// QueryNodeGPUOverview queries hami_node_gpu_overview, the scheduler's
// per-device inventory metric (one series per GPU on a node).
func (c *PrometheusClient) QueryNodeGPUOverview(ctx context.Context, node string) ([]MetricValue, error) {
	return c.queryByMetric(ctx, "hami_node_gpu_overview", node)
}
