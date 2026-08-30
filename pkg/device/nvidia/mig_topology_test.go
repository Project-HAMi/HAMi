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
	"reflect"
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

// a100TopologyProfiles is the A100-40GB allowlist: eight memory slices, seven compute slices.
func a100TopologyProfiles() []device.MigProfile {
	return []device.MigProfile{
		{Name: "1g.5gb", Placements: []device.MigPlacement{{Start: 0, Size: 1}, {Start: 1, Size: 1}, {Start: 2, Size: 1}, {Start: 3, Size: 1}, {Start: 4, Size: 1}, {Start: 5, Size: 1}, {Start: 6, Size: 1}}},
		{Name: "2g.10gb", Placements: []device.MigPlacement{{Start: 0, Size: 2}, {Start: 2, Size: 2}, {Start: 4, Size: 2}}},
		{Name: "3g.20gb", Placements: []device.MigPlacement{{Start: 0, Size: 4}, {Start: 4, Size: 4}}},
		{Name: "7g.40gb", Placements: []device.MigPlacement{{Start: 0, Size: 8}}},
	}
}

// a30TopologyProfiles has four memory slices and four compute slices.
func a30TopologyProfiles() []device.MigProfile {
	return []device.MigProfile{
		{Name: "1g.6gb", Placements: []device.MigPlacement{{Start: 0, Size: 1}, {Start: 1, Size: 1}, {Start: 2, Size: 1}, {Start: 3, Size: 1}}},
		{Name: "2g.12gb", Placements: []device.MigPlacement{{Start: 0, Size: 2}, {Start: 2, Size: 2}}},
		{Name: "4g.24gb", Placements: []device.MigPlacement{{Start: 0, Size: 4}}},
	}
}

// h100TopologyProfiles is the full table NVIDIA reports for H100-80GB, including
// the media-extension 1g, the double-memory 1g and the 4g.
func h100TopologyProfiles() []device.MigProfile {
	single := []device.MigPlacement{{Start: 0, Size: 1}, {Start: 1, Size: 1}, {Start: 2, Size: 1}, {Start: 3, Size: 1}, {Start: 4, Size: 1}, {Start: 5, Size: 1}, {Start: 6, Size: 1}}
	double := []device.MigPlacement{{Start: 0, Size: 2}, {Start: 2, Size: 2}, {Start: 4, Size: 2}, {Start: 6, Size: 2}}
	return []device.MigProfile{
		{Name: "1g.10gb", Placements: single},
		{Name: "1g.10gb+me", Placements: single},
		{Name: "1g.20gb", Placements: double},
		{Name: "2g.20gb", Placements: []device.MigPlacement{{Start: 0, Size: 2}, {Start: 2, Size: 2}, {Start: 4, Size: 2}}},
		{Name: "3g.40gb", Placements: []device.MigPlacement{{Start: 0, Size: 4}, {Start: 4, Size: 4}}},
		{Name: "4g.40gb", Placements: []device.MigPlacement{{Start: 0, Size: 4}}},
		{Name: "7g.80gb", Placements: []device.MigPlacement{{Start: 0, Size: 8}}},
	}
}

// straddlingProfiles has a placement across the midpoint, so the halves are not independent.
func straddlingProfiles() []device.MigProfile {
	return []device.MigProfile{
		{Name: "1g", Placements: []device.MigPlacement{{Start: 0, Size: 1}, {Start: 1, Size: 1}, {Start: 2, Size: 1}, {Start: 3, Size: 1}}},
		{Name: "2g", Placements: []device.MigPlacement{{Start: 1, Size: 2}}},
	}
}

// sequentialPlacements places profiles one at a time, as pods arrive, and returns the chosen starts.
func sequentialPlacements(t *testing.T, allowed []device.MigProfile, occupied []device.MigPlacement, profiles []string) []uint32 {
	t.Helper()
	starts := make([]uint32, 0, len(profiles))
	for _, profile := range profiles {
		placement, ok := selectMigPlacement(allowed, occupied, profile)
		if !ok {
			t.Fatalf("profile %s could not be placed with occupied=%+v", profile, occupied)
		}
		starts = append(starts, placement.Start)
		occupied = append(occupied, placement)
	}
	return starts
}

func TestMigSliceCountComesFromPlacements(t *testing.T) {
	if got := migSliceCount(a100TopologyProfiles()); got != 8 {
		t.Fatalf("A100 slice count = %d, want 8", got)
	}
	if got := migSliceCount(a30TopologyProfiles()); got != 4 {
		t.Fatalf("A30 slice count = %d, want 4", got)
	}
	if got := migSliceCount(nil); got != 0 {
		t.Fatalf("empty table slice count = %d, want 0", got)
	}
	// A zero-width row must not stretch the card.
	malformed := []device.MigProfile{{Name: "x", Placements: []device.MigPlacement{{Start: 20, Size: 0}, {Start: 0, Size: 1}}}}
	if got := migSliceCount(malformed); got != 1 {
		t.Fatalf("malformed table slice count = %d, want 1", got)
	}
}

func TestMigRegionsSplitTheCardInHalves(t *testing.T) {
	tests := []struct {
		name     string
		profiles []device.MigProfile
		want     []migRegion
	}{
		{name: "A100", profiles: a100TopologyProfiles(), want: []migRegion{{start: 0, end: 4}, {start: 4, end: 8}}},
		{name: "A30", profiles: a30TopologyProfiles(), want: []migRegion{{start: 0, end: 2}, {start: 2, end: 4}}},
		{name: "H100 with 4g and double-memory 1g", profiles: h100TopologyProfiles(), want: []migRegion{{start: 0, end: 4}, {start: 4, end: 8}}},
		{name: "straddling placement keeps one region", profiles: straddlingProfiles(), want: []migRegion{{start: 0, end: 4}}},
		{name: "no placements", profiles: nil, want: []migRegion{{start: 0, end: 0}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := migRegions(tc.profiles, migSliceCount(tc.profiles))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("regions = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestMigReservedRegionIsTheUpperHalf(t *testing.T) {
	// A100 and H100: [4,8) holds fewer placements than [0,4). A30: both halves tie, so the upper one wins.
	for _, tc := range []struct {
		name     string
		profiles []device.MigProfile
	}{
		{name: "A100", profiles: a100TopologyProfiles()},
		{name: "A30", profiles: a30TopologyProfiles()},
		{name: "H100", profiles: h100TopologyProfiles()},
	} {
		regions := migRegions(tc.profiles, migSliceCount(tc.profiles))
		if got := migReservedRegion(tc.profiles, regions); got != 1 {
			t.Fatalf("%s reserved region = %d, want 1", tc.name, got)
		}
	}
	single := straddlingProfiles()
	if got := migReservedRegion(single, migRegions(single, migSliceCount(single))); got != 0 {
		t.Fatalf("single-region reserved index = %d, want 0", got)
	}
}

func TestBetterMigPlacementComparesFieldsInOrder(t *testing.T) {
	same := migPlacementScore{emptyRegions: 1, zoneMismatch: 0, edgeDistance: 1, start: 4}
	tests := []struct {
		name string
		a, b migPlacementScore
		want bool
	}{
		{name: "an untouched half beats everything", a: migPlacementScore{emptyRegions: 1, zoneMismatch: 1}, b: migPlacementScore{emptyRegions: 0, edgeDistance: 3}, want: true},
		{name: "the matching zone beats the outer edge", a: migPlacementScore{zoneMismatch: 0, start: 6}, b: migPlacementScore{zoneMismatch: 1, edgeDistance: 3}, want: true},
		{name: "the outer edge beats a lower start", a: migPlacementScore{edgeDistance: 2, start: 6}, b: migPlacementScore{edgeDistance: 0, start: 4}, want: true},
		{name: "lower start breaks the tie", a: migPlacementScore{start: 0}, b: migPlacementScore{start: 1}, want: true},
		{name: "equal scores are not better", a: same, b: same, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := betterMigPlacement(tc.a, tc.b); got != tc.want {
				t.Fatalf("betterMigPlacement(%+v, %+v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestFindMigProfileReturnsTheFirstMatch(t *testing.T) {
	profiles := []device.MigProfile{
		{Name: "1g", Placements: []device.MigPlacement{{Start: 0, Size: 1}}},
		{Name: "1g", Placements: []device.MigPlacement{{Start: 5, Size: 1}}},
	}
	got, ok := findMigProfile(profiles, "1g")
	if !ok || got.Placements[0].Start != 0 {
		t.Fatalf("findMigProfile = %+v, %v; want the first 1g entry", got, ok)
	}
	if _, ok := findMigProfile(profiles, "9g"); ok {
		t.Fatal("unknown profile must not be found")
	}
}

func TestMigProfileFootprintIsTheLargestPlacement(t *testing.T) {
	if got := migProfileFootprint(device.MigProfile{Name: "empty"}); got != 0 {
		t.Fatalf("footprint of a profile without placements = %d, want 0", got)
	}
	mixed := device.MigProfile{Name: "x", Placements: []device.MigPlacement{{Start: 0, Size: 1}, {Start: 0, Size: 2}}}
	if got := migProfileFootprint(mixed); got != 2 {
		t.Fatalf("footprint = %d, want 2", got)
	}
}

func TestOccupiedMigPlacementsFollowsAllocationOrder(t *testing.T) {
	allocations := []device.MigAllocation{
		{Placement: device.MigPlacement{Start: 4, Size: 4}},
		{Placement: device.MigPlacement{Start: 0, Size: 1}},
	}
	got := occupiedMigPlacements(allocations)
	want := []device.MigPlacement{{Start: 4, Size: 4}, {Start: 0, Size: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("occupied = %+v, want %+v", got, want)
	}
	if got := occupiedMigPlacements(nil); len(got) != 0 {
		t.Fatalf("occupied of no allocations = %+v, want empty", got)
	}
}

func TestCanPlaceMigProfilesA100(t *testing.T) {
	allowed := a100TopologyProfiles()

	if !canPlaceMigProfiles(allowed, nil, []string{"3g.20gb", "2g.10gb", "1g.5gb", "1g.5gb"}) {
		t.Fatal("A100 all-balanced geometry should be feasible")
	}
	if !canPlaceMigProfiles(allowed, []device.MigPlacement{{Start: 4, Size: 4}}, []string{"2g.10gb", "1g.5gb", "1g.5gb"}) {
		t.Fatal("remaining all-balanced placements should be feasible")
	}
	if canPlaceMigProfiles(allowed, []device.MigPlacement{{Start: 0, Size: 4}, {Start: 4, Size: 4}}, []string{"1g.5gb"}) {
		t.Fatal("no profile should overlap two active 3g placements")
	}
	if canPlaceMigProfiles(allowed, []device.MigPlacement{{Start: 0, Size: 2}, {Start: 2, Size: 2}, {Start: 4, Size: 2}}, []string{"2g.10gb"}) {
		t.Fatal("fourth 2g placement should be rejected")
	}
	if canPlaceMigProfiles(allowed, nil, []string{"1g.5gb", "9g.unknown"}) {
		t.Fatal("unknown profile names must not be placeable")
	}
}

func TestSelectMigPlacementUsesBalancedPacking(t *testing.T) {
	// 3g takes the reserved upper half so both 2g spans stay open; 2g then takes the lowest span (NVIDIA config 9).
	got := sequentialPlacements(t, a100TopologyProfiles(), nil, []string{"3g.20gb", "2g.10gb", "1g.5gb", "1g.5gb"})
	want := []uint32{4, 0, 2, 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("balanced packing starts = %v, want %v", got, want)
	}
}

func TestSelectMigPlacementPacksEachHalfSeparately(t *testing.T) {
	tests := []struct {
		name     string
		occupied []device.MigPlacement
		arrivals []string
		want     []uint32
	}{
		{
			// Small requests fill the lower half from slot 0 instead of eating the upper half a 3g needs (config 11 path).
			name:     "repeated 1g",
			arrivals: []string{"1g.5gb", "1g.5gb", "1g.5gb", "1g.5gb"},
			want:     []uint32{0, 1, 2, 3},
		},
		{
			// Once the lower half is full, the upper half fills from its outer edge (config 19).
			name:     "seven 1g",
			arrivals: []string{"1g.5gb", "1g.5gb", "1g.5gb", "1g.5gb", "1g.5gb", "1g.5gb", "1g.5gb"},
			want:     []uint32{0, 1, 2, 3, 6, 5, 4},
		},
		{
			// The order that fragments the card under first-fit now fits completely (config 10).
			name:     "mixed small then large",
			arrivals: []string{"1g.5gb", "1g.5gb", "2g.10gb", "3g.20gb"},
			want:     []uint32{0, 1, 2, 4},
		},
		{
			// A lone 3g goes to the reserved upper half so two 2g spans stay open instead of one (config 8).
			name:     "single 3g prefers the upper half",
			arrivals: []string{"3g.20gb", "2g.10gb", "2g.10gb"},
			want:     []uint32{4, 0, 2},
		},
		{
			// The second 3g takes the only span left (config 5).
			name:     "two 3g",
			arrivals: []string{"3g.20gb", "3g.20gb"},
			want:     []uint32{4, 0},
		},
		{
			// A 3g arriving after a 1g still has its half; the next 1g keeps packing below (config 11 path).
			name:     "1g then 3g then 1g",
			occupied: nil,
			arrivals: []string{"1g.5gb", "3g.20gb", "1g.5gb"},
			want:     []uint32{0, 4, 1},
		},
		{
			// Three 2g and a 1g land exactly as NVIDIA config 12.
			name:     "three 2g then 1g",
			arrivals: []string{"2g.10gb", "2g.10gb", "2g.10gb", "1g.5gb"},
			want:     []uint32{0, 2, 4, 6},
		},
		{
			// A running 1g at slot 6 already spoils the upper half; the new 1g joins it there,
			// next to the outer edge, and leaves the lower half whole for the 3g.
			name:     "existing instance blocks one 3g span",
			occupied: []device.MigPlacement{{Start: 6, Size: 1}},
			arrivals: []string{"1g.5gb", "3g.20gb"},
			want:     []uint32{5, 0},
		},
		{
			// With both halves already touched, small requests keep packing the lower half:
			// the 1g takes slot 1 rather than spoiling the upper 2g span, and the 2g still
			// finds [2,4) below, so the span at slot 4 survives for a later request.
			name:     "both halves touched",
			occupied: []device.MigPlacement{{Start: 0, Size: 1}, {Start: 6, Size: 1}},
			arrivals: []string{"1g.5gb", "2g.10gb"},
			want:     []uint32{1, 2},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sequentialPlacements(t, a100TopologyProfiles(), tc.occupied, tc.arrivals)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("starts = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSelectMigPlacementHandlesTheFullH100Table(t *testing.T) {
	tests := []struct {
		name     string
		arrivals []string
		want     []uint32
	}{
		{
			// Double-memory 1g packs the lower half first and then uses [6,8), the only
			// way to reach slice 7 without a 3g.
			name:     "four double-memory 1g",
			arrivals: []string{"1g.20gb", "1g.20gb", "1g.20gb", "1g.20gb"},
			want:     []uint32{0, 2, 6, 4},
		},
		{
			// 3g in the reserved half, everything else packed below: all eight slices used.
			name:     "3g with mixed small profiles",
			arrivals: []string{"3g.40gb", "1g.20gb", "1g.10gb", "1g.10gb"},
			want:     []uint32{4, 0, 2, 3},
		},
		{
			// The media-extension variant shares the plain 1g placements.
			name:     "media extension variant",
			arrivals: []string{"1g.10gb+me", "1g.10gb"},
			want:     []uint32{0, 1},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sequentialPlacements(t, h100TopologyProfiles(), nil, tc.arrivals)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("starts = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPlaceMigProfilesHandles4gWhenRequestedTogether(t *testing.T) {
	allowed := h100TopologyProfiles()
	// The 4g only fits the lower half; it is placed first because it is the pickiest,
	// and the 1g then takes the upper half from its outer edge.
	got, ok := placeMigProfiles(allowed, nil, []string{"1g.10gb", "4g.40gb"})
	if !ok {
		t.Fatal("1g + 4g should fit an empty H100")
	}
	want := []device.MigPlacement{{Start: 6, Size: 1}, {Start: 0, Size: 4}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("placements = %+v, want %+v", got, want)
	}
	got, ok = placeMigProfiles(allowed, nil, []string{"4g.40gb", "3g.40gb"})
	if !ok {
		t.Fatal("4g + 3g should fill an empty H100")
	}
	want = []device.MigPlacement{{Start: 0, Size: 4}, {Start: 4, Size: 4}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("placements = %+v, want %+v", got, want)
	}
}

func TestPlaceMigProfilesBacktracksWhenGreedyFails(t *testing.T) {
	t.Run("review counterexample", func(t *testing.T) {
		// a:[0,1),[4,5) and b:[0,1),[1,2). The greedy path takes a at 0 and strands the
		// second b; the only valid layout is a at 4 with both b placements.
		profiles := []device.MigProfile{
			{Name: "a", Placements: []device.MigPlacement{{Start: 0, Size: 1}, {Start: 4, Size: 1}}},
			{Name: "b", Placements: []device.MigPlacement{{Start: 0, Size: 1}, {Start: 1, Size: 1}}},
		}
		got, ok := placeMigProfiles(profiles, nil, []string{"a", "b", "b"})
		if !ok {
			t.Fatal("a + b + b has a valid layout and must be placed")
		}
		want := []device.MigPlacement{{Start: 4, Size: 1}, {Start: 0, Size: 1}, {Start: 1, Size: 1}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("placements = %+v, want %+v", got, want)
		}
	})
	t.Run("H100 double-memory 1g with four 1g", func(t *testing.T) {
		// The greedy path packs both 1g.20gb into the lower half and leaves only three 1g
		// slots. The search moves the second 1g.20gb to [6,8), the only way to use slice 7.
		got, ok := placeMigProfiles(h100TopologyProfiles(), nil, []string{"1g.20gb", "1g.20gb", "1g.10gb", "1g.10gb", "1g.10gb", "1g.10gb"})
		if !ok {
			t.Fatal("2 x 1g.20gb + 4 x 1g.10gb fills an H100 and must be placed")
		}
		want := []device.MigPlacement{{Start: 0, Size: 2}, {Start: 6, Size: 2}, {Start: 2, Size: 1}, {Start: 3, Size: 1}, {Start: 5, Size: 1}, {Start: 4, Size: 1}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("placements = %+v, want %+v", got, want)
		}
	})
}

func TestPlaceMigProfilesGivesUpWithinBudget(t *testing.T) {
	// Eight 1g add up to eight slices, but slice 7 is unreachable by a 1g, so no layout
	// exists. The search must report that within its budget instead of running unbounded.
	requested := []string{"1g.5gb", "1g.5gb", "1g.5gb", "1g.5gb", "1g.5gb", "1g.5gb", "1g.5gb", "1g.5gb"}
	if _, ok := placeMigProfiles(a100TopologyProfiles(), nil, requested); ok {
		t.Fatal("eight 1g must not fit an A100")
	}
}

func TestSelectMigPlacementFallsBackToFirstFit(t *testing.T) {
	// Without independent halves the score reduces to the lowest free start.
	got := sequentialPlacements(t, straddlingProfiles(), nil, []string{"1g", "1g", "1g"})
	want := []uint32{0, 1, 2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("first-fit starts = %v, want %v", got, want)
	}
}

func TestSelectMigPlacementIgnoresMalformedOccupiedEntries(t *testing.T) {
	// A zero-width entry occupies nothing and an entry past the card touches nothing.
	occupied := []device.MigPlacement{{Start: 0, Size: 0}, {Start: 8, Size: 2}}
	got, ok := selectMigPlacement(a100TopologyProfiles(), occupied, "1g.5gb")
	if !ok || got.Start != 0 || got.Size != 1 {
		t.Fatalf("placement = %+v, %v; want slot 0", got, ok)
	}
}

func TestSelectMigPlacementRejectsWhenNothingFree(t *testing.T) {
	occupied := []device.MigPlacement{{Start: 0, Size: 4}, {Start: 4, Size: 4}}
	if _, ok := selectMigPlacement(a100TopologyProfiles(), occupied, "1g.5gb"); ok {
		t.Fatal("1g must not be placed on a fully occupied card")
	}
	if _, ok := selectMigPlacement(a100TopologyProfiles(), nil, "9g.unknown"); ok {
		t.Fatal("unknown profile must not be placed")
	}
	malformed := []device.MigProfile{
		{Name: "no-placements"},
		{Name: "zero-width", Placements: []device.MigPlacement{{Start: 0, Size: 0}}},
	}
	if _, ok := selectMigPlacement(malformed, nil, "no-placements"); ok {
		t.Fatal("a profile without placements must not be placed")
	}
	if _, ok := selectMigPlacement(malformed, nil, "zero-width"); ok {
		t.Fatal("a zero-width placement must not be handed out")
	}
}

func TestNextMigRequestOrdersByScarcity(t *testing.T) {
	allowed := a100TopologyProfiles()
	requested, ok := resolveMigRequests(allowed, []string{"1g.5gb", "2g.10gb", "3g.20gb"})
	if !ok {
		t.Fatal("known names must resolve")
	}
	if got := nextMigRequest(nil, requested, []bool{false, false, false}); got != 2 {
		t.Fatalf("first pick = %d, want the 3g (2)", got)
	}
	if got := nextMigRequest(nil, requested, []bool{false, false, true}); got != 1 {
		t.Fatalf("second pick = %d, want the 2g (1)", got)
	}
	if got := nextMigRequest(nil, requested, []bool{true, true, true}); got != -1 {
		t.Fatalf("pick with everything placed = %d, want -1", got)
	}

	// With slots 1 and 2 taken, 2g and 3g both have a single free span; the larger footprint goes first.
	tied, _ := resolveMigRequests(allowed, []string{"2g.10gb", "3g.20gb"})
	occupied := []device.MigPlacement{{Start: 1, Size: 1}, {Start: 2, Size: 1}}
	if got := nextMigRequest(occupied, tied, []bool{false, false}); got != 1 {
		t.Fatalf("tied pick = %d, want the 3g (1)", got)
	}

	// Identical requests keep their listed order.
	twins, _ := resolveMigRequests(allowed, []string{"1g.5gb", "1g.5gb"})
	if got := nextMigRequest(nil, twins, []bool{false, false}); got != 0 {
		t.Fatalf("twin pick = %d, want 0", got)
	}
}

func TestPlaceMigProfilesPlacesPickiestFirst(t *testing.T) {
	// Listing the 1g first must not let it take the 3g's last span. The 1g then
	// fills the upper half from its outer edge, so slot 5 rather than slot 4.
	occupied := []device.MigPlacement{{Start: 6, Size: 1}}
	got, ok := placeMigProfiles(a100TopologyProfiles(), occupied, []string{"1g.5gb", "3g.20gb"})
	if !ok {
		t.Fatal("1g + 3g should fit next to a running 1g at slot 6")
	}
	want := []device.MigPlacement{{Start: 5, Size: 1}, {Start: 0, Size: 4}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("placements = %+v, want %+v", got, want)
	}
}

func TestPlaceMigProfilesKeepsLargeProfilesInReservedHalf(t *testing.T) {
	// Both 3g spans are legal; the upper one is the reserved half, so both 2g spans below stay alive (config 8).
	got, ok := placeMigProfiles(a100TopologyProfiles(), nil, []string{"2g.10gb", "2g.10gb", "3g.20gb"})
	if !ok {
		t.Fatal("3g + 2g + 2g should fill an empty A100")
	}
	want := []device.MigPlacement{{Start: 0, Size: 2}, {Start: 2, Size: 2}, {Start: 4, Size: 4}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("placements = %+v, want %+v", got, want)
	}
}

func TestPlaceMigProfilesResultFollowsRequestOrder(t *testing.T) {
	// Placement order is pickiest first, but result[i] must still belong to requested[i].
	got, ok := placeMigProfiles(a100TopologyProfiles(), nil, []string{"1g.5gb", "3g.20gb", "2g.10gb", "1g.5gb"})
	if !ok {
		t.Fatal("1g + 3g + 2g + 1g should fill an empty A100")
	}
	want := []device.MigPlacement{{Start: 2, Size: 1}, {Start: 4, Size: 4}, {Start: 0, Size: 2}, {Start: 3, Size: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("placements = %+v, want %+v", got, want)
	}
}

func TestPlaceMigProfilesIsDeterministic(t *testing.T) {
	requested := []string{"1g.5gb", "2g.10gb", "1g.5gb", "3g.20gb"}
	first, ok := placeMigProfiles(a100TopologyProfiles(), nil, requested)
	if !ok {
		t.Fatal("request should fit an empty A100")
	}
	for i := range 20 {
		again, ok := placeMigProfiles(a100TopologyProfiles(), nil, requested)
		if !ok || !reflect.DeepEqual(again, first) {
			t.Fatalf("run %d produced %+v, want %+v", i, again, first)
		}
	}
}

func TestPlaceMigProfilesHandlesTheFullCardProfile(t *testing.T) {
	allowed := a100TopologyProfiles()
	got, ok := placeMigProfiles(allowed, nil, []string{"7g.40gb"})
	if !ok || !reflect.DeepEqual(got, []device.MigPlacement{{Start: 0, Size: 8}}) {
		t.Fatalf("7g on an empty card = %+v, %v; want slot 0 size 8", got, ok)
	}
	if _, ok := placeMigProfiles(allowed, nil, []string{"7g.40gb", "1g.5gb"}); ok {
		t.Fatal("nothing fits next to a 7g")
	}
	if _, ok := placeMigProfiles(allowed, nil, []string{"1g.5gb", "7g.40gb"}); ok {
		t.Fatal("the 7g is placed first regardless of listing order and must still fail")
	}
	if _, ok := placeMigProfiles(allowed, []device.MigPlacement{{Start: 6, Size: 1}}, []string{"7g.40gb"}); ok {
		t.Fatal("a 7g must not be placed on a card with any running instance")
	}
}

func TestPlaceMigProfilesReportsInfeasible(t *testing.T) {
	allowed := a100TopologyProfiles()
	// Two 3g instances leave no legal slice for anything.
	if _, ok := placeMigProfiles(allowed, []device.MigPlacement{{Start: 0, Size: 4}, {Start: 4, Size: 4}}, []string{"1g.5gb"}); ok {
		t.Fatal("full card must reject a 1g")
	}
	// Eight compute slices are more than the card has.
	if _, ok := placeMigProfiles(allowed, nil, []string{"3g.20gb", "3g.20gb", "1g.5gb"}); ok {
		t.Fatal("two 3g plus a 1g must not fit an A100")
	}
	// 2+2+2+1+1 adds up to eight slices, but slice 7 is only reachable by a 3g.
	if _, ok := placeMigProfiles(allowed, nil, []string{"2g.10gb", "2g.10gb", "2g.10gb", "1g.5gb", "1g.5gb"}); ok {
		t.Fatal("three 2g plus two 1g must not fit an A100")
	}
	if got, ok := placeMigProfiles(allowed, nil, nil); !ok || len(got) != 0 {
		t.Fatalf("empty request = %+v, %v; want empty success", got, ok)
	}
}

func TestPlaceMigProfilesUsesReportedGeometry(t *testing.T) {
	// A30 has four memory slices; the slice count and every start come from the placements.
	allowed := a30TopologyProfiles()
	got := sequentialPlacements(t, allowed, nil, []string{"1g.6gb", "2g.12gb", "1g.6gb"})
	want := []uint32{0, 2, 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("A30 starts = %v, want %v", got, want)
	}
	if _, ok := placeMigProfiles(allowed, nil, []string{"4g.24gb", "1g.6gb"}); ok {
		t.Fatal("4g fills an A30; nothing else may be placed with it")
	}
	if _, ok := placeMigProfiles(allowed, nil, []string{"2g.12gb", "1g.6gb", "1g.6gb"}); !ok {
		t.Fatal("2g + 1g + 1g should fill an A30")
	}
	// A 2g fills a whole A30 half, so it is treated like a 3g on A100 and goes to the reserved half first.
	pair, ok := placeMigProfiles(allowed, nil, []string{"2g.12gb", "2g.12gb"})
	if !ok || !reflect.DeepEqual(pair, []device.MigPlacement{{Start: 2, Size: 2}, {Start: 0, Size: 2}}) {
		t.Fatalf("two 2g on an A30 = %+v, %v; want slots 2 then 0", pair, ok)
	}
}
