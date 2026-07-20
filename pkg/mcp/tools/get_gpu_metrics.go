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

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	klog "k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/mcp/client"
	"github.com/Project-HAMi/HAMi/pkg/mcp/redact"
)

// GetGPUMetricsTool implements the get_gpu_metrics MCP tool.
type GetGPUMetricsTool struct {
	promClient *client.PrometheusClient
}

// NewGetGPUMetricsTool creates a new GetGPUMetricsTool.
func NewGetGPUMetricsTool(promClient *client.PrometheusClient) *GetGPUMetricsTool {
	return &GetGPUMetricsTool{
		promClient: promClient,
	}
}

// Tool returns the MCP tool definition.
func (t *GetGPUMetricsTool) Tool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "get_gpu_metrics",
		Description: "Get GPU metrics from Prometheus. Returns time-series snapshot (last value) for HAMi GPU metrics.",
		InputSchema: &jsonschema.Schema{
			Type:     "object",
			Required: []string{"metric"},
			Properties: map[string]*jsonschema.Schema{
				"metric": {
					Type:        "string",
					Description: "The Prometheus metric name to query (e.g., 'hami_gpu_memory_allocated_bytes', 'hami_gpu_core_allocated_percent', 'hami_gpu_device_count').",
				},
				"node": {
					Type:        "string",
					Description: "Optional node name to filter metrics by.",
				},
			},
		},
	}
}

// GPUMetric represents a GPU metric value.
type GPUMetric struct {
	Metric map[string]string `json:"metric"`
	Value  float64           `json:"value"`
	Time   string            `json:"time"`
}

// Handler returns the tool handler function.
func (t *GetGPUMetricsTool) Handler() func(ctx context.Context, req *mcp.CallToolRequest, args struct {
	Metric string `json:"metric"`
	Node   string `json:"node,omitempty"`
}) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args struct {
		Metric string `json:"metric"`
		Node   string `json:"node,omitempty"`
	}) (*mcp.CallToolResult, any, error) {
		klog.V(2).InfoS("get_gpu_metrics called", "metric", args.Metric, "node", args.Node)

		// Validate metric
		if args.Metric == "" {
			return errorResult("metric is required"), nil, nil
		}

		// Query Prometheus
		results, err := t.promClient.QueryCustomMetric(ctx, args.Metric, args.Node)
		if err != nil {
			return errorResult(fmt.Sprintf("failed to query metric %s: %v", args.Metric, err)), nil, nil
		}

		// Convert to response format
		var metrics []GPUMetric
		for _, r := range results {
			metrics = append(metrics, GPUMetric{
				Metric: r.Metric,
				Value:  r.Value,
				Time:   r.Time.Format("2006-01-02T15:04:05Z"),
			})
		}

		// Marshal to JSON
		data, err := json.Marshal(metrics)
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
