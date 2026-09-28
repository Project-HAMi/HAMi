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
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

const MigProfilePreference = "nvidia.com/mig-profile-preference"

func migProfileSliceKey(profile string) string {
	if idx := strings.Index(profile, "."); idx > 0 {
		return profile[:idx]
	}
	return profile
}

func migProfileMatchesPreference(profile, entry string) bool {
	return profile == entry || migProfileSliceKey(profile) == entry
}

// parseMigProfilePreference splits the annotation value into trimmed,
// non-empty, de-duplicated entries in the order they were listed.
func parseMigProfilePreference(raw string) []string {
	var preferred []string
	for entry := range strings.SplitSeq(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" || slices.Contains(preferred, entry) {
			continue
		}
		preferred = append(preferred, entry)
	}
	return preferred
}

func migProfilePreference(annos map[string]string) []string {
	raw, ok := annos[MigProfilePreference]
	if !ok {
		return nil
	}
	return parseMigProfilePreference(raw)
}

func podMigProfilePreference(pod *corev1.Pod) []string {
	if pod == nil {
		return nil
	}
	return migProfilePreference(pod.GetAnnotations())
}

func migProfileCandidates(profiles []device.MigProfile, preferred []string) []device.MigProfile {
	ordered := migProfilesByMemory(profiles)
	if len(preferred) == 0 {
		return ordered
	}
	candidates := make([]device.MigProfile, 0, len(ordered))
	taken := make([]bool, len(ordered))
	for _, entry := range preferred {
		for i, profile := range ordered {
			if !taken[i] && migProfileMatchesPreference(profile.Name, entry) {
				candidates = append(candidates, profile)
				taken[i] = true
			}
		}
	}
	for i, profile := range ordered {
		if !taken[i] {
			candidates = append(candidates, profile)
		}
	}
	return candidates
}

// migProfileAllowlisted reports whether entry matches a profile in any allowlist entry.
func migProfileAllowlisted(allowlist []device.AllowedMigProfiles, entry string) bool {
	for _, cfg := range allowlist {
		for _, profile := range cfg.Profiles {
			if migProfileMatchesPreference(profile, entry) {
				return true
			}
		}
	}
	return false
}

// validateMigProfilePreference rejects a preference that no allowlisted profile can satisfy.
func (dev *NvidiaGPUDevices) validateMigProfilePreference(pod *corev1.Pod) error {
	if pod == nil {
		return nil
	}
	raw, ok := pod.GetAnnotations()[MigProfilePreference]
	if !ok {
		return nil
	}
	preferred := parseMigProfilePreference(raw)
	if len(preferred) == 0 {
		return fmt.Errorf("invalid %s value %q: expected a comma-separated list of MIG profiles such as \"4g.20gb\" or \"4g\"", MigProfilePreference, raw)
	}
	for _, entry := range preferred {
		if !migProfileAllowlisted(dev.config.MigProfileAllowlist, entry) {
			return fmt.Errorf("invalid %s entry %q: it does not match any profile in migProfileAllowlist", MigProfilePreference, entry)
		}
	}
	return nil
}
