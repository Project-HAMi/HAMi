/*
Copyright 2026 The HAMi Authors.

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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

func TestGetNodesUsageRestoresMigAllocationByProfileAndPlacement(t *testing.T) {
	nodes := newNodeManager()
	nodes.addNode("node1", &device.NodeInfo{
		ID: "node1", Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
		Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: {{
			ID: "GPU-a", Count: 7, Devmem: 40960, Devcore: 100, Mode: nvidia.MigMode, Health: true,
			MIGProfiles: []device.MigProfile{{Name: "1g.5gb", MemoryMB: 5120, Core: 14, Placements: []device.MigPlacement{{Start: 6, Size: 1}}}},
		}}},
	})
	pods := device.NewPodManager()
	pods.AddPod(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		UID: "pod-1", Name: "pod-1", Namespace: "default",
		Annotations: map[string]string{nvidia.MigAllocationsAnnotation: `[{"containerIndex":0,"deviceIndex":0,"gpuUUID":"GPU-a","profile":"1g.5gb","placement":{"start":6,"size":1},"migUUID":"MIG-a","gpuInstanceID":7,"computeInstanceID":0}]`},
	}}, "node1", device.PodDevices{nvidia.NvidiaGPUDevice: {{{UUID: "GPU-a", Usedmem: 5120, Usedcores: 14}}}})
	s := Scheduler{nodeManager: nodes, podManager: pods}
	nodeNames := []string{"node1"}
	usage, _, _, err := s.getNodesUsage(&nodeNames, nil)
	if err != nil {
		t.Fatal(err)
	}
	allocations := (*usage)["node1"].Devices.DeviceLists[0].Device.MigAllocationsInUse
	if len(allocations) != 1 || allocations[0].Profile != "1g.5gb" || allocations[0].Placement != (device.MigPlacement{Start: 6, Size: 1}) || !allocations[0].RuntimeReady || allocations[0].GPUInstanceID != 7 || allocations[0].ComputeInstanceID != 0 || allocations[0].MigUUID != "MIG-a" {
		t.Fatalf("restored allocations: %+v", allocations)
	}
}

func TestGetNodesUsageFailsClosedWithoutMigAllocation(t *testing.T) {
	nodes := newNodeManager()
	nodes.addNode("node1", &device.NodeInfo{
		ID: "node1", Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
		Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: {{
			ID: "GPU-a", Count: 7, Devmem: 40960, Devcore: 100, Mode: nvidia.MigMode, Health: true,
			MIGProfiles: []device.MigProfile{{Name: "1g.5gb", MemoryMB: 5120, Core: 14, Placements: []device.MigPlacement{{Start: 6, Size: 1}}}},
		}}},
	})
	pods := device.NewPodManager()
	pods.AddPod(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "pod-1", Name: "pod-1", Namespace: "default"}}, "node1",
		device.PodDevices{nvidia.NvidiaGPUDevice: {{{UUID: "GPU-a", Usedmem: 5120, Usedcores: 14}}}})
	s := Scheduler{nodeManager: nodes, podManager: pods}
	nodeNames := []string{"node1"}
	usage, _, _, err := s.getNodesUsage(&nodeNames, nil)
	if err != nil {
		t.Fatal(err)
	}
	if (*usage)["node1"].Devices.DeviceLists[0].Device.Health {
		t.Fatal("MIG device must be fail-closed when an allocated Pod lacks profile/placement")
	}
}

func TestGetNodesUsageRestoresUnconsumedMigReservations(t *testing.T) {
	nodes := newNodeManager()
	nodes.addNode("node1", &device.NodeInfo{
		ID: "node1", Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
		Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: {{
			ID: "GPU-a", Count: 7, Devmem: 40960, Devcore: 100, Mode: nvidia.MigMode, Health: true,
			MIGProfiles: []device.MigProfile{{Name: "1g.5gb", MemoryMB: 5120, Core: 14, Placements: []device.MigPlacement{{Start: 5, Size: 1}, {Start: 6, Size: 1}}}},
		}}},
	})
	pods := device.NewPodManager()
	pods.AddPod(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		UID: "pod-1", Name: "pod-1", Namespace: "default",
		Annotations: map[string]string{nvidia.MigAllocationsAnnotation: `[
			{"containerIndex":0,"deviceIndex":0,"gpuUUID":"GPU-a","profile":"1g.5gb","placement":{"start":6,"size":1}},
			{"containerIndex":0,"deviceIndex":1,"gpuUUID":"GPU-a","profile":"1g.5gb","placement":{"start":5,"size":1}}
		]`},
	}}, "node1", device.PodDevices{nvidia.NvidiaGPUDevice: {{{UUID: "GPU-a", Usedmem: 5120, Usedcores: 14}}}})

	s := Scheduler{nodeManager: nodes, podManager: pods}
	nodeNames := []string{"node1"}
	usage, _, _, err := s.getNodesUsage(&nodeNames, nil)
	if err != nil {
		t.Fatal(err)
	}
	deviceUsage := (*usage)["node1"].Devices.DeviceLists[0].Device
	if !deviceUsage.Health {
		t.Fatal("MIG device should remain healthy when every reservation can be restored")
	}
	allocations := deviceUsage.MigAllocationsInUse
	if len(allocations) != 2 || allocations[0].Placement != (device.MigPlacement{Start: 6, Size: 1}) || allocations[1].Placement != (device.MigPlacement{Start: 5, Size: 1}) {
		t.Fatalf("restored allocations: %+v", allocations)
	}
}
