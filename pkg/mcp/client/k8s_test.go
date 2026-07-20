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
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func makeNode(name string, capacity corev1.ResourceList, labels map[string]string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Status: corev1.NodeStatus{
			Capacity: capacity,
		},
	}
}

func makePod(ns, name, phase string, requests corev1.ResourceList) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "main",
					Image: "test",
					Resources: corev1.ResourceRequirements{
						Requests: requests,
					},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPhase(phase),
		},
	}
}

func TestNewK8sClientFromInterface(t *testing.T) {
	cs := fake.NewClientset()
	c := NewK8sClientFromInterface(cs)
	if c == nil || c.clientset == nil {
		t.Fatalf("expected non-nil client and clientset")
	}
}

func TestHasGPUResources(t *testing.T) {
	tests := []struct {
		name string
		node *corev1.Node
		want bool
	}{
		{
			name: "nvidia gpu",
			node: makeNode("n1", corev1.ResourceList{
				corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
			}, nil),
			want: true,
		},
		{
			name: "cambricon vmlu",
			node: makeNode("n2", corev1.ResourceList{
				corev1.ResourceName("cambricon.com/vmlu"): resource.MustParse("1"),
			}, nil),
			want: true,
		},
		{
			name: "no gpu",
			node: makeNode("n3", corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("4"),
			}, nil),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasGPUResources(tt.node); got != tt.want {
				t.Errorf("hasGPUResources() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasGPURequests(t *testing.T) {
	gpuPod := makePod("default", "gpu", "Running", corev1.ResourceList{
		corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
	})
	if !hasGPURequests(gpuPod) {
		t.Errorf("expected pod with nvidia.com/gpu request to be a GPU pod")
	}

	cpuPod := makePod("default", "cpu", "Running", corev1.ResourceList{
		corev1.ResourceCPU: resource.MustParse("1"),
	})
	if hasGPURequests(cpuPod) {
		t.Errorf("expected CPU-only pod not to be a GPU pod")
	}
}

func TestListGPUNodes(t *testing.T) {
	gpuNode := makeNode("gpu-1", corev1.ResourceList{
		corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
	}, map[string]string{"gpu": "on"})
	cpuNode := makeNode("cpu-1", corev1.ResourceList{
		corev1.ResourceCPU: resource.MustParse("8"),
	}, nil)

	cs := fake.NewClientset(gpuNode, cpuNode)
	c := NewK8sClientFromInterface(cs)

	t.Run("returns only GPU nodes", func(t *testing.T) {
		nodes, err := c.ListGPUNodes(context.Background(), "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(nodes) != 1 {
			t.Fatalf("expected 1 GPU node, got %d", len(nodes))
		}
		if nodes[0].Name != "gpu-1" {
			t.Errorf("expected node 'gpu-1', got %s", nodes[0].Name)
		}
	})

	t.Run("filter with non-matching label selector returns empty", func(t *testing.T) {
		nodes, err := c.ListGPUNodes(context.Background(), "gpu=off")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(nodes) != 0 {
			t.Fatalf("expected 0 nodes, got %d", len(nodes))
		}
	})

	t.Run("matching label selector keeps GPU node", func(t *testing.T) {
		nodes, err := c.ListGPUNodes(context.Background(), "gpu=on")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(nodes) != 1 || nodes[0].Name != "gpu-1" {
			t.Errorf("expected node gpu-1, got %+v", nodes)
		}
	})
}

func TestListGPUPods(t *testing.T) {
	gpuPod := makePod("ns1", "gpu-pod", "Running", corev1.ResourceList{
		corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
	})
	pendingGPU := makePod("ns1", "pending-pod", "Pending", corev1.ResourceList{
		corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
	})
	cpuPod := makePod("ns1", "cpu-pod", "Running", corev1.ResourceList{
		corev1.ResourceCPU: resource.MustParse("1"),
	})
	otherNS := makePod("ns2", "other-gpu", "Running", corev1.ResourceList{
		corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
	})

	cs := fake.NewClientset(gpuPod, pendingGPU, cpuPod, otherNS)
	c := NewK8sClientFromInterface(cs)

	t.Run("all namespaces returns only GPU pods", func(t *testing.T) {
		pods, err := c.ListGPUPods(context.Background(), "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(pods) != 3 {
			t.Errorf("expected 3 GPU pods, got %d", len(pods))
		}
	})

	t.Run("namespace filter works", func(t *testing.T) {
		pods, err := c.ListGPUPods(context.Background(), "ns2", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(pods) != 1 || pods[0].Name != "other-gpu" {
			t.Errorf("expected 1 pod 'other-gpu', got %+v", pods)
		}
	})

	t.Run("phase filter works", func(t *testing.T) {
		pods, err := c.ListGPUPods(context.Background(), "ns1", "Pending")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(pods) != 1 || pods[0].Name != "pending-pod" {
			t.Errorf("expected 1 pending pod, got %+v", pods)
		}
	})
}

func TestGetNode(t *testing.T) {
	node := makeNode("n1", corev1.ResourceList{
		corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
	}, nil)
	cs := fake.NewClientset(node)
	c := NewK8sClientFromInterface(cs)

	got, err := c.GetNode(context.Background(), "n1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "n1" {
		t.Errorf("expected node 'n1', got %s", got.Name)
	}

	if _, err := c.GetNode(context.Background(), "missing"); err == nil {
		t.Errorf("expected error for missing node")
	}
}

func TestGetNamespace(t *testing.T) {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns1"}}
	cs := fake.NewClientset(ns)
	c := NewK8sClientFromInterface(cs)

	got, err := c.GetNamespace(context.Background(), "ns1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "ns1" {
		t.Errorf("expected ns 'ns1', got %s", got.Name)
	}

	if _, err := c.GetNamespace(context.Background(), "missing"); err == nil {
		t.Errorf("expected error for missing namespace")
	}
}

func TestGetConfigMap(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "cm1", Namespace: "ns1"},
		Data:       map[string]string{"k": "v"},
	}
	cs := fake.NewClientset(cm)
	c := NewK8sClientFromInterface(cs)

	got, err := c.GetConfigMap(context.Background(), "ns1", "cm1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Data["k"] != "v" {
		t.Errorf("expected configmap data 'v', got %s", got.Data["k"])
	}

	if _, err := c.GetConfigMap(context.Background(), "ns1", "missing"); err == nil {
		t.Errorf("expected error for missing configmap")
	}
}
