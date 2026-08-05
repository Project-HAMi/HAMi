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

// Package resources implements the read-only MCP resources exposing HAMi's
// own configuration.
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

// configResourceURI is the fixed URI clients read to get HAMi's device
// scheduling config. It intentionally does not vary per cluster; namespace
// and name are server-side configuration (see RegisterConfigResource), not
// something a client selects.
const configResourceURI = "hami://config/scheduler"

// RegisterConfigResource registers the hami://config/scheduler resource,
// which reads the ConfigMap at namespace/name — normally the chart's
// "<release>-hami-scheduler-device" ConfigMap (device-config.yaml key), not
// the kube-scheduler ConfigMap. Every value is redacted before being
// returned: ConfigMap.Data entries are treated as opaque YAML/text blobs and
// scanned with redact.RedactBlob, since they aren't JSON that redact.Redact
// could walk.
func RegisterConfigResource(s *mcp.Server, k8sClient *client.K8sClient, namespace, name string) {
	s.AddResource(&mcp.Resource{
		URI:         configResourceURI,
		Name:        "HAMi scheduler configuration",
		Description: fmt.Sprintf("Redacted contents of the HAMi scheduler device ConfigMap (%s/%s).", namespace, name),
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		klog.V(2).InfoS("Reading HAMi config resource", "namespace", namespace, "name", name)

		configMap, err := k8sClient.GetConfigMap(ctx, namespace, name)
		if err != nil {
			return nil, fmt.Errorf("failed to get HAMi config %s/%s: %w", namespace, name, err)
		}

		redactedData := make(map[string]string, len(configMap.Data))
		for key, value := range configMap.Data {
			redactedData[key] = redact.RedactBlob(value)
		}

		data, err := json.Marshal(redactedData)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal config: %w", err)
		}

		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{
				{
					URI:      configResourceURI,
					MIMEType: "application/json",
					Text:     string(data),
				},
			},
		}, nil
	})
}
