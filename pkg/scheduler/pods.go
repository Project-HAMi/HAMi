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

// PodUseDeviceStat count pod use device info.
type PodUseDeviceStat struct {
	// count current node all running success pod num
	TotalPod int
	// only running success pod and use device pod can count.
	UseDevicePod int
}

type podManager struct {
	pods  map[k8stypes.UID]*podInfo
	mutex sync.RWMutex
}

func (m *podManager) init() {
	m.pods = make(map[k8stypes.UID]*podInfo)
}

func (m *podManager) addPod(pod *corev1.Pod, nodeID string, devices util.PodDevices) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	_, ok := m.pods[pod.UID]
	if !ok {
		pi := &podInfo{Name: pod.Name, UID: pod.UID, Namespace: pod.Namespace, NodeID: nodeID, Devices: devices}
		m.pods[pod.UID] = pi
		klog.Infof("Pod added: Name: %s, UID: %s, Namespace: %s, NodeID: %s", pod.Name, pod.UID, pod.Namespace, nodeID)
	}
}

func (m *podManager) delPod(pod *corev1.Pod) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	pi, ok := m.pods[pod.UID]
	if ok {
		klog.Infof("Deleted pod %s with node ID %s", pi.Name, pi.NodeID)
		delete(m.pods, pod.UID)
	}
}

func (m *podManager) ListPods() ([]*corev1.Pod, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	pods := make([]*corev1.Pod, 0)
	for uid := range m.pods {
		pods = append(pods, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				UID: uid,
			},
		})
	}
	return pods, nil
}

func (m *podManager) GetScheduledPods() (map[k8stypes.UID]*podInfo, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	klog.Infof("Getting all scheduled pods with %d nums", len(m.pods))
	return m.pods, nil
}
