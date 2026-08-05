package nvidia

import (
	"testing"

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
