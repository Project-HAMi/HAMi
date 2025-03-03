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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// 统一的 Kubernetes 客户端接口.
type KubeInterface interface {
	GetNode(ctx context.Context, name string, opts metav1.GetOptions) (*corev1.Node, error)
	ListPods(ctx context.Context, namespace string, opts metav1.ListOptions) (*corev1.PodList, error)
	GetPod(ctx context.Context, namespace, name string, opts metav1.GetOptions) (*corev1.Pod, error)
	PatchNode(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions) (*corev1.Node, error)
	PatchPod(ctx context.Context, namespace string, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions) (*corev1.Pod, error)
	CreateNode(ctx context.Context, node *corev1.Node, opts metav1.CreateOptions) (*corev1.Node, error)
}
