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

package amd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeScope(t *testing.T, root, podDir, scope, current, max string) {
	t.Helper()
	dir := filepath.Join(root, "kubepods.slice", podDir, scope)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "dmem.current"), []byte(current), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "dmem.max"), []byte(max), 0o644))
}

func TestReadContainerVRAM(t *testing.T) {
	root := t.TempDir()
	const region = "drm/0000:06:00.0/vram"
	writeScope(t, root, "kubepods-burstable.slice/kubepods-burstable-poda3e5b9dc_cb3f_4ee5_8ea8_1f88a0437cb9.slice",
		"cri-containerd-abc123.scope", region+" 4294967296\n", region+" 8589934592\n")
	writeScope(t, root, "kubepods-pod0123abcd_0000_0000_0000_000000000001.slice",
		"crio-def456.scope", region+" 1024\n", region+" max\n")

	got, err := ReadContainerVRAM(root)
	require.NoError(t, err)
	require.ElementsMatch(t, []ContainerVRAM{
		{PodUID: "a3e5b9dc-cb3f-4ee5-8ea8-1f88a0437cb9", ContainerID: "abc123", Region: region, Used: 4294967296, Limit: 8589934592},
		{PodUID: "0123abcd-0000-0000-0000-000000000001", ContainerID: "def456", Region: region, Used: 1024},
	}, got)
}

func TestReadContainerVRAMSkipsWhatItCannotAttribute(t *testing.T) {
	root := t.TempDir()
	const region = "drm/0000:06:00.0/vram"
	// A scope outside any pod slice, and a container runtime this does not know.
	writeScope(t, root, "kubepods-burstable.slice", "cri-containerd-orphan.scope", region+" 1\n", region+" max\n")
	writeScope(t, root, "kubepods-pod0123abcd_0000_0000_0000_000000000001.slice", "other-xyz.scope", region+" 2\n", region+" max\n")

	got, err := ReadContainerVRAM(root)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestReadContainerVRAMWithoutKubepodsOrDmem(t *testing.T) {
	got, err := ReadContainerVRAM(t.TempDir())
	require.NoError(t, err)
	require.Empty(t, got)

	// A scope on a kernel without the dmem controller has no dmem.current.
	root := t.TempDir()
	dir := filepath.Join(root, "kubepods.slice", "kubepods-pod0123abcd_0000_0000_0000_000000000001.slice", "cri-containerd-abc.scope")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	got, err = ReadContainerVRAM(root)
	require.NoError(t, err)
	require.Empty(t, got)
}
