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
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Project-HAMi/HAMi/pkg/mcp/client"
)

func TestCalculateQuotaUsage(t *testing.T) {
	pods := []*corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "running", Namespace: "ns1"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceName("nvidia.com/gpumem"):   resource.MustParse("2048"),
							corev1.ResourceName("nvidia.com/gpucores"): resource.MustParse("50"),
						},
					},
				}},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "pending", Namespace: "ns1"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceName("nvidia.com/gpumem"):   resource.MustParse("1024"),
							corev1.ResourceName("nvidia.com/gpucores"): resource.MustParse("25"),
						},
					},
				}},
			},
			Status: corev1.PodStatus{Phase: corev1.PodPending},
		},
		{
			// Succeeded pod must be excluded
			ObjectMeta: metav1.ObjectMeta{Name: "done", Namespace: "ns1"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceName("nvidia.com/gpumem"): resource.MustParse("8192"),
						},
					},
				}},
			},
			Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
		},
	}

	tool := NewGetQuotaUsageTool(client.NewK8sClientFromInterface(fake.NewClientset()))
	usage := tool.calculateQuotaUsage("ns1", pods)
	if usage.Namespace != "ns1" {
		t.Errorf("expected namespace ns1, got %s", usage.Namespace)
	}
	// (2048 + 1024) MB / 1024 = 3 GiB
	if usage.GPUMemoryUsedGiB != 3 {
		t.Errorf("expected memory used 3 GiB, got %v", usage.GPUMemoryUsedGiB)
	}
	if usage.GPUCoreUsed != 75 {
		t.Errorf("expected cores used 75, got %v", usage.GPUCoreUsed)
	}
}

func TestGetQuotaUsageTool_Handler(t *testing.T) {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns1"}}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "ns1"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceName("nvidia.com/gpumem"):   resource.MustParse("4096"),
						corev1.ResourceName("nvidia.com/gpucores"): resource.MustParse("80"),
						// Add this so it's recognised as a GPU pod
						corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
					},
				},
			}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	cs := fake.NewClientset(ns, pod)
	tool := NewGetQuotaUsageTool(client.NewK8sClientFromInterface(cs))

	if tool.Tool().Name != "get_quota_usage" {
		t.Errorf("unexpected tool name")
	}

	t.Run("empty namespace returns error", func(t *testing.T) {
		res, _, _ := tool.Handler()(context.Background(), nil, struct {
			Namespace string `json:"namespace"`
		}{})
		if !res.IsError {
			t.Errorf("expected error for empty namespace")
		}
	})

	t.Run("missing namespace returns error", func(t *testing.T) {
		res, _, _ := tool.Handler()(context.Background(), nil, struct {
			Namespace string `json:"namespace"`
		}{Namespace: "missing"})
		if !res.IsError {
			t.Errorf("expected error for missing namespace")
		}
	})

	t.Run("returns aggregated usage", func(t *testing.T) {
		res, _, err := tool.Handler()(context.Background(), nil, struct {
			Namespace string `json:"namespace"`
		}{Namespace: "ns1"})
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if res.IsError {
			t.Fatalf("unexpected error: %s", extractText(res))
		}
		var usage QuotaUsage
		if err := json.Unmarshal([]byte(extractText(res)), &usage); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if usage.Namespace != "ns1" {
			t.Errorf("expected ns1, got %s", usage.Namespace)
		}
		if usage.GPUMemoryUsedGiB != 4 {
			t.Errorf("expected 4 GiB used, got %v", usage.GPUMemoryUsedGiB)
		}
		if usage.GPUCoreUsed != 80 {
			t.Errorf("expected 80 cores used, got %v", usage.GPUCoreUsed)
		}
	})
}
