/*
Copyright 2026 The HAMi Authors.

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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

// schedulerSelector finds the scheduler Deployment the same way WaitForDevicePluginReady
// finds device-plugin pods: by its app.kubernetes.io/component label, since the
// Deployment's own name depends on the Helm release name and isn't fixed.
const schedulerSelector = "app.kubernetes.io/component=" + HamiScheduler

// RestartHamiScheduler triggers a rollout restart of the scheduler Deployment in
// namespace, the same way `kubectl rollout restart` does: by patching the pod
// template with a restartedAt annotation so the Deployment controller replaces
// its pods. Call WaitForHamiSchedulerReady afterward to wait for the new pods.
func RestartHamiScheduler(clientSet *kubernetes.Clientset, namespace string) error {
	ctx := context.TODO()
	deployments, err := clientSet.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: schedulerSelector,
	})
	if err != nil {
		return err
	}
	if len(deployments.Items) == 0 {
		return fmt.Errorf("no deployment labeled %s found in namespace %s", schedulerSelector, namespace)
	}
	patch := []byte(fmt.Sprintf(
		`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":%q}}}}}`,
		time.Now().Format(time.RFC3339)))
	for i := range deployments.Items {
		name := deployments.Items[i].Name
		if _, err := clientSet.AppsV1().Deployments(namespace).Patch(ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{}); err != nil {
			return fmt.Errorf("restart deployment %s: %w", name, err)
		}
	}
	return nil
}

// WaitForHamiSchedulerReady waits until every scheduler Deployment in namespace has
// rolled its latest generation all the way out: observed by the controller, and
// every desired replica updated and available. This is the same condition
// `kubectl rollout status` waits for.
func WaitForHamiSchedulerReady(clientSet *kubernetes.Clientset, namespace string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(context.TODO(), 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		deployments, err := clientSet.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: schedulerSelector,
		})
		if err != nil || len(deployments.Items) == 0 {
			return false, nil
		}
		for i := range deployments.Items {
			dep := &deployments.Items[i]
			if dep.Status.ObservedGeneration < dep.Generation {
				return false, nil
			}
			want := int32(1)
			if dep.Spec.Replicas != nil {
				want = *dep.Spec.Replicas
			}
			if dep.Status.UpdatedReplicas < want || dep.Status.AvailableReplicas < want {
				return false, nil
			}
		}
		return true, nil
	})
}
