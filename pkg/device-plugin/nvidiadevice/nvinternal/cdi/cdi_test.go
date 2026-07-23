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

package cdi

import (
	"testing"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/info"
	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"github.com/stretchr/testify/require"
)

// fakeInfo is a minimal info.Interface used to drive the New() branches.
type fakeInfo struct {
	platform info.Platform
	hasNVML  bool
}

func (f fakeInfo) ResolvePlatform() info.Platform     { return f.platform }
func (f fakeInfo) HasDXCore() (bool, string)          { return false, "" }
func (f fakeInfo) HasNvml() (bool, string)            { return f.hasNVML, "" }
func (f fakeInfo) HasTegraFiles() (bool, string)      { return false, "" }
func (f fakeInfo) HasAnIntegratedGPU() (bool, string) { return false, "" }

func mustStrategies(t *testing.T, s ...string) spec.DeviceListStrategies {
	t.Helper()
	ds, err := spec.NewDeviceListStrategies(s)
	require.NoError(t, err)
	return ds
}

// When no CDI device-list strategy is enabled, New returns the null handler,
// whose QualifiedName yields an empty string.
func TestNew_NoCDIEnabled_ReturnsNull(t *testing.T) {
	h, err := New(fakeInfo{hasNVML: false}, nil, nil,
		WithDeviceListStrategies(mustStrategies(t, "envvar")))
	require.NoError(t, err)
	require.Empty(t, h.QualifiedName("gpu", "0"))
	require.NoError(t, h.CreateSpecFile())
}

// When a CDI device-list strategy is requested but NVML is unavailable (the
// GB10 / CDI-only case), New returns the external handler that can still build
// qualified device names for injection.
func TestNew_NoNVML_CDIEnabled_ReturnsExternal(t *testing.T) {
	h, err := New(fakeInfo{hasNVML: false}, nil, nil,
		WithDeviceListStrategies(mustStrategies(t, "cdi-annotations", "cdi-cri")),
		WithVendor(DefaultVendor))
	require.NoError(t, err)
	require.Equal(t, DefaultVendor+"/gpu=GPU-abc", h.QualifiedName("gpu", "GPU-abc"))
	require.NoError(t, h.CreateSpecFile())
	require.Empty(t, h.AdditionalDevices())
}

// The external handler falls back to DefaultVendor when no vendor is set.
func TestNew_NoNVML_DefaultVendor(t *testing.T) {
	h, err := New(fakeInfo{hasNVML: false}, nil, nil,
		WithDeviceListStrategies(mustStrategies(t, "cdi-cri")))
	require.NoError(t, err)
	require.Equal(t, DefaultVendor+"/gpu=0", h.QualifiedName("gpu", "0"))
}
