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
	"fmt"
	"sort"
	"testing"

	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/util"

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
			// Metax-GPU implements the policy-neutral scorer, so OverrideScore
			// weights its raw "higher is better" score by 10000 under Binpack.
			// The node only carries the losses annotation, so the raw score
			// falls back to 2000 - loss = 2000 - 321 = 1679 for the requested
			// two devices, giving a weighted result of 16790000.
			policy:    "binpack",
			wantScore: 16790000,
		},
		{
			// Under Spread the same policy-neutral raw score of 1679 is inverted
			// (weight -10000), producing -16790000. Because Binpack picks the
			// highest score and Spread picks the lowest, inverting the sign
			// preserves the node ranking across both policies.
			name: "Metax-GPU with spread policy returns inverted weighted score",
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
			policy:    "spread",
			wantScore: -16790000,
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
		{
			// MetaxSDevices is policy-neutral, so OverrideScore
			// weights its raw "higher is better" score by 10000 under Binpack. The
			// two allocated topology-aware devices yield a raw score of 60
			// (see scoreExclusiveDevices), so the weighted result is 600000.
			name: "MetaX-SGPU with binpack policy returns weighted score",
			nodeScore: &NodeScore{
				Node: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
					},
				},
				NodeID: "node1",
				Devices: device.PodDevices{
					"Metax-SGPU": device.PodSingleDevice{
						device.ContainerDevices{
							{
								UUID:      "GPU-3",
								Usedcores: 100,
								CustomInfo: map[string]any{
									"LinkZone": int32(1),
									"Pod.Annotations": map[string]string{
										"metax-tech.com/sgpu-topology-aware": "true",
									},
								},
							},
							{
								UUID:      "GPU-4",
								Usedcores: 100,
								CustomInfo: map[string]any{
									"LinkZone": int32(1),
									"Pod.Annotations": map[string]string{
										"metax-tech.com/sgpu-topology-aware": "true",
									},
								},
							},
						},
					},
				},
				Score: 0,
			},
			devices: []*device.DeviceUsage{
				{ID: "GPU-1", Used: 0, CustomInfo: map[string]any{"LinkZone": int32(1)}},
				{ID: "GPU-2", Used: 0, CustomInfo: map[string]any{"LinkZone": int32(1)}},
				{ID: "GPU-3", Used: 0, CustomInfo: map[string]any{"LinkZone": int32(1)}},
				{ID: "GPU-4", Used: 0, CustomInfo: map[string]any{"LinkZone": int32(1)}},
			},
			policy:    "binpack",
			wantScore: 600000,
		},
		{
			// Under Spread the same policy-neutral raw score of 60 is inverted
			// (weight -10000), producing -600000. Because Binpack picks the highest
			// score and Spread picks the lowest, inverting the sign preserves the
			// original node ranking across both policies.
			name: "MetaX-SGPU with spread policy returns inverted weighted score",
			nodeScore: &NodeScore{
				Node: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
					},
				},
				NodeID: "node1",
				Devices: device.PodDevices{
					"Metax-SGPU": device.PodSingleDevice{
						device.ContainerDevices{
							{
								UUID:      "GPU-3",
								Usedcores: 100,
								CustomInfo: map[string]any{
									"LinkZone": int32(1),
									"Pod.Annotations": map[string]string{
										"metax-tech.com/sgpu-topology-aware": "true",
									},
								},
							},
							{
								UUID:      "GPU-4",
								Usedcores: 100,
								CustomInfo: map[string]any{
									"LinkZone": int32(1),
									"Pod.Annotations": map[string]string{
										"metax-tech.com/sgpu-topology-aware": "true",
									},
								},
							},
						},
					},
				},
				Score: 0,
			},
			devices: []*device.DeviceUsage{
				{ID: "GPU-1", Used: 0, CustomInfo: map[string]any{"LinkZone": int32(1)}},
				{ID: "GPU-2", Used: 0, CustomInfo: map[string]any{"LinkZone": int32(1)}},
				{ID: "GPU-3", Used: 0, CustomInfo: map[string]any{"LinkZone": int32(1)}},
				{ID: "GPU-4", Used: 0, CustomInfo: map[string]any{"LinkZone": int32(1)}},
			},
			policy:    "spread",
			wantScore: -600000,
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

// TestOverrideScoreMetaxGPUOrderingUnchanged verifies that decoupling
// MetaxDevices.ScoreNode from the scheduler policy string does not change which
// node wins for Metax-GPU.
//
// The pre-decoupling code implemented two distinct selection rules: under
// Binpack it preferred the node with the lowest topology loss, and under Spread
// it preferred the node with the highest topology score. When a node advertises
// both annotations they describe the same underlying topology preference, so the
// lowest-loss node is also the highest-score node. This test builds such
// consistent nodes, derives the winner each original rule would have chosen, and
// asserts that the new single policy-neutral score — once weighted and sorted by
// the shared OverrideScore/Less layer exactly as the scheduler does — selects the
// same node under both policies.
func TestOverrideScoreMetaxGPUOrderingUnchanged(t *testing.T) {
	setup(t, &config.Config{
		NvidiaConfig: nvidia.NvidiaConfig{
			ResourceCountName:            "hami.io/gpu",
			ResourceMemoryName:           "hami.io/gpumem",
			ResourceMemoryPercentageName: "hami.io/gpumem-percentage",
			ResourceCoreName:             "hami.io/gpucores",
			DefaultGPUNum:                1,
		},
	})

	// Each node requests two Metax-GPU devices, so the topology annotations are
	// looked up at index "2".
	type metaxNode struct {
		name  string
		loss  int
		score int
	}

	// buildNode returns a NodeScore that advertises both topology annotations for
	// the two requested devices.
	buildNode := func(n metaxNode) *NodeScore {
		return &NodeScore{
			NodeID: n.name,
			Node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: n.name,
					Annotations: map[string]string{
						"metax-tech.com/gpu.topology.losses": fmt.Sprintf("{\"2\":%d}", n.loss),
						"metax-tech.com/gpu.topology.scores": fmt.Sprintf("{\"2\":%d}", n.score),
					},
				},
			},
			Devices: device.PodDevices{
				"Metax-GPU": device.PodSingleDevice{
					device.ContainerDevices{
						{Idx: 0, UUID: n.name + "-0", Type: "gpu", Usedmem: 1024, Usedcores: 2},
						{Idx: 1, UUID: n.name + "-1", Type: "gpu", Usedmem: 1024, Usedcores: 2},
					},
				},
			},
		}
	}

	// originalBinpackWinner is the node the pre-decoupling code selected under
	// Binpack: the lowest topology loss.
	originalBinpackWinner := func(nodes []metaxNode) string {
		best := nodes[0]
		for _, n := range nodes[1:] {
			if n.loss < best.loss {
				best = n
			}
		}
		return best.name
	}

	// originalSpreadWinner is the node the pre-decoupling code selected under
	// Spread: the highest topology score.
	originalSpreadWinner := func(nodes []metaxNode) string {
		best := nodes[0]
		for _, n := range nodes[1:] {
			if n.score > best.score {
				best = n
			}
		}
		return best.name
	}

	// newWinner reproduces the scheduler's selection: weight every node with
	// OverrideScore, sort with the policy-aware Less, and take the last element
	// (see scheduler.go).
	newWinner := func(nodes []metaxNode, policy string) string {
		list := NodeScoreList{Policy: policy}
		for _, n := range nodes {
			ns := buildNode(n)
			ns.OverrideScore([]*device.DeviceUsage{}, policy)
			list.NodeList = append(list.NodeList, ns)
		}
		sort.Sort(&list)
		return list.NodeList[len(list.NodeList)-1].NodeID
	}

	tests := []struct {
		name  string
		nodes []metaxNode
	}{
		{
			name: "best node has both lowest loss and highest score",
			nodes: []metaxNode{
				{name: "node-a", loss: 300, score: 100},
				{name: "node-b", loss: 100, score: 300},
				{name: "node-c", loss: 200, score: 200},
			},
		},
		{
			name: "best node is first in the list",
			nodes: []metaxNode{
				{name: "node-a", loss: 50, score: 500},
				{name: "node-b", loss: 400, score: 100},
				{name: "node-c", loss: 250, score: 250},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantBinpack := originalBinpackWinner(tt.nodes)
			wantSpread := originalSpreadWinner(tt.nodes)
			// Consistent annotations must agree on the best node; otherwise a
			// single policy-neutral score could not preserve both rankings and
			// this test's premise would not hold.
			assert.Equal(t, wantBinpack, wantSpread)

			assert.Equal(t, wantBinpack, newWinner(tt.nodes, util.NodeSchedulerPolicyBinpack.String()))
			assert.Equal(t, wantSpread, newWinner(tt.nodes, util.NodeSchedulerPolicySpread.String()))
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
			name: "Zero capacity devices returns score 0 without panic",
			nodeScore: NodeScore{
				NodeID: "node-zero",
				Score:  0.0,
			},
			devices: DeviceUsageList{
				DeviceLists: []*DeviceListsScore{
					{Device: &device.DeviceUsage{
						Count: 0, Totalcore: 0, Totalmem: 0,
						Used: 0, Usedcores: 0, Usedmem: 0,
					}, Score: 0},
				},
			},
			wantScore: 0,
		},
		{
			name: "Empty device list returns score 0 without panic",
			nodeScore: NodeScore{
				NodeID: "node-empty",
				Score:  0.0,
			},
			devices: DeviceUsageList{
				DeviceLists: []*DeviceListsScore{},
			},
			wantScore: 0,
		},
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

func TestSnapshotDevice(t *testing.T) {
	ns := &NodeScore{}
	tests := []struct {
		name    string
		devices DeviceUsageList
		want    int
	}{
		{
			name:    "empty list",
			devices: DeviceUsageList{DeviceLists: []*DeviceListsScore{}},
			want:    0,
		},
		{
			name: "single device",
			devices: DeviceUsageList{DeviceLists: []*DeviceListsScore{
				{Device: &device.DeviceUsage{ID: "gpu0", Usedmem: 100}},
			}},
			want: 1,
		},
		{
			name: "multiple devices",
			devices: DeviceUsageList{DeviceLists: []*DeviceListsScore{
				{Device: &device.DeviceUsage{ID: "gpu0", Usedmem: 100}},
				{Device: &device.DeviceUsage{ID: "gpu1", Usedmem: 200}},
				{Device: &device.DeviceUsage{ID: "gpu2", Usedmem: 300}},
			}},
			want: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap := ns.SnapshotDevice(tt.devices)
			assert.Equal(t, tt.want, len(snap))
			for i, d := range snap {
				assert.Equal(t, tt.devices.DeviceLists[i].Device.ID, d.ID)
				originalUsedmem := d.Usedmem
				tt.devices.DeviceLists[i].Device.Usedmem = 9999
				assert.Equal(t, originalUsedmem, d.Usedmem)
			}
		})
	}
}
