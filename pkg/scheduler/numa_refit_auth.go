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

package scheduler

import (
	"context"
	"fmt"

	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

// Bound service account tokens carry the pod they were minted for in these
// well-known Extra keys. They are only populated for bound/projected tokens,
// which every pod gets by default since Kubernetes 1.21.
const (
	boundPodNameExtraKey = "authentication.kubernetes.io/pod-name"
	boundPodUIDExtraKey  = "authentication.kubernetes.io/pod-uid"
)

// authenticateRefitCaller verifies that token is a live, bound ServiceAccount
// token belonging to the device-plugin's ServiceAccount (config.DevicePluginNamespace
// / config.DevicePluginServiceAccount), and that the pod it is bound to runs
// on nodeName. Without this, any in-cluster caller that can reach the
// scheduler Service could ask it to move another pod's device allocation.
// See issue #2878.
func (s *Scheduler) authenticateRefitCaller(ctx context.Context, token, nodeName string) error {
	if token == "" {
		return fmt.Errorf("no caller credentials presented")
	}

	review, err := client.GetClient().AuthenticationV1().TokenReviews().Create(ctx, &authenticationv1.TokenReview{
		Spec: authenticationv1.TokenReviewSpec{Token: token},
	}, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("token review request failed: %w", err)
	}
	if !review.Status.Authenticated {
		return fmt.Errorf("token is not authenticated: %s", review.Status.Error)
	}

	expectedUsername := fmt.Sprintf("system:serviceaccount:%s:%s", config.DevicePluginNamespace, config.DevicePluginServiceAccount)
	if review.Status.User.Username != expectedUsername {
		return fmt.Errorf("caller %q is not the device-plugin service account", review.Status.User.Username)
	}

	podName := firstExtraValue(review.Status.User.Extra, boundPodNameExtraKey)
	podUID := firstExtraValue(review.Status.User.Extra, boundPodUIDExtraKey)
	if podName == "" || podUID == "" {
		return fmt.Errorf("token is not a bound pod service account token")
	}

	pod, err := s.podLister.Pods(config.DevicePluginNamespace).Get(podName)
	if err != nil {
		return fmt.Errorf("cannot look up caller pod %s/%s: %w", config.DevicePluginNamespace, podName, err)
	}
	if string(pod.UID) != podUID {
		return fmt.Errorf("caller pod %s/%s no longer matches the token's bound UID", config.DevicePluginNamespace, podName)
	}
	if pod.Spec.NodeName != nodeName {
		return fmt.Errorf("caller pod %s/%s runs on node %q, not %q", config.DevicePluginNamespace, podName, pod.Spec.NodeName, nodeName)
	}
	return nil
}

// firstExtraValue extracts the first string value associated with key in extra,
// or returns "" if the key is missing or has no associated values.
func firstExtraValue(extra map[string]authenticationv1.ExtraValue, key string) string {
	values, ok := extra[key]
	if !ok || len(values) == 0 {
		return ""
	}
	return values[0]
}
