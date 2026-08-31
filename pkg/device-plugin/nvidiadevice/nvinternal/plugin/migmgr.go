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
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/cdi"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/mig"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/rm"
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

func (k migAllocationKey) Placement() nvml.GpuInstancePlacement {
	return nvml.GpuInstancePlacement{Start: k.Start, Size: k.Size}
}

type MigInstanceState string

const (
	StateCreating   MigInstanceState = "Creating"
	StateActive     MigInstanceState = "Active"
	StateIdle       MigInstanceState = "Idle"
	StateReclaiming MigInstanceState = "Reclaiming"
	StateDeleting   MigInstanceState = "Deleting"
	StateError      MigInstanceState = "Error"
)

// migInstance tracks the NVML-level identity of a live MIG GI+CI pair bound to
// a scheduler-reserved profile and physical placement.
type migInstance struct {
	Profile   string // slice group, e.g. "1g"
	Placement nvml.GpuInstancePlacement
	GIID      uint32
	CIID      uint32
	MigUUID   string
	State     MigInstanceState
	LastUsed  time.Time
}

// MigInstanceManager is the single authority over live MIG GI+CI state on a
// node. Keys are the scheduler-reserved profile and physical placement.
//
// Lifecycle: callers must invoke Init once after creation, and Shutdown
// once when done; NVML is not re-initialized per call.
type MigInstanceManager struct {
	mu                  sync.Mutex
	nvmllib             nvml.Interface
	cdiHandler          cdi.Interface
	gpuLocks            map[int]*sync.Mutex
	byAllocation        map[migAllocationKey]*migInstance
	byAllocationMigUUID map[string]migAllocationKey
}

func NewMigInstanceManager(nvmllib ...nvml.Interface) *MigInstanceManager {
	var lib nvml.Interface
	if len(nvmllib) > 0 {
		lib = nvmllib[0]
	}
	return &MigInstanceManager{
		nvmllib:             lib,
		gpuLocks:            make(map[int]*sync.Mutex),
		byAllocation:        make(map[migAllocationKey]*migInstance),
		byAllocationMigUUID: make(map[string]migAllocationKey),
	}
}

func (m *MigInstanceManager) SetCDIHandler(handler cdi.Interface) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cdiHandler = handler
}

func (m *MigInstanceManager) getCDIHandler() cdi.Interface {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cdiHandler
}

// Init initializes NVML for this manager using the centralized NVMLSession.
func (m *MigInstanceManager) Init() error {
	session := rm.GetNVMLSession(m.nvmllib)
	if err := session.Init(); err != nil {
		return fmt.Errorf("mig manager nvml init: %w", err)
	}
	return nil
}

// Shutdown releases the NVML session acquired by Init.
func (m *MigInstanceManager) Shutdown() {
	session := rm.GetNVMLSession(m.nvmllib)
	session.Shutdown()
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

func placementsOverlap(p1, p2 nvml.GpuInstancePlacement) bool {
	end1 := p1.Start + p1.Size
	end2 := p2.Start + p2.Size
	return p1.Start < end2 && p2.Start < end1
}

func (m *MigInstanceManager) destroyMigInstance(gpuIndex int, inst *migInstance) error {
	if inst == nil {
		return nil
	}
	if m.nvmllib != nil {
		dev, ret := m.nvmllib.DeviceGetHandleByIndex(gpuIndex)
		if ret == nvml.ERROR_NOT_FOUND || ret != nvml.SUCCESS {
			return nil
		}
		gi, ret := dev.GetGpuInstanceById(int(inst.GIID))
		if ret == nvml.ERROR_NOT_FOUND || ret != nvml.SUCCESS {
			return nil
		}
		if ci, r := gi.GetComputeInstanceById(int(inst.CIID)); r == nvml.SUCCESS {
			_ = ci.Destroy()
		}
		_ = gi.Destroy()
		return nil
	}
	return destroyMigInstance(gpuIndex, inst)
}

// destroyAndRemoveInstanceLocked destroys the tracked GI+CI and removes its CDI spec.
func (m *MigInstanceManager) destroyAndRemoveInstanceLocked(gpuIndex int, key migAllocationKey, inst *migInstance) error {
	if inst == nil {
		return nil
	}
	m.mu.Lock()
	inst.State = StateDeleting
	m.mu.Unlock()

	if err := m.destroyMigInstance(gpuIndex, inst); err != nil {
		m.mu.Lock()
		inst.State = StateError
		m.mu.Unlock()
		return err
	}
	if cdiH := m.getCDIHandler(); cdiH != nil {
		if err := cdiH.DeleteMigSpecFile(inst.MigUUID); err != nil {
			klog.ErrorS(err, "failed to delete CDI spec file on instance destruction", "uuid", inst.MigUUID)
		}
	}
	m.mu.Lock()
	delete(m.byAllocation, key)
	delete(m.byAllocationMigUUID, inst.MigUUID)
	m.mu.Unlock()
	return nil
}

// ResetIdleGPUs prepares idle MIG-capable GPUs for on-demand instance creation
// through NVML. Busy GPUs are left untouched; idle GPUs
// have MIG mode enabled and all existing GI/CI instances destroyed.
func (m *MigInstanceManager) ResetIdleGPUs(deviceCount int, inUse map[int]struct{}) ([]int, error) {
	reset := []int{}
	for gpuIndex := 0; gpuIndex < deviceCount; gpuIndex++ {
		if _, busy := inUse[gpuIndex]; busy {
			klog.V(4).InfoS("skipping in-use GPU during MIG reset", "gpu", gpuIndex)
			continue
		}
		lk := m.gpuLock(gpuIndex)
		lk.Lock()
		dev, err := deviceHandleByIndex(gpuIndex)
		if err != nil {
			lk.Unlock()
			return nil, fmt.Errorf("lookup device %d for reset: %w", gpuIndex, err)
		}
		migMode, _, ret := dev.GetMigMode()
		if ret != nvml.SUCCESS {
			lk.Unlock()
			return nil, fmt.Errorf("query MIG mode for device %d: %s", gpuIndex, nvml.ErrorString(ret))
		}
		if migMode != nvml.DEVICE_MIG_ENABLE {
			if err := ensureMigModeEnabled(gpuIndex); err != nil {
				lk.Unlock()
				return nil, fmt.Errorf("enable MIG on idle GPU %d: %w", gpuIndex, err)
			}
			dev, err = deviceHandleByIndex(gpuIndex)
			if err != nil {
				lk.Unlock()
				return nil, fmt.Errorf("re-fetch device %d after enabling MIG: %w", gpuIndex, err)
			}
		}
		if err := destroyAllMigInstances(dev); err != nil {
			lk.Unlock()
			return nil, fmt.Errorf("clear existing MIG instances on GPU %d: %w", gpuIndex, err)
		}
		lk.Unlock()
		reset = append(reset, gpuIndex)
	}
	return reset, nil
}

func deviceHandleByIndex(gpuIndex int) (nvml.Device, error) {
	dev, ret := nvml.DeviceGetHandleByIndex(gpuIndex)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("DeviceGetHandleByIndex(%d): %s", gpuIndex, nvml.ErrorString(ret))
	}
	return dev, nil
}

func ensureMigModeEnabled(gpuIndex int) error {
	dev, err := deviceHandleByIndex(gpuIndex)
	if err != nil {
		return err
	}
	currentMode, pendingMode, ret := dev.GetMigMode()
	if ret == nvml.ERROR_NOT_SUPPORTED {
		return nil
	}
	if ret != nvml.SUCCESS {
		return fmt.Errorf("get mig mode: %s", nvml.ErrorString(ret))
	}
	if currentMode == nvml.DEVICE_MIG_ENABLE {
		return nil
	}
	if pendingMode == nvml.DEVICE_MIG_ENABLE {
		return nil
	}
	ret = dev.SetMigMode(nvml.DEVICE_MIG_ENABLE)
	if ret != nvml.SUCCESS {
		return fmt.Errorf("set mig mode enable on gpu %d: %s", gpuIndex, nvml.ErrorString(ret))
	}
	dev, err = deviceHandleByIndex(gpuIndex)
	if err != nil {
		return fmt.Errorf("reacquire device handle after enabling mig mode on gpu %d: %w", gpuIndex, err)
	}
	curMode, pendingMode, ret := dev.GetMigMode()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("recheck mig mode: %s", nvml.ErrorString(ret))
	}
	if curMode == nvml.DEVICE_MIG_ENABLE || pendingMode == nvml.DEVICE_MIG_ENABLE {
		return nil
	}
	return fmt.Errorf("gpu %d mig mode is not enabled after set (current=%d pending=%d)", gpuIndex, curMode, pendingMode)
}

func destroyMigInstance(gpuIndex int, inst *migInstance) error {
	if inst == nil {
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
		return fmt.Errorf("get gpu instance %d on gpu %d: %s", inst.GIID, gpuIndex, nvml.ErrorString(ret))
	}
	ci, ret := gi.GetComputeInstanceById(int(inst.CIID))
	if ret == nvml.SUCCESS {
		if ret := ci.Destroy(); ret != nvml.SUCCESS && ret != nvml.ERROR_NOT_FOUND {
			return fmt.Errorf("destroy compute instance %d: %s", inst.CIID, nvml.ErrorString(ret))
		}
	} else if ret != nvml.ERROR_NOT_FOUND {
		return fmt.Errorf("get compute instance %d: %s", inst.CIID, nvml.ErrorString(ret))
	}
	if ret := gi.Destroy(); ret != nvml.SUCCESS && ret != nvml.ERROR_NOT_FOUND {
		return fmt.Errorf("destroy gpu instance %d: %s", inst.GIID, nvml.ErrorString(ret))
	}
	return nil
}

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
		gis, ret := dev.GetGpuInstances(&nvml.GpuInstanceProfileInfo{Id: giProfileID})
		if ret != nvml.SUCCESS {
			continue
		}
		for _, gi := range gis {
			for _, ciProfileID := range []int{
				nvml.COMPUTE_INSTANCE_PROFILE_1_SLICE,
				nvml.COMPUTE_INSTANCE_PROFILE_2_SLICE,
				nvml.COMPUTE_INSTANCE_PROFILE_3_SLICE,
				nvml.COMPUTE_INSTANCE_PROFILE_4_SLICE,
				nvml.COMPUTE_INSTANCE_PROFILE_6_SLICE,
				nvml.COMPUTE_INSTANCE_PROFILE_7_SLICE,
				nvml.COMPUTE_INSTANCE_PROFILE_8_SLICE,
			} {
				cis, ret := gi.GetComputeInstances(&nvml.ComputeInstanceProfileInfo{Id: ciProfileID})
				if ret != nvml.SUCCESS {
					continue
				}
				for _, ci := range cis {
					_ = ci.Destroy()
				}
			}
			_ = gi.Destroy()
		}
	}
	return nil
}

// Release marks the GI+CI bound to the given MIG UUID as Idle for lazy reclamation.
func (m *MigInstanceManager) Release(migUUID string) error {
	m.mu.Lock()
	key, ok := m.byAllocationMigUUID[migUUID]
	if !ok {
		m.mu.Unlock()
		klog.V(5).InfoS("release: unknown MIG UUID, skipping", "uuid", migUUID)
		return nil
	}
	inst := m.byAllocation[key]
	m.mu.Unlock()
	if inst == nil {
		return nil
	}
	lk := m.gpuLock(key.GPUIndex)
	lk.Lock()
	defer lk.Unlock()

	m.mu.Lock()
	inst = m.byAllocation[key]
	if inst != nil && inst.State == StateActive {
		inst.State = StateIdle
		inst.LastUsed = time.Now()
		klog.InfoS("lazy release: marked MIG allocation as Idle", "uuid", migUUID, "gpu", key.GPUIndex, "profile", key.Profile, "start", key.Start)
	}
	m.mu.Unlock()
	return nil
}

func allocationKey(gpuIndex int, profile string, placement nvml.GpuInstancePlacement) migAllocationKey {
	return migAllocationKey{GPUIndex: gpuIndex, Profile: profile, Start: placement.Start, Size: placement.Size}
}

// EnsureAllocation realizes exactly the scheduler-reserved profile and
// placement. Reuses existing Idle instances matching the key instantly.
func (m *MigInstanceManager) EnsureAllocation(gpuIndex int, profile string, placement nvml.GpuInstancePlacement) (string, bool, error) {
	key := allocationKey(gpuIndex, profile, placement)
	lk := m.gpuLock(gpuIndex)
	lk.Lock()
	defer lk.Unlock()

	m.mu.Lock()
	if inst := m.byAllocation[key]; inst != nil {
		if inst.State == StateIdle || inst.State == StateActive {
			inst.State = StateActive
			inst.LastUsed = time.Now()
			uuid := inst.MigUUID
			m.mu.Unlock()
			klog.InfoS("reused existing idle MIG allocation", "uuid", uuid, "gpu", gpuIndex, "profile", profile, "start", placement.Start)
			return uuid, false, nil
		}
	}
	m.mu.Unlock()

	// Check and evict conflicting Idle instances on the same GPU
	m.mu.Lock()
	var conflictingKeys []migAllocationKey
	for k, inst := range m.byAllocation {
		if k.GPUIndex == gpuIndex && inst.State == StateIdle && placementsOverlap(k.Placement(), placement) {
			conflictingKeys = append(conflictingKeys, k)
		}
	}
	m.mu.Unlock()

	for _, cKey := range conflictingKeys {
		m.mu.Lock()
		cInst := m.byAllocation[cKey]
		m.mu.Unlock()
		if cInst != nil && cInst.State == StateIdle {
			klog.InfoS("evicting conflicting idle MIG instance", "uuid", cInst.MigUUID, "gpu", gpuIndex)
			if err := m.destroyAndRemoveInstanceLocked(gpuIndex, cKey, cInst); err != nil {
				return "", false, fmt.Errorf("evict conflicting idle MIG instance %s: %w", cInst.MigUUID, err)
			}
		}
	}

	if err := ensureMigModeEnabled(gpuIndex); err != nil {
		return "", false, err
	}
	dev, err := deviceHandleByIndex(gpuIndex)
	if err != nil {
		return "", false, err
	}
	sliceKey := profileSliceKey(profile)
	giProfileID, ok := profileNameToGIProfileID[sliceKey]
	if !ok {
		return "", false, fmt.Errorf("unsupported MIG profile %q", profile)
	}
	giProfileInfo, ret := dev.GetGpuInstanceProfileInfo(giProfileID)
	if ret != nvml.SUCCESS {
		return "", false, fmt.Errorf("get GI profile info %d: %s", giProfileID, nvml.ErrorString(ret))
	}
	gi, ret := dev.CreateGpuInstanceWithPlacement(&giProfileInfo, &placement)
	if ret != nvml.SUCCESS {
		return "", false, fmt.Errorf("create GI with placement %+v: %s", placement, nvml.ErrorString(ret))
	}
	giData, ret := gi.GetInfo()
	if ret != nvml.SUCCESS {
		_ = gi.Destroy()
		return "", false, fmt.Errorf("get GI info: %s", nvml.ErrorString(ret))
	}
	ciProfileID, ok := profileNameToCIProfileID[sliceKey]
	if !ok {
		_ = gi.Destroy()
		return "", false, fmt.Errorf("unsupported MIG compute profile %q", profile)
	}
	ciProfileInfo, ret := gi.GetComputeInstanceProfileInfo(ciProfileID, 0)
	if ret != nvml.SUCCESS {
		_ = gi.Destroy()
		return "", false, fmt.Errorf("get CI profile info: %s", nvml.ErrorString(ret))
	}
	ci, ret := gi.CreateComputeInstance(&ciProfileInfo)
	if ret != nvml.SUCCESS {
		_ = gi.Destroy()
		return "", false, fmt.Errorf("create CI: %s", nvml.ErrorString(ret))
	}
	ciData, ret := ci.GetInfo()
	if ret != nvml.SUCCESS {
		_ = ci.Destroy()
		_ = gi.Destroy()
		return "", false, fmt.Errorf("get CI info: %s", nvml.ErrorString(ret))
	}
	migUUID, err := getMigDeviceUUIDFromGI(gi)
	if err != nil {
		_ = ci.Destroy()
		_ = gi.Destroy()
		return "", false, err
	}
	inst := &migInstance{
		Profile:   profile,
		Placement: placement,
		GIID:      giData.Id,
		CIID:      ciData.Id,
		MigUUID:   migUUID,
		State:     StateActive,
		LastUsed:  time.Now(),
	}

	// Generate dynamic CDI spec file with robust rollback on failure
	if cdiH := m.getCDIHandler(); cdiH != nil {
		caps, err := mig.GetMigCapabilityDevicePaths()
		if err != nil {
			_ = ci.Destroy()
			_ = gi.Destroy()
			return "", false, fmt.Errorf("failed to get MIG capability paths for %s: %w", migUUID, err)
		}
		devicePath := fmt.Sprintf("/dev/nvidia%d", gpuIndex)
		if err := cdiH.CreateMigSpecFile(migUUID, devicePath, caps); err != nil {
			_ = ci.Destroy()
			_ = gi.Destroy()
			return "", false, fmt.Errorf("failed to create dynamic CDI spec file for MIG allocation %s: %w", migUUID, err)
		}
	}

	m.mu.Lock()
	m.byAllocation[key] = inst
	m.byAllocationMigUUID[migUUID] = key
	m.mu.Unlock()

	klog.InfoS("created scheduler-reserved MIG allocation", "uuid", migUUID, "gpu", gpuIndex, "profile", profile, "start", placement.Start, "size", placement.Size, "gpuInstanceID", giData.Id, "computeInstanceID", ciData.Id)
	return migUUID, true, nil
}

func getMigDeviceUUIDFromGI(gi nvml.GpuInstance) (string, error) {
	for _, ciProfileID := range []int{
		nvml.COMPUTE_INSTANCE_PROFILE_1_SLICE,
		nvml.COMPUTE_INSTANCE_PROFILE_2_SLICE,
		nvml.COMPUTE_INSTANCE_PROFILE_3_SLICE,
		nvml.COMPUTE_INSTANCE_PROFILE_4_SLICE,
		nvml.COMPUTE_INSTANCE_PROFILE_6_SLICE,
		nvml.COMPUTE_INSTANCE_PROFILE_7_SLICE,
		nvml.COMPUTE_INSTANCE_PROFILE_8_SLICE,
	} {
		cis, ret := gi.GetComputeInstances(&nvml.ComputeInstanceProfileInfo{Id: ciProfileID})
		if ret != nvml.SUCCESS {
			continue
		}
		for _, ci := range cis {
			migDev, ret := ci.GetMigDeviceHandle()
			if ret != nvml.SUCCESS {
				continue
			}
			migUUID, ret := migDev.GetUUID()
			if ret == nvml.SUCCESS && migUUID != "" {
				return migUUID, nil
			}
		}
	}
	return "", fmt.Errorf("unable to resolve MIG device UUID for GI from NVML")
}

// AdoptAllocation associates a pre-existing live MIG instance found via
// annotation or NVML query with this manager's tracking map.
func (m *MigInstanceManager) AdoptAllocation(gpuIndex int, profile, migUUID string, placement nvml.GpuInstancePlacement) error {
	lk := m.gpuLock(gpuIndex)
	lk.Lock()
	defer lk.Unlock()

	dev, err := deviceHandleByIndex(gpuIndex)
	if err != nil {
		return err
	}
	sliceKey := profileSliceKey(profile)
	giProfileID, ok := profileNameToGIProfileID[sliceKey]
	if !ok {
		return fmt.Errorf("unsupported profile %q", profile)
	}
	gis, ret := dev.GetGpuInstances(&nvml.GpuInstanceProfileInfo{Id: giProfileID})
	if ret != nvml.SUCCESS {
		return fmt.Errorf("GetGpuInstances on GPU %d: %s", gpuIndex, nvml.ErrorString(ret))
	}
	for _, gi := range gis {
		giInfo, ret := gi.GetInfo()
		if ret != nvml.SUCCESS {
			continue
		}
		if giInfo.Placement.Start != placement.Start || giInfo.Placement.Size != placement.Size {
			continue
		}
		liveUUID, err := getMigDeviceUUIDFromGI(gi)
		if err != nil || liveUUID != migUUID {
			continue
		}
		ciProfileID := profileNameToCIProfileID[sliceKey]
		cis, ret := gi.GetComputeInstances(&nvml.ComputeInstanceProfileInfo{Id: ciProfileID})
		if ret != nvml.SUCCESS || len(cis) == 0 {
			continue
		}
		ciData, ret := cis[0].GetInfo()
		if ret != nvml.SUCCESS {
			continue
		}

		// Generate dynamic CDI spec file for adopted allocation
		if cdiH := m.getCDIHandler(); cdiH != nil {
			caps, err := mig.GetMigCapabilityDevicePaths()
			if err != nil {
				return fmt.Errorf("failed to get MIG capability paths during adoption for %s: %w", migUUID, err)
			}
			devicePath := fmt.Sprintf("/dev/nvidia%d", gpuIndex)
			if err := cdiH.CreateMigSpecFile(migUUID, devicePath, caps); err != nil {
				return fmt.Errorf("failed to create CDI spec file during adoption for %s: %w", migUUID, err)
			}
		}

		key := allocationKey(gpuIndex, profile, placement)
		m.mu.Lock()
		m.byAllocation[key] = &migInstance{
			Profile:   profile,
			Placement: placement,
			GIID:      giInfo.Id,
			CIID:      ciData.Id,
			MigUUID:   migUUID,
			State:     StateActive,
			LastUsed:  time.Now(),
		}
		m.byAllocationMigUUID[migUUID] = key
		m.mu.Unlock()
		return nil
	}
	return fmt.Errorf("annotated MIG allocation %s profile=%s placement=%+v is not live", migUUID, profile, placement)
}

// ReclaimExpiredIdleInstances destroys Idle instances that have exceeded the TTL.
func (m *MigInstanceManager) ReclaimExpiredIdleInstances(ttl time.Duration) error {
	m.mu.Lock()
	var expiredKeys []migAllocationKey
	now := time.Now()
	for key, inst := range m.byAllocation {
		if inst.State == StateIdle && now.Sub(inst.LastUsed) >= ttl {
			expiredKeys = append(expiredKeys, key)
		}
	}
	m.mu.Unlock()

	for _, key := range expiredKeys {
		lk := m.gpuLock(key.GPUIndex)
		lk.Lock()
		m.mu.Lock()
		inst := m.byAllocation[key]
		m.mu.Unlock()
		if inst != nil && inst.State == StateIdle && now.Sub(inst.LastUsed) >= ttl {
			klog.InfoS("reclaiming expired idle MIG instance", "uuid", inst.MigUUID, "idleDuration", now.Sub(inst.LastUsed))
			if err := m.destroyAndRemoveInstanceLocked(key.GPUIndex, key, inst); err != nil {
				lk.Unlock()
				return err
			}
		}
		lk.Unlock()
	}
	return nil
}

func (m *MigInstanceManager) ReconcileActiveAllocations(active map[migAllocationKey]struct{}) error {
	m.mu.Lock()
	keys := make([]migAllocationKey, 0, len(m.byAllocation))
	for k := range m.byAllocation {
		keys = append(keys, k)
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
		if inst != nil && inst.State == StateActive {
			inst.State = StateIdle
			inst.LastUsed = time.Now()
			klog.InfoS("reconcile: marked active allocation as Idle", "uuid", inst.MigUUID, "gpu", key.GPUIndex)
		}
		m.mu.Unlock()
		lk.Unlock()
	}
	return nil
}

func (m *MigInstanceManager) ActiveAllocations() []nvidia.MigAllocation {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []nvidia.MigAllocation
	for key, inst := range m.byAllocation {
		out = append(out, nvidia.MigAllocation{
			GPUIndex:  key.GPUIndex,
			Profile:   key.Profile,
			Placement: nvidia.MigPlacement{Start: key.Start, Size: key.Size},
			UUID:      inst.MigUUID,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].GPUIndex != out[j].GPUIndex {
			return out[i].GPUIndex < out[j].GPUIndex
		}
		return out[i].Placement.Start < out[j].Placement.Start
	})
	return out
}
