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
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/klog/v2"
)

// Fake Kubernetes 客户端（用于测试）.
type FakeClient struct {
	client kubernetes.Interface
}

var _ KubeInterface = (*FakeClient)(nil)

var (
	fakeClientOnce sync.Once
	fakeClient     *FakeClient
)

// NewFakeClient 创建测试用的 Kubernetes 客户端（单例模式）.
func NewFakeClient() *FakeClient {
	fakeClientOnce.Do(func() {
		fakeClient = &FakeClient{
			client: fake.NewSimpleClientset(),
		}
	})
	return fakeClient
}

// 获取单个 Node 信息.
func (f *FakeClient) GetNode(ctx context.Context, name string, opts metav1.GetOptions) (*corev1.Node, error) {
	klog.V(4).InfoS("Fake: Retrieving node", "node", name)
	return f.client.CoreV1().Nodes().Get(ctx, name, opts)
}

// 获取单个 Pod 信息.
func (f *FakeClient) GetPod(ctx context.Context, namespace, name string, opts metav1.GetOptions) (*corev1.Pod, error) {
	klog.V(4).InfoS("Fake: Retrieving pod", "namespace", namespace, "pod", name)
	return f.client.CoreV1().Pods(namespace).Get(ctx, name, opts)
}

// 获取 Pod 列表.
func (f *FakeClient) ListPods(ctx context.Context, namespace string, opts metav1.ListOptions) (*corev1.PodList, error) {
	klog.V(4).InfoS("Fake: Listing pods", "namespace", namespace)
	return f.client.CoreV1().Pods(namespace).List(ctx, opts)
}

// 修补 Node.
func (f *FakeClient) PatchNode(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions) (*corev1.Node, error) {
	klog.V(4).InfoS("Fake: Patching node", "node", name)
	node, err := f.client.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get node %s for patching: %w", name, err)
	}
	return f.client.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
}

// 修补 Pod.
func (f *FakeClient) PatchPod(ctx context.Context, namespace string, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions) (*corev1.Pod, error) {
	klog.V(4).InfoS("Fake: Patching pod", "namespace", namespace, "pod", name)
	pod, err := f.client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod %s/%s for patching: %w", namespace, name, err)
	}
	return f.client.CoreV1().Pods(namespace).Update(ctx, pod, metav1.UpdateOptions{})
}

// 创建 Node.
func (f *FakeClient) CreateNode(ctx context.Context, node *corev1.Node, opts metav1.CreateOptions) (*corev1.Node, error) {
	klog.V(4).InfoS("Fake: Creating node", "node", node.Name)
	return f.client.CoreV1().Nodes().Create(ctx, node, opts)
}
