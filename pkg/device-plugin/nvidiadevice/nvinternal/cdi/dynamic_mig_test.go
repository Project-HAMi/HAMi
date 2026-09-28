/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * Copyright (c) 2026 The HAMi Authors.
 */

package cdi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/NVIDIA/nvidia-container-toolkit/pkg/nvcdi"
	nvcdspec "github.com/NVIDIA/nvidia-container-toolkit/pkg/nvcdi/spec"
	"github.com/stretchr/testify/require"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdiparser "tags.cncf.io/container-device-interface/pkg/parser"
	cdspec "tags.cncf.io/container-device-interface/specs-go"
)

type fakeDynamicMIGLib struct {
	commonEnv  string
	parentPath string
}

func (fakeDynamicMIGLib) GetSpec(...string) (nvcdspec.Interface, error) { return nil, nil }
func (fakeDynamicMIGLib) GetAllDeviceSpecs() ([]cdspec.Device, error)   { return nil, nil }
func (f *fakeDynamicMIGLib) GetCommonEdits() (*cdiapi.ContainerEdits, error) {
	return &cdiapi.ContainerEdits{ContainerEdits: &cdspec.ContainerEdits{Env: []string{f.commonEnv}}}, nil
}
func (f *fakeDynamicMIGLib) GetDeviceSpecsByID(...string) ([]cdspec.Device, error) {
	return []cdspec.Device{{Name: "GPU-parent", ContainerEdits: cdspec.ContainerEdits{
		DeviceNodes: []*cdspec.DeviceNode{{Path: f.parentPath, Type: "c", Major: 195, Minor: 0}},
	}}}, nil
}

func testDynamicMIGHandler(t *testing.T) *cdiHandler {
	t.Helper()
	root := t.TempDir()
	proc := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proc, "devices"), []byte("Character devices:\n 195 nvidia\n 238 nvidia-caps\nBlock devices:\n"), 0600))
	base := filepath.Join(proc, "driver/nvidia/capabilities/gpu0/mig/gi1")
	require.NoError(t, os.MkdirAll(filepath.Join(base, "ci2"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(base, "access"), []byte("DeviceFileMinor: 42\nDeviceFileMode: 438\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(base, "ci2/access"), []byte("DeviceFileMinor: 43\nDeviceFileMode: 438\n"), 0600))
	return &cdiHandler{
		vendor: "k8s.device-plugin.nvidia.com", dynamicMIGRoot: root, dynamicMIGProcRoot: proc,
		cdilibs: map[string]nvcdi.SpecGenerator{"gpu": &fakeDynamicMIGLib{commonEnv: "NVIDIA_VISIBLE_DEVICES=void", parentPath: "/dev/nvidia0"}}, driverRoot: "/", devRoot: "/",
	}
}

func TestDynamicMIGCDILifecycleWithoutGPU(t *testing.T) {
	h := testDynamicMIGHandler(t)
	dev := DynamicMIGDevice{MIGUUID: "MIG-GPU-parent/1/2", ParentGPUUUID: "GPU-parent", ParentMinor: 0, GPUInstanceID: 1, ComputeInstanceID: 2}
	qualified, err := h.EnsureDynamicMIGDevice(dev)
	require.NoError(t, err)
	_, class, _, err := cdiparser.ParseQualifiedName(qualified)
	require.NoError(t, err)
	require.Equal(t, DynamicMIGClass, class)
	path, _, err := h.dynamicMIGPath(dev.MIGUUID)
	require.NoError(t, err)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var saved cdspec.Spec
	require.NoError(t, json.Unmarshal(data, &saved))
	require.Equal(t, dev.MIGUUID, saved.Devices[0].Annotations[dynamicMIGUUIDAnnotation])
	require.Len(t, saved.Devices[0].ContainerEdits.DeviceNodes, 3)
	require.Equal(t, "/dev/nvidia-caps/nvidia-cap42", saved.Devices[0].ContainerEdits.DeviceNodes[1].Path)
	require.Equal(t, int64(238), saved.Devices[0].ContainerEdits.DeviceNodes[2].Major)
	require.Equal(t, int64(43), saved.Devices[0].ContainerEdits.DeviceNodes[2].Minor)
	require.Equal(t, os.FileMode(0666), *saved.Devices[0].ContainerEdits.DeviceNodes[2].FileMode)
	_, err = cdiapi.ReadSpec(path, 0)
	require.NoError(t, err)
	cache, err := cdiapi.NewCache(cdiapi.WithSpecDirs(h.dynamicMIGRoot))
	require.NoError(t, err)
	require.NotNil(t, cache.GetDevice(qualified))
	second, err := h.EnsureDynamicMIGDevice(dev)
	require.NoError(t, err)
	require.Equal(t, qualified, second)
	dataAfterReuse, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, data, dataAfterReuse)
	require.NoError(t, os.WriteFile(path, []byte("invalid CDI spec"), 0600))
	third, err := h.EnsureDynamicMIGDevice(dev)
	require.Error(t, err)
	require.Empty(t, third)
	dataAfterInvalidEnsure, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, []byte("invalid CDI spec"), dataAfterInvalidEnsure)
	require.NoError(t, os.WriteFile(path, data, 0600))
	require.NoError(t, h.RemoveDynamicMIGDevice(dev.MIGUUID))
	require.NoError(t, h.RemoveDynamicMIGDevice(dev.MIGUUID))
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err))
}

func TestDynamicMIGCDIStartupRecovery(t *testing.T) {
	h := testDynamicMIGHandler(t)
	dev := DynamicMIGDevice{MIGUUID: "MIG-live", ParentGPUUUID: "GPU-parent", ParentMinor: 0, GPUInstanceID: 1, ComputeInstanceID: 2}
	stale := DynamicMIGDevice{MIGUUID: "MIG-stale", ParentGPUUUID: "GPU-parent", ParentMinor: 0, GPUInstanceID: 1, ComputeInstanceID: 2}
	_, err := h.EnsureDynamicMIGDevice(stale)
	require.NoError(t, err)
	require.NoError(t, h.ReplaceDynamicMIGDevices([]DynamicMIGDevice{dev}))
	stalePath, _, err := h.dynamicMIGPath(stale.MIGUUID)
	require.NoError(t, err)
	_, err = os.Stat(stalePath)
	require.True(t, os.IsNotExist(err))
	livePath, _, err := h.dynamicMIGPath(dev.MIGUUID)
	require.NoError(t, err)
	_, err = cdiapi.ReadSpec(livePath, 0)
	require.NoError(t, err)
}

func TestDynamicMIGCDIRegeneratesWhenCompleteSpecChanges(t *testing.T) {
	h := testDynamicMIGHandler(t)
	dev := DynamicMIGDevice{MIGUUID: "MIG-live", ParentGPUUUID: "GPU-parent", ParentMinor: 0, GPUInstanceID: 1, ComputeInstanceID: 2}
	_, err := h.EnsureDynamicMIGDevice(dev)
	require.NoError(t, err)
	path, _, err := h.dynamicMIGPath(dev.MIGUUID)
	require.NoError(t, err)
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	// This simulates a toolkit/driver update that changes common NVIDIA edits.
	lib := h.cdilibs["gpu"].(*fakeDynamicMIGLib)
	lib.commonEnv = "NVIDIA_VISIBLE_DEVICES=all"
	_, err = h.EnsureDynamicMIGDevice(dev)
	require.NoError(t, err)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotEqual(t, before, after)
	var saved cdspec.Spec
	require.NoError(t, json.Unmarshal(after, &saved))
	require.Contains(t, saved.ContainerEdits.Env, "NVIDIA_VISIBLE_DEVICES=all")
}

func TestDynamicMIGCDIReuseDoesNotRewriteValidSpec(t *testing.T) {
	h := testDynamicMIGHandler(t)
	dev := DynamicMIGDevice{MIGUUID: "MIG-live", ParentGPUUUID: "GPU-parent", ParentMinor: 0, GPUInstanceID: 1, ComputeInstanceID: 2}
	_, err := h.EnsureDynamicMIGDevice(dev)
	require.NoError(t, err)
	path, _, err := h.dynamicMIGPath(dev.MIGUUID)
	require.NoError(t, err)
	old := time.Unix(1, 0)
	require.NoError(t, os.Chtimes(path, old, old))

	_, err = h.EnsureDynamicMIGDevice(dev)
	require.NoError(t, err)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, old, info.ModTime())
}

func TestDynamicMIGCDIEnsurePreservesUnownedFile(t *testing.T) {
	h := testDynamicMIGHandler(t)
	dev := DynamicMIGDevice{MIGUUID: "MIG-live", ParentGPUUUID: "GPU-parent", ParentMinor: 0, GPUInstanceID: 1, ComputeInstanceID: 2}
	_, err := h.EnsureDynamicMIGDevice(dev)
	require.NoError(t, err)
	path, _, err := h.dynamicMIGPath(dev.MIGUUID)
	require.NoError(t, err)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var saved cdspec.Spec
	require.NoError(t, json.Unmarshal(raw, &saved))
	saved.Kind = "example.com/administrator-owned"
	raw, err = json.Marshal(saved)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0600))

	_, err = h.EnsureDynamicMIGDevice(dev)
	require.Error(t, err)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, raw, after)
}

func TestDynamicMIGCDIStartupRecoveryAllowsMissingDirectoryWithoutLiveDevices(t *testing.T) {
	h := testDynamicMIGHandler(t)
	h.dynamicMIGRoot = filepath.Join(t.TempDir(), "missing")
	require.NoError(t, h.ReplaceDynamicMIGDevices(nil))
}

func TestDynamicMIGCDIRemovalPreservesUnownedFile(t *testing.T) {
	h := testDynamicMIGHandler(t)
	dev := DynamicMIGDevice{MIGUUID: "MIG-live", ParentGPUUUID: "GPU-parent", ParentMinor: 0, GPUInstanceID: 1, ComputeInstanceID: 2}
	_, err := h.EnsureDynamicMIGDevice(dev)
	require.NoError(t, err)
	path, _, err := h.dynamicMIGPath(dev.MIGUUID)
	require.NoError(t, err)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var saved cdspec.Spec
	require.NoError(t, json.Unmarshal(raw, &saved))
	saved.Kind = "example.com/administrator-owned"
	raw, err = json.Marshal(saved)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0600))

	err = h.RemoveDynamicMIGDevice(dev.MIGUUID)
	require.Error(t, err)
	_, err = os.Stat(path)
	require.NoError(t, err)
}

func TestDynamicMIGCDIRemovalRejectsFilenameUUIDMismatch(t *testing.T) {
	h := testDynamicMIGHandler(t)
	dev := DynamicMIGDevice{MIGUUID: "MIG-live", ParentGPUUUID: "GPU-parent", ParentMinor: 0, GPUInstanceID: 1, ComputeInstanceID: 2}
	_, err := h.EnsureDynamicMIGDevice(dev)
	require.NoError(t, err)
	path, _, err := h.dynamicMIGPath(dev.MIGUUID)
	require.NoError(t, err)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var saved cdspec.Spec
	require.NoError(t, json.Unmarshal(raw, &saved))
	saved.Devices[0].Annotations[dynamicMIGUUIDAnnotation] = "MIG-different"
	raw, err = json.Marshal(saved)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0600))

	err = h.RemoveDynamicMIGDevice(dev.MIGUUID)
	require.Error(t, err)
	_, err = os.Stat(path)
	require.NoError(t, err)
}

func TestDynamicMIGCDIStartupRecoveryPreservesUnownedFile(t *testing.T) {
	h := testDynamicMIGHandler(t)
	dev := DynamicMIGDevice{MIGUUID: "MIG-stale", ParentGPUUUID: "GPU-parent", ParentMinor: 0, GPUInstanceID: 1, ComputeInstanceID: 2}
	_, err := h.EnsureDynamicMIGDevice(dev)
	require.NoError(t, err)
	path, _, err := h.dynamicMIGPath(dev.MIGUUID)
	require.NoError(t, err)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var saved cdspec.Spec
	require.NoError(t, json.Unmarshal(raw, &saved))
	saved.Kind = "example.com/administrator-owned"
	raw, err = json.Marshal(saved)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0600))

	err = h.ReplaceDynamicMIGDevices(nil)
	require.Error(t, err)
	_, err = os.Stat(path)
	require.NoError(t, err)
}

func TestDynamicMIGCDIRejectsUnsafeIdentityAndMissingCaps(t *testing.T) {
	h := testDynamicMIGHandler(t)
	_, err := h.EnsureDynamicMIGDevice(DynamicMIGDevice{MIGUUID: "../bad", ParentGPUUUID: "GPU-parent"})
	require.Error(t, err)
	_, err = h.EnsureDynamicMIGDevice(DynamicMIGDevice{MIGUUID: "MIG-missing", ParentGPUUUID: "GPU-parent", ParentMinor: 3})
	require.Error(t, err)
}

func TestDynamicMIGCDIPublicationFailureReturnsNoName(t *testing.T) {
	h := testDynamicMIGHandler(t)
	notDirectory := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(notDirectory, []byte("not a directory"), 0600))
	h.dynamicMIGRoot = notDirectory
	name, err := h.EnsureDynamicMIGDevice(DynamicMIGDevice{
		MIGUUID: "MIG-test", ParentGPUUUID: "GPU-parent", ParentMinor: 0,
		GPUInstanceID: 1, ComputeInstanceID: 2,
	})
	require.Error(t, err)
	require.Empty(t, name)
}

func TestDynamicMIGCDIConcurrentIndependentFiles(t *testing.T) {
	h := testDynamicMIGHandler(t)
	devices := []DynamicMIGDevice{
		{MIGUUID: "MIG-first", ParentGPUUUID: "GPU-parent", ParentMinor: 0, GPUInstanceID: 1, ComputeInstanceID: 2},
		{MIGUUID: "MIG-second", ParentGPUUUID: "GPU-parent", ParentMinor: 0, GPUInstanceID: 1, ComputeInstanceID: 2},
	}
	var wg sync.WaitGroup
	errors := make(chan error, len(devices))
	for _, dev := range devices {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := h.EnsureDynamicMIGDevice(dev)
			errors <- err
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	for _, dev := range devices {
		path, _, err := h.dynamicMIGPath(dev.MIGUUID)
		require.NoError(t, err)
		_, err = cdiapi.ReadSpec(path, 0)
		require.NoError(t, err)
	}
}
