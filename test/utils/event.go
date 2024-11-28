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

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

func GetEvents(clientSet *kubernetes.Clientset, namespace string, listOptions metav1.ListOptions) ([]v1.Event, error) {
	events, err := clientSet.CoreV1().Events(namespace).List(context.TODO(), listOptions)
	if err != nil {
		return nil, err
	}

	return events.Items, nil
}

func GetPodEvents(clientSet *kubernetes.Clientset, namespace, podName string) ([]v1.Event, error) {
	listOption := metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.kind=Pod,involvedObject.name=%s", podName),
	}

	events, err := GetEvents(clientSet, namespace, listOption)
	if err != nil {
		klog.Errorf("Failed to list events for pod %s in namespace %s: %v", podName, namespace, err)
		return nil, err
	}

	return events, nil
}
