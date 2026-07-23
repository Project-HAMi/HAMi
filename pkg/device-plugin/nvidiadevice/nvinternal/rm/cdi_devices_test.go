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

package rm

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"github.com/stretchr/testify/require"
)

// writeCDISpec writes a minimal NVIDIA device-plugin CDI spec exposing the
// supplied device names (plus the "all" meta-device) into dir.
func writeCDISpec(t *testing.T, dir string, names ...string) {
	t.Helper()

	devices := ""
	for _, n := range append(names, cdiAllDevice) {
		devices += `    {"name": "` + n + `", "containerEdits": {"deviceNodes": [{"path": "/dev/nvidia0"}]}},
`
	}
	content := `{
  "cdiVersion": "0.5.0",
  "kind": "` + CDIVendor + `/` + CDIClass + `",
  "devices": [
` + devices[:len(devices)-2] + `
  ]
}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, CDIVendor+"-"+CDIClass+".json"), []byte(content), 0600))
}

func gpuOnlyConfig() *spec.Config {
	return &spec.Config{
		Resources: spec.Resources{
			GPUs: []spec.Resource{{Pattern: "*", Name: "nvidia.com/gpu"}},
		},
	}
}

func TestListCDIGPUDevices(t *testing.T) {
	dir := t.TempDir()
	writeCDISpec(t, dir, "0", "1")

	names, err := listCDIGPUDevices([]string{dir})
	require.NoError(t, err)
	sort.Strings(names)
	// The "all" meta-device must be excluded.
	require.Equal(t, []string{"0", "1"}, names)
}

func TestListCDIGPUDevices_NoSpecs(t *testing.T) {
	names, err := listCDIGPUDevices([]string{t.TempDir()})
	require.NoError(t, err)
	require.Empty(t, names)
}

func TestHasCDISpecs(t *testing.T) {
	empty := t.TempDir()
	require.False(t, HasCDISpecs([]string{empty}))

	withSpec := t.TempDir()
	writeCDISpec(t, withSpec, "0")
	require.True(t, HasCDISpecs([]string{withSpec}))
}

func TestBuildCDIDeviceMap(t *testing.T) {
	dir := t.TempDir()
	writeCDISpec(t, dir, "0", "1")

	deviceMap, err := buildCDIDeviceMap(gpuOnlyConfig(), []string{dir})
	require.NoError(t, err)

	devices := deviceMap["nvidia.com/gpu"]
	require.Len(t, devices, 2)
	// Device IDs must be the CDI device names so the allocation path can build
	// a matching qualified CDI device name for injection.
	ids := devices.GetIDs()
	sort.Strings(ids)
	require.Equal(t, []string{"0", "1"}, ids)
}

func TestNewCDIResourceManagers(t *testing.T) {
	dir := t.TempDir()
	writeCDISpec(t, dir, "0")

	// Point discovery at the temp spec dir for the duration of the test.
	orig := cdiSpecDirs
	cdiSpecDirs = []string{dir}
	defer func() { cdiSpecDirs = orig }()

	rms, err := NewCDIResourceManagers(gpuOnlyConfig())
	require.NoError(t, err)
	require.Len(t, rms, 1)
	require.Equal(t, spec.ResourceName("nvidia.com/gpu"), rms[0].Resource())
	require.Len(t, rms[0].Devices(), 1)
	// CDI resource managers must not perform NVML health checks.
	require.NoError(t, rms[0].CheckHealth(nil, nil, nil, nil))
}
