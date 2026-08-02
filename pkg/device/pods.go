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
	"maps"
	"reflect"
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
}

// PodUseDeviceStat counts pod use device info.
type PodUseDeviceStat struct {
	TotalPod     int // Count of all running pods on the current node
	UseDevicePod int // Count of running pods that use devices
}

type PodManager struct {
	pods         map[k8stypes.UID]*PodInfo
	reservations map[k8stypes.UID]struct{}
	mutex        sync.RWMutex
}

func NewPodManager() *PodManager {
	pm := &PodManager{
		pods:         make(map[k8stypes.UID]*PodInfo),
		reservations: make(map[k8stypes.UID]struct{}),
	}
	klog.InfoS("Pod manager initialized", "podCount", len(pm.pods))
	return pm
}

func (m *PodManager) AddPod(pod *corev1.Pod, nodeID string, devices PodDevices) bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	pi, exists := m.pods[pod.UID]
	if !exists {
		pi = &PodInfo{
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
	} else if _, reserved := m.reservations[pod.UID]; reserved {
		if pi.NodeID == nodeID && reservationMatchesInformerDevices(pi.Devices, devices) {
			// The informer observed the allocation written by Bind. Clear the
			// reservation marker so later informer updates reconcile normally.
			pi.Pod = pod
			delete(m.reservations, pod.UID)
			klog.V(5).InfoS("Pod reservation observed by informer",
				"pod", klog.KRef(pod.Namespace, pod.Name),
				"nodeID", nodeID,
			)
		} else {
			klog.V(5).InfoS("Ignoring informer update for bind-owned reservation",
				"pod", klog.KRef(pod.Namespace, pod.Name),
				"reservedNodeID", pi.NodeID,
				"informerNodeID", nodeID,
			)
		}
	} else {
		pi.Devices = devices
		klog.V(5).InfoS("Pod devices updated",
			"pod", klog.KRef(pod.Namespace, pod.Name),
			"devices", devices,
		)
	}

	return !exists
}

func reservationMatchesInformerDevices(reserved, observed PodDevices) bool {
	if reflect.DeepEqual(reserved, observed) {
		return true
	}

	// DecodePodDevices preserves the trailing annotation separator as an empty
	// container entry. Remove only that decoder artifact before comparing with
	// the in-memory allocation produced by Bind.
	normalized := observed.DeepCopy()
	for deviceType, containers := range normalized {
		if len(containers) > 0 && len(containers[len(containers)-1]) == 0 {
			normalized[deviceType] = containers[:len(containers)-1]
		}
	}
	return reflect.DeepEqual(reserved, normalized)
}

// ReservePodIfAbsent records a bind-owned allocation without replacing an
// allocation installed concurrently by an informer or another binding attempt.
func (m *PodManager) ReservePodIfAbsent(pod *corev1.Pod, nodeID string, devices PodDevices) bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if _, exists := m.pods[pod.UID]; exists {
		return false
	}
	m.pods[pod.UID] = &PodInfo{
		Pod:     pod,
		NodeID:  nodeID,
		Devices: devices,
	}
	m.reservations[pod.UID] = struct{}{}
	klog.InfoS("Pod allocation reserved",
		"pod", klog.KRef(pod.Namespace, pod.Name),
		"nodeID", nodeID,
		"devices", devices,
	)
	return true
}

// ReplacePodReservation atomically replaces the expected allocation with a
// bind-owned reservation.
func (m *PodManager) ReplacePodReservation(pod *corev1.Pod, expected *PodInfo, nodeID string, devices PodDevices) bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	current, exists := m.pods[pod.UID]
	if !exists || expected == nil || current.NodeID != expected.NodeID ||
		!reservationMatchesInformerDevices(expected.Devices, current.Devices) {
		klog.V(5).InfoS("Pod reservation replacement rejected",
			"pod", klog.KRef(pod.Namespace, pod.Name),
			"exists", exists,
		)
		return false
	}
	m.pods[pod.UID] = &PodInfo{Pod: pod, NodeID: nodeID, Devices: devices}
	m.reservations[pod.UID] = struct{}{}
	return true
}

func (m *PodManager) UpdatePod(pod *corev1.Pod) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if pi, exists := m.pods[pod.UID]; exists {
		pi.Pod = pod
		klog.V(5).InfoS("Pod object updated in cache (terminating state)",
			"pod", klog.KRef(pod.Namespace, pod.Name),
			"deletionTimestamp", pod.DeletionTimestamp,
		)
	}
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
		delete(m.reservations, pod.UID)
	} else {
		klog.InfoS("Pod not found for deletion",
			"pod", klog.KRef(pod.Namespace, pod.Name),
		)
	}
}

func (m *PodManager) GetPod(pod *corev1.Pod) (*PodInfo, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	pi, ok := m.pods[pod.UID]
	if !ok {
		return nil, false
	}
	return pi.DeepCopy(), true
}

func (m *PodManager) TakeAndDeletePod(pod *corev1.Pod) (*PodInfo, bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	pi, ok := m.pods[pod.UID]
	if ok {
		delete(m.pods, pod.UID)
		delete(m.reservations, pod.UID)
		klog.InfoS("Pod taken and deleted", "pod", klog.KRef(pod.Namespace, pod.Name), "nodeID", pi.NodeID)
	}
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
		pods = append(pods, pod.DeepCopy())
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

func (p *PodInfo) DeepCopy() *PodInfo {
	if p == nil {
		return nil
	}
	return &PodInfo{
		Pod:     p.Pod.DeepCopy(),
		NodeID:  p.NodeID,
		Devices: p.Devices.DeepCopy(),
	}
}

func (pd PodDevices) DeepCopy() PodDevices {
	if pd == nil {
		return nil
	}
	dup := make(PodDevices, len(pd))
	for k, v := range pd {
		dup[k] = v.DeepCopy()
	}
	return dup
}

func (psd PodSingleDevice) DeepCopy() PodSingleDevice {
	if psd == nil {
		return nil
	}
	dup := make(PodSingleDevice, len(psd))
	for i, cd := range psd {
		dup[i] = cd.DeepCopy()
	}
	return dup
}

func (cd ContainerDevices) DeepCopy() ContainerDevices {
	if cd == nil {
		return nil
	}
	dup := make(ContainerDevices, len(cd))
	for i, c := range cd {
		dup[i] = c.DeepCopy()
	}
	return dup
}

func (c ContainerDevice) DeepCopy() ContainerDevice {
	dup := ContainerDevice{
		Idx:       c.Idx,
		UUID:      c.UUID,
		Type:      c.Type,
		Usedmem:   c.Usedmem,
		Usedcores: c.Usedcores,
	}
	if c.CustomInfo != nil {
		dup.CustomInfo = make(map[string]any, len(c.CustomInfo))
		maps.Copy(dup.CustomInfo, c.CustomInfo)
	}
	return dup
}

func (m *PodManager) GetScheduledPods() (map[k8stypes.UID]*PodInfo, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	podCount := len(m.pods)
	klog.InfoS("Retrieved scheduled pods",
		"podCount", podCount,
	)

	// Return a shallow copy of the pods map to avoid race conditions.
	// This prevents a "concurrent map iteration and map write" fatal error.
	podsCopy := make(map[k8stypes.UID]*PodInfo, podCount)
	maps.Copy(podsCopy, m.pods)
	return podsCopy, nil
}
