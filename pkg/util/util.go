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

package util

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/util/client"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

var (
	HandshakeAnnos map[string]string
)

func init() {
	HandshakeAnnos = make(map[string]string)
}

func GetNode(nodename string) (*corev1.Node, error) {
	if nodename == "" {
		klog.ErrorS(nil, "Node name is empty")
		return nil, fmt.Errorf("nodename is empty")
	}

	c := client.GetClient()
	if c == nil {
		return nil, fmt.Errorf("kubernetes client is not initialized")
	}

	klog.V(5).InfoS("Fetching node", "nodeName", nodename)
	n, err := c.CoreV1().Nodes().Get(context.Background(), nodename, metav1.GetOptions{})
	if err != nil {
		switch {
		case apierrors.IsNotFound(err):
			klog.ErrorS(err, "Node not found", "nodeName", nodename)
			return nil, fmt.Errorf("node %s not found", nodename)
		case apierrors.IsUnauthorized(err):
			klog.ErrorS(err, "Unauthorized to access node", "nodeName", nodename)
			return nil, fmt.Errorf("unauthorized to access node %s", nodename)
		default:
			klog.ErrorS(err, "Failed to get node", "nodeName", nodename)
			return nil, fmt.Errorf("failed to get node %s: %v", nodename, err)
		}
	}

	klog.V(5).InfoS("Successfully fetched node", "nodeName", nodename)
	return n, nil
}

func GetPendingPod(ctx context.Context, node string) (*corev1.Pod, error) {
	pod, err := GetAllocatePodByNode(ctx, node)
	if err != nil {
		return nil, err
	}
	if pod != nil {
		return pod, nil
	}
	// filter pods for this node.
	selector := fmt.Sprintf("spec.nodeName=%s", node)
	podListOptions := metav1.ListOptions{
		FieldSelector: selector,
	}
	podlist, err := client.GetClient().CoreV1().Pods("").List(ctx, podListOptions)
	if err != nil {
		return nil, err
	}
	for _, p := range podlist.Items {
		if p.Status.Phase != corev1.PodPending {
			continue
		}
		if _, ok := p.Annotations[BindTimeAnnotations]; !ok {
			continue
		}
		if phase, ok := p.Annotations[DeviceBindPhase]; !ok {
			continue
		} else {
			// Allow both "allocating" and "success" phases for multi-container pods
			// where some containers have already been allocated but others are still pending
			if phase != DeviceBindAllocating && phase != DeviceBindSuccess {
				continue
			}
		}
		if n, ok := p.Annotations[AssignedNodeAnnotations]; !ok {
			continue
		} else {
			if strings.Compare(n, node) == 0 {
				return &p, nil
			}
		}
	}
	return nil, fmt.Errorf("no binding pod found on node %s", node)
}

func GetAllocatePodByNode(ctx context.Context, nodeName string) (*corev1.Pod, error) {
	node, err := client.GetClient().CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if value, ok := node.Annotations[nodelock.NodeLockKey]; ok {
		klog.V(2).Infof("node annotation key is %s, value is %s ", nodelock.NodeLockKey, value)
		_, ns, name, err := nodelock.ParseNodeLock(value)
		if err != nil {
			return nil, err
		}
		if ns == "" || name == "" {
			return nil, nil
		}
		return client.GetClient().CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
	}
	return nil, nil
}

func PatchNodeAnnotations(node *corev1.Node, annotations map[string]string) error {
	if node == nil {
		return fmt.Errorf("node is nil")
	}
	c := client.GetClient()
	if c == nil {
		return fmt.Errorf("kubernetes client is not initialized")
	}
	type patchMetadata struct {
		Annotations map[string]string `json:"annotations,omitempty"`
	}
	type patchNode struct {
		Metadata patchMetadata `json:"metadata"`
	}

	p := patchNode{}
	p.Metadata.Annotations = annotations

	bytes, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = c.CoreV1().Nodes().
		Patch(context.Background(), node.Name, k8stypes.MergePatchType, bytes, metav1.PatchOptions{})
	if err != nil {
		klog.Infoln("annotations=", annotations)
		klog.Infof("patch node %v failed, %v", node.Name, err)
	}
	return err
}

func AllNonSidecarInitContainersSucceeded(pod *corev1.Pod) bool {
	if len(pod.Spec.InitContainers) == 0 {
		return false
	}
	statusByName := make(map[string]corev1.ContainerStatus, len(pod.Status.InitContainerStatuses))
	for _, s := range pod.Status.InitContainerStatuses {
		statusByName[s.Name] = s
	}
	for i := range pod.Spec.InitContainers {
		c := &pod.Spec.InitContainers[i]
		if IsSidecarContainer(c) {
			continue
		}
		s, ok := statusByName[c.Name]
		if !ok || s.State.Terminated == nil || s.State.Terminated.ExitCode != 0 {
			return false
		}
	}
	return true
}

func PatchPodAnnotations(pod *corev1.Pod, annotations map[string]string) error {
	if pod == nil {
		return fmt.Errorf("pod is nil")
	}
	type patchMetadata struct {
		Annotations map[string]string `json:"annotations,omitempty"`
		Labels      map[string]string `json:"labels,omitempty"`
	}
	type patchPod struct {
		Metadata patchMetadata `json:"metadata"`
	}

	p := patchPod{}
	p.Metadata.Annotations = annotations
	label := make(map[string]string)
	if v, ok := annotations[AssignedNodeAnnotations]; ok && v != "" {
		label[AssignedNodeAnnotations] = v
		p.Metadata.Labels = label
	}

	bytes, err := json.Marshal(p)
	if err != nil {
		return err
	}
	klog.V(5).Infof("patch pod %s/%s annotation content is %s", pod.Namespace, pod.Name, string(bytes))
	_, err = client.GetClient().CoreV1().Pods(pod.Namespace).
		Patch(context.Background(), pod.Name, k8stypes.MergePatchType, bytes, metav1.PatchOptions{})
	if err != nil {
		klog.Infof("patch pod %v failed, %v", pod.Name, err)
	}
	return err
}

func PatchPodLabels(namespace, name string, labels map[string]string) error {
	type patchMetadata struct {
		Labels map[string]string `json:"labels,omitempty"`
	}
	type patchPod struct {
		Metadata patchMetadata `json:"metadata"`
	}

	p := patchPod{
		Metadata: patchMetadata{
			Labels: labels,
		},
	}

	bytes, err := json.Marshal(p)
	if err != nil {
		return err
	}
	klog.V(5).InfoS("Patching pod labels", "namespace", namespace, "name", name, "labels", labels)
	_, err = client.GetClient().CoreV1().Pods(namespace).
		Patch(context.Background(), name, k8stypes.MergePatchType, bytes, metav1.PatchOptions{})
	if err != nil {
		klog.ErrorS(err, "Failed to patch pod labels", "namespace", namespace, "name", name)
	}
	return err
}

func InitKlogFlags() *flag.FlagSet {
	// Init log flags
	flagset := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(flagset)

	return flagset
}

func MarkAnnotationsToDelete(devType string, nn string) error {
	n, err := GetNode(nn)
	if err != nil {
		klog.Errorln("get node failed", err.Error())
		return err
	}
	return RemoveNodeAnnotation(n, devType)
}

func RemoveNodeAnnotation(node *corev1.Node, annotationKeys ...string) error {
	if node == nil {
		return fmt.Errorf("node is nil")
	}
	annos := make(map[string]any, len(annotationKeys))
	for _, key := range annotationKeys {
		annos[key] = nil
	}
	patch := map[string]any{
		"metadata": map[string]any{
			"annotations": annos,
		},
	}
	bytes, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	c := client.GetClient()
	if c == nil {
		return fmt.Errorf("kubernetes client is not initialized")
	}
	_, err = c.CoreV1().Nodes().
		Patch(context.Background(), node.Name, k8stypes.MergePatchType, bytes, metav1.PatchOptions{})
	if err != nil {
		klog.Infoln("remove annotation failed for node", node.Name, "annotationKeys", annotationKeys)
	}
	return err
}

// IsValidGPUSchedulerPolicy reports whether policy names GPU scheduling
// policies HAMi recognizes. The value is read as a comma-separated ordered
// list, the same way PolicyContains and the sort-key chain read it, so
// "binpack,numa" and "mutex,topology-aware" are both accepted. An empty or
// blank value is not.
func IsValidGPUSchedulerPolicy(policy string) bool {
	if strings.TrimSpace(policy) == "" {
		return false
	}
	for p := range strings.SplitSeq(policy, ",") {
		switch SchedulerPolicyName(strings.TrimSpace(p)) {
		case GPUSchedulerPolicyBinpack, GPUSchedulerPolicySpread, GPUSchedulerPolicyNuma,
			GPUSchedulerPolicyMutex, GPUSchedulerPolicyTopology:
		default:
			return false
		}
	}
	return true
}

// IsValidNodeSchedulerPolicy reports whether policy names a node scheduling
// policy HAMi recognizes. NodeScoreList.Less has no chain form, so only a
// single name is meaningful here. The value is trimmed before it is compared,
// so surrounding whitespace is read the same way IsValidGPUSchedulerPolicy
// reads it.
func IsValidNodeSchedulerPolicy(policy string) bool {
	switch SchedulerPolicyName(strings.TrimSpace(policy)) {
	case NodeSchedulerPolicyBinpack, NodeSchedulerPolicySpread:
		return true
	default:
		return false
	}
}

// GetGPUSchedulerPolicyByPod returns the GPU scheduling policy to apply to
// task, preferring its GPUSchedulerPolicyAnnotationKey annotation over
// defaultPolicy. An annotation that does not name a known policy is ignored
// with a warning, so defaultPolicy stands.
func GetGPUSchedulerPolicyByPod(defaultPolicy string, task *corev1.Pod) string {
	userGPUPolicy := defaultPolicy
	if task != nil && task.Annotations != nil {
		if value, ok := task.Annotations[GPUSchedulerPolicyAnnotationKey]; ok {
			if IsValidGPUSchedulerPolicy(value) {
				userGPUPolicy = value
			} else {
				klog.Warningf("ignoring unrecognized %s=%q on pod %s/%s, using configured policy %q",
					GPUSchedulerPolicyAnnotationKey, value, task.Namespace, task.Name, defaultPolicy)
			}
		}
	}
	return userGPUPolicy
}

// PolicyContains reports whether policy names name, treating policy as a
// comma-separated ordered list (e.g. "binpack,numa"). A single value with no
// comma is compared directly, so existing single-policy callers are unaffected.
func PolicyContains(policy string, name SchedulerPolicyName) bool {
	target := name.String()
	for p := range strings.SplitSeq(policy, ",") {
		if strings.TrimSpace(p) == target {
			return true
		}
	}
	return false
}

func IsPodInTerminatedState(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	return pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded
}

func IsPodTerminating(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	return pod.DeletionTimestamp != nil
}

func AllContainersCreated(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	return len(pod.Status.ContainerStatuses) >= len(pod.Spec.Containers)
}

func IsSidecarContainer(c *corev1.Container) bool {
	return c != nil && c.RestartPolicy != nil &&
		*c.RestartPolicy == corev1.ContainerRestartPolicyAlways
}

// EmitNodeWarningEvent emits a Warning event on the given Node with deduplication.
func EmitNodeWarningEvent(node *corev1.Node, reason, message string, dedupWindow time.Duration) {
	c := client.GetClient()
	if c == nil {
		klog.Warningf("cannot emit node event for %s: Kubernetes client not initialized", node.Name)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fieldSel := fmt.Sprintf(
		"involvedObject.kind=Node,involvedObject.name=%s,involvedObject.uid=%s,reason=%s",
		node.Name, string(node.UID), reason,
	)
	existing, err := c.CoreV1().Events(corev1.NamespaceDefault).List(ctx, metav1.ListOptions{
		FieldSelector: fieldSel,
	})
	if err != nil {
		klog.Warningf("failed to list events for node %s: %v; will attempt create", node.Name, err)
	}

	now := metav1.Now()

	if err == nil && len(existing.Items) > 0 {
		// Client-side filter: the field selector is a server-side optimization; re-check
		// here so the function is correct even against fake clients or non-compliant servers.
		var latest *corev1.Event
		for i := range existing.Items {
			ev := &existing.Items[i]
			if ev.InvolvedObject.UID != node.UID || ev.Reason != reason {
				continue
			}
			if latest == nil || ev.LastTimestamp.After(latest.LastTimestamp.Time) {
				latest = ev
			}
		}
		if latest != nil && now.Sub(latest.LastTimestamp.Time) <= dedupWindow {
			latest.Count++
			latest.LastTimestamp = now
			latest.Message = message
			if _, err := c.CoreV1().Events(corev1.NamespaceDefault).Update(ctx, latest, metav1.UpdateOptions{}); err != nil {
				klog.Warningf("failed to update node event for %s: %v", node.Name, err)
			}
			return
		}
	}

	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: node.Name + "-",
			Namespace:    corev1.NamespaceDefault,
		},
		InvolvedObject: corev1.ObjectReference{
			APIVersion: "v1",
			Kind:       "Node",
			Name:       node.Name,
			UID:        node.UID,
		},
		Reason:         reason,
		Message:        message,
		Type:           corev1.EventTypeWarning,
		Count:          1,
		FirstTimestamp: now,
		LastTimestamp:  now,
		Source:         corev1.EventSource{Component: "hami-device-plugin"},
	}
	if _, err := c.CoreV1().Events(corev1.NamespaceDefault).Create(ctx, event, metav1.CreateOptions{}); err != nil {
		klog.Warningf("failed to create node event for %s: %v", node.Name, err)
	}
}

func IsPodGroupMember(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	if pod.Labels[PodGroupLabel] != "" {
		return true
	}
	if sg := pod.Spec.SchedulingGroup; sg != nil && sg.PodGroupName != nil && *sg.PodGroupName != "" {
		return true
	}
	return false
}
