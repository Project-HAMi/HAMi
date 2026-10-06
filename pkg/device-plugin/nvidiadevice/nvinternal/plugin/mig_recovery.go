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
	"fmt"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"k8s.io/klog/v2"
)

var orderedMIGProfiles = []string{"1g", "2g", "3g", "4g", "6g", "7g", "8g"}

// RestoreAllocations rebuilds manager state from the actual NVML GI/CI layout.
// Active Pod allocations must be adopted before this call. Unknown instances
// are preserved unless adoptExisting is explicitly enabled for a HAMi-exclusive
// MIG layout.
func (m *MigInstanceManager) RestoreAllocations(deviceCount int, inUse map[int]struct{}, adoptExisting bool) error {
	done, err := m.beginOperation()
	if err != nil {
		return err
	}
	defer done()

	for gpuIndex := 0; gpuIndex < deviceCount; gpuIndex++ {
		lk := m.gpuLock(gpuIndex)
		lk.Lock()
		_, busy := inUse[gpuIndex]
		if !busy {
			if err := m.ensureMigModeEnabled(gpuIndex); err != nil {
				lk.Unlock()
				return err
			}
		}
		dev, err := m.deviceHandleByIndex(gpuIndex)
		if err != nil {
			lk.Unlock()
			return err
		}
		current, _, ret := dev.GetMigMode()
		if ret == nvml.ERROR_NOT_SUPPORTED {
			lk.Unlock()
			continue
		}
		if ret != nvml.SUCCESS {
			lk.Unlock()
			return fmt.Errorf("gpu %d get MIG mode during recovery: %s", gpuIndex, nvml.ErrorString(ret))
		}
		if current != nvml.DEVICE_MIG_ENABLE {
			lk.Unlock()
			continue
		}
		if err := m.restoreGPUAllocationsLocked(gpuIndex, dev, adoptExisting, !busy); err != nil {
			lk.Unlock()
			return err
		}
		lk.Unlock()
	}
	return nil
}

func (m *MigInstanceManager) restoreGPUAllocationsLocked(gpuIndex int, dev nvml.Device, adoptExisting, allowOrphanCleanup bool) error {
	for _, profile := range orderedMIGProfiles {
		profileInfo, ret := dev.GetGpuInstanceProfileInfo(profileNameToGIProfileID[profile])
		if ret == nvml.ERROR_NOT_SUPPORTED || ret == nvml.ERROR_INVALID_ARGUMENT {
			continue
		}
		if ret != nvml.SUCCESS {
			return fmt.Errorf("get GI profile %s on gpu %d during recovery: %s", profile, gpuIndex, nvml.ErrorString(ret))
		}
		instances, ret := dev.GetGpuInstances(&profileInfo)
		if ret == nvml.ERROR_NOT_SUPPORTED || ret == nvml.ERROR_INVALID_ARGUMENT {
			continue
		}
		if ret != nvml.SUCCESS {
			return fmt.Errorf("list GI profile %s on gpu %d during recovery: %s", profile, gpuIndex, nvml.ErrorString(ret))
		}
		for _, gi := range instances {
			giInfo, ret := gi.GetInfo()
			if ret != nvml.SUCCESS {
				return fmt.Errorf("get GI info on gpu %d during recovery: %s", gpuIndex, nvml.ErrorString(ret))
			}
			ciInfo, found, err := liveComputeInstance(gi)
			if err != nil {
				return fmt.Errorf("recover GI %d on gpu %d: %w", giInfo.Id, gpuIndex, err)
			}
			if !found {
				if !adoptExisting || !allowOrphanCleanup {
					klog.InfoS("preserved unowned or busy MIG GPU instance without compute instance", "gpu", gpuIndex, "gpuInstanceID", giInfo.Id)
					continue
				}
				if ret := gi.Destroy(); ret != nvml.SUCCESS && ret != nvml.ERROR_NOT_FOUND {
					return fmt.Errorf("destroy orphan GI %d on gpu %d during recovery: %s", giInfo.Id, gpuIndex, nvml.ErrorString(ret))
				}
				klog.InfoS("destroyed orphan MIG GPU instance without compute instance", "gpu", gpuIndex, "gpuInstanceID", giInfo.Id)
				continue
			}
			migUUID, err := findMigUUIDForGI(dev, giInfo.Id)
			if err != nil {
				return err
			}

			m.mu.Lock()
			if existingKey, ok := m.byAllocationMigUUID[migUUID]; ok {
				existing := m.byAllocation[existingKey]
				valid := existing != nil && existing.GIID == giInfo.Id && existing.CIID == ciInfo.Id && existing.Placement == giInfo.Placement
				m.mu.Unlock()
				if !valid {
					return fmt.Errorf("recovered MIG identity %s conflicts with active allocation", migUUID)
				}
				continue
			}
			if !adoptExisting {
				m.mu.Unlock()
				klog.InfoS("preserved unowned MIG allocation during recovery", "uuid", migUUID, "gpu", gpuIndex, "profile", profile, "start", giInfo.Placement.Start)
				continue
			}
			key := allocationKey(gpuIndex, profile, giInfo.Placement)
			if existing := m.byAllocation[key]; existing != nil {
				m.mu.Unlock()
				return fmt.Errorf("recovered MIG allocation conflicts at gpu %d profile=%s placement=%+v", gpuIndex, profile, giInfo.Placement)
			}
			m.byAllocation[key] = &migInstance{
				Profile: profile, Placement: giInfo.Placement, GIID: giInfo.Id, CIID: ciInfo.Id,
				MigUUID: migUUID, State: migInstanceIdle, LastUsed: m.now(),
			}
			m.byAllocationMigUUID[migUUID] = key
			m.mu.Unlock()
			klog.InfoS("restored idle MIG allocation from NVML", "uuid", migUUID, "gpu", gpuIndex, "profile", profile, "start", giInfo.Placement.Start)
		}
	}
	return nil
}

func liveComputeInstance(gi nvml.GpuInstance) (nvml.ComputeInstanceInfo, bool, error) {
	var found *nvml.ComputeInstanceInfo
	inspected := false
	for profileID := 0; profileID < nvml.COMPUTE_INSTANCE_PROFILE_COUNT; profileID++ {
		profileInfo, ret := gi.GetComputeInstanceProfileInfo(profileID, nvml.COMPUTE_INSTANCE_ENGINE_PROFILE_SHARED)
		if ret == nvml.ERROR_NOT_SUPPORTED || ret == nvml.ERROR_INVALID_ARGUMENT {
			continue
		}
		if ret != nvml.SUCCESS {
			return nvml.ComputeInstanceInfo{}, false, fmt.Errorf("get CI profile %d: %s", profileID, nvml.ErrorString(ret))
		}
		instances, ret := gi.GetComputeInstances(&profileInfo)
		if ret == nvml.ERROR_NOT_SUPPORTED || ret == nvml.ERROR_INVALID_ARGUMENT {
			continue
		}
		if ret != nvml.SUCCESS {
			return nvml.ComputeInstanceInfo{}, false, fmt.Errorf("list CI profile %d: %s", profileID, nvml.ErrorString(ret))
		}
		inspected = true
		for _, ci := range instances {
			info, ret := ci.GetInfo()
			if ret != nvml.SUCCESS {
				return nvml.ComputeInstanceInfo{}, false, fmt.Errorf("get CI info: %s", nvml.ErrorString(ret))
			}
			if found != nil && found.Id != info.Id {
				return nvml.ComputeInstanceInfo{}, false, fmt.Errorf("multiple compute instances are not supported in one dynamic MIG GI")
			}
			copy := info
			found = &copy
		}
	}
	if found == nil {
		if !inspected {
			return nvml.ComputeInstanceInfo{}, false, fmt.Errorf("no supported compute instance profile could be inspected")
		}
		return nvml.ComputeInstanceInfo{}, false, nil
	}
	return *found, true, nil
}
