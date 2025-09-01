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

package policy

import (
	"testing"

	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNodeScoreListLen(t *testing.T) {
	tests := []struct {
		name     string
		list     NodeScoreList
		expected int
	}{
		{
			name:     "empty list",
			list:     NodeScoreList{NodeList: []*NodeScore{}, Policy: "default"},
			expected: 0,
		},
		{
			name: "list with elements",
			list: NodeScoreList{
				NodeList: []*NodeScore{
					{
						NodeID: "node1",
						Node: &corev1.Node{
							ObjectMeta: metav1.ObjectMeta{Name: "node1"},
							Spec:       corev1.NodeSpec{},
							Status:     corev1.NodeStatus{},
						},
						Devices: device.PodDevices{},
						Score:   85.5,
					},
					{
						NodeID: "node2",
						Node: &corev1.Node{
							ObjectMeta: metav1.ObjectMeta{Name: "node2"},
							Spec:       corev1.NodeSpec{},
							Status:     corev1.NodeStatus{},
						},
						Devices: device.PodDevices{},
						Score:   90.0,
					},
					{
						NodeID: "node3",
						Node: &corev1.Node{
							ObjectMeta: metav1.ObjectMeta{Name: "node3"},
							Spec:       corev1.NodeSpec{},
							Status:     corev1.NodeStatus{},
						},
						Devices: device.PodDevices{},
						Score:   78.0,
					},
				},
				Policy: "custom",
			},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.list.Len(); got != tt.expected {
				t.Errorf("NodeScoreList.Len() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestNodeSwap(t *testing.T) {
	node1 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node1"},
	}
	node2 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node2"},
	}

	nodes := []*NodeScore{
		{NodeID: "1", Node: node1, Score: 1.0},
		{NodeID: "2", Node: node2, Score: 2.0},
	}
	list := NodeScoreList{NodeList: nodes}

	i, j := 0, 1

	originalI := list.NodeList[i]
	originalJ := list.NodeList[j]

	list.Swap(i, j)

	if list.NodeList[i] != originalJ || list.NodeList[j] != originalI {
		t.Errorf("Swap failed: expected (%v, %v), got (%v, %v)", originalJ, originalI, list.NodeList[i], list.NodeList[j])
	}
}

func TestLess(t *testing.T) {
	tests := []struct {
		name          string
		nodeScoreList NodeScoreList
		i             int
		j             int
		expected      bool
	}{
		{
			name: "Spread strategy, i score higher",
			nodeScoreList: NodeScoreList{
				NodeList: []*NodeScore{
					{NodeID: "node1", Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}, Score: 20.0},
					{NodeID: "node2", Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node2"}}, Score: 10.0},
				},
				Policy: "spread",
			},
			i:        0,
			j:        1,
			expected: true,
		},
		{
			name: "Spread strategy,j score higher",
			nodeScoreList: NodeScoreList{
				NodeList: []*NodeScore{
					{NodeID: "node1", Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}, Score: 10.0},
					{NodeID: "node2", Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node2"}}, Score: 20.0},
				},
				Policy: "spread",
			},
			i:        0,
			j:        1,
			expected: false,
		},
		{
			name: "Default strategy (Binpack), i score lower",
			nodeScoreList: NodeScoreList{
				NodeList: []*NodeScore{
					{NodeID: "node1", Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}, Score: 10.0},
					{NodeID: "node2", Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node2"}}, Score: 20.0},
				},
				Policy: "binpack",
			},
			i:        0,
			j:        1,
			expected: true,
		},
		{
			name: "Default strategy (Binpack), j score lower",
			nodeScoreList: NodeScoreList{
				NodeList: []*NodeScore{
					{NodeID: "node1", Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}, Score: 20.0},
					{NodeID: "node2", Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node2"}}, Score: 10.0},
				},
				Policy: "binpack",
			},
			i:        0,
			j:        1,
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := test.nodeScoreList.Less(test.i, test.j)
			assert.Equal(t, test.expected, result)
		})
	}
}

// setup initializes the devices with a given configuration.
func setup(t *testing.T, sConfig *config.Config) {
	if err := config.InitDevicesWithConfig(sConfig); err != nil {
		klog.Fatalf("Failed to initialize devices with config: %v", err)
	}
}

// TestOverrideScore tests the OverrideScore method for different scenarios.
func TestOverrideScore(t *testing.T) {
	sConfig := &config.Config{
		NvidiaConfig: nvidia.NvidiaConfig{
			ResourceCountName:            "hami.io/gpu",
			ResourceMemoryName:           "hami.io/gpumem",
			ResourceMemoryPercentageName: "hami.io/gpumem-percentage",
			ResourceCoreName:             "hami.io/gpucores",
			DefaultMemory:                0,
			DefaultCores:                 0,
			DefaultGPUNum:                1,
		},
	}
	setup(t, sConfig)

	tests := []struct {
		name      string
		nodeScore *NodeScore
		devices   []*device.DeviceUsage
		policy    string
		wantScore float32
	}{
		{
			name: "Device score greater than zero",
			nodeScore: &NodeScore{
				Node: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Annotations: map[string]string{
							"metax-tech.com/gpu.topology.losses": "{\"1\":123,\"2\":321}",
						},
					},
				},
				NodeID: "node1",
				Devices: device.PodDevices{
					"DCU": device.PodSingleDevice{
						device.ContainerDevices{
							{Idx: 1, UUID: "uuid1", Type: "gpu", Usedmem: 1024, Usedcores: 2},
							{Idx: 2, UUID: "uuid2", Type: "gpu", Usedmem: 2048, Usedcores: 4},
						},
					},
					"Metax-GPU": device.PodSingleDevice{
						device.ContainerDevices{
							{Idx: 1, UUID: "uuid1", Type: "gpu", Usedmem: 1024, Usedcores: 2},
							{Idx: 2, UUID: "uuid2", Type: "gpu", Usedmem: 2048, Usedcores: 4},
						},
					},
				},
				Score: 0,
			},
			devices: []*device.DeviceUsage{
				{
					Count:     4,
					Totalcore: 8,
					Totalmem:  4096,
					Type:      "gpu",
					Used:      0,
					Usedcores: 0,
					Usedmem:   0,
				},
			},
			policy:    "binpack",
			wantScore: 1679,
		},
		{
			name: "Device score equal to zero",
			nodeScore: &NodeScore{
				Node: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Annotations: map[string]string{
							"ccc": "cpu:123,gpu:321",
						},
					},
				},
				NodeID: "node1",
				Devices: device.PodDevices{
					"DCU": device.PodSingleDevice{
						device.ContainerDevices{
							{Idx: 1, UUID: "uuid1", Type: "gpu", Usedmem: 1024, Usedcores: 2},
							{Idx: 2, UUID: "uuid2", Type: "gpu", Usedmem: 2048, Usedcores: 4},
						},
					},
					"Metax-GPU": device.PodSingleDevice{
						device.ContainerDevices{
							{Idx: 1, UUID: "uuid1", Type: "gpu", Usedmem: 1024, Usedcores: 2},
							{Idx: 2, UUID: "uuid2", Type: "gpu", Usedmem: 2048, Usedcores: 4},
						},
					},
				},
				Score: 0,
			},
			devices: []*device.DeviceUsage{
				{
					Count:     4,
					Totalcore: 8,
					Totalmem:  4096,
					Type:      "gpu",
					Used:      0,
					Usedcores: 0,
					Usedmem:   0,
				},
			},
			policy:    "binpack",
			wantScore: 0,
		},
		// Add more test cases here to cover other scenarios and policies.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.nodeScore.OverrideScore(tt.devices, tt.policy)
			if gotScore := tt.nodeScore.Score; gotScore != tt.wantScore {
				t.Errorf("OverrideScore() gotScore = %v, want %v", gotScore, tt.wantScore)
			}
		})
	}
}

func TestComputeDefaultScore(t *testing.T) {
	device1 := &device.DeviceUsage{
		ID:        "device1",
		Index:     1,
		Used:      50,
		Count:     100,
		Usedmem:   100,
		Totalmem:  100,
		Totalcore: 100,
		Usedcores: 100,
		Numa:      0,
		Type:      "GPU",
		Health:    true,
	}

	device2 := &device.DeviceUsage{
		ID:        "device2",
		Index:     2,
		Used:      75,
		Count:     150,
		Usedmem:   200,
		Totalmem:  200,
		Totalcore: 200,
		Usedcores: 200,
		Numa:      1,
		Type:      "CPU",
		Health:    false,
	}
	tests := []struct {
		name      string
		nodeScore NodeScore
		devices   DeviceUsageList
		wantScore float32
	}{
		{
			name: "Test with no devices",
			nodeScore: NodeScore{
				NodeID: "node1",
				Score:  0.0,
			},
			devices: DeviceUsageList{
				DeviceLists: []*DeviceListsScore{
					{Device: device1, Score: 0},
					{Device: device2, Score: 0},
				},
			},
			wantScore: 25,
		},
		{
			name: "Test with devices",
			nodeScore: NodeScore{
				NodeID: "node2",
			},
			devices: DeviceUsageList{
				DeviceLists: []*DeviceListsScore{
					{Device: device1, Score: 1},
					{Device: device2, Score: 1},
				},
			},
			wantScore: 25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.nodeScore.ComputeDefaultScore(tt.devices)
			if tt.nodeScore.Score != tt.wantScore {
				t.Errorf("NodeScore.ComputeDefaultScore() = %v, want %v", tt.nodeScore.Score, tt.wantScore)
			}
		})
	}
}
