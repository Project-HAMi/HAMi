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
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	nvmlmock "github.com/NVIDIA/go-nvml/pkg/nvml/mock"
	"github.com/stretchr/testify/require"
)

type fakeMIGOwnershipStore struct {
	records   map[string]migOwnershipRecord
	saveErr   error
	removeErr error
	removed   []string
}

func (s *fakeMIGOwnershipStore) Load() (map[string]migOwnershipRecord, error) {
	return s.records, nil
}

func (s *fakeMIGOwnershipStore) Save(record migOwnershipRecord) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	if s.records == nil {
		s.records = make(map[string]migOwnershipRecord)
	}
	s.records[record.MIGUUID] = record
	return nil
}

func (s *fakeMIGOwnershipStore) Remove(uuid string) error {
	if s.removeErr != nil {
		return s.removeErr
	}
	s.removed = append(s.removed, uuid)
	delete(s.records, uuid)
	return nil
}

func testMIGOwnershipRecord() migOwnershipRecord {
	return migOwnershipRecord{
		MIGUUID: "MIG-owned", ParentGPUUUID: "GPU-parent", Profile: "1g.5gb",
		Placement: migOwnershipPlacement{Start: 1, Size: 1}, GPUInstanceID: 2, ComputeInstanceID: 3,
	}
}

func TestValidateMIGOwnershipRecord(t *testing.T) {
	valid := testMIGOwnershipRecord()
	valid.Version = dynamicMIGOwnershipVersion
	require.NoError(t, validateMIGOwnershipRecord(valid))

	for _, tc := range []struct {
		name   string
		mutate func(*migOwnershipRecord)
		want   string
	}{
		{"version", func(r *migOwnershipRecord) { r.Version++ }, "unsupported dynamic MIG ownership version"},
		{"MIG UUID", func(r *migOwnershipRecord) { r.MIGUUID = "GPU-not-mig" }, "invalid MIG UUID"},
		{"parent GPU", func(r *migOwnershipRecord) { r.ParentGPUUUID = "" }, "lacks parent GPU or profile"},
		{"profile", func(r *migOwnershipRecord) { r.Profile = "" }, "lacks parent GPU or profile"},
		{"placement", func(r *migOwnershipRecord) { r.Placement.Size = 0 }, "empty placement"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := valid
			tc.mutate(&record)
			require.ErrorContains(t, validateMIGOwnershipRecord(record), tc.want)
		})
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

func TestPersistMIGOwnershipRecordsOneAllocation(t *testing.T) {
	dev := &nvmlmock.Device{GetUUIDFunc: func() (string, nvml.Return) { return "GPU-parent", nvml.SUCCESS }}
	manager := initializedLazyMIGManager(t, dev)
	placement := nvml.GpuInstancePlacement{Start: 2, Size: 1}
	key := allocationKey(0, "1g.5gb", placement)
	manager.byAllocation[key] = &migInstance{
		Profile: key.Profile, Placement: placement, GIID: 4, CIID: 5,
		MigUUID: "MIG-one", State: migInstanceActive,
	}
	manager.byAllocationMigUUID["MIG-one"] = key
	store := &fakeMIGOwnershipStore{}
	plugin := &NvidiaDevicePlugin{
		migMgr: manager, migOwnership: store,
		pendingMIGOwnershipRemovals: map[string]struct{}{"MIG-one": {}},
	}

	require.NoError(t, plugin.persistMIGOwnership(key))
	require.Equal(t, "GPU-parent", store.records["MIG-one"].ParentGPUUUID)
	require.NotContains(t, plugin.pendingMIGOwnershipRemovals, "MIG-one")

	store.saveErr = errors.New("write failed")
	err := plugin.persistMIGOwnership(key)
	require.ErrorContains(t, err, "persist ownership of MIG instance MIG-one")
}

func TestMIGOwnershipRemovalRetriesAndPreservesLiveInstances(t *testing.T) {
	manager := initializedLazyMIGManager(t, &nvmlmock.Device{})
	store := &fakeMIGOwnershipStore{removeErr: errors.New("remove failed")}
	plugin := &NvidiaDevicePlugin{migMgr: manager, migOwnership: store}

	plugin.removeMIGOwnership([]string{"MIG-gone"})
	require.Contains(t, plugin.pendingMIGOwnershipRemovals, "MIG-gone")
	require.ErrorContains(t, plugin.retryMIGOwnershipRemovals(), "remove dynamic MIG ownership record MIG-gone")
	require.Contains(t, plugin.pendingMIGOwnershipRemovals, "MIG-gone")

	store.removeErr = nil
	require.NoError(t, plugin.retryMIGOwnershipRemovals())
	require.Equal(t, []string{"MIG-gone"}, store.removed)
	require.NotContains(t, plugin.pendingMIGOwnershipRemovals, "MIG-gone")

	placement := nvml.GpuInstancePlacement{Start: 0, Size: 1}
	key := allocationKey(0, "1g.5gb", placement)
	manager.byAllocation[key] = &migInstance{MigUUID: "MIG-live", State: migInstanceIdle}
	manager.byAllocationMigUUID["MIG-live"] = key
	plugin.pendingMIGOwnershipRemovals["MIG-live"] = struct{}{}
	require.NoError(t, plugin.retryMIGOwnershipRemovals())
	require.NotContains(t, plugin.pendingMIGOwnershipRemovals, "MIG-live")
	require.NotContains(t, store.removed, "MIG-live")
}
