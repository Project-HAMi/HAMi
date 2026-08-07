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

package nvidia

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

func a100MigProfiles() []device.MigProfile {
	return []device.MigProfile{
		{Name: "1g.5gb", MemoryMB: 5120, Core: 14, SliceCount: 1, InstanceCount: 7, Placements: []device.MigPlacement{{Start: 0, Size: 1}, {Start: 1, Size: 1}, {Start: 2, Size: 1}, {Start: 3, Size: 1}, {Start: 4, Size: 1}, {Start: 5, Size: 1}, {Start: 6, Size: 1}}},
		{Name: "2g.10gb", MemoryMB: 10240, Core: 28, SliceCount: 2, InstanceCount: 3, Placements: []device.MigPlacement{{Start: 0, Size: 2}, {Start: 2, Size: 2}, {Start: 4, Size: 2}}},
		{Name: "3g.20gb", MemoryMB: 20480, Core: 42, SliceCount: 3, InstanceCount: 2, Placements: []device.MigPlacement{{Start: 0, Size: 4}, {Start: 4, Size: 4}}},
	}
}

func TestAddResourceUsageUsesReportedProfileAndPlacement(t *testing.T) {
	dev := &NvidiaGPUDevices{}
	usage := &device.DeviceUsage{ID: "GPU-a", Mode: MigMode, MigProfiles: a100MigProfiles()}
	ctr := &device.ContainerDevice{UUID: "GPU-a", Usedmem: 10000}
	if err := dev.AddResourceUsage(nil, usage, ctr); err != nil {
		t.Fatalf("allocate MIG capability: %v", err)
	}
	if ctr.UUID != "GPU-a" {
		t.Fatalf("physical UUID was rewritten: %s", ctr.UUID)
	}
	if ctr.Usedmem != 10240 || ctr.Usedcores != 28 {
		t.Fatalf("allocated resources=(%d,%d), want reported 2g metadata", ctr.Usedmem, ctr.Usedcores)
	}
	if ctr.CustomInfo[MigProfileCustomInfo] != "2g.10gb" || ctr.CustomInfo[MigPlacementCustomInfo] != (device.MigPlacement{Start: 0, Size: 2}) {
		t.Fatalf("unexpected scheduler reservation: %+v", ctr.CustomInfo)
	}
}

func TestCustomFilterUsesReportedPlacementCapacity(t *testing.T) {
	dev := &NvidiaGPUDevices{}
	usage := &device.DeviceUsage{
		Mode: MigMode, MigProfiles: a100MigProfiles(),
		MigAllocationsInUse: []device.MigAllocation{
			{Profile: "2g.10gb", Placement: device.MigPlacement{Start: 0, Size: 2}},
			{Profile: "2g.10gb", Placement: device.MigPlacement{Start: 2, Size: 2}},
			{Profile: "2g.10gb", Placement: device.MigPlacement{Start: 4, Size: 2}},
		},
	}
	if dev.CustomFilterRule(nil, device.ContainerDeviceRequest{Memreq: 10000}, nil, usage) {
		t.Fatal("fourth 2g request should not fit reported placements")
	}
	if !dev.CustomFilterRule(nil, device.ContainerDeviceRequest{Memreq: 5000}, nil, usage) {
		t.Fatal("1g request should fit the remaining placement")
	}
}

func TestCustomFilterIgnoresQueuedAllocationsForOtherGPUs(t *testing.T) {
	dev := &NvidiaGPUDevices{}
	usage := &device.DeviceUsage{
		ID: "GPU-a", Mode: MigMode, MigProfiles: a100MigProfiles(),
		MigAllocationsInUse: []device.MigAllocation{
			{Profile: "2g.10gb", Placement: device.MigPlacement{Start: 0, Size: 2}},
			{Profile: "2g.10gb", Placement: device.MigPlacement{Start: 2, Size: 2}},
			{Profile: "2g.10gb", Placement: device.MigPlacement{Start: 4, Size: 2}},
		},
	}
	queued := device.ContainerDevices{{UUID: "GPU-b", Usedmem: 5000}}
	if !dev.CustomFilterRule(nil, device.ContainerDeviceRequest{Memreq: 5000}, queued, usage) {
		t.Fatal("allocation queued on another GPU consumed this GPU's remaining placement")
	}
	queued[0].UUID = "GPU-a"
	if dev.CustomFilterRule(nil, device.ContainerDeviceRequest{Memreq: 5000}, queued, usage) {
		t.Fatal("two allocations on this GPU should not fit its single remaining placement")
	}
}

func TestFitUsesEffectivePercentageMemoryForMigPlacement(t *testing.T) {
	dev := InitNvidiaDevice(NvidiaConfig{})
	usage := &device.DeviceUsage{
		ID: "GPU-a", Type: NvidiaGPUDevice, Mode: MigMode, Health: true,
		Count: 7, Used: 3, Totalmem: 40960, Usedmem: 30720, Totalcore: 100,
		MigProfiles: a100MigProfiles(),
		MigAllocationsInUse: []device.MigAllocation{
			{Profile: "2g.10gb", Placement: device.MigPlacement{Start: 0, Size: 2}},
			{Profile: "2g.10gb", Placement: device.MigPlacement{Start: 2, Size: 2}},
			{Profile: "2g.10gb", Placement: device.MigPlacement{Start: 4, Size: 2}},
		},
	}
	request := device.ContainerDeviceRequest{
		Nums: 1, Type: NvidiaGPUDevice, MemPercentagereq: 25,
	}
	fit, _, _ := dev.Fit([]*device.DeviceUsage{usage}, request, &corev1.Pod{}, &device.NodeInfo{}, &device.PodDevices{})
	if fit {
		t.Fatal("25 percent memory request should not fit when only a 1g placement remains")
	}
}
