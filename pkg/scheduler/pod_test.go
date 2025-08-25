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
	"reflect"
	"sync"
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/stretchr/testify/assert"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

func TestPodInfo(t *testing.T) {
	tests := []struct {
		name     string
		podInfo  podInfo
		expected podInfo
	}{
		{
			name:     "Empty podInfo",
			podInfo:  podInfo{},
			expected: podInfo{},
		},
		{
			name: "Filled podInfo",
			podInfo: podInfo{
				Namespace: "default",
				Name:      "my-pod",
				UID:       k8stypes.UID("12345678"),
				NodeID:    "node1",
				Devices: device.PodDevices{
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
			expected: podInfo{
				Namespace: "default",
				Name:      "my-pod",
				UID:       k8stypes.UID("12345678"),
				NodeID:    "node1",
				Devices: device.PodDevices{
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
	podManager := &podManager{
		pods:  make(map[k8stypes.UID]*podInfo),
		mutex: sync.RWMutex{},
	}

	pod1 := &podInfo{
		Namespace: "default",
		Name:      "pod1",
		UID:       k8stypes.UID("uid1"),
		NodeID:    "node1",
		Devices:   device.PodDevices{"device1": {{}}},
		CtrIDs:    []string{"ctr1"},
	}
	pod2 := &podInfo{
		Namespace: "default",
		Name:      "pod2",
		UID:       k8stypes.UID("uid2"),
		NodeID:    "node2",
		Devices:   device.PodDevices{"device2": {{}}},
		CtrIDs:    []string{"ctr2"},
	}
	podManager.pods[pod1.UID] = pod1
	podManager.pods[pod2.UID] = pod2

	scheduledPods, err := podManager.GetScheduledPods()

	assert.NoError(t, err, "GetScheduledPods should not return an error")
	assert.NotNil(t, scheduledPods, "The result should not be nil")
	assert.Equal(t, 2, len(scheduledPods), "The number of scheduled pods should be 2")

	expectedPods := map[k8stypes.UID]*podInfo{
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
