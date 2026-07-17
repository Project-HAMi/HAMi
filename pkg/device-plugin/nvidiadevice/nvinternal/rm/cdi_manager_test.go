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
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCDISpecDirs(t *testing.T) {
	require.NotEmpty(t, CDISpecDirs())
}

// listCDIGPUDevices must ignore specs for other vendors/classes.
func TestListCDIGPUDevices_FiltersOtherVendors(t *testing.T) {
	dir := t.TempDir()
	writeCDISpec(t, dir, "0") // our vendor/class

	// A spec from a different vendor/class that must be ignored.
	other := `{
  "cdiVersion": "0.5.0",
  "kind": "example.com/other",
  "devices": [
    {"name": "x", "containerEdits": {"deviceNodes": [{"path": "/dev/null"}]}}
  ]
}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "example.com-other.json"), []byte(other), 0600))

	names, err := listCDIGPUDevices([]string{dir})
	require.NoError(t, err)
	require.Equal(t, []string{"0"}, names)
}

func TestCDIResourceManager_AllocationHelpers(t *testing.T) {
	dir := t.TempDir()
	writeCDISpec(t, dir, "0", "1")

	orig := cdiSpecDirs
	cdiSpecDirs = []string{dir}
	defer func() { cdiSpecDirs = orig }()

	rms, err := NewCDIResourceManagers(gpuOnlyConfig())
	require.NoError(t, err)
	require.Len(t, rms, 1)
	r := rms[0]

	ids := r.Devices().GetIDs()
	require.Len(t, ids, 2)

	// GetPreferredAllocation returns a size-limited distributed allocation.
	alloc, err := r.GetPreferredAllocation(ids, nil, 1)
	require.NoError(t, err)
	require.Len(t, alloc, 1)
	require.Contains(t, ids, alloc[0])

	// CDI devices are injected via CDI, so no explicit device paths.
	require.Nil(t, r.GetDevicePaths(ids))
}
