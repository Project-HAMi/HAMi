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
	"sort"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

// migRegion is a half-open range [start, end) of memory slices.
type migRegion struct {
	start uint32
	end   uint32
}

func (r migRegion) size() uint32 {
	return r.end - r.start
}

// contains reports whether p lies entirely inside the region.
func (r migRegion) contains(p device.MigPlacement) bool {
	return p.Size > 0 && p.Start >= r.start && p.Start+p.Size <= r.end
}

// free reports whether nothing in occupied touches the region.
func (r migRegion) free(occupied []device.MigPlacement) bool {
	return !migPlacementConflicts(device.MigPlacement{Start: r.start, Size: r.size()}, occupied)
}

// migRegions splits the card at the midpoint. If any placement other than the
// full-card one straddles the midpoint, the whole card is a single region and
// scoring degrades to first-fit rather than misplacing on an unknown table.
func migRegions(profiles []device.MigProfile, sliceCount int) []migRegion {
	whole := []migRegion{{start: 0, end: uint32(sliceCount)}}
	if sliceCount < 2 {
		return whole
	}
	mid := uint32(sliceCount / 2)
	for _, profile := range profiles {
		for _, p := range profile.Placements {
			if p.Size == 0 || p.Size >= uint32(sliceCount) {
				continue
			}
			if p.Start < mid && p.Start+p.Size > mid {
				return whole
			}
		}
	}
	return []migRegion{{start: 0, end: mid}, {start: mid, end: uint32(sliceCount)}}
}

func migReservedRegion(profiles []device.MigProfile, regions []migRegion) int {
	reserved, best := 0, -1
	for i, region := range regions {
		count := 0
		for _, profile := range profiles {
			for _, p := range profile.Placements {
				if region.contains(p) {
					count++
				}
			}
		}
		if best < 0 || count <= best {
			reserved, best = i, count
		}
	}
	return reserved
}

// migLayout is the placement geometry of one card, derived once per request
// from the profile table the device plugin reports.
type migLayout struct {
	regions  []migRegion
	reserved int
}

func newMigLayout(profiles []device.MigProfile) migLayout {
	regions := migRegions(profiles, migSliceCount(profiles))
	return migLayout{regions: regions, reserved: migReservedRegion(profiles, regions)}
}

// migPlacementScore ranks one candidate placement; fields are compared in order.
type migPlacementScore struct {
	emptyRegions int
	zoneMismatch int
	edgeDistance uint32
	start        uint32
}

// betterMigPlacement reports whether a should be preferred over b.
func betterMigPlacement(a, b migPlacementScore) bool {
	if a.emptyRegions != b.emptyRegions {
		return a.emptyRegions > b.emptyRegions
	}
	if a.zoneMismatch != b.zoneMismatch {
		return a.zoneMismatch < b.zoneMismatch
	}
	if a.edgeDistance != b.edgeDistance {
		return a.edgeDistance > b.edgeDistance
	}
	return a.start < b.start
}

// score rates the card as it would look with candidate added.
func (l migLayout) score(occupied []device.MigPlacement, candidate device.MigPlacement) migPlacementScore {
	after := make([]device.MigPlacement, 0, len(occupied)+1)
	after = append(after, occupied...)
	after = append(after, candidate)
	score := migPlacementScore{start: candidate.Start}
	for i, region := range l.regions {
		if region.free(after) {
			score.emptyRegions++
		}
		if len(l.regions) < 2 || !region.contains(candidate) {
			continue
		}
		if (candidate.Size == region.size()) != (i == l.reserved) {
			score.zoneMismatch = 1
		}
		if i == 0 {
			score.edgeDistance = region.end - (candidate.Start + candidate.Size)
		} else {
			score.edgeDistance = candidate.Start - region.start
		}
	}
	return score
}

// rank returns the free placements of profile, best score first. The order is
// deterministic: equal scores keep the order of the reported placement table.
func (l migLayout) rank(occupied []device.MigPlacement, profile device.MigProfile) []device.MigPlacement {
	candidates := freeMigPlacements(profile, occupied)
	if len(candidates) < 2 {
		return candidates
	}
	scores := make([]migPlacementScore, len(candidates))
	for i, candidate := range candidates {
		scores[i] = l.score(occupied, candidate)
	}
	order := make([]int, len(candidates))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		return betterMigPlacement(scores[order[i]], scores[order[j]])
	})
	ranked := make([]device.MigPlacement, len(candidates))
	for i, idx := range order {
		ranked[i] = candidates[idx]
	}
	return ranked
}

// place assigns every requested profile, pickiest first, without revisiting a
// choice. result[i] belongs to requested[i]. It fails as soon as one profile
// has no free placement, leaving the card untouched.
func (l migLayout) place(occupied []device.MigPlacement, requested []device.MigProfile) ([]device.MigPlacement, bool) {
	used := make([]device.MigPlacement, 0, len(occupied)+len(requested))
	used = append(used, occupied...)
	result := make([]device.MigPlacement, len(requested))
	placed := make([]bool, len(requested))
	for range requested {
		idx := nextMigRequest(used, requested, placed)
		if idx < 0 {
			return nil, false
		}
		ranked := l.rank(used, requested[idx])
		if len(ranked) == 0 {
			return nil, false
		}
		result[idx] = ranked[0]
		used = append(used, ranked[0])
		placed[idx] = true
	}
	return result, true
}

// migPlacementsOverlap reports whether two placements share a slice. A
// zero-width placement occupies nothing and never overlaps.
func migPlacementsOverlap(a, b device.MigPlacement) bool {
	if a.Size == 0 || b.Size == 0 {
		return false
	}
	return a.Start < b.Start+b.Size && b.Start < a.Start+a.Size
}

// migPlacementConflicts reports whether candidate overlaps any occupied placement.
func migPlacementConflicts(candidate device.MigPlacement, occupied []device.MigPlacement) bool {
	for _, existing := range occupied {
		if migPlacementsOverlap(candidate, existing) {
			return true
		}
	}
	return false
}

// findMigProfile returns the first profile with the given name.
func findMigProfile(profiles []device.MigProfile, name string) (device.MigProfile, bool) {
	for _, profile := range profiles {
		if profile.Name == name {
			return profile, true
		}
	}
	return device.MigProfile{}, false
}

// occupiedMigPlacements extracts the placements held by running allocations.
func occupiedMigPlacements(allocations []device.MigAllocation) []device.MigPlacement {
	out := make([]device.MigPlacement, 0, len(allocations))
	for _, allocation := range allocations {
		out = append(out, allocation.Placement)
	}
	return out
}

// migSliceCount is the furthest slice any reported placement reaches (8 on A100, 4 on A30).
func migSliceCount(profiles []device.MigProfile) int {
	count := 0
	for _, profile := range profiles {
		for _, placement := range profile.Placements {
			if placement.Size == 0 {
				continue
			}
			if end := int(placement.Start + placement.Size); end > count {
				count = end
			}
		}
	}
	return count
}

// freeMigPlacements returns the placements of profile that overlap nothing in
// occupied. Zero-width entries are malformed table rows, not slots, and are skipped.
func freeMigPlacements(profile device.MigProfile, occupied []device.MigPlacement) []device.MigPlacement {
	free := make([]device.MigPlacement, 0, len(profile.Placements))
	for _, candidate := range profile.Placements {
		if candidate.Size == 0 {
			continue
		}
		if !migPlacementConflicts(candidate, occupied) {
			free = append(free, candidate)
		}
	}
	return free
}

// rankedMigPlacements returns the free placements of profile, best score first.
func rankedMigPlacements(profiles []device.MigProfile, occupied []device.MigPlacement, profile device.MigProfile) []device.MigPlacement {
	return newMigLayout(profiles).rank(occupied, profile)
}

// selectMigPlacement picks the best free placement for one instance of profileName.
func selectMigPlacement(profiles []device.MigProfile, occupied []device.MigPlacement, profileName string) (device.MigPlacement, bool) {
	profile, ok := findMigProfile(profiles, profileName)
	if !ok {
		return device.MigPlacement{}, false
	}
	ranked := newMigLayout(profiles).rank(occupied, profile)
	if len(ranked) == 0 {
		return device.MigPlacement{}, false
	}
	return ranked[0], true
}

// migProfileFootprint is the number of slices one instance occupies.
func migProfileFootprint(profile device.MigProfile) uint32 {
	footprint := uint32(0)
	for _, placement := range profile.Placements {
		if placement.Size > footprint {
			footprint = placement.Size
		}
	}
	return footprint
}

// resolveMigRequests looks up every requested name; an unknown name fails the request.
func resolveMigRequests(profiles []device.MigProfile, requested []string) ([]device.MigProfile, bool) {
	resolved := make([]device.MigProfile, len(requested))
	for i, name := range requested {
		profile, ok := findMigProfile(profiles, name)
		if !ok {
			return nil, false
		}
		resolved[i] = profile
	}
	return resolved, true
}

// nextMigRequest picks the unplaced request with the fewest free placements,
// then the larger footprint, then the name. It returns -1 once all are placed.
func nextMigRequest(occupied []device.MigPlacement, requested []device.MigProfile, placed []bool) int {
	best := -1
	bestFree := 0
	bestSize := uint32(0)
	for idx, profile := range requested {
		if placed[idx] {
			continue
		}
		free := len(freeMigPlacements(profile, occupied))
		size := migProfileFootprint(profile)
		switch {
		case best == -1,
			free < bestFree,
			free == bestFree && size > bestSize,
			free == bestFree && size == bestSize && profile.Name < requested[best].Name:
			best, bestFree, bestSize = idx, free, size
		}
	}
	return best
}

func placeMigProfiles(profiles []device.MigProfile, occupied []device.MigPlacement, requested []string) ([]device.MigPlacement, bool) {
	if len(requested) == 0 {
		return nil, true
	}
	resolved, ok := resolveMigRequests(profiles, requested)
	if !ok {
		return nil, false
	}
	return newMigLayout(profiles).place(occupied, resolved)
}

// canPlaceMigProfiles reports whether every requested profile can be placed without overlap.
func canPlaceMigProfiles(profiles []device.MigProfile, occupied []device.MigPlacement, requested []string) bool {
	_, ok := placeMigProfiles(profiles, occupied, requested)
	return ok
}
