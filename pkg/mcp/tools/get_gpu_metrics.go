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
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Project-HAMi/HAMi/pkg/mcp/client"
)

// allowedMetrics is every HAMi metric name this tool will query, verified
// against cmd/scheduler/metrics.go and cmd/vGPUmonitor/metrics.go. This is
// an allowlist, not a passthrough: get_gpu_metrics takes a metric name, not
// a raw PromQL string, so a client can't inject arbitrary queries.
var allowedMetrics = map[string]string{
	// Scheduler extender (label set: node, device_uuid, device_index, device_type)
	"hami_gpu_memory_limit_bytes":          "Device memory limit for a certain GPU",
	"hami_gpu_core_limit_ratio":            "Device core limit for a certain GPU",
	"hami_gpu_memory_allocated_bytes":      "Device memory allocated for a certain GPU",
	"hami_gpu_shared_count":                "Number of containers sharing this GPU",
	"hami_gpu_core_allocated_ratio":        "Device core allocated for a certain GPU",
	"hami_node_gpu_overview":               "GPU overview on a certain node",
	"hami_node_gpu_memory_allocated_ratio": "GPU memory allocated percentage on a certain GPU",
	"hami_node_gpu_mig_instance_info":      "GPU sharing mode: 0 hami-core, 1 mig, 2 mps",
	"hami_vgpu_memory_allocated_bytes":     "vGPU memory allocated",
	"hami_vgpu_core_allocated_ratio":       "vGPU core allocated",
	"hami_resource_quota_used":             "ResourceQuota usage tracked by the scheduler",

	// vGPUmonitor (label set: namespace, pod, container, vdevice_index, device_uuid, ...)
	"hami_host_gpu_memory_used_bytes":            "Host GPU memory usage in bytes",
	"hami_host_gpu_utilization_ratio":            "Host GPU core utilization ratio (0-100)",
	"hami_vgpu_memory_used_bytes":                "vGPU device memory usage in bytes",
	"hami_vgpu_memory_limit_bytes":               "vGPU device memory limit in bytes",
	"hami_container_device_memory_bytes":         "Container device memory usage in bytes",
	"hami_container_device_utilization_ratio":    "Container device SM utilization ratio",
	"hami_container_last_kernel_elapsed_seconds": "Seconds since last kernel execution in container",
	"hami_mig_device_info":                       "MIG device information for container",
}

// GPUMetric is one Prometheus sample for get_gpu_metrics.
type GPUMetric struct {
	Metric map[string]string `json:"metric"`
	Value  float64           `json:"value"`
	Time   string            `json:"time"` // RFC3339, UTC
}

type getGPUMetricsInput struct {
	Metric string `json:"metric" jsonschema:"HAMi Prometheus metric name, e.g. hami_gpu_memory_allocated_bytes"`
	Node   string `json:"node,omitempty" jsonschema:"optional node name to filter by (adds a node= label match)"`
}

// RegisterGetGPUMetrics registers the get_gpu_metrics tool.
func RegisterGetGPUMetrics(s *mcp.Server, promClient *client.PrometheusClient) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_gpu_metrics",
		Description: metricsToolDescription(),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in getGPUMetricsInput) (*mcp.CallToolResult, any, error) {
		if in.Metric == "" {
			return errorResult("get_gpu_metrics: metric is required"), nil, nil
		}
		if _, ok := allowedMetrics[in.Metric]; !ok {
			return errorResult("get_gpu_metrics: unknown metric %q; see tool description for the allowed list", in.Metric), nil, nil
		}

		results, err := promClient.Query(ctx, queryFor(in.Metric, in.Node))
		if err != nil {
			return errorResult("get_gpu_metrics: %v", err), nil, nil
		}

		metrics := make([]GPUMetric, 0, len(results))
		for _, r := range results {
			metrics = append(metrics, GPUMetric{
				Metric: r.Metric,
				Value:  r.Value,
				Time:   r.Time.UTC().Format(time.RFC3339),
			})
		}

		data, err := json.Marshal(metrics)
		if err != nil {
			return errorResult("get_gpu_metrics: marshal result: %v", err), nil, nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
		}, nil, nil
	})
}

func queryFor(metric, node string) string {
	if node == "" {
		return metric
	}
	return fmt.Sprintf(`%s{node=%q}`, metric, node)
}

func metricsToolDescription() string {
	names := make([]string, 0, len(allowedMetrics))
	for name := range allowedMetrics {
		names = append(names, name)
	}
	sort.Strings(names)
	return "Query a HAMi GPU metric from Prometheus. Allowed metric names: " + strings.Join(names, ", ") + "."
}
