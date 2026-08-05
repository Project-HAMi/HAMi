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

	"github.com/Project-HAMi/HAMi/pkg/device"
)

func TestCanPlaceMigProfilesA100(t *testing.T) {
	allowed := []device.MigProfile{
		{Name: "1g.5gb", Placements: []device.MigPlacement{{Start: 0, Size: 1}, {Start: 1, Size: 1}, {Start: 2, Size: 1}, {Start: 3, Size: 1}, {Start: 4, Size: 1}, {Start: 5, Size: 1}, {Start: 6, Size: 1}}},
		{Name: "2g.10gb", Placements: []device.MigPlacement{{Start: 0, Size: 2}, {Start: 2, Size: 2}, {Start: 4, Size: 2}}},
		{Name: "3g.20gb", Placements: []device.MigPlacement{{Start: 0, Size: 4}, {Start: 4, Size: 4}}},
	}

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
}

func TestSelectMigPlacementUsesBalancedPacking(t *testing.T) {
	allowed := []device.MigProfile{
		{Name: "1g.5gb", Placements: []device.MigPlacement{{Start: 0, Size: 1}, {Start: 1, Size: 1}, {Start: 2, Size: 1}, {Start: 3, Size: 1}, {Start: 4, Size: 1}, {Start: 5, Size: 1}, {Start: 6, Size: 1}}},
		{Name: "2g.10gb", Placements: []device.MigPlacement{{Start: 0, Size: 2}, {Start: 2, Size: 2}, {Start: 4, Size: 2}}},
		{Name: "3g.20gb", Placements: []device.MigPlacement{{Start: 0, Size: 4}, {Start: 4, Size: 4}}},
	}
	occupied := []device.MigPlacement{}
	for _, tc := range []struct {
		profile string
		start   uint32
	}{
		{profile: "3g.20gb", start: 4},
		{profile: "2g.10gb", start: 0},
		{profile: "1g.5gb", start: 3},
		{profile: "1g.5gb", start: 2},
	} {
		placement, ok := selectMigPlacement(allowed, occupied, tc.profile)
		if !ok || placement.Start != tc.start {
			t.Fatalf("profile %s placement = %+v, %v; want start %d", tc.profile, placement, ok, tc.start)
		}
		occupied = append(occupied, placement)
	}
}
