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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	klog "k8s.io/klog/v2"
)

// K8sClient wraps the Kubernetes client for read-only operations.
type K8sClient struct {
	clientset kubernetes.Interface
}

// NewK8sClient creates a new Kubernetes client.
// If kubeconfig is empty, it will try in-cluster config first, then fall back to ~/.kube/config.
func NewK8sClient(kubeconfig string) (*K8sClient, error) {
	config, err := getKubeConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to get kubeconfig: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return &K8sClient{
		clientset: clientset,
	}, nil
}

// NewK8sClientFromInterface creates a K8sClient backed by an existing kubernetes.Interface.
// It is intended for tests and callers that want to inject a fake or shared clientset.
func NewK8sClientFromInterface(clientset kubernetes.Interface) *K8sClient {
	return &K8sClient{
		clientset: clientset,
	}
}

// getKubeConfig returns the Kubernetes configuration.
func getKubeConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}

	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err == nil {
		return config, nil
	}

	// Fall back to ~/.kube/config
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	kubeconfigPath := filepath.Join(home, ".kube", "config")
	if _, err := os.Stat(kubeconfigPath); err == nil {
		return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	}

	return nil, fmt.Errorf("no kubeconfig found and not running in-cluster")
}

// ListGPUNodes lists nodes with GPU resources.
func (c *K8sClient) ListGPUNodes(ctx context.Context, labelSelector string) ([]*corev1.Node, error) {
	nodes, err := c.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	// Filter nodes with GPU resources
	var gpuNodes []*corev1.Node
	for i := range nodes.Items {
		node := &nodes.Items[i]
		if hasGPUResources(node) {
			gpuNodes = append(gpuNodes, node)
		}
	}

	klog.V(4).InfoS("Listed GPU nodes", "count", len(gpuNodes), "labelSelector", labelSelector)
	return gpuNodes, nil
}

// hasGPUResources checks if a node has GPU resources.
func hasGPUResources(node *corev1.Node) bool {
	gpuResourceNames := []string{
		"nvidia.com/gpu",
		"nvidia.com/gpumem",
		"nvidia.com/gpucores",
		"cambricon.com/vmlu",
		"hygon.com/dcunum",
		"metax-tech.com/sgpu",
		"enflame.com/drs-gcu",
		"kunlunxin.com/xpu",
		"vastaitech.com/va",
	}

	for _, resourceName := range gpuResourceNames {
		if _, ok := node.Status.Capacity[corev1.ResourceName(resourceName)]; ok {
			return true
		}
	}

	return false
}

// listPodsPageSize bounds memory use when an MCP caller asks for pods
// across all namespaces. Pods are paginated by the apiserver and the
// per-page filter runs in this loop, so this is also the upper bound on the
// allocation we hold for one page.
const listPodsPageSize int64 = 500

// ListGPUPods lists pods that have GPU resource requests. It paginates
// through results from the apiserver and pushes the phase filter into a
// fieldSelector so the apiserver can short-circuit on its side.
func (c *K8sClient) ListGPUPods(ctx context.Context, namespace string, phase string) ([]*corev1.Pod, error) {
	listOptions := metav1.ListOptions{
		Limit: listPodsPageSize,
	}
	if phase != "" {
		listOptions.FieldSelector = "status.phase=" + phase
	}

	var gpuPods []*corev1.Pod
	for {
		var pods *corev1.PodList
		var err error
		if namespace == "" {
			pods, err = c.clientset.CoreV1().Pods("").List(ctx, listOptions)
		} else {
			pods, err = c.clientset.CoreV1().Pods(namespace).List(ctx, listOptions)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to list pods: %w", err)
		}

		for i := range pods.Items {
			pod := &pods.Items[i]
			// Phase fieldSelector is honored by the real apiserver but not
			// by client-go's fake; re-apply the filter here so tests and
			// any apiserver that ignores the selector still get a correct
			// result.
			if phase != "" && string(pod.Status.Phase) != phase {
				continue
			}
			if hasGPURequests(pod) {
				gpuPods = append(gpuPods, pod)
			}
		}

		if pods.Continue == "" {
			break
		}
		listOptions.Continue = pods.Continue
	}

	klog.V(4).InfoS("Listed GPU pods", "count", len(gpuPods), "namespace", namespace, "phase", phase)
	return gpuPods, nil
}

// hasGPURequests checks if a pod has GPU resource requests.
func hasGPURequests(pod *corev1.Pod) bool {
	gpuResourceNames := []string{
		"nvidia.com/gpu",
		"nvidia.com/gpumem",
		"nvidia.com/gpucores",
		"cambricon.com/vmlu",
		"hygon.com/dcunum",
		"metax-tech.com/sgpu",
		"enflame.com/drs-gcu",
		"kunlunxin.com/xpu",
		"vastaitech.com/va",
	}

	for _, container := range pod.Spec.Containers {
		for _, resourceName := range gpuResourceNames {
			if container.Resources.Requests != nil {
				if _, ok := container.Resources.Requests[corev1.ResourceName(resourceName)]; ok {
					return true
				}
			}
		}
	}

	return false
}

// GetNode gets a specific node by name.
func (c *K8sClient) GetNode(ctx context.Context, name string) (*corev1.Node, error) {
	node, err := c.clientset.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get node %s: %w", name, err)
	}

	klog.V(4).InfoS("Got node", "name", name)
	return node, nil
}

// GetNamespace gets a specific namespace by name.
func (c *K8sClient) GetNamespace(ctx context.Context, name string) (*corev1.Namespace, error) {
	ns, err := c.clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get namespace %s: %w", name, err)
	}

	klog.V(4).InfoS("Got namespace", "name", name)
	return ns, nil
}

// GetConfigMap gets a specific configmap by namespace and name.
func (c *K8sClient) GetConfigMap(ctx context.Context, namespace, name string) (*corev1.ConfigMap, error) {
	cm, err := c.clientset.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get configmap %s/%s: %w", namespace, name, err)
	}

	klog.V(4).InfoS("Got configmap", "namespace", namespace, "name", name)
	return cm, nil
}
