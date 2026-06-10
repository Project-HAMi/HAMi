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
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Project-HAMi/HAMi/pkg/mcp/client"
)

func TestSplitAndClean(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"single", "a", []string{"a"}},
		{"three values", "a,b,c", []string{"a", "b", "c"}},
		{"trim spaces", " a , b ,c ", []string{"a", "b", "c"}},
		{"skip empty", "a,,b", []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitAndClean(tt.in, ",")
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitAndClean(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestExtractGPUPodInfo(t *testing.T) {
	tool := NewListGPUPodsTool(client.NewK8sClientFromInterface(fake.NewClientset()))

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns1",
			Name:      "pod1",
			Annotations: map[string]string{
				"hami.io/gpu-devices-to-use": "uuid-a, uuid-b ,uuid-c",
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
			Containers: []corev1.Container{
				{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
						},
					},
				},
				{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	info := tool.extractGPUPodInfo(pod)
	if info.Namespace != "ns1" || info.Name != "pod1" || info.Node != "node-1" {
		t.Errorf("unexpected basic fields: %+v", info)
	}
	if info.RequestedGPU != 3 {
		t.Errorf("expected sum=3, got %d", info.RequestedGPU)
	}
	if info.Status != "Running" {
		t.Errorf("expected status Running, got %s", info.Status)
	}
	if !reflect.DeepEqual(info.AllocatedDeviceUUIDs, []string{"uuid-a", "uuid-b", "uuid-c"}) {
		t.Errorf("expected 3 UUIDs, got %v", info.AllocatedDeviceUUIDs)
	}
}

func TestExtractGPUPodInfo_NvidiaAnnotation(t *testing.T) {
	tool := NewListGPUPodsTool(client.NewK8sClientFromInterface(fake.NewClientset()))
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"nvidia.com/gpu-devices-to-use": "uuid-x",
			},
		},
	}
	info := tool.extractGPUPodInfo(pod)
	if !reflect.DeepEqual(info.AllocatedDeviceUUIDs, []string{"uuid-x"}) {
		t.Errorf("expected single UUID, got %v", info.AllocatedDeviceUUIDs)
	}
}

func TestListGPUPodsTool_Handler(t *testing.T) {
	gpuPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "gpu-pod", Namespace: "ns1"},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
			Containers: []corev1.Container{{
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
					},
				},
			}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	cpuPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "cpu-pod", Namespace: "ns1"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("1"),
					},
				},
			}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	cs := fake.NewClientset(gpuPod, cpuPod)
	k8s := client.NewK8sClientFromInterface(cs)
	tool := NewListGPUPodsTool(k8s)

	if tool.Tool().Name != "list_gpu_pods" {
		t.Errorf("unexpected tool name: %s", tool.Tool().Name)
	}

	res, _, err := tool.Handler()(context.Background(), nil, struct {
		Namespace string `json:"namespace,omitempty"`
		Phase     string `json:"phase,omitempty"`
	}{Namespace: "ns1"})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", extractText(res))
	}

	var pods []GPUPodInfo
	if err := json.Unmarshal([]byte(extractText(res)), &pods); err != nil {
		t.Fatalf("invalid response JSON: %v\nbody=%s", err, extractText(res))
	}
	if len(pods) != 1 || pods[0].Name != "gpu-pod" {
		t.Errorf("expected only gpu-pod, got %+v", pods)
	}
}
