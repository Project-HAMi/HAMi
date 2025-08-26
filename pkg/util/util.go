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

	klog.InfoS("Fetching node", "nodeName", nodename)
	n, err := client.GetClient().CoreV1().Nodes().Get(context.Background(), nodename, metav1.GetOptions{})
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

	klog.InfoS("Successfully fetched node", "nodeName", nodename)
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
			if strings.Compare(phase, DeviceBindAllocating) != 0 {
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
	type patchPod struct {
		Metadata patchMetadata `json:"metadata"`
		//Spec     patchSpec     `json:"spec,omitempty"`
	}

	p := patchPod{}
	p.Metadata.Annotations = annotations

	bytes, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = client.GetClient().CoreV1().Nodes().
		Patch(context.Background(), node.Name, k8stypes.StrategicMergePatchType, bytes, metav1.PatchOptions{})
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
		//Spec     patchSpec     `json:"spec,omitempty"`
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
		Patch(context.Background(), pod.Name, k8stypes.StrategicMergePatchType, bytes, metav1.PatchOptions{})
	if err != nil {
		klog.Infof("patch pod %v failed, %v", pod.Name, err)
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
	tmppat := make(map[string]string)
	tmppat[devType] = "Deleted_" + time.Now().Format(time.DateTime)
	n, err := GetNode(nn)
	if err != nil {
		klog.Errorln("get node failed", err.Error())
		return err
	}
	return PatchNodeAnnotations(n, tmppat)
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

func AllContainersCreated(pod *corev1.Pod) bool {
	return len(pod.Status.ContainerStatuses) >= len(pod.Spec.Containers)
}
