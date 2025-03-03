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
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Mock Kubernetes 客户端（用于测试）.
type MockClient struct {
	nodes map[string]*corev1.Node
	pods  map[types.NamespacedName]*corev1.Pod
}

var _ KubeInterface = (*MockClient)(nil)

var (
	mockClientOnce sync.Once
	mockClient     *MockClient
)

// NewMockClient 创建模拟的 Kubernetes 客户端（单例模式）.
func NewMockClient() *MockClient {
	mockClientOnce.Do(func() {
		mockClient = &MockClient{
			nodes: make(map[string]*corev1.Node),
			pods:  make(map[types.NamespacedName]*corev1.Pod),
		}
	})
	return mockClient
}

// 模拟获取单个 Node 信息.
func (m *MockClient) GetNode(ctx context.Context, name string, opts metav1.GetOptions) (*corev1.Node, error) {
	if m.nodes == nil {
		m.nodes = make(map[string]*corev1.Node)
	}

	node, exists := m.nodes[name]
	if !exists {
		return nil, fmt.Errorf("node %s not found", name)
	}
	return node, nil
}

// 模拟获取单个 Pod 信息.
func (m *MockClient) GetPod(ctx context.Context, namespace, name string, opts metav1.GetOptions) (*corev1.Pod, error) {
	if m.pods == nil {
		m.pods = make(map[types.NamespacedName]*corev1.Pod)
	}

	key := types.NamespacedName{Namespace: namespace, Name: name}
	pod, exists := m.pods[key]
	if !exists {
		return nil, fmt.Errorf("pod %s/%s not found", namespace, name)
	}
	return pod, nil
}

// 模拟获取 Pod 列表.
func (m *MockClient) ListPods(ctx context.Context, namespace string, opts metav1.ListOptions) (*corev1.PodList, error) {
	if m.pods == nil {
		m.pods = make(map[types.NamespacedName]*corev1.Pod)
		return &corev1.PodList{Items: []corev1.Pod{}}, nil
	}

	podList := &corev1.PodList{
		Items: []corev1.Pod{},
	}

	for key, pod := range m.pods {
		if namespace == "" || key.Namespace == namespace {
			// 支持简单的标签选择器过滤
			if opts.LabelSelector != "" && !podMatchesSelector(pod, opts.LabelSelector) {
				continue
			}
			podList.Items = append(podList.Items, *pod)
		}
	}

	return podList, nil
}

// 简单的标签选择器匹配逻辑 (实际实现中可能需要更复杂的解析).
func podMatchesSelector(pod *corev1.Pod, selector string) bool {
	// 这里简化处理，实际应该使用 k8s 的标签选择器解析
	// 假设 selector 格式为 "key=value"
	for k, v := range pod.Labels {
		if fmt.Sprintf("%s=%s", k, v) == selector {
			return true
		}
	}
	return false
}

// 模拟修补 Node.
func (m *MockClient) PatchNode(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions) (*corev1.Node, error) {
	if m.nodes == nil {
		m.nodes = make(map[string]*corev1.Node)
	}

	node, exists := m.nodes[name]
	if !exists {
		return nil, fmt.Errorf("node %s not found", name)
	}

	// 实际场景中应该根据 patch 数据修改节点
	// 这里简化处理，仅返回存储的节点
	return node, nil
}

// 模拟修补 Pod.
func (m *MockClient) PatchPod(ctx context.Context, namespace string, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions) (*corev1.Pod, error) {
	if m.pods == nil {
		m.pods = make(map[types.NamespacedName]*corev1.Pod)
	}

	key := types.NamespacedName{Namespace: namespace, Name: name}
	pod, exists := m.pods[key]
	if !exists {
		return nil, fmt.Errorf("pod %s/%s not found", namespace, name)
	}

	// 实际场景中应该根据 patch 数据修改 pod
	// 这里简化处理，仅返回存储的 pod
	return pod, nil
}

// 模拟创建 Node.
func (m *MockClient) CreateNode(ctx context.Context, node *corev1.Node, opts metav1.CreateOptions) (*corev1.Node, error) {
	if m.nodes == nil {
		m.nodes = make(map[string]*corev1.Node)
	}

	if _, exists := m.nodes[node.Name]; exists {
		return nil, fmt.Errorf("node %s already exists", node.Name)
	}

	// 创建一个深拷贝以避免外部修改
	nodeCopy := node.DeepCopy()
	m.nodes[node.Name] = nodeCopy

	return nodeCopy, nil
}
