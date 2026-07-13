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

package resources

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	klog "k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/mcp/client"
	"github.com/Project-HAMi/HAMi/pkg/mcp/redact"
)

// ConfigResource implements the HAMi configuration MCP resource.
type ConfigResource struct {
	k8sClient *client.K8sClient
}

// NewConfigResource creates a new ConfigResource.
func NewConfigResource(k8sClient *client.K8sClient) *ConfigResource {
	return &ConfigResource{
		k8sClient: k8sClient,
	}
}

// Resource returns the MCP resource definition.
func (r *ConfigResource) Resource() *mcp.Resource {
	return &mcp.Resource{
		URI:         "hami://config/scheduler",
		Name:        "HAMi Scheduler Configuration",
		Description: "The current HAMi scheduler configuration from the ConfigMap.",
		MIMEType:    "application/json",
	}
}

// Handler returns the resource handler function.
func (r *ConfigResource) Handler() func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		klog.V(2).InfoS("Reading HAMi config resource")

		// Get the HAMi scheduler config ConfigMap
		configMap, err := r.k8sClient.GetConfigMap(ctx, "hami-system", "hami-scheduler-config")
		if err != nil {
			return nil, fmt.Errorf("failed to get HAMi config: %w", err)
		}

		// Extract config data
		configData := make(map[string]any)
		for key, value := range configMap.Data {
			configData[key] = value
		}

		// Marshal to JSON
		data, err := json.Marshal(configData)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal config: %w", err)
		}

		// Apply redaction
		redactedData := redact.Redact(string(data))

		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{
				{
					URI:      req.Params.URI,
					MIMEType: "application/json",
					Text:     redactedData,
				},
			},
		}, nil
	}
}
