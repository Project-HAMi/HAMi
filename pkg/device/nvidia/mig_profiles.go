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
	"strings"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

// ValidateMigProfileAllowlist validates policy only. Profile capacity,
// memory, compute metadata and placements always come from NVML on the node.
func ValidateMigProfileAllowlist(in []device.AllowedMigProfiles) error {
	for _, cfg := range in {
		if len(cfg.Models) == 0 || len(cfg.Profiles) == 0 {
			return fmt.Errorf("MIG profile allowlist must define models and profiles")
		}
		for _, profile := range cfg.Profiles {
			parts := strings.SplitN(profile, ".", 2)
			if len(parts) != 2 || len(parts[0]) < 2 || !strings.HasSuffix(parts[0], "g") {
				return fmt.Errorf("invalid MIG profile %q", profile)
			}
		}
	}
	return nil
}
