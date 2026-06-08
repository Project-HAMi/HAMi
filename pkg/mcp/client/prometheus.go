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
	"regexp"
	"strconv"
	"strings"
	"time"

	klog "k8s.io/klog/v2"
)

// PrometheusClient wraps the Prometheus HTTP API for read-only queries.
type PrometheusClient struct {
	baseURL    string
	httpClient *http.Client
}

// PrometheusResponse represents a Prometheus API response.
type PrometheusResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string            `json:"resultType"`
		Result     []PrometheusResult `json:"result"`
	} `json:"data"`
	Error     string `json:"error,omitempty"`
	ErrorType string `json:"errorType,omitempty"`
}

// PrometheusResult represents a single Prometheus query result.
type PrometheusResult struct {
	Metric map[string]string `json:"metric"`
	Value  []interface{}     `json:"value"`
}

// MetricValue represents a parsed metric value.
type MetricValue struct {
	Metric map[string]string `json:"metric"`
	Value  float64           `json:"value"`
	Time   time.Time         `json:"time"`
}

// NewPrometheusClient creates a new Prometheus client.
func NewPrometheusClient(baseURL string) (*PrometheusClient, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("prometheus URL is required")
	}

	// Validate URL
	_, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid prometheus URL: %w", err)
	}

	return &PrometheusClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// QueryInstant performs an instant query against Prometheus.
func (c *PrometheusClient) QueryInstant(ctx context.Context, query string) ([]MetricValue, error) {
	endpoint := fmt.Sprintf("%s/api/v1/query", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	q := req.URL.Query()
	q.Set("query", query)
	req.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus query failed with status %d: %s", resp.StatusCode, string(body))
	}

	var promResp PrometheusResponse
	if err := json.Unmarshal(body, &promResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if promResp.Status != "success" {
		return nil, fmt.Errorf("prometheus query failed: %s", promResp.Error)
	}

	// Parse results
	var results []MetricValue
	for _, r := range promResp.Data.Result {
		if len(r.Value) < 2 {
			continue
		}

		// Parse timestamp
		ts, ok := r.Value[0].(float64)
		if !ok {
			continue
		}

		// Parse value — Prometheus returns numbers as strings, including
		// "+Inf" / "-Inf" / "NaN" for non-finite series. strconv.ParseFloat
		// handles all three; fmt.Sscanf("%f") would silently drop them.
		valStr, ok := r.Value[1].(string)
		if !ok {
			continue
		}

		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			klog.V(4).InfoS("skipping unparseable Prometheus value", "raw", valStr, "err", err)
			continue
		}

		results = append(results, MetricValue{
			Metric: r.Metric,
			Value:  val,
			Time:   time.Unix(int64(ts), 0),
		})
	}

	klog.V(4).InfoS("Prometheus query executed", "query", query, "results", len(results))
	return results, nil
}

// QueryGPUMemoryAllocated queries GPU memory allocation metrics.
func (c *PrometheusClient) QueryGPUMemoryAllocated(ctx context.Context, node string) ([]MetricValue, error) {
	return c.queryByMetric(ctx, "hami_gpu_memory_allocated_bytes", node)
}

// QueryGPUCoreAllocated queries GPU core allocation metrics.
func (c *PrometheusClient) QueryGPUCoreAllocated(ctx context.Context, node string) ([]MetricValue, error) {
	return c.queryByMetric(ctx, "hami_gpu_core_allocated_percent", node)
}

// QueryGPUDeviceCount queries GPU device count metrics.
func (c *PrometheusClient) QueryGPUDeviceCount(ctx context.Context, node string) ([]MetricValue, error) {
	return c.queryByMetric(ctx, "hami_gpu_device_count", node)
}

// QueryCustomMetric queries a custom Prometheus metric. The metric name must
// match an allowlisted prefix (hami_/dcgm_/container_) and the node label must
// match Kubernetes' DNS-1123 subdomain shape; both guards prevent PromQL
// injection through user-controlled input.
func (c *PrometheusClient) QueryCustomMetric(ctx context.Context, metric string, node string) ([]MetricValue, error) {
	return c.queryByMetric(ctx, metric, node)
}

// metricNameRE allows only Prometheus-legal metric names. The allowlist of
// prefixes is enforced separately in validateMetricName.
var metricNameRE = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)

// allowedMetricPrefixes is the set of metric-name prefixes the MCP server is
// willing to forward. Restricting to known GPU-related families avoids
// turning the MCP into a generic Prometheus proxy.
var allowedMetricPrefixes = []string{"hami_", "dcgm_", "container_", "node_gpu_"}

// nodeLabelRE matches a DNS-1123 subdomain (which is what Kubernetes node
// names must be). Anything outside this set is rejected outright rather than
// PromQL-escaped, which avoids surprising operators with cryptic errors.
var nodeLabelRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)

func validateMetricName(name string) error {
	if name == "" {
		return fmt.Errorf("metric name is required")
	}
	if !metricNameRE.MatchString(name) {
		return fmt.Errorf("metric name %q contains invalid characters", name)
	}
	for _, p := range allowedMetricPrefixes {
		if strings.HasPrefix(name, p) {
			return nil
		}
	}
	return fmt.Errorf("metric %q is not in the allowlist (allowed prefixes: %v)", name, allowedMetricPrefixes)
}

func validateNodeLabel(node string) error {
	if node == "" {
		return nil
	}
	if len(node) > 253 {
		return fmt.Errorf("node label %q exceeds 253 characters", node)
	}
	if !nodeLabelRE.MatchString(node) {
		return fmt.Errorf("node label %q is not a valid DNS-1123 subdomain", node)
	}
	return nil
}

// queryByMetric assembles the PromQL expression after validating both the
// metric name and the optional node label.
func (c *PrometheusClient) queryByMetric(ctx context.Context, metric, node string) ([]MetricValue, error) {
	if err := validateMetricName(metric); err != nil {
		return nil, err
	}
	if err := validateNodeLabel(node); err != nil {
		return nil, err
	}
	query := metric
	if node != "" {
		query = fmt.Sprintf(`%s{node="%s"}`, metric, node)
	}
	return c.QueryInstant(ctx, query)
}
