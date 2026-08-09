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
	"k8s.io/apimachinery/pkg/util/validation"
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
	_, err = client.GetClient().CoreV1().Nodes().
		Patch(context.Background(), node.Name, k8stypes.MergePatchType, bytes, metav1.PatchOptions{})
	if err != nil {
		klog.Infoln("annotations=", annotations)
		klog.Infof("patch node %v failed, %v", node.Name, err)
	}
	return err
}

func PatchPodAnnotations(pod *corev1.Pod, annotations map[string]string) error {
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
		if errs := validation.IsValidLabelValue(v); len(errs) == 0 {
			label[AssignedNodeAnnotations] = v
			p.Metadata.Labels = label
		} else {
			klog.Warningf("Node name %s is not a valid label value (errs: %v), omitting label %s for pod %s/%s", v, errs, AssignedNodeAnnotations, pod.Namespace, pod.Name)
		}
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

func GetGPUSchedulerPolicyByPod(defaultPolicy string, task *corev1.Pod) string {
	userGPUPolicy := defaultPolicy
	if task != nil && task.Annotations != nil {
		if value, ok := task.Annotations[GPUSchedulerPolicyAnnotationKey]; ok {
			userGPUPolicy = value
		}
	}
	return userGPUPolicy
}

func IsPodInTerminatedState(pod *corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded
}

func IsPodTerminating(pod *corev1.Pod) bool {
	return pod.DeletionTimestamp != nil
}

func AllContainersCreated(pod *corev1.Pod) bool {
	return len(pod.Status.ContainerStatuses) >= len(pod.Spec.Containers)
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
