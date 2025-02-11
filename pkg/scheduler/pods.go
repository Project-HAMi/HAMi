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

package scheduler

import (
	"sync"

	"github.com/Project-HAMi/HAMi/pkg/util"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

type podInfo struct {
	Namespace string
	Name      string
	UID       k8stypes.UID
	NodeID    string
	Devices   util.PodDevices
	CtrIDs    []string
}

// PodUseDeviceStat counts pod use device info.
type PodUseDeviceStat struct {
	TotalPod     int // Count of all running pods on the current node
	UseDevicePod int // Count of running pods that use devices
}

type podManager struct {
	pods  map[k8stypes.UID]*podInfo
	mutex sync.RWMutex
}

func (m *podManager) init() {
	m.pods = make(map[k8stypes.UID]*podInfo)
	klog.InfoS("Pod manager initialized", "podCount", len(m.pods))
}

func (m *podManager) addPod(pod *corev1.Pod, nodeID string, devices util.PodDevices) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	_, exists := m.pods[pod.UID]
	if !exists {
		pi := &podInfo{
			Name:      pod.Name,
			UID:       pod.UID,
			Namespace: pod.Namespace,
			NodeID:    nodeID,
			Devices:   devices,
		}
		m.pods[pod.UID] = pi
		klog.InfoS("Pod added",
			"pod", klog.KRef(pod.Namespace, pod.Name),
			"nodeID", nodeID,
			"devices", devices,
		)
	} else {
		m.pods[pod.UID].Devices = devices
		klog.InfoS("Pod devices updated",
			"pod", klog.KRef(pod.Namespace, pod.Name),
			"devices", devices,
		)
	}
}

func (m *podManager) delPod(pod *corev1.Pod) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	pi, exists := m.pods[pod.UID]
	if exists {
		klog.InfoS("Pod deleted",
			"pod", klog.KRef(pod.Namespace, pod.Name),
			"nodeID", pi.NodeID,
		)
		delete(m.pods, pod.UID)
	} else {
		klog.InfoS("Pod not found for deletion",
			"pod", klog.KRef(pod.Namespace, pod.Name),
		)
	}
}

func (m *podManager) ListPodsUID() ([]*corev1.Pod, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	pods := make([]*corev1.Pod, 0, len(m.pods))
	for uid := range m.pods {
		pods = append(pods, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				UID: uid,
			},
		})
	}
	klog.InfoS("Listed pod UIDs",
		"podCount", len(pods),
	)
	return pods, nil
}

func (m *podManager) ListPodsInfo() []*podInfo {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	pods := make([]*podInfo, 0, len(m.pods))
	for _, pod := range m.pods {
		pods = append(pods, pod)
		klog.InfoS("Pod info",
			"pod", klog.KRef(pod.Namespace, pod.Name),
			"nodeID", pod.NodeID,
			"devices", pod.Devices,
		)
	}
	klog.V(5).InfoS("Listed pod infos",
		"podCount", len(pods),
	)
	return pods
}

func (m *podManager) GetScheduledPods() (map[k8stypes.UID]*podInfo, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	podCount := len(m.pods)
	klog.InfoS("Retrieved scheduled pods",
		"podCount", podCount,
	)
	return m.pods, nil
}
