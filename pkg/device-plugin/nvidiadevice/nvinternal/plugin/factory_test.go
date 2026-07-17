/*
Copyright 2024 The HAMi Authors.

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

package plugin

import (
	"testing"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/info"
	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"github.com/stretchr/testify/require"

	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

type fakeInfo struct {
	platform info.Platform
	hasNVML  bool
}

func (f fakeInfo) ResolvePlatform() info.Platform     { return f.platform }
func (f fakeInfo) HasDXCore() (bool, string)          { return false, "" }
func (f fakeInfo) HasNvml() (bool, string)            { return f.hasNVML, "" }
func (f fakeInfo) HasTegraFiles() (bool, string)      { return false, "" }
func (f fakeInfo) HasAnIntegratedGPU() (bool, string) { return false, "" }

func strategies(t *testing.T, s ...string) spec.DeviceListStrategies {
	t.Helper()
	ds, err := spec.NewDeviceListStrategies(s)
	require.NoError(t, err)
	return ds
}

func TestResolveStrategy(t *testing.T) {
	cdi := strategies(t, "cdi-cri")
	envvar := strategies(t, "envvar")

	cases := []struct {
		name     string
		input    string
		platform info.Platform
		strat    spec.DeviceListStrategies
		want     string
	}{
		{"explicit nvml", "nvml", "", envvar, "nvml"},
		{"explicit cdi", "cdi", "", envvar, "cdi"},
		{"explicit tegra", "tegra", "", envvar, "tegra"},
		{"auto->nvml", "auto", info.PlatformNVML, envvar, "nvml"},
		{"auto->wsl->nvml", "auto", info.PlatformWSL, envvar, "nvml"},
		{"auto->tegra", "auto", info.PlatformTegra, envvar, "tegra"},
		{"auto unknown, non-cdi list -> unchanged", "auto", info.PlatformUnknown, envvar, "auto"},
		// Unknown platform with a CDI list strategy but no CDI specs present on
		// the test host: the fallback condition is false, so the input is returned.
		{"empty unknown, cdi list, no specs -> unchanged", "", info.PlatformUnknown, cdi, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := &options{infolib: fakeInfo{platform: tc.platform}, deviceListStrategies: tc.strat}
			require.Equal(t, tc.want, o.resolveStrategy(tc.input))
		})
	}
}

func ptrStr(s string) *string { return &s }

// getResourceManagers with an explicit "cdi" strategy exercises the CDI branch.
// No CDI specs exist on the test host, so it returns an empty (nil) set.
func TestGetResourceManagers_CDI(t *testing.T) {
	cfg := &spec.Config{}
	cfg.Flags.DeviceDiscoveryStrategy = ptrStr("cdi")
	cfg.Resources.GPUs = []spec.Resource{{Pattern: "*", Name: "nvidia.com/gpu"}}

	o := &options{
		infolib:              fakeInfo{},
		deviceListStrategies: strategies(t, "cdi-cri"),
		config:               &nvidia.DeviceConfig{Config: cfg},
	}
	rms, err := o.getResourceManagers()
	require.NoError(t, err)
	require.Empty(t, rms)
	require.Equal(t, "cdi", o.resolvedStrategy)
}

// An unresolved strategy hits the default branch; with failOnInitError=false it
// returns no managers and no error.
func TestGetResourceManagers_Invalid(t *testing.T) {
	cfg := &spec.Config{}
	cfg.Flags.DeviceDiscoveryStrategy = ptrStr("auto")

	o := &options{
		infolib:              fakeInfo{platform: info.PlatformUnknown},
		deviceListStrategies: strategies(t, "envvar"),
		config:               &nvidia.DeviceConfig{Config: cfg},
		failOnInitError:      false,
	}
	rms, err := o.getResourceManagers()
	require.NoError(t, err)
	require.Empty(t, rms)
}
