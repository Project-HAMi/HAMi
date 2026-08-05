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

	"github.com/Project-HAMi/HAMi/pkg/device"
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
	return &K8sClient{clientset: clientset}, nil
}

// NewK8sClientFromInterface creates a K8sClient backed by an existing kubernetes.Interface.
// It is intended for tests and callers that want to inject a fake or shared clientset.
func NewK8sClientFromInterface(clientset kubernetes.Interface) *K8sClient {
	return &K8sClient{clientset: clientset}
}

func getKubeConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	if config, err := rest.InClusterConfig(); err == nil {
		return config, nil
	}
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

// gpuCountResourceNames returns the "count" resource for every registered
// device backend (e.g. nvidia.com/gpu, huawei.com/Ascend910). It is derived
// from device.GetDevices() so a new vendor needs no change here.
func gpuCountResourceNames() []corev1.ResourceName {
	devs := device.GetDevices()
	names := make([]corev1.ResourceName, 0, len(devs))
	for _, dev := range devs {
		if n := dev.GetResourceNames().ResourceCountName; n != "" {
			names = append(names, corev1.ResourceName(n))
		}
	}
	return names
}

// gpuRequestResourceNames additionally includes each backend's memory/core
// resources, since some pods only set those and not the count resource.
func gpuRequestResourceNames() []corev1.ResourceName {
	var names []corev1.ResourceName
	for _, dev := range device.GetDevices() {
		rn := dev.GetResourceNames()
		for _, n := range []string{rn.ResourceCountName, rn.ResourceMemoryName, rn.ResourceCoreName} {
			if n != "" {
				names = append(names, corev1.ResourceName(n))
			}
		}
	}
	return names
}

func hasGPUResources(node *corev1.Node) bool {
	for _, rn := range gpuCountResourceNames() {
		if q, ok := node.Status.Capacity[rn]; ok && !q.IsZero() {
			return true
		}
	}
	return false
}

func hasGPURequests(pod *corev1.Pod) bool {
	names := gpuRequestResourceNames()
	check := func(ctrs []corev1.Container) bool {
		for _, c := range ctrs {
			for _, rn := range names {
				if q, ok := c.Resources.Limits[rn]; ok && !q.IsZero() {
					return true
				}
				if q, ok := c.Resources.Requests[rn]; ok && !q.IsZero() {
					return true
				}
			}
		}
		return false
	}
	return check(pod.Spec.Containers) || check(pod.Spec.InitContainers)
}

// ListGPUNodes lists nodes with GPU resources from any registered vendor.
func (c *K8sClient) ListGPUNodes(ctx context.Context, labelSelector string) ([]*corev1.Node, error) {
	nodes, err := c.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}
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

// listPodsPageSize bounds memory use when an MCP caller asks for pods across
// all namespaces; pods are paginated by the apiserver.
const listPodsPageSize int64 = 500

// ListGPUPods lists pods with GPU resource requests/limits from any
// registered vendor, paginating through the apiserver.
func (c *K8sClient) ListGPUPods(ctx context.Context, namespace string, phase string) ([]*corev1.Pod, error) {
	listOptions := metav1.ListOptions{Limit: listPodsPageSize}
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
			// The fake clientset used in tests ignores fieldSelector; re-apply here
			// so tests and any apiserver that ignores the selector still work.
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

// GetNode gets a specific node by name.
func (c *K8sClient) GetNode(ctx context.Context, name string) (*corev1.Node, error) {
	node, err := c.clientset.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get node %s: %w", name, err)
	}
	return node, nil
}

// GetNamespace gets a specific namespace by name.
func (c *K8sClient) GetNamespace(ctx context.Context, name string) (*corev1.Namespace, error) {
	ns, err := c.clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get namespace %s: %w", name, err)
	}
	return ns, nil
}

// GetConfigMap gets a specific ConfigMap by namespace and name.
func (c *K8sClient) GetConfigMap(ctx context.Context, namespace, name string) (*corev1.ConfigMap, error) {
	cm, err := c.clientset.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get configmap %s/%s: %w", namespace, name, err)
	}
	return cm, nil
}

// ListResourceQuotas lists ResourceQuotas in a namespace, used by get_quota_usage
// to read the configured hard limits alongside HAMi's own live usage tracking.
func (c *K8sClient) ListResourceQuotas(ctx context.Context, namespace string) ([]corev1.ResourceQuota, error) {
	list, err := c.clientset.CoreV1().ResourceQuotas(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list resourcequotas in %s: %w", namespace, err)
	}
	return list.Items, nil
}
