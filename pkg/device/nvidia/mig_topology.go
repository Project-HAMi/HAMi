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

// migPlacementScore ranks one candidate placement; fields are compared in order.
type migPlacementScore struct {
	largestFreeRun int    // longest run of free slices left after placing
	survivors      int    // legal placements of every profile still free after placing
	start          uint32 // lower start wins ties
}

// betterMigPlacement reports whether a should be preferred over b.
func betterMigPlacement(a, b migPlacementScore) bool {
	if a.largestFreeRun != b.largestFreeRun {
		return a.largestFreeRun > b.largestFreeRun
	}
	if a.survivors != b.survivors {
		return a.survivors > b.survivors
	}
	return a.start < b.start
}

func migPlacementsOverlap(a, b device.MigPlacement) bool {
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

func findMigProfile(profiles []device.MigProfile, name string) (device.MigProfile, bool) {
	for _, profile := range profiles {
		if profile.Name == name {
			return profile, true
		}
	}
	return device.MigProfile{}, false
}

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
			if end := int(placement.Start + placement.Size); end > count {
				count = end
			}
		}
	}
	return count
}

// freeMigPlacements returns the placements of profile that overlap nothing in occupied.
func freeMigPlacements(profile device.MigProfile, occupied []device.MigPlacement) []device.MigPlacement {
	free := make([]device.MigPlacement, 0, len(profile.Placements))
	for _, candidate := range profile.Placements {
		if !migPlacementConflicts(candidate, occupied) {
			free = append(free, candidate)
		}
	}
	return free
}

// largestFreeMigRun returns the longest run of consecutive free slices.
func largestFreeMigRun(sliceCount int, occupied []device.MigPlacement) int {
	if sliceCount <= 0 {
		return 0
	}
	used := make([]bool, sliceCount)
	for _, placement := range occupied {
		for slice := placement.Start; slice < placement.Start+placement.Size; slice++ {
			if int(slice) < sliceCount {
				used[slice] = true
			}
		}
	}
	best, current := 0, 0
	for _, taken := range used {
		if taken {
			current = 0
			continue
		}
		current++
		if current > best {
			best = current
		}
	}
	return best
}

// survivingMigPlacements counts the free legal placements across every profile.
func survivingMigPlacements(profiles []device.MigProfile, occupied []device.MigPlacement) int {
	total := 0
	for _, profile := range profiles {
		total += len(freeMigPlacements(profile, occupied))
	}
	return total
}

// scoreMigPlacement scores the card as it would look with candidate added.
func scoreMigPlacement(profiles []device.MigProfile, sliceCount int, occupied []device.MigPlacement, candidate device.MigPlacement) migPlacementScore {
	after := make([]device.MigPlacement, 0, len(occupied)+1)
	after = append(after, occupied...)
	after = append(after, candidate)
	return migPlacementScore{
		largestFreeRun: largestFreeMigRun(sliceCount, after),
		survivors:      survivingMigPlacements(profiles, after),
		start:          candidate.Start,
	}
}

// rankedMigPlacements returns the free placements of profile, best score first.
func rankedMigPlacements(profiles []device.MigProfile, occupied []device.MigPlacement, profile device.MigProfile) []device.MigPlacement {
	candidates := freeMigPlacements(profile, occupied)
	if len(candidates) < 2 {
		return candidates
	}
	sliceCount := migSliceCount(profiles)
	scores := make([]migPlacementScore, len(candidates))
	for i, candidate := range candidates {
		scores[i] = scoreMigPlacement(profiles, sliceCount, occupied, candidate)
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

// selectMigPlacement picks the best free placement for one instance of profileName.
func selectMigPlacement(profiles []device.MigProfile, occupied []device.MigPlacement, profileName string) (device.MigPlacement, bool) {
	profile, ok := findMigProfile(profiles, profileName)
	if !ok {
		return device.MigPlacement{}, false
	}
	ranked := rankedMigPlacements(profiles, occupied, profile)
	if len(ranked) == 0 {
		return device.MigPlacement{}, false
	}
	return ranked[0], true
}

// migProfileFootprint is the number of slices one instance occupies.
func migProfileFootprint(profile device.MigProfile) uint32 {
	if len(profile.Placements) == 0 {
		return 0
	}
	return profile.Placements[0].Size
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

// greedyPlaceMigProfiles places requests pickiest first, best score first, never revisiting a choice.
func greedyPlaceMigProfiles(profiles []device.MigProfile, occupied []device.MigPlacement, requested []string) ([]device.MigPlacement, bool) {
	resolved, ok := resolveMigRequests(profiles, requested)
	if !ok {
		return nil, false
	}
	used := make([]device.MigPlacement, 0, len(occupied)+len(requested))
	used = append(used, occupied...)
	result := make([]device.MigPlacement, len(requested))
	placed := make([]bool, len(requested))
	for range requested {
		idx := nextMigRequest(used, resolved, placed)
		ranked := rankedMigPlacements(profiles, used, resolved[idx])
		if len(ranked) == 0 {
			return nil, false
		}
		result[idx] = ranked[0]
		used = append(used, ranked[0])
		placed[idx] = true
	}
	return result, true
}

// backtrackMigProfiles searches every assignment, trying candidates best score first.
func backtrackMigProfiles(profiles []device.MigProfile, occupied []device.MigPlacement, requested []string) ([]device.MigPlacement, bool) {
	resolved, ok := resolveMigRequests(profiles, requested)
	if !ok {
		return nil, false
	}
	used := make([]device.MigPlacement, 0, len(occupied)+len(requested))
	used = append(used, occupied...)
	result := make([]device.MigPlacement, len(requested))
	placed := make([]bool, len(requested))
	var place func(remaining int) bool
	place = func(remaining int) bool {
		if remaining == 0 {
			return true
		}
		idx := nextMigRequest(used, resolved, placed)
		placed[idx] = true
		for _, candidate := range rankedMigPlacements(profiles, used, resolved[idx]) {
			used = append(used, candidate)
			result[idx] = candidate
			if place(remaining - 1) {
				return true
			}
			used = used[:len(used)-1]
		}
		placed[idx] = false
		return false
	}
	if !place(len(requested)) {
		return nil, false
	}
	return result, true
}

// placeMigProfiles runs the greedy pass, then backtracking if it fails. result[i] belongs to requested[i].
func placeMigProfiles(profiles []device.MigProfile, occupied []device.MigPlacement, requested []string) ([]device.MigPlacement, bool) {
	if len(requested) == 0 {
		return nil, true
	}
	if placements, ok := greedyPlaceMigProfiles(profiles, occupied, requested); ok {
		return placements, true
	}
	return backtrackMigProfiles(profiles, occupied, requested)
}

// canPlaceMigProfiles reports whether every requested profile can be placed without overlap.
func canPlaceMigProfiles(profiles []device.MigProfile, occupied []device.MigPlacement, requested []string) bool {
	_, ok := placeMigProfiles(profiles, occupied, requested)
	return ok
}
