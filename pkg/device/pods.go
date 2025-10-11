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

package device

import (
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

type PodInfo struct {
	*corev1.Pod
	NodeID  string
	Devices PodDevices
	CtrIDs  []string
}

// PodUseDeviceStat counts pod use device info.
type PodUseDeviceStat struct {
	TotalPod     int // Count of all running pods on the current node
	UseDevicePod int // Count of running pods that use devices
}

type PodManager struct {
	pods  map[k8stypes.UID]*PodInfo
	mutex sync.RWMutex
}

func NewPodManager() *PodManager {
	pm := &PodManager{
		pods: make(map[k8stypes.UID]*PodInfo),
	}
	klog.InfoS("Pod manager initialized", "podCount", len(pm.pods))
	return pm
}

func (m *PodManager) AddPod(pod *corev1.Pod, nodeID string, devices PodDevices) bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	_, exists := m.pods[pod.UID]
	if !exists {
		pi := &PodInfo{
			Pod:     pod,
			NodeID:  nodeID,
			Devices: devices,
		}
		m.pods[pod.UID] = pi
		klog.InfoS("Pod added",
			"pod", klog.KRef(pod.Namespace, pod.Name),
			"nodeID", nodeID,
			"devices", devices,
		)
	} else {
		m.pods[pod.UID].Devices = devices
		klog.V(5).InfoS("Pod devices updated",
			"pod", klog.KRef(pod.Namespace, pod.Name),
			"devices", devices,
		)
	}

	return !exists
}

func (m *PodManager) DelPod(pod *corev1.Pod) {
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

func (m *PodManager) GetPod(pod *corev1.Pod) (*PodInfo, bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	pi, ok := m.pods[pod.UID]
	return pi, ok
}

func (m *PodManager) ListPodsUID() ([]*corev1.Pod, error) {
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

func (m *PodManager) ListPodsInfo() []*PodInfo {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	pods := make([]*PodInfo, 0, len(m.pods))
	for _, pod := range m.pods {
		pods = append(pods, pod)
		klog.V(5).InfoS("Pod info",
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

func (m *PodManager) GetScheduledPods() (map[k8stypes.UID]*PodInfo, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	podCount := len(m.pods)
	klog.InfoS("Retrieved scheduled pods",
		"podCount", podCount,
	)
	return m.pods, nil
}
