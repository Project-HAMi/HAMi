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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
	"github.com/Project-HAMi/HAMi/pkg/util"
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

func twoNvidiaDeviceRequest(mode string) (*corev1.Pod, device.ContainerDeviceRequest, device.ContainerDeviceRequests) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "two-nvidia-devices",
			Namespace: "multi-device-test",
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "workload",
			Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
				"nvidia.com/gpu":    resource.MustParse("2"),
				"nvidia.com/gpumem": resource.MustParse("4000"),
			}},
		}}},
	}
	if mode != "" {
		pod.Annotations = map[string]string{nvidia.AllocateMode: mode}
	}
	gpuCount := pod.Spec.Containers[0].Resources.Limits["nvidia.com/gpu"]
	gpuMemory := pod.Spec.Containers[0].Resources.Limits["nvidia.com/gpumem"]
	request := device.ContainerDeviceRequest{
		Nums:   int32(gpuCount.Value()),
		Type:   nvidia.NvidiaGPUDevice,
		Memreq: int32(gpuMemory.Value()),
	}
	return pod, request, device.ContainerDeviceRequests{
		device.InRequestDevices[nvidia.NvidiaGPUDevice]: request,
	}
}

func singleNvidiaGPUNode(mode string, placements []device.MigPlacement) NodeUsage {
	gpu := &device.DeviceUsage{
		ID: "GPU-a", Count: 7, Totalmem: 40960, Totalcore: 100,
		Mode: mode, Type: nvidia.NvidiaGPUDevice, Health: true,
	}
	if mode == nvidia.MigMode {
		gpu.MigProfiles = []device.MigProfile{{
			Name: "1g.5gb", MemoryMB: 5120, Core: 14, Placements: placements,
		}}
	}
	return NodeUsage{
		Node:     &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "single-gpu-node"}},
		NodeInfo: &device.NodeInfo{},
		Devices:  policy.DeviceUsageList{DeviceLists: []*policy.DeviceListsScore{{Device: gpu}}},
	}
}

func TestIsMIGRequest(t *testing.T) {
	migDevices := []*device.DeviceUsage{{Mode: nvidia.MigMode}}
	hamiCoreDevices := []*device.DeviceUsage{{Mode: nvidia.HamiCoreMode}}
	migPod, _, _ := twoNvidiaDeviceRequest(nvidia.MigMode)
	hamiCorePod, _, _ := twoNvidiaDeviceRequest(nvidia.HamiCoreMode)
	nvidiaRequest := device.ContainerDeviceRequest{Type: nvidia.NvidiaGPUDevice}

	tests := []struct {
		name    string
		request device.ContainerDeviceRequest
		devices []*device.DeviceUsage
		pod     *corev1.Pod
		want    bool
	}{
		{
			name: "non-NVIDIA request", request: device.ContainerDeviceRequest{Type: "AMD"},
			devices: migDevices, pod: migPod,
		},
		{
			name: "explicit HAMi-core mode", request: nvidiaRequest,
			devices: migDevices, pod: hamiCorePod,
		},
		{
			name: "MIG mode without Pod", request: nvidiaRequest,
			devices: migDevices, want: true,
		},
		{
			name: "MIG mode annotation", request: nvidiaRequest,
			devices: migDevices, pod: migPod, want: true,
		},
		{
			name: "no MIG device", request: nvidiaRequest,
			devices: hamiCoreDevices,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isMIGRequest(test.request, test.devices, test.pod); got != test.want {
				t.Fatalf("isMIGRequest() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestFitInDevicesAllowsMultipleMigPlacementsOnSingleGPU(t *testing.T) {
	pod, request, requests := twoNvidiaDeviceRequest(nvidia.MigMode)
	node := singleNvidiaGPUNode(nvidia.MigMode, []device.MigPlacement{
		{Start: 0, Size: 1}, {Start: 1, Size: 1}, {Start: 2, Size: 1}, {Start: 3, Size: 1},
		{Start: 4, Size: 1}, {Start: 5, Size: 1}, {Start: 6, Size: 1},
	})

	devPlugin := device.GetDevices()[nvidia.NvidiaGPUDevice]
	directFit, directDevices, directReason := devPlugin.Fit(getNodeResources(node, nvidia.NvidiaGPUDevice), request, pod, node.NodeInfo, &device.PodDevices{})
	if !directFit || len(directDevices[nvidia.NvidiaGPUDevice]) != 2 {
		t.Fatalf("NVIDIA MIG Fit() = %v, devices = %+v, reason = %q; want two legal placements", directFit, directDevices, directReason)
	}

	allocated := make(device.PodDevices)
	fit, reason := fitInDevices(&node, requests, pod, node.NodeInfo, &allocated, util.DefaultDeviceScoringWeights())
	if !fit {
		t.Fatalf("fitInDevices() = false, reason = %q; NVIDIA MIG Fit() accepted the request", reason)
	}
	got := allocated[nvidia.NvidiaGPUDevice]
	if len(got) != 1 || len(got[0]) != 2 || got[0][0].UUID != "GPU-a" || got[0][1].UUID != "GPU-a" {
		t.Fatalf("allocated devices = %+v, want two MIG placements on GPU-a", got)
	}
	firstPlacement, firstOK := got[0][0].CustomInfo[nvidia.MigPlacementCustomInfo].(device.MigPlacement)
	secondPlacement, secondOK := got[0][1].CustomInfo[nvidia.MigPlacementCustomInfo].(device.MigPlacement)
	if !firstOK || !secondOK || firstPlacement == secondPlacement {
		t.Fatalf("allocated placements = %+v, %+v, want two distinct placements", firstPlacement, secondPlacement)
	}
}

func TestFitInDevicesRejectsInsufficientMigPlacementsOnSingleGPU(t *testing.T) {
	pod, request, requests := twoNvidiaDeviceRequest(nvidia.MigMode)
	node := singleNvidiaGPUNode(nvidia.MigMode, []device.MigPlacement{{Start: 6, Size: 1}})

	devPlugin := device.GetDevices()[nvidia.NvidiaGPUDevice]
	directFit, _, directReason := devPlugin.Fit(getNodeResources(node, nvidia.NvidiaGPUDevice), request, pod, node.NodeInfo, &device.PodDevices{})
	if directFit || common.ParseReason(directReason)[common.CardMigTopologyInfeasible] != 1 {
		t.Fatalf("NVIDIA MIG Fit() = %v, reason = %q; want placement infeasibility", directFit, directReason)
	}

	allocated := make(device.PodDevices)
	fit, reason := fitInDevices(&node, requests, pod, node.NodeInfo, &allocated, util.DefaultDeviceScoringWeights())
	if fit {
		t.Fatalf("fitInDevices() = true, devices = %+v; want insufficient placement failure", allocated)
	}
	if reason != directReason {
		t.Fatalf("fitInDevices() reason = %q, want MIG Fit() reason %q", reason, directReason)
	}
}

func TestFitInDevicesRetainsPhysicalCountCheckForNonMigGPU(t *testing.T) {
	pod, _, requests := twoNvidiaDeviceRequest(nvidia.HamiCoreMode)
	node := singleNvidiaGPUNode(nvidia.HamiCoreMode, nil)
	allocated := make(device.PodDevices)

	fit, reason := fitInDevices(&node, requests, pod, node.NodeInfo, &allocated, util.DefaultDeviceScoringWeights())
	if fit || reason != common.NodeInsufficientDevice {
		t.Fatalf("fitInDevices() = %v, reason = %q, devices = %+v; want physical-device-count rejection", fit, reason, allocated)
	}
}
