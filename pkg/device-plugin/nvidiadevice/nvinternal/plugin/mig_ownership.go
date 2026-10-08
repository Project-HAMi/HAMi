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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/cdi"
)

const (
	dynamicMIGOwnershipVersion    = 1
	dynamicMIGOwnershipRoot       = "/var/run/cdi/hami-dynamic-mig-state"
	dynamicMIGOwnershipFilePrefix = "hami-owned-"
)

type migOwnershipPlacement struct {
	Start uint32 `json:"start"`
	Size  uint32 `json:"size"`
}

// migOwnershipRecord is durable proof that HAMi created or adopted one MIG
// GI/CI pair. It is retained while the instance is active or cached as idle.
type migOwnershipRecord struct {
	Version           int                   `json:"version"`
	MIGUUID           string                `json:"migUUID"`
	ParentGPUUUID     string                `json:"parentGPUUUID"`
	Profile           string                `json:"profile"`
	Placement         migOwnershipPlacement `json:"placement"`
	GPUInstanceID     uint32                `json:"gpuInstanceID"`
	ComputeInstanceID uint32                `json:"computeInstanceID"`
}

type dynamicMIGOwnershipStore interface {
	Load() (map[string]migOwnershipRecord, error)
	Save(migOwnershipRecord) error
	Remove(string) error
}

type fileMIGOwnershipStore struct {
	root string
	mu   sync.Mutex
}

func newFileMIGOwnershipStore(root string) *fileMIGOwnershipStore {
	return &fileMIGOwnershipStore{root: root}
}

func (s *fileMIGOwnershipStore) path(uuid string) (string, error) {
	name, err := cdi.DynamicMIGName(uuid)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.root, dynamicMIGOwnershipFilePrefix+name+".json"), nil
}

func validateMIGOwnershipRecord(record migOwnershipRecord) error {
	if record.Version != dynamicMIGOwnershipVersion {
		return fmt.Errorf("unsupported dynamic MIG ownership version %d", record.Version)
	}
	if _, err := cdi.DynamicMIGName(record.MIGUUID); err != nil {
		return err
	}
	if strings.TrimSpace(record.ParentGPUUUID) == "" || strings.TrimSpace(record.Profile) == "" {
		return fmt.Errorf("dynamic MIG ownership record %q lacks parent GPU or profile", record.MIGUUID)
	}
	if record.Placement.Size == 0 {
		return fmt.Errorf("dynamic MIG ownership record %q has an empty placement", record.MIGUUID)
	}
	return nil
}

func (s *fileMIGOwnershipStore) Load() (map[string]migOwnershipRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	records := make(map[string]migOwnershipRecord)
	entries, err := os.ReadDir(s.root)
	if os.IsNotExist(err) {
		return records, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read dynamic MIG ownership directory: %w", err)
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), dynamicMIGOwnershipFilePrefix) || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return nil, fmt.Errorf("dynamic MIG ownership path %q is not a regular file", filepath.Join(s.root, entry.Name()))
		}
		path := filepath.Join(s.root, entry.Name())
		record, err := readMIGOwnershipRecord(path)
		if err != nil {
			return nil, err
		}
		expected, err := s.path(record.MIGUUID)
		if err != nil {
			return nil, err
		}
		if filepath.Clean(path) != filepath.Clean(expected) {
			return nil, fmt.Errorf("dynamic MIG ownership filename does not match UUID %q", record.MIGUUID)
		}
		if _, exists := records[record.MIGUUID]; exists {
			return nil, fmt.Errorf("duplicate dynamic MIG ownership record for %q", record.MIGUUID)
		}
		records[record.MIGUUID] = record
	}
	return records, nil
}

func readMIGOwnershipRecord(path string) (migOwnershipRecord, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return migOwnershipRecord{}, err
	}
	var record migOwnershipRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return migOwnershipRecord{}, fmt.Errorf("decode dynamic MIG ownership file %q: %w", path, err)
	}
	if err := validateMIGOwnershipRecord(record); err != nil {
		return migOwnershipRecord{}, fmt.Errorf("validate dynamic MIG ownership file %q: %w", path, err)
	}
	return record, nil
}

func (s *fileMIGOwnershipStore) Save(record migOwnershipRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	record.Version = dynamicMIGOwnershipVersion
	if err := validateMIGOwnershipRecord(record); err != nil {
		return err
	}
	path, err := s.path(record.MIGUUID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.root, 0750); err != nil {
		return fmt.Errorf("create dynamic MIG ownership directory: %w", err)
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to replace non-regular dynamic MIG ownership path %q", path)
		}
		existing, err := readMIGOwnershipRecord(path)
		if err != nil {
			return err
		}
		if existing.MIGUUID != record.MIGUUID {
			return fmt.Errorf("refusing to replace dynamic MIG ownership file %q for a different UUID", path)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(s.root, ".hami-owned-*")
	if err != nil {
		return fmt.Errorf("create dynamic MIG ownership temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("publish dynamic MIG ownership record: %w", err)
	}
	return syncMIGOwnershipDirectory(s.root)
}

func (s *fileMIGOwnershipStore) Remove(uuid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path, err := s.path(uuid)
	if err != nil {
		return err
	}
	record, err := readMIGOwnershipRecord(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if record.MIGUUID != uuid {
		return fmt.Errorf("refusing to remove dynamic MIG ownership file %q for a different UUID", path)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return syncMIGOwnershipDirectory(s.root)
}

func syncMIGOwnershipDirectory(root string) error {
	dir, err := os.Open(root)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (plugin *NvidiaDevicePlugin) trackedMIGOwnershipRecord(key migAllocationKey) (migOwnershipRecord, error) {
	plugin.migMgr.mu.Lock()
	inst := plugin.migMgr.byAllocation[key]
	if inst == nil {
		plugin.migMgr.mu.Unlock()
		return migOwnershipRecord{}, fmt.Errorf("MIG allocation is not tracked for gpu %d profile=%s placement=%d:%d", key.GPUIndex, key.Profile, key.Start, key.Size)
	}
	record := migOwnershipRecord{
		Version: dynamicMIGOwnershipVersion, MIGUUID: inst.MigUUID, Profile: inst.Profile,
		Placement:     migOwnershipPlacement{Start: inst.Placement.Start, Size: inst.Placement.Size},
		GPUInstanceID: inst.GIID, ComputeInstanceID: inst.CIID,
	}
	plugin.migMgr.mu.Unlock()

	dev, ret := plugin.migMgr.nvmllib.DeviceGetHandleByIndex(key.GPUIndex)
	if ret != nvml.SUCCESS {
		return migOwnershipRecord{}, fmt.Errorf("get parent GPU %d for MIG ownership: %s", key.GPUIndex, nvml.ErrorString(ret))
	}
	record.ParentGPUUUID, ret = dev.GetUUID()
	if ret != nvml.SUCCESS {
		return migOwnershipRecord{}, fmt.Errorf("get parent GPU UUID for MIG ownership: %s", nvml.ErrorString(ret))
	}
	return record, nil
}

// persistTrackedMIGOwnership records every allocation whose ownership is
// already established by a live Pod, a prior ownership file, or explicit
// adoption. Unknown NVML instances never enter the manager and are not saved.
func (plugin *NvidiaDevicePlugin) persistTrackedMIGOwnership() error {
	if plugin.migOwnership == nil {
		return nil
	}
	plugin.migMgr.mu.Lock()
	keys := make([]migAllocationKey, 0, len(plugin.migMgr.byAllocation))
	for key := range plugin.migMgr.byAllocation {
		keys = append(keys, key)
	}
	plugin.migMgr.mu.Unlock()
	for _, key := range keys {
		record, err := plugin.trackedMIGOwnershipRecord(key)
		if err != nil {
			return err
		}
		if err := plugin.migOwnership.Save(record); err != nil {
			return fmt.Errorf("persist ownership of MIG instance %s: %w", record.MIGUUID, err)
		}
		delete(plugin.pendingMIGOwnershipRemovals, record.MIGUUID)
	}
	return nil
}

func (plugin *NvidiaDevicePlugin) persistMIGOwnership(key migAllocationKey) error {
	if plugin.migOwnership == nil {
		return nil
	}
	record, err := plugin.trackedMIGOwnershipRecord(key)
	if err != nil {
		return err
	}
	if err := plugin.migOwnership.Save(record); err != nil {
		return fmt.Errorf("persist ownership of MIG instance %s: %w", record.MIGUUID, err)
	}
	delete(plugin.pendingMIGOwnershipRemovals, record.MIGUUID)
	return nil
}

func (plugin *NvidiaDevicePlugin) removeMIGOwnership(uuids []string) {
	if plugin.migOwnership == nil || len(uuids) == 0 {
		return
	}
	if plugin.pendingMIGOwnershipRemovals == nil {
		plugin.pendingMIGOwnershipRemovals = make(map[string]struct{})
	}
	for _, uuid := range uuids {
		if err := plugin.migOwnership.Remove(uuid); err != nil {
			klog.ErrorS(err, "failed to remove dynamic MIG ownership record", "uuid", uuid)
			plugin.pendingMIGOwnershipRemovals[uuid] = struct{}{}
			continue
		}
		delete(plugin.pendingMIGOwnershipRemovals, uuid)
	}
}

func (plugin *NvidiaDevicePlugin) retryMIGOwnershipRemovals() error {
	if plugin.migOwnership == nil || len(plugin.pendingMIGOwnershipRemovals) == 0 {
		return nil
	}
	var retryErr error
	for uuid := range plugin.pendingMIGOwnershipRemovals {
		plugin.migMgr.mu.Lock()
		_, live := plugin.migMgr.byAllocationMigUUID[uuid]
		plugin.migMgr.mu.Unlock()
		if live {
			delete(plugin.pendingMIGOwnershipRemovals, uuid)
			continue
		}
		if err := plugin.migOwnership.Remove(uuid); err != nil {
			retryErr = errors.Join(retryErr, fmt.Errorf("remove dynamic MIG ownership record %s: %w", uuid, err))
			continue
		}
		delete(plugin.pendingMIGOwnershipRemovals, uuid)
	}
	return retryErr
}
