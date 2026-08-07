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
	"sort"
	"strings"
	"sync"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"k8s.io/klog/v2"
)

var profileNameToGIProfileID = map[string]int{
	"1g": nvml.GPU_INSTANCE_PROFILE_1_SLICE,
	"2g": nvml.GPU_INSTANCE_PROFILE_2_SLICE,
	"3g": nvml.GPU_INSTANCE_PROFILE_3_SLICE,
	"4g": nvml.GPU_INSTANCE_PROFILE_4_SLICE,
	"6g": nvml.GPU_INSTANCE_PROFILE_6_SLICE,
	"7g": nvml.GPU_INSTANCE_PROFILE_7_SLICE,
	"8g": nvml.GPU_INSTANCE_PROFILE_8_SLICE,
}

var profileNameToCIProfileID = map[string]int{
	"1g": nvml.COMPUTE_INSTANCE_PROFILE_1_SLICE,
	"2g": nvml.COMPUTE_INSTANCE_PROFILE_2_SLICE,
	"3g": nvml.COMPUTE_INSTANCE_PROFILE_3_SLICE,
	"4g": nvml.COMPUTE_INSTANCE_PROFILE_4_SLICE,
	"6g": nvml.COMPUTE_INSTANCE_PROFILE_6_SLICE,
	"7g": nvml.COMPUTE_INSTANCE_PROFILE_7_SLICE,
	"8g": nvml.COMPUTE_INSTANCE_PROFILE_8_SLICE,
}

type migAllocationKey struct {
	GPUIndex int
	Profile  string
	Start    uint32
	Size     uint32
}

// migInstance tracks the nvml-level identity of a MIG GI+CI pair bound to a
// slot. Absent means the slot's GI+CI have been destroyed (e.g. on task end)
// but we remember the profile and placement so we can recreate the instance
// at the same physical slice when the next task claims this slot.
type migInstance struct {
	Profile   string // slice group, e.g. "1g"
	Placement nvml.GpuInstancePlacement
	Present   bool
	GIID      uint32
	CIID      uint32
	MigUUID   string
}

// MigInstanceManager is the single authority over live MIG GI+CI state on a
// node. Keys are the scheduler-reserved profile and physical placement.
type MigInstanceManager struct {
	mu                  sync.Mutex
	gpuLocks            map[int]*sync.Mutex
	byAllocation        map[migAllocationKey]*migInstance
	byAllocationMigUUID map[string]migAllocationKey
}

func NewMigInstanceManager() *MigInstanceManager {
	return &MigInstanceManager{
		gpuLocks:            make(map[int]*sync.Mutex),
		byAllocation:        make(map[migAllocationKey]*migInstance),
		byAllocationMigUUID: make(map[string]migAllocationKey),
	}
}

func (m *MigInstanceManager) gpuLock(gpuIndex int) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	lk, ok := m.gpuLocks[gpuIndex]
	if !ok {
		lk = &sync.Mutex{}
		m.gpuLocks[gpuIndex] = lk
	}
	return lk
}

func profileSliceKey(profile string) string {
	if idx := strings.Index(profile, "."); idx > 0 {
		return profile[:idx]
	}
	return profile
}

// ResetIdleGPUs prepares idle MIG-capable GPUs for on-demand slot creation
// through NVML. Busy GPUs are left untouched; idle GPUs
// have MIG mode enabled and all existing GI/CI instances destroyed.
func (m *MigInstanceManager) ResetIdleGPUs(deviceCount int, inUse map[int]struct{}) ([]int, error) {
	reset := []int{}
	for gpuIndex := 0; gpuIndex < deviceCount; gpuIndex++ {
		if _, busy := inUse[gpuIndex]; busy {
			continue
		}

		lk := m.gpuLock(gpuIndex)
		lk.Lock()
		if err := ensureMigModeEnabled(gpuIndex); err != nil {
			lk.Unlock()
			return reset, err
		}
		dev, err := deviceHandleByIndex(gpuIndex)
		if err != nil {
			lk.Unlock()
			return reset, err
		}
		if err := destroyAllMigInstances(dev); err != nil {
			lk.Unlock()
			return reset, err
		}

		m.mu.Lock()
		for key := range m.byAllocation {
			if key.GPUIndex == gpuIndex {
				delete(m.byAllocation, key)
			}
		}
		for uuid, key := range m.byAllocationMigUUID {
			if key.GPUIndex == gpuIndex {
				delete(m.byAllocationMigUUID, uuid)
			}
		}
		m.mu.Unlock()
		lk.Unlock()
		reset = append(reset, gpuIndex)
	}
	sort.Ints(reset)
	return reset, nil
}

func deviceHandleByIndex(gpuIndex int) (nvml.Device, error) {
	dev, ret := nvml.DeviceGetHandleByIndex(gpuIndex)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("nvml get handle by index %d: %s", gpuIndex, nvml.ErrorString(ret))
	}
	return dev, nil
}

// ensureMigModeEnabled turns on MIG mode via NVML when the card is currently
// in non-MIG mode. No-op when MIG mode is unsupported (non-MIG cards) so the
// caller can invoke it uniformly.
//
// SetMigMode may reset/unbind the device; callers must re-fetch the device
// handle after this returns successfully before further NVML operations.
func ensureMigModeEnabled(gpuIndex int) error {
	dev, err := deviceHandleByIndex(gpuIndex)
	if err != nil {
		return err
	}
	curMode, pendingMode, ret := dev.GetMigMode()
	if ret == nvml.ERROR_NOT_SUPPORTED {
		return nil
	}
	if ret != nvml.SUCCESS {
		return fmt.Errorf("gpu %d get mig mode: %s", gpuIndex, nvml.ErrorString(ret))
	}
	if curMode == nvml.DEVICE_MIG_ENABLE {
		if pendingMode == nvml.DEVICE_MIG_ENABLE {
			return nil
		}
		return fmt.Errorf("gpu %d mig mode disable is pending (current=enable pending=%d)", gpuIndex, pendingMode)
	}
	if pendingMode == nvml.DEVICE_MIG_ENABLE {
		return fmt.Errorf("gpu %d mig mode enable is pending; GPU reset required", gpuIndex)
	}

	activation, ret := dev.SetMigMode(nvml.DEVICE_MIG_ENABLE)
	if ret != nvml.SUCCESS {
		return fmt.Errorf("gpu %d set mig mode: %s", gpuIndex, nvml.ErrorString(ret))
	}
	if activation != nvml.SUCCESS {
		return fmt.Errorf("gpu %d activate mig mode: %s", gpuIndex, nvml.ErrorString(activation))
	}

	dev, err = deviceHandleByIndex(gpuIndex)
	if err != nil {
		return err
	}
	curMode, pendingMode, ret = dev.GetMigMode()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("gpu %d verify mig mode after set: %s", gpuIndex, nvml.ErrorString(ret))
	}
	if curMode == nvml.DEVICE_MIG_ENABLE {
		return nil
	}
	if pendingMode == nvml.DEVICE_MIG_ENABLE {
		return fmt.Errorf("gpu %d mig mode enable is pending after set; GPU reset required", gpuIndex)
	}
	return fmt.Errorf("gpu %d mig mode is not enabled after set (current=%d pending=%d)", gpuIndex, curMode, pendingMode)
}

// destroyPresentMigInstance destroys the tracked GI+CI on hardware. Returns
// nil when the instance is already gone or was destroyed successfully.
func destroyPresentMigInstance(gpuIndex int, inst *migInstance) error {
	if inst == nil || !inst.Present {
		return nil
	}
	dev, err := deviceHandleByIndex(gpuIndex)
	if err != nil {
		return err
	}
	gi, ret := dev.GetGpuInstanceById(int(inst.GIID))
	if ret == nvml.ERROR_NOT_FOUND {
		return nil
	}
	if ret != nvml.SUCCESS {
		return fmt.Errorf("get GI %d on gpu %d: %s", inst.GIID, gpuIndex, nvml.ErrorString(ret))
	}
	if ci, r := gi.GetComputeInstanceById(int(inst.CIID)); r == nvml.SUCCESS {
		if d := ci.Destroy(); d != nvml.SUCCESS {
			return fmt.Errorf("destroy CI %d on gpu %d: %s", inst.CIID, gpuIndex, nvml.ErrorString(d))
		}
	} else if r != nvml.ERROR_NOT_FOUND {
		return fmt.Errorf("get CI %d on gpu %d: %s", inst.CIID, gpuIndex, nvml.ErrorString(r))
	}
	if d := gi.Destroy(); d != nvml.SUCCESS {
		return fmt.Errorf("destroy GI %d on gpu %d: %s", inst.GIID, gpuIndex, nvml.ErrorString(d))
	}
	return nil
}

// destroyAllMigInstances enumerates and destroys every GI+CI on the device.
// Used on template switches when no scheduler-allocated slot is in use.
func destroyAllMigInstances(dev nvml.Device) error {
	for _, giProfileID := range []int{
		nvml.GPU_INSTANCE_PROFILE_1_SLICE,
		nvml.GPU_INSTANCE_PROFILE_2_SLICE,
		nvml.GPU_INSTANCE_PROFILE_3_SLICE,
		nvml.GPU_INSTANCE_PROFILE_4_SLICE,
		nvml.GPU_INSTANCE_PROFILE_6_SLICE,
		nvml.GPU_INSTANCE_PROFILE_7_SLICE,
		nvml.GPU_INSTANCE_PROFILE_8_SLICE,
	} {
		info, ret := dev.GetGpuInstanceProfileInfo(giProfileID)
		if ret != nvml.SUCCESS {
			continue
		}
		gis, ret := dev.GetGpuInstances(&info)
		if ret != nvml.SUCCESS {
			continue
		}
		for _, gi := range gis {
			for ciProfileID := 0; ciProfileID < nvml.COMPUTE_INSTANCE_PROFILE_COUNT; ciProfileID++ {
				ciInfo, r := gi.GetComputeInstanceProfileInfo(ciProfileID, nvml.COMPUTE_INSTANCE_ENGINE_PROFILE_SHARED)
				if r != nvml.SUCCESS {
					continue
				}
				cis, r := gi.GetComputeInstances(&ciInfo)
				if r != nvml.SUCCESS {
					continue
				}
				for _, ci := range cis {
					if d := ci.Destroy(); d != nvml.SUCCESS {
						return fmt.Errorf("destroy compute instance: %s", nvml.ErrorString(d))
					}
				}
			}
			if d := gi.Destroy(); d != nvml.SUCCESS {
				return fmt.Errorf("destroy gpu instance: %s", nvml.ErrorString(d))
			}
		}
	}
	return nil
}

// Release destroys the GI+CI bound to the given MIG UUID.
func (m *MigInstanceManager) Release(migUUID string) error {
	m.mu.Lock()
	key, ok := m.byAllocationMigUUID[migUUID]
	m.mu.Unlock()
	if !ok {
		klog.V(5).InfoS("release: unknown MIG UUID, skipping", "uuid", migUUID)
		return nil
	}
	lk := m.gpuLock(key.GPUIndex)
	lk.Lock()
	defer lk.Unlock()
	m.mu.Lock()
	inst := m.byAllocation[key]
	m.mu.Unlock()
	if inst == nil || !inst.Present {
		return nil
	}
	if err := destroyPresentMigInstance(key.GPUIndex, inst); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.byAllocation, key)
	delete(m.byAllocationMigUUID, migUUID)
	m.mu.Unlock()
	klog.InfoS("released MIG allocation", "uuid", migUUID, "gpu", key.GPUIndex, "profile", key.Profile, "start", key.Start)
	return nil
}
func allocationKey(gpuIndex int, profile string, placement nvml.GpuInstancePlacement) migAllocationKey {
	return migAllocationKey{GPUIndex: gpuIndex, Profile: profile, Start: placement.Start, Size: placement.Size}
}

// EnsureAllocation realizes exactly the scheduler-reserved profile and
// placement. It returns whether this call created the instance, allowing the
// caller to roll back only its own partial allocation. It never retries
// another placement.
func (m *MigInstanceManager) EnsureAllocation(gpuIndex int, profile string, placement nvml.GpuInstancePlacement) (string, bool, error) {
	key := allocationKey(gpuIndex, profile, placement)
	lk := m.gpuLock(gpuIndex)
	lk.Lock()
	defer lk.Unlock()

	m.mu.Lock()
	if inst := m.byAllocation[key]; inst != nil && inst.Present {
		uuid := inst.MigUUID
		m.mu.Unlock()
		return uuid, false, nil
	}
	m.mu.Unlock()

	if err := ensureMigModeEnabled(gpuIndex); err != nil {
		return "", false, err
	}
	profileKey := profileSliceKey(profile)
	giProfileID, ok := profileNameToGIProfileID[profileKey]
	if !ok {
		return "", false, fmt.Errorf("unsupported MIG profile %q", profile)
	}
	ciProfileID, ok := profileNameToCIProfileID[profileKey]
	if !ok {
		return "", false, fmt.Errorf("unsupported MIG compute profile %q", profile)
	}
	dev, err := deviceHandleByIndex(gpuIndex)
	if err != nil {
		return "", false, err
	}
	giInfo, ret := dev.GetGpuInstanceProfileInfo(giProfileID)
	if ret != nvml.SUCCESS {
		return "", false, fmt.Errorf("get GI profile %s: %s", profile, nvml.ErrorString(ret))
	}
	possible, ret := dev.GetGpuInstancePossiblePlacements(&giInfo)
	if ret != nvml.SUCCESS {
		return "", false, fmt.Errorf("get placements for %s: %s", profile, nvml.ErrorString(ret))
	}
	valid := false
	for _, candidate := range possible {
		if candidate == placement {
			valid = true
			break
		}
	}
	if !valid {
		return "", false, fmt.Errorf("scheduler selected invalid placement %+v for profile %s", placement, profile)
	}
	gi, ret := dev.CreateGpuInstanceWithPlacement(&giInfo, &placement)
	if ret != nvml.SUCCESS {
		return "", false, fmt.Errorf("create GI profile=%s placement=%+v: %s", profile, placement, nvml.ErrorString(ret))
	}
	giData, ret := gi.GetInfo()
	if ret != nvml.SUCCESS {
		gi.Destroy()
		return "", false, fmt.Errorf("get GI info: %s", nvml.ErrorString(ret))
	}
	ciInfo, ret := gi.GetComputeInstanceProfileInfo(ciProfileID, nvml.COMPUTE_INSTANCE_ENGINE_PROFILE_SHARED)
	if ret != nvml.SUCCESS {
		gi.Destroy()
		return "", false, fmt.Errorf("get CI profile info: %s", nvml.ErrorString(ret))
	}
	ci, ret := gi.CreateComputeInstance(&ciInfo)
	if ret != nvml.SUCCESS {
		gi.Destroy()
		return "", false, fmt.Errorf("create CI: %s", nvml.ErrorString(ret))
	}
	ciData, ret := ci.GetInfo()
	if ret != nvml.SUCCESS {
		ci.Destroy()
		gi.Destroy()
		return "", false, fmt.Errorf("get CI info: %s", nvml.ErrorString(ret))
	}
	migUUID, err := findMigUUIDForGI(dev, giData.Id)
	if err != nil {
		ci.Destroy()
		gi.Destroy()
		return "", false, err
	}
	inst := &migInstance{Profile: profile, Placement: placement, Present: true, GIID: giData.Id, CIID: ciData.Id, MigUUID: migUUID}
	m.mu.Lock()
	m.byAllocation[key] = inst
	m.byAllocationMigUUID[migUUID] = key
	m.mu.Unlock()
	klog.InfoS("created scheduler-reserved MIG allocation", "uuid", migUUID, "gpu", gpuIndex, "profile", profile, "start", placement.Start, "size", placement.Size, "gpuInstanceID", giData.Id, "computeInstanceID", ciData.Id)
	return migUUID, true, nil
}

func (m *MigInstanceManager) AllocationRuntimeInfo(gpuIndex int, profile string, placement nvml.GpuInstancePlacement) (migAllocationRuntimeInfo, bool) {
	key := allocationKey(gpuIndex, profile, placement)
	m.mu.Lock()
	defer m.mu.Unlock()
	inst := m.byAllocation[key]
	if inst == nil || !inst.Present {
		return migAllocationRuntimeInfo{}, false
	}
	return migAllocationRuntimeInfo{
		MigUUID:   inst.MigUUID,
		Profile:   inst.Profile,
		Placement: inst.Placement,
		GIID:      inst.GIID,
		CIID:      inst.CIID,
	}, true
}

func (m *MigInstanceManager) AdoptAllocation(gpuIndex int, profile, migUUID string, placement nvml.GpuInstancePlacement, gpuInstanceID, computeInstanceID uint32) error {
	lk := m.gpuLock(gpuIndex)
	lk.Lock()
	defer lk.Unlock()
	dev, err := deviceHandleByIndex(gpuIndex)
	if err != nil {
		return err
	}
	giProfileID, ok := profileNameToGIProfileID[profileSliceKey(profile)]
	if !ok {
		return fmt.Errorf("unsupported MIG profile %q", profile)
	}
	profileInfo, ret := dev.GetGpuInstanceProfileInfo(giProfileID)
	if ret != nvml.SUCCESS {
		return fmt.Errorf("get GI profile %s: %s", profile, nvml.ErrorString(ret))
	}
	instances, ret := dev.GetGpuInstances(&profileInfo)
	if ret != nvml.SUCCESS {
		return fmt.Errorf("list GI profile %s: %s", profile, nvml.ErrorString(ret))
	}
	ciProfileID := profileIDToCIProfileID(giProfileID)
	for _, gi := range instances {
		giInfo, r := gi.GetInfo()
		if r != nvml.SUCCESS || giInfo.Placement != placement || giInfo.Id != gpuInstanceID {
			continue
		}
		actualUUID, findErr := findMigUUIDForGI(dev, giInfo.Id)
		if findErr != nil || actualUUID != migUUID {
			continue
		}
		ciInfo, r := gi.GetComputeInstanceProfileInfo(ciProfileID, nvml.COMPUTE_INSTANCE_ENGINE_PROFILE_SHARED)
		if r != nvml.SUCCESS {
			continue
		}
		cis, r := gi.GetComputeInstances(&ciInfo)
		if r != nvml.SUCCESS || len(cis) == 0 {
			continue
		}
		ciData, r := cis[0].GetInfo()
		if r != nvml.SUCCESS || ciData.Id != computeInstanceID {
			continue
		}
		key := allocationKey(gpuIndex, profile, placement)
		m.mu.Lock()
		m.byAllocation[key] = &migInstance{Profile: profile, Placement: placement, Present: true, GIID: giInfo.Id, CIID: ciData.Id, MigUUID: migUUID}
		m.byAllocationMigUUID[migUUID] = key
		m.mu.Unlock()
		return nil
	}
	return fmt.Errorf("annotated MIG allocation %s profile=%s placement=%+v is not live", migUUID, profile, placement)
}

func (m *MigInstanceManager) ReconcileActiveAllocations(active map[migAllocationKey]struct{}) error {
	m.mu.Lock()
	keys := make([]migAllocationKey, 0, len(m.byAllocation))
	for key, inst := range m.byAllocation {
		if inst.Present {
			keys = append(keys, key)
		}
	}
	m.mu.Unlock()
	for _, key := range keys {
		if _, ok := active[key]; ok {
			continue
		}
		lk := m.gpuLock(key.GPUIndex)
		lk.Lock()
		m.mu.Lock()
		inst := m.byAllocation[key]
		m.mu.Unlock()
		if inst != nil && inst.Present {
			oldUUID := inst.MigUUID
			if err := destroyPresentMigInstance(key.GPUIndex, inst); err != nil {
				lk.Unlock()
				return err
			}
			m.mu.Lock()
			delete(m.byAllocation, key)
			delete(m.byAllocationMigUUID, oldUUID)
			m.mu.Unlock()
		}
		lk.Unlock()
	}
	return nil
}

type migAllocationRuntimeInfo struct {
	MigUUID   string
	Profile   string
	Placement nvml.GpuInstancePlacement
	GIID      uint32
	CIID      uint32
}

func findMigUUIDForGI(dev nvml.Device, giID uint32) (string, error) {
	maxCount, ret := dev.GetMaxMigDeviceCount()
	if ret != nvml.SUCCESS {
		return "", fmt.Errorf("get max MIG device count: %s", nvml.ErrorString(ret))
	}
	for i := 0; i < maxCount; i++ {
		migDev, ret := dev.GetMigDeviceHandleByIndex(i)
		if ret != nvml.SUCCESS {
			continue
		}
		gotGI, ret := migDev.GetGpuInstanceId()
		if ret != nvml.SUCCESS {
			continue
		}
		if uint32(gotGI) == giID {
			uuid, ret := migDev.GetUUID()
			if ret != nvml.SUCCESS {
				return "", fmt.Errorf("get MIG UUID: %s", nvml.ErrorString(ret))
			}
			return uuid, nil
		}
	}
	return "", fmt.Errorf("no MIG device found for GI %d", giID)
}

// pickFreePlacement returns a placement for the given GI profile that does
// not overlap with any of the placements already in use on this GPU.
func pickFreePlacement(dev nvml.Device, info *nvml.GpuInstanceProfileInfo, inUse map[uint32]uint32) (nvml.GpuInstancePlacement, error) {
	possible, ret := dev.GetGpuInstancePossiblePlacements(info)
	if ret != nvml.SUCCESS {
		return nvml.GpuInstancePlacement{}, fmt.Errorf("get possible placements: %s", nvml.ErrorString(ret))
	}
	return chooseFreePlacement(possible, inUse, preferHighPlacement(info.SliceCount))
}

func preferHighPlacement(sliceCount uint32) bool {
	return sliceCount == 1 || sliceCount == 3
}

func sortPlacements(possible []nvml.GpuInstancePlacement, preferHigh bool) {
	sort.SliceStable(possible, func(i, j int) bool {
		if preferHigh {
			return possible[i].Start > possible[j].Start
		}
		return possible[i].Start < possible[j].Start
	})
}

func placementCandidates(previous nvml.GpuInstancePlacement, possible []nvml.GpuInstancePlacement) []nvml.GpuInstancePlacement {
	candidates := make([]nvml.GpuInstancePlacement, 0, len(possible)+1)
	if previous.Size != 0 {
		candidates = append(candidates, previous)
	}
	for _, candidate := range possible {
		if previous.Size != 0 && candidate == previous {
			continue
		}
		candidates = append(candidates, candidate)
	}
	return candidates
}

// chooseFreePlacement packs 1g and 3g instances from high addresses while 2g
// instances use low addresses. This matches NVIDIA's A100 balanced placement
// (2g at 0:2, 1g at 2:1 and 3:1, 3g at 4:4) while retaining the canonical
// 1x1g + 3x2g layout.
func chooseFreePlacement(possible []nvml.GpuInstancePlacement, inUse map[uint32]uint32, preferHigh bool) (nvml.GpuInstancePlacement, error) {
	sortPlacements(possible, preferHigh)
	for _, p := range possible {
		if !placementOverlaps(p, inUse) {
			return p, nil
		}
	}
	return nvml.GpuInstancePlacement{}, fmt.Errorf("no free placement for profile")
}

func placementOverlaps(p nvml.GpuInstancePlacement, inUse map[uint32]uint32) bool {
	for start, size := range inUse {
		if p.Start < start+size && start < p.Start+p.Size {
			return true
		}
	}
	return false
}

func profileIDToCIProfileID(giProfileID int) int {
	switch giProfileID {
	case nvml.GPU_INSTANCE_PROFILE_1_SLICE:
		return nvml.COMPUTE_INSTANCE_PROFILE_1_SLICE
	case nvml.GPU_INSTANCE_PROFILE_2_SLICE:
		return nvml.COMPUTE_INSTANCE_PROFILE_2_SLICE
	case nvml.GPU_INSTANCE_PROFILE_3_SLICE:
		return nvml.COMPUTE_INSTANCE_PROFILE_3_SLICE
	case nvml.GPU_INSTANCE_PROFILE_4_SLICE:
		return nvml.COMPUTE_INSTANCE_PROFILE_4_SLICE
	case nvml.GPU_INSTANCE_PROFILE_6_SLICE:
		return nvml.COMPUTE_INSTANCE_PROFILE_6_SLICE
	case nvml.GPU_INSTANCE_PROFILE_7_SLICE:
		return nvml.COMPUTE_INSTANCE_PROFILE_7_SLICE
	case nvml.GPU_INSTANCE_PROFILE_8_SLICE:
		return nvml.COMPUTE_INSTANCE_PROFILE_8_SLICE
	}
	return nvml.COMPUTE_INSTANCE_PROFILE_1_SLICE
}

func giProfileIDToSliceKey(giProfileID int) string {
	switch giProfileID {
	case nvml.GPU_INSTANCE_PROFILE_1_SLICE:
		return "1g"
	case nvml.GPU_INSTANCE_PROFILE_2_SLICE:
		return "2g"
	case nvml.GPU_INSTANCE_PROFILE_3_SLICE:
		return "3g"
	case nvml.GPU_INSTANCE_PROFILE_4_SLICE:
		return "4g"
	case nvml.GPU_INSTANCE_PROFILE_6_SLICE:
		return "6g"
	case nvml.GPU_INSTANCE_PROFILE_7_SLICE:
		return "7g"
	case nvml.GPU_INSTANCE_PROFILE_8_SLICE:
		return "8g"
	}
	return ""
}
