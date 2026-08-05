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

	"github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"
	klog "k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/mcp/client"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

// AllocatedDevice is one device slice allocated to a specific container.
type AllocatedDevice struct {
	DeviceName     string `json:"deviceName"`     // vendor common word, e.g. "NVIDIA"
	ContainerIndex int    `json:"containerIndex"` // index into the pod's container list
	UUID           string `json:"uuid"`
	Type           string `json:"type"`
	UsedMemMiB     int32  `json:"usedMemMiB"`
	UsedCoresPct   int32  `json:"usedCoresPct"`
}

// GPUPodInfo summarizes one GPU-requesting pod for list_gpu_pods.
type GPUPodInfo struct {
	Namespace        string            `json:"namespace"`
	Name             string            `json:"name"`
	Node             string            `json:"node"`
	RequestedGPU     int64             `json:"requestedGPU"`
	AllocatedDevices []AllocatedDevice `json:"allocatedDevices,omitempty"`
	Status           string            `json:"status"`
}

type listGPUPodsInput struct {
	Namespace string `json:"namespace,omitempty" jsonschema:"optional namespace to filter pods; all namespaces if omitted"`
	Phase     string `json:"phase,omitempty" jsonschema:"optional pod phase filter: Running, Pending, Succeeded, Failed, or Unknown"`
}

// RegisterListGPUPods registers the list_gpu_pods tool.
func RegisterListGPUPods(s *mcp.Server, k8sClient *client.K8sClient) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "list_gpu_pods",
		Description: "List pods that request GPU resources from any HAMi-supported vendor, " +
			"including which physical/virtual devices each container was allocated. Read-only.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in listGPUPodsInput) (*mcp.CallToolResult, any, error) {
		pods, err := k8sClient.ListGPUPods(ctx, in.Namespace, in.Phase)
		if err != nil {
			return errorResult("list_gpu_pods: %v", err), nil, nil
		}

		infos := make([]GPUPodInfo, 0, len(pods))
		for _, p := range pods {
			infos = append(infos, extractGPUPodInfo(p))
		}

		data, err := json.Marshal(infos)
		if err != nil {
			return errorResult("list_gpu_pods: marshal result: %v", err), nil, nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
		}, nil, nil
	})
}

func extractGPUPodInfo(pod *corev1.Pod) GPUPodInfo {
	info := GPUPodInfo{
		Namespace: pod.Namespace,
		Name:      pod.Name,
		Node:      pod.Spec.NodeName,
		Status:    string(pod.Status.Phase),
	}
	if info.Node == "" {
		// Bound by the extender but not yet reflected in Spec.NodeName.
		info.Node = pod.Annotations[util.AssignedNodeAnnotations] // hami.io/vgpu-node
	}

	info.AllocatedDevices = podAllocations(pod)
	info.RequestedGPU = requestedGPUCount(pod)
	return info
}

// requestedGPUCount sums each vendor's count resource (e.g. nvidia.com/gpu)
// across every container's resource requests/limits. Unlike reading
// allocation annotations, this works for Pending pods too, since it reflects
// what was asked for, not what's been bound.
func requestedGPUCount(pod *corev1.Pod) int64 {
	countNames := make(map[corev1.ResourceName]struct{})
	for _, dev := range device.GetDevices() {
		if n := dev.GetResourceNames().ResourceCountName; n != "" {
			countNames[corev1.ResourceName(n)] = struct{}{}
		}
	}

	var total int64
	sum := func(ctrs []corev1.Container) {
		for _, c := range ctrs {
			for name := range countNames {
				if q, ok := c.Resources.Limits[name]; ok {
					total += q.Value()
					continue // avoid double-counting if Requests is also set
				}
				if q, ok := c.Resources.Requests[name]; ok {
					total += q.Value()
				}
			}
		}
	}
	sum(pod.Spec.Containers)
	sum(pod.Spec.InitContainers)
	return total
}

// podAllocations decodes every vendor's post-bind allocation annotation
// (device.SupportDevices, e.g. hami.io/vgpu-devices-allocated for NVIDIA)
// found on the pod. Vendors the pod didn't request contribute nothing.
func podAllocations(pod *corev1.Pod) []AllocatedDevice {
	pd, err := device.DecodePodDevices(device.SupportDevices, pod.Annotations)
	if err != nil {
		klog.V(4).InfoS("failed to decode pod device annotations",
			"pod", klog.KObj(pod), "err", err)
		return nil
	}

	var out []AllocatedDevice
	for devName, perContainer := range pd {
		for ctrIdx, ctrDevs := range perContainer {
			for _, cd := range ctrDevs {
				if cd.UUID == "" {
					// Placeholder preserving container-index alignment for a
					// container that requested no device; not a real allocation.
					continue
				}
				out = append(out, AllocatedDevice{
					DeviceName:     devName,
					ContainerIndex: ctrIdx,
					UUID:           cd.UUID,
					Type:           cd.Type,
					UsedMemMiB:     cd.Usedmem,
					UsedCoresPct:   cd.Usedcores,
				})
			}
		}
	}
	return out
}
