// Copyright 2021 Cambricon, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mlu

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strconv"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
)

func (m *CambriconDevicePlugin) getCandidatePods(ctx context.Context) ([]*v1.Pod, error) {
	candidatePods := []*v1.Pod{}
	allPods, err := m.getPendingPodsInNode(ctx)
	if err != nil {
		return candidatePods, err
	}
	for _, pod := range allPods {
		current := pod
		if isMLUMemoryAssumedPod(&current) {
			candidatePods = append(candidatePods, &current)
		}
	}
	sort.Slice(candidatePods, func(i, j int) bool {
		return getAssumeTimeFromPodAnnotation(candidatePods[i]) < getAssumeTimeFromPodAnnotation(candidatePods[j])
	})
	return candidatePods, nil
}

func (m *CambriconDevicePlugin) getPendingPodsInNode(ctx context.Context) ([]v1.Pod, error) {
	pods := []v1.Pod{}
	podMap := make(map[types.UID]bool)

	selector := fields.SelectorFromSet(fields.Set{"spec.nodeName": m.nodeHostname, "status.phase": "Pending"})
	podList, err := m.clientset.CoreV1().Pods(v1.NamespaceAll).List(ctx, metav1.ListOptions{
		FieldSelector: selector.String(),
	})
	for i := 0; i < retries && err != nil; i++ {
		log.Printf("list pods error %v, retried %d times", err, i)
		time.Sleep(100 * time.Second)
		podList, err = m.clientset.CoreV1().Pods(v1.NamespaceAll).List(ctx, metav1.ListOptions{
			FieldSelector: selector.String(),
		})
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get pending pods assigned to node %v", m.nodeHostname)
	}

	for _, pod := range podList.Items {
		if _, ok := podMap[pod.UID]; !ok {
			pods = append(pods, pod)
			podMap[pod.UID] = true
		}
	}
	return pods, nil
}

func isMLUMemoryAssumedPod(pod *v1.Pod) bool {
	if !requestsMLUMemory(pod) {
		return false
	}

	if _, ok := pod.ObjectMeta.Annotations[mluMemResourceAssumeTime]; !ok {
		return false
	}

	if assigned, ok := pod.ObjectMeta.Annotations[mluMemResourceAssigned]; ok && assigned == "false" {
		return true
	}

	return false
}

func requestsMLUMemory(pod *v1.Pod) bool {
	r := false
	for _, c := range pod.Spec.Containers {
		if _, ok := c.Resources.Limits[v1.ResourceName(mluMemResourceName)]; ok {
			r = true
			break
		}
	}
	return r
}

func getAssumeTimeFromPodAnnotation(pod *v1.Pod) (assumeTime uint64) {
	if assumeTimeStr, ok := pod.ObjectMeta.Annotations[mluMemResourceAssumeTime]; ok {
		u64, err := strconv.ParseUint(assumeTimeStr, 10, 64)
		if err != nil {
			log.Printf("Failed to parse assume Timestamp %s due to %v", assumeTimeStr, err)
		} else {
			assumeTime = u64
		}
	}
	return assumeTime
}

func getIndexFromAnnotation(pod *v1.Pod) (uint, error) {
	value, found := pod.ObjectMeta.Annotations[mluMemSplitIndex]
	if !found {
		return 0, fmt.Errorf("pod annotation %s not found", mluMemSplitIndex)
	}
	index, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("strconv value %v, %v", value, err)
	}
	if index < 0 {
		return 0, fmt.Errorf("index %d less than 0", index)
	}
	return uint(index), nil
}

func podContainerCountWithMlu(pod *v1.Pod) uint {
	count := 0
	for _, c := range pod.Spec.InitContainers {
		if _, ok := c.Resources.Limits[v1.ResourceName(mluMemResourceName)]; ok {
			count++
			log.Printf("namespace %s pod %s init container %s uses mlu-mem, just allocate the mlu and ignore memory limit", pod.Namespace, pod.Name, c.Name)
		}
	}
	for _, c := range pod.Spec.Containers {
		if _, ok := c.Resources.Limits[v1.ResourceName(mluMemResourceName)]; ok {
			count++
		}
	}
	return uint(count)
}

func (m *CambriconDevicePlugin) releaseNodeLock() error {
	node, err := m.clientset.CoreV1().Nodes().Get(context.TODO(), m.nodeHostname, metav1.GetOptions{})

	if err != nil {
		return err
	}
	newNode := node.DeepCopy()

	if newNode.Annotations != nil {
		if time, ok := newNode.Annotations[mluMemLock]; ok {
			log.Printf("node lock timestamp %s", time)
			delete(newNode.Annotations, mluMemLock)
		} else {
			log.Println("Lock is released, No Need to update node")
			return nil
		}
	}
	_, err = m.clientset.CoreV1().Nodes().Update(context.TODO(), newNode, metav1.UpdateOptions{})

	if err != nil {
		log.Printf("Failed to release node lock %s, err %v", mluMemLock, err)
	} else {
		log.Printf("release node lock %s successfully.", mluMemLock)
	}
	return err
}

func (m *CambriconDevicePlugin) patchMLUCount(count int) error {

	patchAnnotations := map[string]interface{}{
		"metadata": map[string]map[string]string{"annotations": {
			mluResourceCount: fmt.Sprintf("%d", count),
		}}}

	b, err := json.Marshal(patchAnnotations)
	if err != nil {
		return err
	}

	_, err = m.clientset.CoreV1().Nodes().Patch(context.TODO(), m.nodeHostname, types.StrategicMergePatchType, b, metav1.PatchOptions{})
	if err != nil {
		log.Printf("Failed to update Capacity %s.", mluResourceCount)
	} else {
		log.Printf("Updated Capacity %s to %d successfully.", mluResourceCount, count)
	}
	return err
}
