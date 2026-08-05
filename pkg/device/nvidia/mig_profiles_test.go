package nvidia

import (
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

func TestValidateMigProfileAllowlist(t *testing.T) {
	valid := []device.AllowedMigProfiles{{Models: []string{"A100"}, Profiles: []string{"1g.5gb", "2g.10gb"}}}
	if err := ValidateMigProfileAllowlist(valid); err != nil {
		t.Fatalf("valid allowlist rejected: %v", err)
	}
	invalid := []device.AllowedMigProfiles{{Models: []string{"A100"}}}
	if err := ValidateMigProfileAllowlist(invalid); err == nil {
		t.Fatal("allowlist without profiles should fail")
	}
}

func TestEncodeDecodeMigAllocations(t *testing.T) {
	pd := device.PodSingleDevice{{{
		UUID: "GPU-a", Type: NvidiaGPUDevice, Usedmem: 5120, Usedcores: 14,
		CustomInfo: map[string]any{
			MigProfileCustomInfo:   "1g.5gb",
			MigPlacementCustomInfo: device.MigPlacement{Start: 6, Size: 1},
		},
	}}}
	raw, ok := EncodeMigAllocations(pd)
	if !ok {
		t.Fatal("expected MIG allocation annotation")
	}
	allocations, err := DecodeMigAllocations(raw)
	if err != nil {
		t.Fatalf("decode allocation: %v", err)
	}
	if len(allocations) != 1 {
		t.Fatalf("allocations=%d, want 1", len(allocations))
	}
	allocation := allocations[0]
	if allocation.ContainerIndex != 0 || allocation.DeviceIndex != 0 || allocation.GPUUUID != "GPU-a" || allocation.Profile != "1g.5gb" || allocation.Placement != (device.MigPlacement{Start: 6, Size: 1}) {
		t.Fatalf("unexpected allocation: %+v", allocation)
	}
}

func TestDecodeMigAllocationsRejectsDuplicateDeviceIndex(t *testing.T) {
	raw := `[{"containerIndex":0,"deviceIndex":0,"gpuUUID":"GPU-a","profile":"1g.5gb","placement":{"start":6,"size":1}},{"containerIndex":0,"deviceIndex":0,"gpuUUID":"GPU-a","profile":"1g.5gb","placement":{"start":5,"size":1}}]`
	if _, err := DecodeMigAllocations(raw); err == nil {
		t.Fatal("duplicate container/device allocation index should fail")
	}
}
