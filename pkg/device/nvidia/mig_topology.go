package nvidia

import (
	"sort"
	"strings"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

func preferHighMigPlacement(profile string) bool {
	return strings.HasPrefix(profile, "1g.") || strings.HasPrefix(profile, "3g.")
}

func orderedMigPlacements(profile string, placements []device.MigPlacement) []device.MigPlacement {
	ordered := append([]device.MigPlacement(nil), placements...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if preferHighMigPlacement(profile) {
			return ordered[i].Start > ordered[j].Start
		}
		return ordered[i].Start < ordered[j].Start
	})
	return ordered
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

func selectMigPlacement(profiles []device.MigProfile, occupied []device.MigPlacement, profileName string) (device.MigPlacement, bool) {
	profile, ok := findMigProfile(profiles, profileName)
	if !ok {
		return device.MigPlacement{}, false
	}
	for _, candidate := range orderedMigPlacements(profileName, profile.Placements) {
		conflict := false
		for _, existing := range occupied {
			if migPlacementsOverlap(candidate, existing) {
				conflict = true
				break
			}
		}
		if !conflict {
			return candidate, true
		}
	}
	return device.MigPlacement{}, false
}

func migPlacementsOverlap(a, b device.MigPlacement) bool {
	return a.Start < b.Start+b.Size && b.Start < a.Start+a.Size
}

// canPlaceMigProfiles determines whether every requested profile can be
// assigned one of the profile-specific placements reported by NVML without
// overlapping an already-running or newly-selected instance.
func canPlaceMigProfiles(capabilities []device.MigProfile, occupied []device.MigPlacement, requested []string) bool {
	if len(requested) == 0 {
		return true
	}
	placements := make(map[string][]device.MigPlacement, len(capabilities))
	for _, profile := range capabilities {
		placements[profile.Name] = profile.Placements
	}
	ordered := append([]string(nil), requested...)
	sort.SliceStable(ordered, func(i, j int) bool {
		left, right := placements[ordered[i]], placements[ordered[j]]
		if len(left) != len(right) {
			return len(left) < len(right)
		}
		if len(left) == 0 {
			return false
		}
		return left[0].Size > right[0].Size
	})

	used := append([]device.MigPlacement(nil), occupied...)
	var place func(int) bool
	place = func(idx int) bool {
		if idx == len(ordered) {
			return true
		}
		candidates := orderedMigPlacements(ordered[idx], placements[ordered[idx]])
		for _, candidate := range candidates {
			conflict := false
			for _, existing := range used {
				if migPlacementsOverlap(candidate, existing) {
					conflict = true
					break
				}
			}
			if conflict {
				continue
			}
			used = append(used, candidate)
			if place(idx + 1) {
				return true
			}
			used = used[:len(used)-1]
		}
		return false
	}
	return place(0)
}
