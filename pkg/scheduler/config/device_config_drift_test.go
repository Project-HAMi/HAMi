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
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v2"
	"gotest.tools/v3/assert"
)

const (
	// chartDeviceConfigPath is the chart template whose fallback branch renders
	// the device config a default `helm install` ships.
	chartDeviceConfigPath = "../../../charts/hami/templates/scheduler/device-configmap.yaml"
	// chartValuesPath supplies the values substituted into that fallback branch.
	chartValuesPath = "../../../charts/hami/values.yaml"
	// chartFallbackIndent is the indentation the fallback branch carries because
	// it is nested under the ConfigMap's data key. It is stripped to recover the
	// device config as the scheduler receives it.
	chartFallbackIndent = "    "
)

var (
	// chartFallbackStart and chartFallbackEnd delimit the `{{- else }}` branch,
	// the one used when a chart user supplies no device-config.content override.
	chartFallbackStart = "{{- else }}"
	chartFallbackEnd   = "{{ end }}"

	// chartValueRef matches the only template construct the fallback branch
	// uses: a .Values lookup with an optional `| default <literal>` fallback.
	// The branch has no conditionals or loops, which is what makes rendering it
	// here a substitution rather than a reimplementation of Helm.
	chartValueRef = regexp.MustCompile(`\{\{\s*\.Values\.([A-Za-z0-9_.]+)\s*(?:\|\s*default\s+(\S+?)\s*)?\}\}`)
)

// chartValues loads the chart's default values, the ones a user gets when they
// override nothing.
func chartValues(t *testing.T) map[any]any {
	t.Helper()

	raw, err := os.ReadFile(chartValuesPath)
	assert.NilError(t, err, "read chart values")

	values := map[any]any{}
	assert.NilError(t, yaml.Unmarshal(raw, &values), "unmarshal chart values")
	return values
}

// lookupChartValue resolves a dotted .Values path against the chart's values.
func lookupChartValue(values map[any]any, path string) (any, bool) {
	var current any = values
	for segment := range strings.SplitSeq(path, ".") {
		node, ok := current.(map[any]any)
		if !ok {
			return nil, false
		}
		current, ok = node[segment]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

// renderChartFallback returns the device config the chart produces by default:
// the `{{- else }}` branch with its .Values references substituted and its
// ConfigMap indentation removed.
func renderChartFallback(t *testing.T) string {
	t.Helper()

	raw, err := os.ReadFile(chartDeviceConfigPath)
	assert.NilError(t, err, "read chart device config template")
	values := chartValues(t)

	var rendered []string
	inFallback := false
	for line := range strings.SplitSeq(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if !inFallback {
			inFallback = trimmed == chartFallbackStart
			continue
		}
		if trimmed == chartFallbackEnd {
			inFallback = false
			continue
		}

		var missing []string
		line = chartValueRef.ReplaceAllStringFunc(line, func(ref string) string {
			groups := chartValueRef.FindStringSubmatch(ref)
			path, fallback := groups[1], groups[2]
			value, ok := lookupChartValue(values, path)
			if !ok {
				if fallback == "" {
					missing = append(missing, path)
					return ref
				}
				return strings.Trim(fallback, `"`)
			}
			return fmt.Sprintf("%v", value)
		})
		assert.Assert(t, len(missing) == 0, "chart references .Values.%s, which %s does not define", strings.Join(missing, ", .Values."), chartValuesPath)

		rendered = append(rendered, strings.TrimPrefix(line, chartFallbackIndent))
	}

	out := strings.Join(rendered, "\n")
	assert.Assert(t, strings.Contains(out, "nvidia:"), "found no device config in %s; the template layout changed and this guard needs updating", chartDeviceConfigPath)
	return out
}

// vendorSections returns the top-level vendor keys of a device config document.
func vendorSections(t *testing.T, document string) []string {
	t.Helper()

	parsed := yaml.MapSlice{}
	assert.NilError(t, yaml.Unmarshal([]byte(document), &parsed), "unmarshal device config")

	sections := make([]string, 0, len(parsed))
	for _, item := range parsed {
		sections = append(sections, fmt.Sprintf("%v", item.Key))
	}
	return sections
}

// TestDefaultDeviceConfigCoversChartVendors fails when a vendor is added to the
// chart's default device config but not to defaultDeviceConfig. It duplicates
// part of TestDefaultDeviceConfigMatchesChart deliberately: a whole missing
// vendor is the most likely drift, and naming it beats reading a struct diff.
func TestDefaultDeviceConfigCoversChartVendors(t *testing.T) {
	fixture := map[string]any{}
	assert.NilError(t, yaml.Unmarshal([]byte(defaultDeviceConfig), &fixture), "unmarshal defaultDeviceConfig")

	for _, vendor := range vendorSections(t, renderChartFallback(t)) {
		_, ok := fixture[vendor]
		assert.Assert(t, ok, "vendor %q is configured in %s but missing from defaultDeviceConfig; add it so tests exercise the shipped default", vendor, chartDeviceConfigPath)
	}
}

// TestDefaultDeviceConfigMatchesChart renders the chart's default device config
// and requires defaultDeviceConfig to parse into exactly the same Config. It
// compares against the chart itself rather than against literals copied out of
// it, so a changed field value cannot pass while the fixture has drifted.
func TestDefaultDeviceConfigMatchesChart(t *testing.T) {
	var fromChart Config
	assert.NilError(t, yaml.Unmarshal([]byte(renderChartFallback(t)), &fromChart), "unmarshal rendered chart device config")

	var fromFixture Config
	assert.NilError(t, yaml.Unmarshal([]byte(defaultDeviceConfig), &fromFixture), "unmarshal defaultDeviceConfig")

	assert.DeepEqual(t, fromChart, fromFixture)
}
