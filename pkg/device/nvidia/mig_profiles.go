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
