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

package config

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v2"
	"gotest.tools/v3/assert"
)

// chartDeviceConfigPath is the chart template whose fallback branch renders the
// device config a default `helm install` ships.
const chartDeviceConfigPath = "../../../charts/hami/templates/scheduler/device-configmap.yaml"

// chartVendorKey matches a top-level vendor section of the rendered device
// config. Sections sit at exactly four spaces of indentation inside the
// template's data block; their nested fields and list items are indented
// further or start with "-", so neither is picked up.
var chartVendorKey = regexp.MustCompile(`^ {4}([A-Za-z][A-Za-z0-9]*):`)

// chartFallbackVendors returns the vendor sections of the chart's built-in
// device config, that is the `{{- else }}` branch used when a chart user
// supplies no device-config.content override.
func chartFallbackVendors(t *testing.T) []string {
	t.Helper()

	raw, err := os.ReadFile(chartDeviceConfigPath)
	assert.NilError(t, err, "read chart device config template")

	var vendors []string
	inFallback := false
	for line := range strings.SplitSeq(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "{{- else }}":
			inFallback = true
			continue
		case inFallback && trimmed == "{{ end }}":
			inFallback = false
			continue
		}
		if !inFallback || strings.HasPrefix(trimmed, "{{") {
			continue
		}
		if m := chartVendorKey.FindStringSubmatch(line); m != nil {
			vendors = append(vendors, m[1])
		}
	}

	assert.Assert(t, len(vendors) > 0, "found no vendor sections in %s; the template layout changed and this guard needs updating", chartDeviceConfigPath)
	return vendors
}

// TestDefaultDeviceConfigCoversChartVendors fails when a vendor is added to the
// chart's default device config but not to defaultDeviceConfig. The two are
// maintained by hand, so without this guard the fixture silently stops
// representing what a default install runs.
func TestDefaultDeviceConfigCoversChartVendors(t *testing.T) {
	fixture := map[string]any{}
	assert.NilError(t, yaml.Unmarshal([]byte(defaultDeviceConfig), &fixture), "unmarshal defaultDeviceConfig")

	for _, vendor := range chartFallbackVendors(t) {
		_, ok := fixture[vendor]
		assert.Assert(t, ok, "vendor %q is configured in %s but missing from defaultDeviceConfig; add it so tests exercise the shipped default", vendor, chartDeviceConfigPath)
	}
}

// TestDefaultDeviceConfigMatchesChartDefaults pins the values that a bare
// resource-count-only fixture used to omit. Each one gates behaviour that no
// test could otherwise reach: an unset resource name matches no container spec,
// and SuperPod gates 910C module pair allocation.
func TestDefaultDeviceConfigMatchesChartDefaults(t *testing.T) {
	var cfg Config
	assert.NilError(t, yaml.Unmarshal([]byte(defaultDeviceConfig), &cfg), "unmarshal defaultDeviceConfig")

	t.Run("backends missing from the fixture are configured", func(t *testing.T) {
		assert.Equal(t, cfg.EnflameConfig.ResourceNameGCU, "enflame.com/gcu")
		assert.Equal(t, cfg.EnflameConfig.ResourceNameDRSGCU, "enflame.com/drs-gcu")
		assert.Equal(t, cfg.EnflameConfig.ResourceNameMemory, "enflame.com/gcu-memory")
		assert.Equal(t, cfg.EnflameConfig.ResourceNameCore, "enflame.com/gcu-core")
		assert.Equal(t, cfg.VastaiConfig.ResourceCountName, "vastaitech.com/va")
		assert.Equal(t, cfg.BirenConfig.ResourceCountName, "birentech.com/gpu")
	})

	t.Run("vgpu and memory/core resource names are set", func(t *testing.T) {
		assert.Equal(t, cfg.MetaxConfig.ResourceVCountName, "metax-tech.com/sgpu")
		assert.Equal(t, cfg.MetaxConfig.ResourceVMemoryName, "metax-tech.com/vmemory")
		assert.Equal(t, cfg.MetaxConfig.ResourceVCoreName, "metax-tech.com/vcore")
		assert.Equal(t, cfg.AMDGPUConfig.ResourceMemoryName, "amd.com/gpumem")
		assert.Equal(t, cfg.AMDGPUConfig.ResourceCoreName, "amd.com/gpucores")
		assert.Equal(t, cfg.HygonConfig.MemoryFactor, int32(1))
	})

	t.Run("nvidia mig profile allowlist is populated", func(t *testing.T) {
		assert.Assert(t, len(cfg.NvidiaConfig.MigProfileAllowlist) > 0,
			"migProfileAllowlist is empty, so no MIG allowlist path is reachable from this fixture")
	})

	t.Run("ascend chips carry core resource and scaling fields", func(t *testing.T) {
		assert.Assert(t, len(cfg.VNPUs.Configs) > 0, "no Ascend chips configured")
		for _, chip := range cfg.VNPUs.Configs {
			assert.Assert(t, chip.ResourceCoreName != "", "chip %q has no resourceCoreName", chip.CommonWord)
			// 910C is the one chart chip that carries no memoryFactor.
			if chip.CommonWord != "Ascend910C" {
				assert.Equal(t, chip.MemoryFactor, int32(1), "chip %q memoryFactor", chip.CommonWord)
			}
		}
	})

	t.Run("ascend chips present in the chart are present here", func(t *testing.T) {
		byCommonWord := map[string]int{}
		for i, chip := range cfg.VNPUs.Configs {
			byCommonWord[chip.CommonWord] = i
		}
		for _, commonWord := range []string{
			"Ascend910A", "Ascend910B2", "Ascend910B3",
			"Ascend910B4-1", "Ascend910B4", "Ascend310P", "Ascend910C",
		} {
			_, ok := byCommonWord[commonWord]
			assert.Assert(t, ok, "chip %q is in the chart default but missing here", commonWord)
		}

		c910 := cfg.VNPUs.Configs[byCommonWord["Ascend910C"]]
		assert.Equal(t, c910.ChipName, "Ascend910")
		// SuperPod gates 910C module pair allocation in the ascend backend; with
		// it unset that branch is unreachable from the default fixture.
		assert.Assert(t, c910.SuperPod, "Ascend910C must set superPod")
	})
}
