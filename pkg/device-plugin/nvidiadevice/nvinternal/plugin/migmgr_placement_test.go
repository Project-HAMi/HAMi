package plugin

import (
	"testing"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

func TestChooseFreePlacementPackingDirection(t *testing.T) {
	possible := []nvml.GpuInstancePlacement{
		{Start: 0, Size: 1},
		{Start: 6, Size: 1},
		{Start: 3, Size: 1},
	}
	high, err := chooseFreePlacement(append([]nvml.GpuInstancePlacement(nil), possible...), nil, true)
	if err != nil || high.Start != 6 {
		t.Fatalf("high packing = %+v, %v; want start 6", high, err)
	}
	low, err := chooseFreePlacement(append([]nvml.GpuInstancePlacement(nil), possible...), nil, false)
	if err != nil || low.Start != 0 {
		t.Fatalf("low packing = %+v, %v; want start 0", low, err)
	}
	free, err := chooseFreePlacement(append([]nvml.GpuInstancePlacement(nil), possible...), map[uint32]uint32{6: 1}, true)
	if err != nil || free.Start != 3 {
		t.Fatalf("overlap-aware high packing = %+v, %v; want start 3", free, err)
	}
}

func TestPreferHighPlacementMatchesA100BalancedLayout(t *testing.T) {
	if !preferHighPlacement(1) || !preferHighPlacement(3) {
		t.Fatal("1g and 3g profiles must pack from high placements")
	}
	if preferHighPlacement(2) {
		t.Fatal("2g profiles must pack from low placements")
	}
}

func TestPlacementCandidatesPreferPreviousThenFallback(t *testing.T) {
	previous := nvml.GpuInstancePlacement{Start: 6, Size: 1}
	possible := []nvml.GpuInstancePlacement{
		{Start: 5, Size: 1},
		{Start: 3, Size: 1},
		{Start: 6, Size: 1},
	}
	want := []nvml.GpuInstancePlacement{
		{Start: 6, Size: 1},
		{Start: 5, Size: 1},
		{Start: 3, Size: 1},
	}
	got := placementCandidates(previous, possible)
	if len(got) != len(want) {
		t.Fatalf("candidate count = %d, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
