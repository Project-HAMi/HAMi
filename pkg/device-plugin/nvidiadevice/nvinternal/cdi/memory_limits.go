// Copyright 2026 The HAMi Authors.
// SPDX-License-Identifier: Apache-2.0
package cdi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	specs "tags.cncf.io/container-device-interface/specs-go"
)

// ValidateMemoryLimitsHooks checks the published specs that the runtime will
// consume, not only the linked toolkit version. A disabled hook is an error.
func (c *cdiHandler) ValidateMemoryLimitsHooks(ids []string) error {
	generator := c.cdilibs["gpu"]
	if generator == nil {
		return fmt.Errorf("mps-v3 requires the NVIDIA GPU CDI generator")
	}
	expected, err := generator.GetSpec()
	if err != nil {
		return err
	}
	name, err := cdiapi.GenerateNameForSpec(expected.Raw())
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(cdiRoot, name+".json"))
	if err != nil {
		return fmt.Errorf("read published GPU CDI spec: %w", err)
	}
	var spec specs.Spec
	if err = json.Unmarshal(data, &spec); err != nil {
		return err
	}
	return validateMemoryLimitsSpec(&spec, ids)
}

func validateMemoryLimitsSpec(spec *specs.Spec, ids []string) error {
	for _, id := range ids {
		found := false
		for _, dev := range spec.Devices {
			if dev.Name != id {
				continue
			}
			for _, hook := range dev.ContainerEdits.Hooks {
				if hook.Path != "" && hook.HookName == "createRuntime" && slices.Contains(hook.Args, "apply-cuda-memory-limits") && slices.Contains(hook.Args, id) {
					found = true
				}
			}
		}
		if !found {
			return fmt.Errorf("GPU %s has no published createRuntime apply-cuda-memory-limits hook (requires Toolkit >=1.20.1 and UUID device IDs)", id)
		}
	}
	return nil
}
