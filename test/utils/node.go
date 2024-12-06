package utils

import (
	"context"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

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

	return updatedNode, nil
}

func AddNodeLabel(clientSet *kubernetes.Clientset, nodeName, labelKey, labelValue string) (*v1.Node, error) {
	node, err := clientSet.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}
	node.Labels[labelKey] = labelValue

	return UpdateNode(clientSet, node)
}

func RemoveNodeLabel(clientSet *kubernetes.Clientset, nodeName, labelKey string) (*v1.Node, error) {
	node, err := clientSet.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	if node.Labels != nil {
		delete(node.Labels, labelKey)
	}

	return UpdateNode(clientSet, node)
}
