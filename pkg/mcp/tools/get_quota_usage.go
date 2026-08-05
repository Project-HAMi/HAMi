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
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/mcp/client"
)

// ResourceUsage is the live usage and configured hard limit for one
// HAMi-managed resource (a memory or core resource name from some vendor's
// device.ResourceNames) in a namespace.
//
// Hard is a pointer so "no ResourceQuota covers this resource" serializes as
// an absent field rather than as 0, which an LLM consumer could otherwise
// misread as "quota is zero" instead of "quota not set".
type ResourceUsage struct {
	Resource string `json:"resource"` // e.g. nvidia.com/gpumem, nvidia.com/gpucores
	Used     int64  `json:"used"`
	Hard     *int64 `json:"hard,omitempty"`
}

// QuotaUsage is the get_quota_usage tool's output.
type QuotaUsage struct {
	Namespace string          `json:"namespace"`
	Resources []ResourceUsage `json:"resources"`
}

type getQuotaUsageInput struct {
	Namespace string `json:"namespace" jsonschema:"namespace to check GPU quota usage for"`
}

// RegisterGetQuotaUsage registers the get_quota_usage tool.
func RegisterGetQuotaUsage(s *mcp.Server, k8sClient *client.K8sClient) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "get_quota_usage",
		Description: "Get current GPU resource usage for a namespace (summed from live pod " +
			"allocations) alongside any hard limits configured via Kubernetes ResourceQuota " +
			"objects (limits.<resource> keys). A resource with no hard field has no configured " +
			"limit, which is not the same as a limit of zero. Read-only.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in getQuotaUsageInput) (*mcp.CallToolResult, any, error) {
		if in.Namespace == "" {
			return errorResult("get_quota_usage: namespace is required"), nil, nil
		}

		pods, err := k8sClient.ListGPUPods(ctx, in.Namespace, "")
		if err != nil {
			return errorResult("get_quota_usage: listing pods: %v", err), nil, nil
		}

		used := make(map[string]int64)
		for _, pod := range pods {
			if isTerminalPhase(pod.Status.Phase) {
				continue
			}
			for _, ad := range podAllocations(pod) {
				used[nvidiaMemResourceNameFor(ad.DeviceName)] += int64(ad.UsedMemMiB)
				used[nvidiaCoreResourceNameFor(ad.DeviceName)] += int64(ad.UsedCoresPct)
			}
		}

		quotas, err := k8sClient.ListResourceQuotas(ctx, in.Namespace)
		if err != nil {
			return errorResult("get_quota_usage: listing resourcequotas: %v", err), nil, nil
		}
		hard := make(map[string]int64)
		for _, rq := range quotas {
			for name, qty := range rq.Spec.Hard {
				key := name.String()
				if !strings.HasPrefix(key, "limits.") {
					continue
				}
				resourceName := strings.TrimPrefix(key, "limits.")
				if !device.IsManagedQuota(resourceName) {
					continue
				}
				if v, ok := qty.AsInt64(); ok {
					hard[resourceName] += v
				}
			}
		}

		// Union of every resource we saw usage or a hard limit for, so a
		// resource with a limit but zero usage still appears.
		names := make(map[string]struct{}, len(used)+len(hard))
		for k := range used {
			names[k] = struct{}{}
		}
		for k := range hard {
			names[k] = struct{}{}
		}

		usage := QuotaUsage{Namespace: in.Namespace}
		for name := range names {
			ru := ResourceUsage{Resource: name, Used: used[name]}
			if v, ok := hard[name]; ok {
				ru.Hard = &v
			}
			usage.Resources = append(usage.Resources, ru)
		}

		data, err := json.Marshal(usage)
		if err != nil {
			return errorResult("get_quota_usage: marshal result: %v", err), nil, nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
		}, nil, nil
	})
}

// nvidiaMemResourceNameFor and nvidiaCoreResourceNameFor map an
// AllocatedDevice's vendor common word back to that vendor's memory/core
// resource name, so usage can be bucketed the same way ResourceQuota keys
// are (e.g. "nvidia.com/gpumem"). Looked up from device.GetDevices() rather
// than hardcoded so a new vendor needs no change here.
func nvidiaMemResourceNameFor(commonWord string) string {
	if dev, ok := device.GetDevices()[commonWord]; ok {
		return dev.GetResourceNames().ResourceMemoryName
	}
	return commonWord + "/unknown-memory"
}

func nvidiaCoreResourceNameFor(commonWord string) string {
	if dev, ok := device.GetDevices()[commonWord]; ok {
		return dev.GetResourceNames().ResourceCoreName
	}
	return commonWord + "/unknown-cores"
}
