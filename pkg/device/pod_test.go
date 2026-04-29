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
	"reflect"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

func TestPodInfo(t *testing.T) {
	tests := []struct {
		name     string
		podInfo  PodInfo
		expected PodInfo
	}{
		{
			name:     "Empty podInfo",
			podInfo:  PodInfo{},
			expected: PodInfo{},
		},
		{
			name: "Filled podInfo",
			podInfo: PodInfo{
				Pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "my-pod",
						UID:       k8stypes.UID("12345678"),
					},
				},
				NodeID: "node1",
				Devices: PodDevices{
					"device1": {
						{
							{
								Idx: 1,
							},
						},
					},
				},
				CtrIDs: []string{"ctr1", "ctr2"},
			},
			expected: PodInfo{
				Pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "my-pod",
						UID:       k8stypes.UID("12345678"),
					},
				},
				NodeID: "node1",
				Devices: PodDevices{
					"device1": {
						{
							{
								Idx: 1,
							},
						},
					},
				},
				CtrIDs: []string{"ctr1", "ctr2"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !reflect.DeepEqual(tt.podInfo, tt.expected) {
				t.Errorf("Expected %v, got %v", tt.expected, tt.podInfo)
			}
		})
	}
}

func TestPodUseDeviceStat(t *testing.T) {
	tests := []struct {
		name     string
		stat     PodUseDeviceStat
		expected PodUseDeviceStat
	}{
		{
			name:     "Empty PodUseDeviceStat",
			stat:     PodUseDeviceStat{},
			expected: PodUseDeviceStat{},
		},
		{
			name: "Filled PodUseDeviceStat",
			stat: PodUseDeviceStat{
				TotalPod:     10,
				UseDevicePod: 5,
			},
			expected: PodUseDeviceStat{
				TotalPod:     10,
				UseDevicePod: 5,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !reflect.DeepEqual(tt.stat, tt.expected) {
				t.Errorf("Expected %v, got %v", tt.expected, tt.stat)
			}
		})
	}
}
func TestGetScheduledPods(t *testing.T) {
	podManager := &PodManager{
		pods:  make(map[k8stypes.UID]*PodInfo),
		mutex: sync.RWMutex{},
	}

	pod1 := &PodInfo{
		Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "pod1",
				UID:       k8stypes.UID("uid1"),
			},
		},
		NodeID:  "node1",
		Devices: PodDevices{"device1": {{}}},
		CtrIDs:  []string{"ctr1"},
	}
	pod2 := &PodInfo{
		Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "pod2",
				UID:       k8stypes.UID("uid2"),
			},
		},

		NodeID:  "node2",
		Devices: PodDevices{"device2": {{}}},
		CtrIDs:  []string{"ctr2"},
	}
	podManager.pods[pod1.UID] = pod1
	podManager.pods[pod2.UID] = pod2

	scheduledPods, err := podManager.GetScheduledPods()

	assert.NoError(t, err, "GetScheduledPods should not return an error")
	assert.NotNil(t, scheduledPods, "The result should not be nil")
	assert.Equal(t, 2, len(scheduledPods), "The number of scheduled pods should be 2")

	expectedPods := map[k8stypes.UID]*PodInfo{
		pod1.UID: pod1,
		pod2.UID: pod2,
	}
	for uid, pod := range scheduledPods {
		expectedPod := expectedPods[uid]
		assert.NotNil(t, expectedPod, "Pod with UID %s should exist in the expected pods", uid)
		assert.Equal(t, expectedPod.Namespace, pod.Namespace, "Namespace should match")
		assert.Equal(t, expectedPod.Name, pod.Name, "Name should match")
		assert.Equal(t, expectedPod.UID, pod.UID, "UID should match")
		assert.Equal(t, expectedPod.NodeID, pod.NodeID, "NodeID should match")
		assert.Equal(t, expectedPod.Devices, pod.Devices, "Devices should match")
		assert.Equal(t, expectedPod.CtrIDs, pod.CtrIDs, "CtrIDs should match")
	}
}

func TestGetPod(t *testing.T) {
	podManager := NewPodManager()

	podManager.pods["uid1"] = &PodInfo{
		Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "pod1",
				UID:       k8stypes.UID("uid1"),
			},
		},
		NodeID:  "node1",
		Devices: PodDevices{"device1": {{}}},
		CtrIDs:  []string{"ctr1"},
	}

	podManager.pods["uid2"] = &PodInfo{
		Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "pod2",
				UID:       k8stypes.UID("uid2"),
			},
		},
		NodeID:  "node2",
		Devices: PodDevices{"device2": {{}}},
		CtrIDs:  []string{"ctr2"},
	}

	for _, ts := range []struct {
		name     string
		pod      *corev1.Pod
		expected bool
	}{
		{
			name: "test pod exist",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "pod1",
					UID:       k8stypes.UID("uid1"),
				},
			},
			expected: true,
		},
	} {
		t.Run(ts.name, func(t *testing.T) {
			_, ok := podManager.GetPod(ts.pod)

			if ok != ts.expected {
				t.Errorf("Expected %v, got %v", ts.expected, ok)
			}
		})
	}
}

func TestAddPod(t *testing.T) {
	podManager := NewPodManager()
	podManager.pods["uid1"] = &PodInfo{
		Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "pod1",
				UID:       k8stypes.UID("uid1"),
			},
		},
		NodeID:  "node1",
		Devices: PodDevices{"device1": {{}}},
		CtrIDs:  []string{"ctr1"},
	}

	for _, ts := range []struct {
		name   string
		pod    *corev1.Pod
		node   string
		podDev PodDevices

		expectedPods map[k8stypes.UID]*PodInfo
	}{
		{
			name: "test pod exist",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "pod2",
					UID:       k8stypes.UID("uid2"),
				},
			},
			node:   "node2",
			podDev: PodDevices{"device2": {{}}},

			expectedPods: map[k8stypes.UID]*PodInfo{
				"uid1": {
					Pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "default",
							Name:      "pod1",
							UID:       k8stypes.UID("uid1"),
						},
					},
					NodeID:  "node1",
					Devices: PodDevices{"device1": {{}}},
					CtrIDs:  []string{"ctr1"},
				},
				"uid2": {
					Pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "default",
							Name:      "pod2",
							UID:       k8stypes.UID("uid2"),
						},
					},
					NodeID:  "node2",
					Devices: PodDevices{"device2": {{}}},
					CtrIDs:  nil,
				},
			},
		},
	} {
		t.Run(ts.name, func(t *testing.T) {
			podManager.AddPod(ts.pod, ts.node, ts.podDev)

			if !reflect.DeepEqual(podManager.pods, ts.expectedPods) {
				t.Errorf("Expected %v, got %v", ts.expectedPods, podManager.pods)
			}
		})
	}
}

func TestUpdatePod(t *testing.T) {
	podManager := NewPodManager()

	originalPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "pod1",
			UID:       k8stypes.UID("uid1"),
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	podManager.pods["uid1"] = &PodInfo{
		Pod:     originalPod,
		NodeID:  "node1",
		Devices: PodDevices{"device1": {{}}},
	}

	for _, ts := range []struct {
		name               string
		updatedPod         *corev1.Pod
		expectInCache      bool
		expectNodeID       string
		expectDevices      PodDevices
		expectDelTimestamp bool
	}{
		{
			name: "update terminating pod preserves NodeID and Devices",
			updatedPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:         "default",
					Name:              "pod1",
					UID:               k8stypes.UID("uid1"),
					DeletionTimestamp: func() *metav1.Time { t := metav1.Now(); return &t }(),
				},
				Status: corev1.PodStatus{Phase: corev1.PodRunning},
			},
			expectInCache:      true,
			expectNodeID:       "node1",
			expectDevices:      PodDevices{"device1": {{}}},
			expectDelTimestamp: true,
		},
		{
			name: "update non-existent pod is a no-op",
			updatedPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "ghost-pod",
					UID:       k8stypes.UID("uid-ghost"),
				},
			},
			expectInCache: false,
		},
	} {
		t.Run(ts.name, func(t *testing.T) {
			podManager.UpdatePod(ts.updatedPod)

			pi, ok := podManager.pods[ts.updatedPod.UID]
			assert.Equal(t, ts.expectInCache, ok)

			if ts.expectInCache {
				assert.Equal(t, ts.expectNodeID, pi.NodeID, "NodeID must be preserved")
				assert.Equal(t, ts.expectDevices, pi.Devices, "Devices must be preserved")
				assert.Equal(t, ts.expectDelTimestamp, pi.DeletionTimestamp != nil, "DeletionTimestamp should be updated")
			}
		})
	}
}


func TestPodInfoDeepCopy(t *testing.T) {
	tests := []struct {
		name     string
		original *PodInfo
	}{
		{
			name:     "nil input",
			original: nil,
		},
		{
			name:     "empty struct",
			original: &PodInfo{},
		},
		{
			name: "fully populated",
			original: &PodInfo{
				Pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "my-pod",
						UID:       k8stypes.UID("12345678"),
					},
				},
				NodeID: "node1",
				Devices: PodDevices{
					"NVIDIA": {
						{
							ContainerDevice{UUID: "GPU-0", Type: "NVIDIA", Usedmem: 100, Usedcores: 10},
						},
					},
				},
				CtrIDs: []string{"ctr1", "ctr2"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			copy := tt.original.DeepCopy()

			if tt.original == nil {
				if copy != nil {
					t.Fatalf("expected nil, got %v", copy)
				}
				return
			}

			// 1. Copy must be deeply equal to original.
			assert.Equal(t, tt.original.NodeID, copy.NodeID)
			assert.Equal(t, tt.original.CtrIDs, copy.CtrIDs)
			assert.Equal(t, tt.original.Devices, copy.Devices)
			if tt.original.Pod != nil {
				assert.Equal(t, tt.original.Pod.Name, copy.Pod.Name)
			}

			// 2. Mutating the copy must not affect the original.
			if copy.Pod != nil {
				originalPodName := tt.original.Pod.Name
				copy.Pod.Name = "mutated-pod"
				assert.Equal(t, tt.original.Pod.Name, originalPodName)
			}
			originalNodeID := tt.original.NodeID
			copy.NodeID = "mutated-node"
			assert.Equal(t, tt.original.NodeID, originalNodeID)
			originalCtrIDsLen := len(tt.original.CtrIDs)
			copy.CtrIDs = append(copy.CtrIDs, "ctr3")
			assert.Equal(t, len(tt.original.CtrIDs), originalCtrIDsLen)
			if copy.Devices == nil {
				copy.Devices = make(PodDevices)
			}
			copy.Devices["AMD"] = PodSingleDevice{
				ContainerDevices{{UUID: "AMD-0", Type: "AMD"}},
			}
			_, exists := tt.original.Devices["AMD"]
			assert.False(t, exists, "original Devices should not have AMD key")
		})
	}
}

func TestPodDevicesDeepCopy(t *testing.T) {
	original := PodDevices{
		"NVIDIA": {
			{
				ContainerDevice{UUID: "GPU-0", Type: "NVIDIA", Usedmem: 100, Usedcores: 10},
			},
		},
	}

	copy := original.DeepCopy()

	// 1. Copy must be deeply equal to original.
	assert.Equal(t, original, copy)

	// 2. Mutating the copy must not affect the original.
	copy["AMD"] = PodSingleDevice{
		ContainerDevices{{UUID: "AMD-0", Type: "AMD"}},
	}
	copy["NVIDIA"][0][0].UUID = "mutated-gpu"

	_, exists := original["AMD"]
	assert.False(t, exists, "original should not have AMD key")
	assert.Equal(t, original["NVIDIA"][0][0].UUID, "GPU-0")
}

func TestPodSingleDeviceDeepCopy(t *testing.T) {
	original := PodSingleDevice{
		{
			ContainerDevice{UUID: "GPU-0", Type: "NVIDIA", Usedmem: 100, Usedcores: 10},
		},
		{
			ContainerDevice{UUID: "GPU-1", Type: "NVIDIA", Usedmem: 200, Usedcores: 20},
		},
	}

	copy := original.DeepCopy()

	// 1. Copy must be deeply equal to original.
	assert.Equal(t, original, copy)

	// 2. Mutating the copy must not affect the original.
	copy[0][0].UUID = "mutated-gpu"

	assert.Equal(t, original[0][0].UUID, "GPU-0")
}

func TestContainerDevicesDeepCopy(t *testing.T) {
	original := ContainerDevices{
		{UUID: "GPU-0", Type: "NVIDIA", Usedmem: 100, Usedcores: 10},
		{UUID: "GPU-1", Type: "NVIDIA", Usedmem: 200, Usedcores: 20},
	}

	copy := original.DeepCopy()

	// 1. Copy must be deeply equal to original.
	assert.Equal(t, original, copy)

	// 2. Mutating the copy must not affect the original.
	copy[0].UUID = "mutated-gpu"

	assert.Equal(t, original[0].UUID, "GPU-0")
}

func TestContainerDeviceDeepCopy(t *testing.T) {
	original := ContainerDevice{
		Idx:        0,
		UUID:       "GPU-0",
		Type:       "NVIDIA",
		Usedmem:    100,
		Usedcores:  10,
		CustomInfo: map[string]any{"key1": "value1"},
	}

	copy := original.DeepCopy()

	// 1. Copy must be deeply equal to original.
	assert.Equal(t, original, copy)

	// 2. Mutating the copy must not affect the original.
	copy.UUID = "mutated-gpu"
	copy.CustomInfo["key2"] = "value2"

	assert.Equal(t, original.UUID, "GPU-0")
	_, exists := original.CustomInfo["key2"]
	assert.False(t, exists, "original CustomInfo should not have key2")
}
