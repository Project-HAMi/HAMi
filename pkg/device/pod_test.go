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
func TestGetScheduledPodsReturnsDeepCopy(t *testing.T) {
	podManager := NewPodManager()

	pod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "pod1",
			UID:       k8stypes.UID("uid1"),
		},
	}
	pod1Devices := PodDevices{
		"NVIDIA": {
			{
				{
					Idx:       0,
					UUID:      "GPU-1",
					Type:      "NVIDIA",
					Usedmem:   1000,
					Usedcores: 50,
					CustomInfo: map[string]any{
						"annotations": map[string]string{
							"metax.com/gpu": "true",
						},
					},
				},
			},
		},
	}

	podManager.AddPod(pod1, "node1", pod1Devices)

	scheduledPods, err := podManager.GetScheduledPods()

	assert.NoError(t, err, "GetScheduledPods should not return an error")
	assert.NotNil(t, scheduledPods, "The result should not be nil")
	assert.Equal(t, 1, len(scheduledPods), "The number of scheduled pods should be 1")

	got, ok := scheduledPods[pod1.UID]
	assert.True(t, ok)

	// 1. Existing Pod pointer is kept (retaining pointer is intentional)
	assert.Same(t, pod1, got.Pod, "Pod pointer should be preserved without calling Pod.DeepCopy()")
	assert.Equal(t, "node1", got.NodeID)

	// 2. Scalar device allocation fields match
	gotDev := got.Devices["NVIDIA"][0][0]
	assert.Equal(t, "GPU-1", gotDev.UUID)
	assert.Equal(t, "NVIDIA", gotDev.Type)
	assert.Equal(t, int32(1000), gotDev.Usedmem)
	assert.Equal(t, int32(50), gotDev.Usedcores)

	// 3. CustomInfo is intentionally omitted (nil) in metrics snapshot
	assert.Nil(t, gotDev.CustomInfo, "CustomInfo should be nil in metrics snapshot")

	// 4. Device allocation fields are independent; mutating snapshot does not affect PodManager
	got.Devices["NVIDIA"][0][0].UUID = "MUTATED-GPU"
	got.Devices["NVIDIA"][0][0].Usedmem = 9999
	got.Devices["NVIDIA"][0][0].Usedcores = 99

	originalInfo, ok := podManager.GetPod(pod1)
	assert.True(t, ok)
	origDev := originalInfo.Devices["NVIDIA"][0][0]
	assert.Equal(t, "GPU-1", origDev.UUID, "Original UUID should remain unmutated")
	assert.Equal(t, int32(1000), origDev.Usedmem, "Original Usedmem should remain unmutated")
	assert.Equal(t, int32(50), origDev.Usedcores, "Original Usedcores should remain unmutated")
	assert.NotNil(t, origDev.CustomInfo, "Original CustomInfo should remain present in PodManager")
}

func TestGetScheduledPods(t *testing.T) {
	TestGetScheduledPodsReturnsDeepCopy(t)
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
			assert.Equal(t, tt.original.Devices, copy.Devices)
			if tt.original.Pod != nil {
				assert.Equal(t, tt.original.Name, copy.Name)
			}

			// 2. Mutating the copy must not affect the original.
			if copy.Pod != nil {
				originalPodName := tt.original.Name
				copy.Name = "mutated-pod"
				assert.Equal(t, tt.original.Name, originalPodName)
			}
			originalNodeID := tt.original.NodeID
			copy.NodeID = "mutated-node"
			assert.Equal(t, tt.original.NodeID, originalNodeID)
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

	// 1. Scalar fields match original.
	assert.Equal(t, original.Idx, copy.Idx)
	assert.Equal(t, original.UUID, copy.UUID)
	assert.Equal(t, original.Type, copy.Type)
	assert.Equal(t, original.Usedmem, copy.Usedmem)
	assert.Equal(t, original.Usedcores, copy.Usedcores)

	// 2. CustomInfo is intentionally omitted (nil).
	assert.Nil(t, copy.CustomInfo, "CustomInfo should be intentionally omitted in DeepCopy")

	// 3. Mutating scalar fields of the copy does not affect the original.
	copy.UUID = "mutated-gpu"
	assert.Equal(t, "GPU-0", original.UUID)
}

func TestListPodsInfoReturnsDeepCopy(t *testing.T) {
	pm := NewPodManager()
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "uid-1", Name: "p", Namespace: "ns"}}
	pm.AddPod(pod, "node-1", PodDevices{"dev": {{{UUID: "GPU-0"}}}})

	listed := pm.ListPodsInfo()
	assert.Len(t, listed, 1)

	listed[0].NodeID = "mutated"
	listed[0].Devices["dev"][0][0].UUID = "mutated"

	inner := pm.pods[k8stypes.UID("uid-1")]
	assert.Equal(t, "node-1", inner.NodeID)
	assert.Equal(t, "GPU-0", inner.Devices["dev"][0][0].UUID)
}

func TestTakeAndDeletePodIsAtomic(t *testing.T) {
	pm := NewPodManager()
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "uid-1", Name: "p", Namespace: "ns"}}
	pm.AddPod(pod, "node-1", PodDevices{})
	pi1, ok1 := pm.TakeAndDeletePod(pod)
	pi2, ok2 := pm.TakeAndDeletePod(pod)
	assert.True(t, ok1)
	assert.NotNil(t, pi1)
	assert.False(t, ok2)
	assert.Nil(t, pi2)
}
