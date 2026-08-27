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

// a100WithTwoSliceProfile adds 1g.10gb, the only small profile that can reach slice 7.
func a100WithTwoSliceProfile() []device.MigProfile {
	return append(a100TopologyProfiles(), device.MigProfile{
		Name: "1g.10gb", Placements: []device.MigPlacement{{Start: 0, Size: 2}, {Start: 2, Size: 2}, {Start: 4, Size: 2}, {Start: 6, Size: 2}},
	})
}

// a30TopologyProfiles has four memory slices and four compute slices.
func a30TopologyProfiles() []device.MigProfile {
	return []device.MigProfile{
		{Name: "1g.6gb", Placements: []device.MigPlacement{{Start: 0, Size: 1}, {Start: 1, Size: 1}, {Start: 2, Size: 1}, {Start: 3, Size: 1}}},
		{Name: "2g.12gb", Placements: []device.MigPlacement{{Start: 0, Size: 2}, {Start: 2, Size: 2}}},
		{Name: "4g.24gb", Placements: []device.MigPlacement{{Start: 0, Size: 4}}},
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

func TestSelectMigPlacementUsesBalancedPacking(t *testing.T) {
	// 3g takes the top half so both 2g spans stay open; 2g then takes the lowest span.
	got := sequentialPlacements(t, a100TopologyProfiles(), nil, []string{"3g.20gb", "2g.10gb", "1g.5gb", "1g.5gb"})
	want := []uint32{4, 0, 2, 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("balanced packing starts = %v, want %v", got, want)
	}
}

func TestSelectMigPlacementKeepsLargestFreeRun(t *testing.T) {
	tests := []struct {
		name     string
		occupied []device.MigPlacement
		arrivals []string
		want     []uint32
	}{
		{
			// Small requests fill from the bottom instead of eating the top slices a 3g needs.
			name:     "repeated 1g",
			arrivals: []string{"1g.5gb", "1g.5gb", "1g.5gb", "1g.5gb"},
			want:     []uint32{0, 1, 2, 3},
		},
		{
			// The order that fragments the card under first-fit now fits completely.
			name:     "mixed small then large",
			arrivals: []string{"1g.5gb", "1g.5gb", "2g.10gb", "3g.20gb"},
			want:     []uint32{0, 1, 2, 4},
		},
		{
			// A lone 3g goes to the top so two 2g spans stay open instead of one.
			name:     "single 3g prefers the top half",
			arrivals: []string{"3g.20gb", "2g.10gb", "2g.10gb"},
			want:     []uint32{4, 0, 2},
		},
		{
			// A running 1g at slot 6 rules out the top 3g span; the new 1g must not take slot 0 too.
			name:     "existing instance blocks one 3g span",
			occupied: []device.MigPlacement{{Start: 6, Size: 1}},
			arrivals: []string{"1g.5gb", "3g.20gb"},
			want:     []uint32{5, 0},
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

func TestSelectMigPlacementRejectsWhenNothingFree(t *testing.T) {
	occupied := []device.MigPlacement{{Start: 0, Size: 4}, {Start: 4, Size: 4}}
	if _, ok := selectMigPlacement(a100TopologyProfiles(), occupied, "1g.5gb"); ok {
		t.Fatal("1g must not be placed on a fully occupied card")
	}
	if _, ok := selectMigPlacement(a100TopologyProfiles(), nil, "9g.unknown"); ok {
		t.Fatal("unknown profile must not be placed")
	}
}

func TestPlaceMigProfilesPlacesPickiestFirst(t *testing.T) {
	// Listing the 1g first must not let it take the 3g's last span.
	occupied := []device.MigPlacement{{Start: 6, Size: 1}}
	got, ok := placeMigProfiles(a100TopologyProfiles(), occupied, []string{"1g.5gb", "3g.20gb"})
	if !ok {
		t.Fatal("1g + 3g should fit next to a running 1g at slot 6")
	}
	want := []device.MigPlacement{{Start: 4, Size: 1}, {Start: 0, Size: 4}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("placements = %+v, want %+v", got, want)
	}
}

func TestPlaceMigProfilesBreaksTiesWithLookahead(t *testing.T) {
	// Both 3g spans leave a run of four; only the top one keeps both 2g spans alive.
	got, ok := placeMigProfiles(a100TopologyProfiles(), nil, []string{"2g.10gb", "2g.10gb", "3g.20gb"})
	if !ok {
		t.Fatal("3g + 2g + 2g should fill an empty A100")
	}
	want := []device.MigPlacement{{Start: 0, Size: 2}, {Start: 2, Size: 2}, {Start: 4, Size: 4}}
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

func TestPlaceMigProfilesFallsBackToBacktracking(t *testing.T) {
	// Greedy parks the 1g.10gb at 2-3 for the longer free run, leaving only slots 5 and 6 for three 1g.5gb.
	// Backtracking finds the only working layout: 1g.10gb at 6-7.
	allowed := a100WithTwoSliceProfile()
	occupied := []device.MigPlacement{{Start: 0, Size: 2}, {Start: 4, Size: 1}}
	requested := []string{"1g.5gb", "1g.5gb", "1g.5gb", "1g.10gb"}

	if _, ok := greedyPlaceMigProfiles(allowed, occupied, requested); ok {
		t.Fatal("greedy pass was expected to fail on this layout")
	}
	got, ok := placeMigProfiles(allowed, occupied, requested)
	if !ok {
		t.Fatal("backtracking should find the layout with 1g.10gb at slot 6")
	}
	if got[3] != (device.MigPlacement{Start: 6, Size: 2}) {
		t.Fatalf("1g.10gb placed at %+v, want start 6", got[3])
	}
	seen := map[uint32]bool{}
	for i := range 3 {
		if got[i].Size != 1 || seen[got[i].Start] {
			t.Fatalf("1g.5gb placements = %+v, want three distinct single slices", got[:3])
		}
		seen[got[i].Start] = true
	}
	if !seen[2] || !seen[3] || !seen[5] {
		t.Fatalf("1g.5gb placements = %+v, want slots 2, 3 and 5", got[:3])
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
	if got, ok := placeMigProfiles(allowed, nil, nil); !ok || len(got) != 0 {
		t.Fatalf("empty request = %+v, %v; want empty success", got, ok)
	}
}

func TestPlaceMigProfilesUsesReportedGeometry(t *testing.T) {
	// A30 has four memory slices; the slice count and every start come from the placements.
	allowed := a30TopologyProfiles()
	if count := migSliceCount(allowed); count != 4 {
		t.Fatalf("A30 slice count = %d, want 4", count)
	}
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
}
