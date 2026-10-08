/*
 * Copyright (c) 2026, HAMi.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

package plugin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	nvmlmock "github.com/NVIDIA/go-nvml/pkg/nvml/mock"
	"github.com/stretchr/testify/require"
)

func testMIGOwnershipRecord() migOwnershipRecord {
	return migOwnershipRecord{
		MIGUUID: "MIG-owned", ParentGPUUUID: "GPU-parent", Profile: "1g.5gb",
		Placement: migOwnershipPlacement{Start: 1, Size: 1}, GPUInstanceID: 2, ComputeInstanceID: 3,
	}
}

func TestFileMIGOwnershipStoreLifecycle(t *testing.T) {
	store := newFileMIGOwnershipStore(t.TempDir())
	record := testMIGOwnershipRecord()

	require.NoError(t, store.Save(record))
	records, err := store.Load()
	require.NoError(t, err)
	require.Equal(t, dynamicMIGOwnershipVersion, records[record.MIGUUID].Version)
	require.Equal(t, record.ParentGPUUUID, records[record.MIGUUID].ParentGPUUUID)
	require.Equal(t, record.Profile, records[record.MIGUUID].Profile)
	require.Equal(t, record.Placement, records[record.MIGUUID].Placement)

	record.Profile = "1g.10gb"
	require.NoError(t, store.Save(record))
	records, err = store.Load()
	require.NoError(t, err)
	require.Equal(t, record.Profile, records[record.MIGUUID].Profile)

	require.NoError(t, store.Remove(record.MIGUUID))
	require.NoError(t, store.Remove(record.MIGUUID))
	records, err = store.Load()
	require.NoError(t, err)
	require.Empty(t, records)
}

func TestFileMIGOwnershipStoreRejectsMismatchedFilename(t *testing.T) {
	root := t.TempDir()
	store := newFileMIGOwnershipStore(root)
	record := testMIGOwnershipRecord()
	require.NoError(t, store.Save(record))

	path, err := store.path(record.MIGUUID)
	require.NoError(t, err)
	wrong := filepath.Join(root, dynamicMIGOwnershipFilePrefix+"mig-wrong.json")
	require.NoError(t, os.Rename(path, wrong))

	_, err = store.Load()
	require.ErrorContains(t, err, "filename does not match")
	_, statErr := os.Stat(wrong)
	require.NoError(t, statErr)
}

func TestFileMIGOwnershipStorePreservesInvalidRecord(t *testing.T) {
	root := t.TempDir()
	store := newFileMIGOwnershipStore(root)
	record := testMIGOwnershipRecord()
	require.NoError(t, store.Save(record))
	path, err := store.path(record.MIGUUID)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("not-json"), 0600))

	_, err = store.Load()
	require.ErrorContains(t, err, "decode dynamic MIG ownership")
	require.Error(t, store.Remove(record.MIGUUID))
	_, statErr := os.Stat(path)
	require.NoError(t, statErr)
}

func TestPluginPersistsAndRemovesTrackedMIGOwnership(t *testing.T) {
	dev := &nvmlmock.Device{GetUUIDFunc: func() (string, nvml.Return) { return "GPU-parent", nvml.SUCCESS }}
	manager := initializedLazyMIGManager(t, dev)
	placement := nvml.GpuInstancePlacement{Start: 1, Size: 1}
	key := allocationKey(0, "1g.5gb", placement)
	manager.byAllocation[key] = &migInstance{
		Profile: key.Profile, Placement: placement, GIID: 2, CIID: 3,
		MigUUID: "MIG-owned", State: migInstanceIdle,
	}
	manager.byAllocationMigUUID["MIG-owned"] = key
	store := newFileMIGOwnershipStore(t.TempDir())
	plugin := &NvidiaDevicePlugin{migMgr: manager, migOwnership: store}

	require.NoError(t, plugin.persistTrackedMIGOwnership())
	records, err := store.Load()
	require.NoError(t, err)
	require.Equal(t, testMIGOwnershipRecord().MIGUUID, records["MIG-owned"].MIGUUID)
	require.Equal(t, key.Profile, records["MIG-owned"].Profile)

	delete(manager.byAllocation, key)
	delete(manager.byAllocationMigUUID, "MIG-owned")
	plugin.removeReclaimedMIGArtifacts([]string{"MIG-owned"})
	records, err = store.Load()
	require.NoError(t, err)
	require.Empty(t, records)
}
