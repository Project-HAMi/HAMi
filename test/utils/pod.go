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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

var Pod = &corev1.Pod{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "gpu-pod",
		Namespace: "default",
	},
	Spec: corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name:    "cuda-container",
				Image:   "nvcr.io/nvidia/k8s/cuda-sample:vectoradd-cuda12.5.0",
				Command: []string{"/bin/sh"},
				Args:    []string{"-c", "sleep 86400"},
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu":      resource.MustParse("1"),
						"nvidia.com/gpumem":   resource.MustParse(GPUPodMemory),
						"nvidia.com/gpucores": resource.MustParse(GPUPodCore),
					},
				},
			},
		},
	},
}

func GetPods(clientSet *kubernetes.Clientset, namespace string) (*corev1.PodList, error) {
	pods, err := clientSet.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		klog.Errorf("Failed to list Pods in namespace %s: %v", namespace, err)
		return nil, err
	}

	return pods, nil
}

func CreatePod(clientSet *kubernetes.Clientset, pod *corev1.Pod, namespace string) (*corev1.Pod, error) {
	time.Sleep(15 * time.Second)
	createdPod, err := clientSet.CoreV1().Pods(namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
	if err != nil {
		klog.Errorf("Failed to create Pod %s in namespace %s: %v", pod.Name, namespace, err)
		return nil, err
	}

	return createdPod, nil
}

func DeletePod(clientSet *kubernetes.Clientset, namespace, podName string) error {
	err := clientSet.CoreV1().Pods(namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{})
	if err != nil {
		klog.Errorf("Failed to delete Pod %s in namespace %s: %v", podName, namespace, err)
		return err
	}
	return nil
}

func WaitForPodRunning(clientSet kubernetes.Interface, namespace, podName string) error {
	const (
		checkInterval = 5 * time.Second // Interval for checking Pod status
		timeout       = 5 * time.Minute // Increased timeout for GPU Pods
	)

	return wait.PollImmediate(checkInterval, timeout, func() (bool, error) {
		// Fetch the Pod object from the Kubernetes API
		pod, err := clientSet.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
		if err != nil {
			return false, fmt.Errorf("failed to get pod %s/%s: %v", namespace, podName, err)
		}

		// Print Pod status for debugging
		fmt.Printf("Pod %s/%s status: %s\n", namespace, podName, pod.Status.Phase)

		// Check if the Pod is in the Running state
		if pod.Status.Phase == corev1.PodRunning {
			return true, nil
		}

		// Check if the Pod is in a Failed or Unknown state
		if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodUnknown {
			return false, fmt.Errorf("pod %s/%s is in failed or unknown state: %s", namespace, podName, pod.Status.Phase)
		}

		// Print Pod events for debugging
		events, err := clientSet.CoreV1().Events(namespace).List(context.TODO(), metav1.ListOptions{
			FieldSelector: fmt.Sprintf("involvedObject.name=%s", podName),
		})
		if err == nil {
			for _, event := range events.Items {
				fmt.Printf("Event: %s - %s\n", event.Reason, event.Message)
			}
		}

		// If the Pod is not in Running, Failed, or Unknown state, continue waiting
		return false, nil
	})
}
