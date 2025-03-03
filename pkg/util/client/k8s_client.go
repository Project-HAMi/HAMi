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
	"os"
	"path/filepath"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

var ClientOnce sync.Once

// GetK8sClient creates a real Kubernetes client (singleton pattern).
// Uses closure to maintain client as a local variable.
var GetK8sClient = (func() func() *K8sClient {
	var client *K8sClient

	return func() *K8sClient {
		var err error
		ClientOnce.Do(func() {
			client, err = createRealClient()
			if err != nil {
				klog.Fatalf("Failed to create Kubernetes client: %v", err)
			}
		})
		return client
	}
})()

type K8sClient struct {
	client kubernetes.Interface
}

var _ KubeInterface = (*K8sClient)(nil)

func createRealClient() (*K8sClient, error) {
	kubeConfigPath := os.Getenv("KUBECONFIG")
	if kubeConfigPath == "" {
		kubeConfigPath = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	if err != nil {
		klog.ErrorS(err, "BuildConfigFromFlags failed for file %s: %v. Using in-cluster config.", kubeConfigPath, err)
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to get in-cluster config: %w", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return &K8sClient{client: clientset}, nil
}

func (c *K8sClient) GetNode(ctx context.Context, name string, opts metav1.GetOptions) (*corev1.Node, error) {
	klog.V(4).InfoS("Retrieving node", "node", name)
	node, err := c.client.CoreV1().Nodes().Get(ctx, name, opts)
	if err != nil {
		klog.ErrorS(err, "Failed to get node", "node", name)
		return nil, fmt.Errorf("failed to get node %s: %w", name, err)
	}
	klog.V(4).InfoS("Successfully retrieved node", "node", name)
	return node, nil
}

func (c *K8sClient) GetPod(ctx context.Context, namespace, name string, opts metav1.GetOptions) (*corev1.Pod, error) {
	klog.V(4).InfoS("Retrieving pod", "namespace", namespace, "pod", name)
	pod, err := c.client.CoreV1().Pods(namespace).Get(ctx, name, opts)
	if err != nil {
		klog.ErrorS(err, "Failed to get pod", "namespace", namespace, "pod", name)
		return nil, fmt.Errorf("failed to get pod %s/%s: %w", namespace, name, err)
	}
	klog.V(4).InfoS("Successfully retrieved pod", "namespace", namespace, "pod", name)
	return pod, nil
}

func (c *K8sClient) ListPods(ctx context.Context, namespace string, opts metav1.ListOptions) (*corev1.PodList, error) {
	klog.V(4).InfoS("Listing pods", "namespace", namespace, "options", opts)
	pods, err := c.client.CoreV1().Pods(namespace).List(ctx, opts)
	if err != nil {
		klog.ErrorS(err, "Failed to list pods", "namespace", namespace)
		return nil, fmt.Errorf("failed to list pods in namespace %s: %w", namespace, err)
	}
	klog.V(4).InfoS("Successfully listed pods", "namespace", namespace, "count", len(pods.Items))
	return pods, nil
}

func (c *K8sClient) PatchNode(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions) (*corev1.Node, error) {
	klog.V(4).InfoS("Patching node", "node", name, "patchType", pt)
	node, err := c.client.CoreV1().Nodes().Patch(ctx, name, pt, data, opts)
	if err != nil {
		klog.ErrorS(err, "Failed to patch node", "node", name)
		return nil, fmt.Errorf("failed to patch node %s: %w", name, err)
	}
	klog.V(4).InfoS("Successfully patched node", "node", name)
	return node, nil
}

func (c *K8sClient) PatchPod(ctx context.Context, namespace string, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions) (*corev1.Pod, error) {
	klog.V(4).InfoS("Patching pod", "namespace", namespace, "pod", name, "patchType", pt)
	pod, err := c.client.CoreV1().Pods(namespace).Patch(ctx, name, pt, data, opts)
	if err != nil {
		klog.ErrorS(err, "Failed to patch pod", "namespace", namespace, "pod", name)
		return nil, fmt.Errorf("failed to patch pod %s/%s: %w", namespace, name, err)
	}
	klog.V(4).InfoS("Successfully patched pod", "namespace", namespace, "pod", name)
	return pod, nil
}

func (c *K8sClient) CreateNode(ctx context.Context, node *corev1.Node, opts metav1.CreateOptions) (*corev1.Node, error) {
	klog.V(4).InfoS("Creating node", "node", node.Name)
	createdNode, err := c.client.CoreV1().Nodes().Create(ctx, node, opts)
	if err != nil {
		klog.ErrorS(err, "Failed to create node", "node", node.Name)
		return nil, fmt.Errorf("failed to create node %s: %w", node.Name, err)
	}
	klog.V(4).InfoS("Successfully created node", "node", node.Name)
	return createdNode, nil
}

func (c *K8sClient) UpdateNode(ctx context.Context, node *corev1.Node, opts metav1.UpdateOptions) (*corev1.Node, error) {
	klog.V(4).InfoS("Updating node", "node", node.Name)
	updatedNode, err := c.client.CoreV1().Nodes().Update(ctx, node, opts)
	if err != nil {
		klog.ErrorS(err, "Failed to update node", "node", node.Name)
		return nil, fmt.Errorf("failed to update node %s: %w", node.Name, err)
	}
	klog.V(4).InfoS("Successfully updated node", "node", node.Name)
	return updatedNode, nil
}
