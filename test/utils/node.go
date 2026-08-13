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

package utils

import (
	"context"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
)

// GetGPUNode returns the name of the first node that has nvidia.com/gpu capacity.
// It falls back to the first node in the cluster if no GPU node is found.
func GetGPUNode(clientSet *kubernetes.Clientset) (string, error) {
	nodes, err := clientSet.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return "", err
	}
	for _, node := range nodes.Items {
		if _, ok := node.Status.Capacity["nvidia.com/gpu"]; ok {
			return node.Name, nil
		}
	}
	if len(nodes.Items) > 0 {
		return nodes.Items[0].Name, nil
	}
	return "", fmt.Errorf("no nodes found in the cluster")
}

func GetNodes(clientSet *kubernetes.Clientset) (*v1.NodeList, error) {
	nodes, err := clientSet.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		klog.Errorf("Failed to get nodes: %v", err)
		return nil, err
	}

	return nodes, nil
}

func UpdateNode(clientSet *kubernetes.Clientset, node *v1.Node) (*v1.Node, error) {
	updatedNode, err := clientSet.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	if err != nil {
		klog.Errorf("Failed to update node %s: %v", node.Name, err)
		return nil, err
	}

	time.Sleep(time.Second * 30)
	return updatedNode, nil
}

func AddNodeLabel(clientSet *kubernetes.Clientset, nodeName, labelKey, labelValue string) (*v1.Node, error) {
	var updatedNode *v1.Node
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		node, err := clientSet.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if node.Labels == nil {
			node.Labels = make(map[string]string)
		}

		if val, ok := node.Labels[labelKey]; ok && val == labelValue {
			updatedNode = node
			return nil // Label already set to the desired value
		}

		node.Labels[labelKey] = labelValue

		updatedNode, err = UpdateNode(clientSet, node)
		return err
	})

	if err != nil {
		return nil, err
	}
	return updatedNode, nil
}

func RemoveNodeLabel(clientSet *kubernetes.Clientset, nodeName, labelKey string) (*v1.Node, error) {
	var updatedNode *v1.Node
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		node, err := clientSet.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if node.Labels != nil {
			if _, ok := node.Labels[labelKey]; !ok {
				updatedNode = node
				return nil // Label already removed
			}
			delete(node.Labels, labelKey)
		} else {
			updatedNode = node
			return nil
		}

		updatedNode, err = UpdateNode(clientSet, node)
		return err
	})

	if err != nil {
		return nil, err
	}
	return updatedNode, nil
}
