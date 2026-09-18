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

package enflame

import (
	"flag"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// resetGlobals zeroes all package-level enflame variables before the test
// and restores them afterwards. Both ParseConfig (on registration) and
// InitEnflameDevice rewrite these globals, so without this guard tests in
// this file would leak state into other test files in the package (and
// vice versa). The device.SupportDevices map written by InitEnflameDevice
// is idempotent and shared by design, so it is not reset here.
func resetGlobals(t *testing.T) {
	t.Helper()
	old := [8]string{
		EnflameResourceNameGCU, EnflameResourceNameDRSGCU,
		EnflameResourceNameGCUMemory, EnflameResourceNameGCUCore,
		EnflameResourceNameVGCU, EnflameResourceNameVGCUPercentage,
		enflameDRSGCUFlagName, enflameVGCULegacyFlagName,
	}
	t.Cleanup(func() {
		EnflameResourceNameGCU, EnflameResourceNameDRSGCU = old[0], old[1]
		EnflameResourceNameGCUMemory, EnflameResourceNameGCUCore = old[2], old[3]
		EnflameResourceNameVGCU, EnflameResourceNameVGCUPercentage = old[4], old[5]
		enflameDRSGCUFlagName, enflameVGCULegacyFlagName = old[6], old[7]
	})
	EnflameResourceNameGCU, EnflameResourceNameDRSGCU = "", ""
	EnflameResourceNameGCUMemory, EnflameResourceNameGCUCore = "", ""
	EnflameResourceNameVGCU, EnflameResourceNameVGCUPercentage = "", ""
	enflameDRSGCUFlagName, enflameVGCULegacyFlagName = "", ""
}

// parseFlags registers the enflame flags on a fresh FlagSet and parses args.
func parseFlags(t *testing.T, args string) {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	ParseConfig(fs)
	if err := fs.Parse(strings.Fields(args)); err != nil {
		t.Fatalf("failed to parse flags %q: %v", args, err)
	}
}

// TestIssue3016_FlagOrderDependency covers
// https://github.com/Project-HAMi/HAMi/issues/3016:
// --enflame-drs-gcu-resource-name and --enflame-vgcu-resource-name used to
// share one binding, so when both flags were provided the last one parsed
// silently won. With the separate binding the parsed values no longer depend
// on the argument order.
func TestIssue3016_FlagOrderDependency(t *testing.T) {
	resetGlobals(t)

	parseFlags(t, "--enflame-drs-gcu-resource-name=custom-a --enflame-vgcu-resource-name=custom-b")
	newFlagWhenDrsFirst := enflameDRSGCUFlagName
	legacyFlagWhenDrsFirst := enflameVGCULegacyFlagName

	parseFlags(t, "--enflame-vgcu-resource-name=custom-b --enflame-drs-gcu-resource-name=custom-a")
	newFlagWhenVgcuFirst := enflameDRSGCUFlagName
	legacyFlagWhenVgcuFirst := enflameVGCULegacyFlagName

	if newFlagWhenDrsFirst != newFlagWhenVgcuFirst {
		t.Errorf("new flag value depends on argument order: %q vs %q", newFlagWhenDrsFirst, newFlagWhenVgcuFirst)
	}
	if legacyFlagWhenDrsFirst != legacyFlagWhenVgcuFirst {
		t.Errorf("legacy flag value depends on argument order: %q vs %q", legacyFlagWhenDrsFirst, legacyFlagWhenVgcuFirst)
	}
	if newFlagWhenDrsFirst != "custom-a" {
		t.Errorf("expected new flag value %q, got %q", "custom-a", newFlagWhenDrsFirst)
	}
	if legacyFlagWhenDrsFirst != "custom-b" {
		t.Errorf("expected legacy flag value %q, got %q", "custom-b", legacyFlagWhenDrsFirst)
	}
}

// TestDRSGCUFlagResolution verifies the deterministic precedence applied by
// InitEnflameDevice when the yaml config is silent: an explicit
// --enflame-drs-gcu-resource-name wins over the legacy
// --enflame-vgcu-resource-name alias, which wins over the default.
func TestDRSGCUFlagResolution(t *testing.T) {
	testcases := []struct {
		name     string
		args     string
		expected string
	}{
		{
			name:     "no flags falls back to default",
			args:     "",
			expected: "enflame.com/drs-gcu",
		},
		{
			name:     "new flag only",
			args:     "--enflame-drs-gcu-resource-name=custom-new",
			expected: "custom-new",
		},
		{
			name:     "legacy alias only",
			args:     "--enflame-vgcu-resource-name=custom-legacy",
			expected: "custom-legacy",
		},
		{
			name:     "new flag wins over legacy alias",
			args:     "--enflame-vgcu-resource-name=custom-legacy --enflame-drs-gcu-resource-name=custom-new",
			expected: "custom-new",
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			resetGlobals(t)
			parseFlags(t, tc.args)
			InitEnflameDevice(EnflameConfig{})
			if EnflameResourceNameDRSGCU != tc.expected {
				t.Errorf("args %q: expected %q, got %q", tc.args, tc.expected, EnflameResourceNameDRSGCU)
			}
		})
	}
}

// TestDRSGCUYamlPrecedenceOverFlags verifies that the yaml config keeps
// winning over the flags, matching the behavior of InitEnflameDevice before
// the flag fallback was introduced.
func TestDRSGCUYamlPrecedenceOverFlags(t *testing.T) {
	resetGlobals(t)
	parseFlags(t, "--enflame-drs-gcu-resource-name=from-flag")

	InitEnflameDevice(EnflameConfig{ResourceNameDRSGCU: "from-yaml"})
	if EnflameResourceNameDRSGCU != "from-yaml" {
		t.Errorf("expected yaml value to win, got %q", EnflameResourceNameDRSGCU)
	}

	// yaml legacy key still works when the new key is absent.
	InitEnflameDevice(EnflameConfig{ResourceNameVGCU: "from-yaml-legacy"})
	if EnflameResourceNameDRSGCU != "from-yaml-legacy" {
		t.Errorf("expected yaml legacy value to win over flags, got %q", EnflameResourceNameDRSGCU)
	}

	// yaml new key wins over the yaml legacy key when both are set.
	InitEnflameDevice(EnflameConfig{ResourceNameDRSGCU: "from-yaml", ResourceNameVGCU: "from-yaml-legacy"})
	if EnflameResourceNameDRSGCU != "from-yaml" {
		t.Errorf("expected yaml new key to win over yaml legacy key, got %q", EnflameResourceNameDRSGCU)
	}
}

// TestDRSGCUIsolationAcrossReinits guards against a subtle regression: the
// flag values must live in dedicated variables, so that a second call to
// InitEnflameDevice with a different (or empty) yaml config cannot leak the
// previously resolved value back into the fallback chain.
func TestDRSGCUIsolationAcrossReinits(t *testing.T) {
	resetGlobals(t)

	// First init: yaml sets a name, flags are silent.
	InitEnflameDevice(EnflameConfig{ResourceNameDRSGCU: "first-yaml"})
	if EnflameResourceNameDRSGCU != "first-yaml" {
		t.Fatalf("expected %q, got %q", "first-yaml", EnflameResourceNameDRSGCU)
	}

	// Second init: yaml is silent now. The resolved name must fall back to
	// the default, NOT to the value resolved by the first call.
	InitEnflameDevice(EnflameConfig{})
	if EnflameResourceNameDRSGCU != defaultEnflameDRSGCUResourceName {
		t.Errorf("re-init leaked previous value: expected default %q, got %q",
			defaultEnflameDRSGCUResourceName, EnflameResourceNameDRSGCU)
	}

	// Third init: flags are set; they must survive re-inits untouched.
	enflameDRSGCUFlagName = "flag-value"
	InitEnflameDevice(EnflameConfig{})
	if EnflameResourceNameDRSGCU != "flag-value" {
		t.Errorf("expected flag value %q to win over default, got %q", "flag-value", EnflameResourceNameDRSGCU)
	}
}

// TestResourceNameMismatchSilentlySkipsPod documents the production impact of
// issue #3016: when the configured resource name does not match the name a
// pod requests, the pod is silently skipped by HAMi.
func TestResourceNameMismatchSilentlySkipsPod(t *testing.T) {
	resetGlobals(t)
	dev := &EnflameDevices{}
	newCtr := func() *corev1.Container {
		return &corev1.Container{
			Name: "worker",
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					// Pod written the legacy way, e.g. pre-existing workloads.
					corev1.ResourceName("enflame.com/vgcu"): resource.MustParse("2"),
				},
			},
		}
	}

	// Flag order resolved the configured name to the new name; pod uses the legacy name.
	EnflameResourceNameDRSGCU = "enflame.com/drs-gcu"

	mutated, err := dev.MutateAdmission(newCtr(), &corev1.Pod{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mutated {
		t.Errorf("expected HAMi to skip the container when names mismatch, got mutated=true")
	}

	req := dev.GenerateResourceRequests(newCtr())
	if req.Nums != 0 {
		t.Errorf("expected zero device request when names mismatch, got %+v", req)
	}

	// Control: names match — HAMi claims the container.
	EnflameResourceNameDRSGCU = "enflame.com/vgcu"

	mutated2, err2 := dev.MutateAdmission(newCtr(), &corev1.Pod{})
	if err2 != nil {
		t.Fatalf("unexpected error: %v", err2)
	}
	if !mutated2 {
		t.Errorf("expected HAMi to claim the container when names match")
	}
	req2 := dev.GenerateResourceRequests(newCtr())
	if req2.Nums != 1 {
		t.Errorf("expected one device request when names match, got %+v", req2)
	}
}
