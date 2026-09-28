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
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

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

// nvidiaMIGGettingStartedURL documents Ampere GPU-reset and VM-reboot
// requirements when toggling MIG mode.
const nvidiaMIGGettingStartedURL = "https://docs.nvidia.com/datacenter/tesla/mig-user-guide/latest/getting-started-with-mig.html"

// errMigModeNeedsReset is returned when NVML reports a MIG enable/disable
// that has not taken effect, including SetMigMode activationStatus
// ERROR_RESET_REQUIRED. Match with errors.Is.
var errMigModeNeedsReset = errors.New("MIG mode change requires a GPU reset or VM reboot")

func errMigModePending(gpuIndex int, action string, current, pending int) error {
	return fmt.Errorf("gpu %d MIG %s is pending (current=%d pending=%d): %w. Manual operator action may be needed: try nvidia-smi --gpu-reset, or reboot the VM if the hypervisor does not allow GPU reset. See %s",
		gpuIndex, action, current, pending, errMigModeNeedsReset, nvidiaMIGGettingStartedURL)
}

// errMigModeActivation wraps SetMigMode's activationStatus. ERROR_RESET_REQUIRED
// means the mode change was accepted but needs a GPU reset or VM reboot.
func errMigModeActivation(gpuIndex int, action string, activation nvml.Return) error {
	if activation == nvml.ERROR_RESET_REQUIRED {
		return fmt.Errorf("gpu %d %s mig mode: %s: %w. Manual operator action may be needed: try nvidia-smi --gpu-reset, or reboot the VM if the hypervisor does not allow GPU reset. See %s",
			gpuIndex, action, nvml.ErrorString(activation), errMigModeNeedsReset, nvidiaMIGGettingStartedURL)
	}
	return fmt.Errorf("gpu %d %s mig mode: %s", gpuIndex, action, nvml.ErrorString(activation))
}

type migAllocationKey struct {
	GPUIndex int
	Profile  string
	Start    uint32
	Size     uint32
}

type migInstanceState string

const (
	migInstanceCreating   migInstanceState = "Creating"
	migInstanceActive     migInstanceState = "Active"
	migInstanceIdle       migInstanceState = "Idle"
	migInstanceReclaiming migInstanceState = "Reclaiming"
	migInstanceDeleting   migInstanceState = "Deleting"
	migInstanceError      migInstanceState = "Error"
)

// migInstance tracks the NVML-level identity of a live MIG GI+CI pair bound to
// a scheduler-reserved profile and physical placement.
type migInstance struct {
	Profile   string // slice group, e.g. "1g"
	Placement nvml.GpuInstancePlacement
	GIID      uint32
	CIID      uint32
	MigUUID   string
	State     migInstanceState
	LastUsed  time.Time
}

type migAllocationResult struct {
	MigUUID   string
	Created   bool
	Reused    bool
	Reclaimed []string
}

// MigInstanceManager is the single authority over live MIG GI+CI state on a
// node. Keys are the scheduler-reserved profile and physical placement.
//
// Callers must invoke Init once before using other methods, and Shutdown
// once when done; NVML is not re-initialized per call.
type MigInstanceManager struct {
	// sessionMu protects admission; operations finish before NVML is released.
	sessionMu   sync.Mutex
	operations  sync.WaitGroup
	nvmllib     nvml.Interface
	initialized bool
	closing     chan struct{}

	mu                  sync.Mutex
	gpuLocks            map[int]*sync.Mutex
	byAllocation        map[migAllocationKey]*migInstance
	byAllocationMigUUID map[string]migAllocationKey
	now                 func() time.Time
}

func NewMigInstanceManager() *MigInstanceManager {
	return newMigInstanceManager(nvml.New())
}

func newMigInstanceManager(lib nvml.Interface) *MigInstanceManager {
	return &MigInstanceManager{
		nvmllib:             lib,
		gpuLocks:            make(map[int]*sync.Mutex),
		byAllocation:        make(map[migAllocationKey]*migInstance),
		byAllocationMigUUID: make(map[string]migAllocationKey),
		now:                 time.Now,
	}
}

// Init acquires one NVML session for a plugin start cycle. Repeated calls
// while running are harmless; initialization may be retried after failure.
func (m *MigInstanceManager) Init() error {
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	if m.closing != nil {
		return fmt.Errorf("MIG manager is shutting down")
	}
	if m.initialized {
		return nil
	}
	if m.nvmllib == nil {
		return fmt.Errorf("MIG manager NVML library is not configured")
	}
	if ret := m.nvmllib.Init(); ret != nvml.SUCCESS {
		return fmt.Errorf("nvml Init: %s", nvml.ErrorString(ret))
	}
	m.mu.Lock()
	clear(m.byAllocation)
	clear(m.byAllocationMigUUID)
	m.mu.Unlock()
	m.initialized = true
	klog.V(4).InfoS("MIG manager NVML session initialized")
	return nil
}

// beginOperation admits a complete operation, including error cleanup.
// Internal helpers must not re-enter admission while an operation is active.
func (m *MigInstanceManager) beginOperation() (func(), error) {
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	if !m.initialized || m.closing != nil {
		return nil, fmt.Errorf("MIG manager NVML session is not running")
	}
	m.operations.Add(1)
	return m.operations.Done, nil
}

// Shutdown rejects new work, drains admitted operations, and releases exactly
// the session acquired by Init. Concurrent/repeated calls are safe.
func (m *MigInstanceManager) Shutdown() {
	m.sessionMu.Lock()
	if done := m.closing; done != nil {
		m.sessionMu.Unlock()
		<-done
		return
	}
	if !m.initialized {
		m.sessionMu.Unlock()
		return
	}
	done := make(chan struct{})
	m.closing = done
	m.sessionMu.Unlock()
	m.operations.Wait()
	if ret := m.nvmllib.Shutdown(); ret != nvml.SUCCESS {
		klog.ErrorS(fmt.Errorf("%s", nvml.ErrorString(ret)), "nvml Shutdown failed")
	} else {
		klog.V(4).InfoS("MIG manager NVML session shut down")
	}
	m.sessionMu.Lock()
	m.initialized = false
	m.closing = nil
	close(done)
	m.sessionMu.Unlock()
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

// ResetIdleGPUs prepares idle MIG-capable GPUs for on-demand instance creation
// through NVML. Busy GPUs are left untouched; idle GPUs
// have MIG mode enabled and all existing GI/CI instances destroyed.
func (m *MigInstanceManager) ResetIdleGPUs(deviceCount int, inUse map[int]struct{}) ([]int, error) {
	done, err := m.beginOperation()
	if err != nil {
		return nil, err
	}
	defer done()

	reset := []int{}
	for gpuIndex := 0; gpuIndex < deviceCount; gpuIndex++ {
		if _, busy := inUse[gpuIndex]; busy {
			continue
		}

		lk := m.gpuLock(gpuIndex)
		lk.Lock()
		if err := m.ensureMigModeEnabled(gpuIndex); err != nil {
			lk.Unlock()
			return reset, err
		}
		dev, err := m.deviceHandleByIndex(gpuIndex)
		if err != nil {
			lk.Unlock()
			return reset, err
		}
		if err := destroyAllMigInstances(dev); err != nil {
			lk.Unlock()
			return reset, err
		}

		m.clearAllocationsForGPU(gpuIndex)
		lk.Unlock()
		reset = append(reset, gpuIndex)
	}
	sort.Ints(reset)
	return reset, nil
}

// DisableIdleGPUs turns off MIG on idle GPUs so the node can register as
// hami-core. GPUs with running work are left unchanged; if those GPUs are
// still in MIG, the call fails so the plugin does not advertise hami-core
// while hardware remains partitioned.
func (m *MigInstanceManager) DisableIdleGPUs(deviceCount int, inUse map[int]struct{}) ([]int, error) {
	done, err := m.beginOperation()
	if err != nil {
		return nil, err
	}
	defer done()

	disabled := []int{}
	for gpuIndex := 0; gpuIndex < deviceCount; gpuIndex++ {
		lk := m.gpuLock(gpuIndex)
		lk.Lock()
		if _, busy := inUse[gpuIndex]; busy {
			enabled, err := m.migCurrentlyEnabled(gpuIndex)
			lk.Unlock()
			if err != nil {
				return disabled, err
			}
			if enabled {
				return disabled, fmt.Errorf("gpu %d is in use; cannot disable MIG", gpuIndex)
			}
			continue
		}
		if err := m.ensureMigModeDisabled(gpuIndex); err != nil {
			lk.Unlock()
			return disabled, err
		}
		m.clearAllocationsForGPU(gpuIndex)
		lk.Unlock()
		disabled = append(disabled, gpuIndex)
	}
	sort.Ints(disabled)
	return disabled, nil
}

func (m *MigInstanceManager) clearAllocationsForGPU(gpuIndex int) {
	m.mu.Lock()
	defer m.mu.Unlock()
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
}

func (m *MigInstanceManager) deviceHandleByIndex(gpuIndex int) (nvml.Device, error) {
	dev, ret := m.nvmllib.DeviceGetHandleByIndex(gpuIndex)
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
func (m *MigInstanceManager) ensureMigModeEnabled(gpuIndex int) error {
	dev, err := m.deviceHandleByIndex(gpuIndex)
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
		return errMigModePending(gpuIndex, "disable", curMode, pendingMode)
	}
	if pendingMode == nvml.DEVICE_MIG_ENABLE {
		return errMigModePending(gpuIndex, "enable", curMode, pendingMode)
	}

	activation, ret := dev.SetMigMode(nvml.DEVICE_MIG_ENABLE)
	if ret != nvml.SUCCESS {
		return fmt.Errorf("gpu %d set mig mode: %s", gpuIndex, nvml.ErrorString(ret))
	}
	if activation != nvml.SUCCESS {
		return errMigModeActivation(gpuIndex, "activate", activation)
	}

	dev, err = m.deviceHandleByIndex(gpuIndex)
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
		return errMigModePending(gpuIndex, "enable", curMode, pendingMode)
	}
	return fmt.Errorf("gpu %d mig mode is not enabled after set (current=%d pending=%d)", gpuIndex, curMode, pendingMode)
}

func (m *MigInstanceManager) migCurrentlyEnabled(gpuIndex int) (bool, error) {
	dev, err := m.deviceHandleByIndex(gpuIndex)
	if err != nil {
		return false, err
	}
	curMode, pendingMode, ret := dev.GetMigMode()
	if ret == nvml.ERROR_NOT_SUPPORTED {
		return false, nil
	}
	if ret != nvml.SUCCESS {
		return false, fmt.Errorf("gpu %d get mig mode: %s", gpuIndex, nvml.ErrorString(ret))
	}
	return curMode == nvml.DEVICE_MIG_ENABLE || pendingMode == nvml.DEVICE_MIG_ENABLE, nil
}

// ensureMigModeDisabled turns off MIG mode via NVML after destroying leftover
// GI/CI instances. No-op when MIG is unsupported or already disabled.
//
// SetMigMode may reset/unbind the device; callers must re-fetch the device
// handle after this returns successfully before further NVML operations.
func (m *MigInstanceManager) ensureMigModeDisabled(gpuIndex int) error {
	dev, err := m.deviceHandleByIndex(gpuIndex)
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
	if curMode == nvml.DEVICE_MIG_DISABLE {
		if pendingMode == nvml.DEVICE_MIG_DISABLE {
			return nil
		}
		return errMigModePending(gpuIndex, "enable", curMode, pendingMode)
	}
	if pendingMode == nvml.DEVICE_MIG_DISABLE {
		return errMigModePending(gpuIndex, "disable", curMode, pendingMode)
	}

	if err := destroyAllMigInstances(dev); err != nil {
		return err
	}

	activation, ret := dev.SetMigMode(nvml.DEVICE_MIG_DISABLE)
	if ret != nvml.SUCCESS {
		return fmt.Errorf("gpu %d set mig mode: %s", gpuIndex, nvml.ErrorString(ret))
	}
	if activation != nvml.SUCCESS {
		return errMigModeActivation(gpuIndex, "deactivate", activation)
	}

	dev, err = m.deviceHandleByIndex(gpuIndex)
	if err != nil {
		return err
	}
	curMode, pendingMode, ret = dev.GetMigMode()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("gpu %d verify mig mode after set: %s", gpuIndex, nvml.ErrorString(ret))
	}
	if curMode == nvml.DEVICE_MIG_DISABLE {
		return nil
	}
	if pendingMode == nvml.DEVICE_MIG_DISABLE {
		return errMigModePending(gpuIndex, "disable", curMode, pendingMode)
	}
	return fmt.Errorf("gpu %d mig mode is not disabled after set (current=%d pending=%d)", gpuIndex, curMode, pendingMode)
}

// destroyMigInstance destroys the tracked GI+CI on hardware. Returns
// nil when the instance is already gone or was destroyed successfully.
func (m *MigInstanceManager) destroyMigInstance(gpuIndex int, inst *migInstance) error {
	if inst == nil {
		return nil
	}
	dev, err := m.deviceHandleByIndex(gpuIndex)
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
// It is used to reset idle GPUs before accepting scheduler allocations and to
// clear leftover instances before disabling MIG.
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

func normalizedMIGState(inst *migInstance) migInstanceState {
	if inst == nil {
		return migInstanceError
	}
	if inst.State == "" {
		return migInstanceActive
	}
	return inst.State
}

func placementsOverlap(a, b nvml.GpuInstancePlacement) bool {
	return a.Start < b.Start+b.Size && b.Start < a.Start+a.Size
}

// Release permanently destroys the GI+CI bound to the given MIG UUID. It is
// reserved for failed-allocation rollback and explicit reclamation; normal Pod
// completion is handled by reconciliation, which marks the instance idle.
func (m *MigInstanceManager) Release(migUUID string) error {
	done, err := m.beginOperation()
	if err != nil {
		return err
	}
	defer done()

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
	if inst == nil {
		return nil
	}
	m.mu.Lock()
	inst.State = migInstanceDeleting
	m.mu.Unlock()
	if err := m.destroyMigInstance(key.GPUIndex, inst); err != nil {
		m.mu.Lock()
		inst.State = migInstanceError
		m.mu.Unlock()
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
func (m *MigInstanceManager) EnsureAllocation(gpuIndex int, profile string, placement nvml.GpuInstancePlacement) (migAllocationResult, error) {
	done, err := m.beginOperation()
	if err != nil {
		return migAllocationResult{}, err
	}
	defer done()

	key := allocationKey(gpuIndex, profile, placement)
	lk := m.gpuLock(gpuIndex)
	lk.Lock()
	defer lk.Unlock()

	m.mu.Lock()
	if inst := m.byAllocation[key]; inst != nil && normalizedMIGState(inst) != migInstanceError {
		state := normalizedMIGState(inst)
		if state == migInstanceDeleting || state == migInstanceReclaiming {
			m.mu.Unlock()
			return migAllocationResult{}, fmt.Errorf("MIG allocation %s is in state %s", inst.MigUUID, state)
		}
		inst.State = migInstanceActive
		inst.LastUsed = m.now()
		result := migAllocationResult{MigUUID: inst.MigUUID, Reused: state == migInstanceIdle}
		m.mu.Unlock()
		if state == migInstanceIdle {
			klog.InfoS("reused idle MIG allocation", "uuid", result.MigUUID, "gpu", gpuIndex, "profile", profile, "start", placement.Start)
		}
		return result, nil
	}
	compatibleKey, compatible := m.compatibleIdleAllocationLocked(gpuIndex, profile, placement)
	if compatible {
		inst := m.byAllocation[compatibleKey]
		delete(m.byAllocation, compatibleKey)
		m.byAllocation[key] = inst
		m.byAllocationMigUUID[inst.MigUUID] = key
		inst.Profile = profile
		inst.State = migInstanceActive
		inst.LastUsed = m.now()
		result := migAllocationResult{MigUUID: inst.MigUUID, Reused: true}
		m.mu.Unlock()
		klog.InfoS("reused recovered idle MIG allocation", "uuid", result.MigUUID, "gpu", gpuIndex, "profile", profile, "start", placement.Start)
		return result, nil
	}
	m.mu.Unlock()

	result := migAllocationResult{}
	if err := m.ensureMigModeEnabled(gpuIndex); err != nil {
		return result, err
	}
	profileKey := profileSliceKey(profile)
	giProfileID, ok := profileNameToGIProfileID[profileKey]
	if !ok {
		return result, fmt.Errorf("unsupported MIG profile %q", profile)
	}
	ciProfileID, ok := profileNameToCIProfileID[profileKey]
	if !ok {
		return result, fmt.Errorf("unsupported MIG compute profile %q", profile)
	}
	dev, err := m.deviceHandleByIndex(gpuIndex)
	if err != nil {
		return result, err
	}
	giInfo, ret := dev.GetGpuInstanceProfileInfo(giProfileID)
	if ret != nvml.SUCCESS {
		return result, fmt.Errorf("get GI profile %s: %s", profile, nvml.ErrorString(ret))
	}
	possible, ret := dev.GetGpuInstancePossiblePlacements(&giInfo)
	if ret != nvml.SUCCESS {
		return result, fmt.Errorf("get placements for %s: %s", profile, nvml.ErrorString(ret))
	}
	valid := false
	for _, candidate := range possible {
		if candidate == placement {
			valid = true
			break
		}
	}
	if !valid {
		return result, fmt.Errorf("scheduler selected invalid placement %+v for profile %s", placement, profile)
	}
	reclaimed, err := m.reclaimBlockingIdleLocked(gpuIndex, placement)
	result.Reclaimed = reclaimed
	if err != nil {
		return result, err
	}
	creating := &migInstance{Profile: profile, Placement: placement, State: migInstanceCreating, LastUsed: m.now()}
	gi, ret := dev.CreateGpuInstanceWithPlacement(&giInfo, &placement)
	if ret != nvml.SUCCESS {
		return result, fmt.Errorf("create GI profile=%s placement=%+v: %s", profile, placement, nvml.ErrorString(ret))
	}
	giData, ret := gi.GetInfo()
	if ret != nvml.SUCCESS {
		gi.Destroy()
		return result, fmt.Errorf("get GI info: %s", nvml.ErrorString(ret))
	}
	ciInfo, ret := gi.GetComputeInstanceProfileInfo(ciProfileID, nvml.COMPUTE_INSTANCE_ENGINE_PROFILE_SHARED)
	if ret != nvml.SUCCESS {
		gi.Destroy()
		return result, fmt.Errorf("get CI profile info: %s", nvml.ErrorString(ret))
	}
	ci, ret := gi.CreateComputeInstance(&ciInfo)
	if ret != nvml.SUCCESS {
		gi.Destroy()
		return result, fmt.Errorf("create CI: %s", nvml.ErrorString(ret))
	}
	ciData, ret := ci.GetInfo()
	if ret != nvml.SUCCESS {
		ci.Destroy()
		gi.Destroy()
		return result, fmt.Errorf("get CI info: %s", nvml.ErrorString(ret))
	}
	migUUID, err := findMigUUIDForGI(dev, giData.Id)
	if err != nil {
		ci.Destroy()
		gi.Destroy()
		return result, err
	}
	creating.GIID = giData.Id
	creating.CIID = ciData.Id
	creating.MigUUID = migUUID
	creating.State = migInstanceActive
	m.mu.Lock()
	m.byAllocation[key] = creating
	m.byAllocationMigUUID[migUUID] = key
	m.mu.Unlock()
	result.MigUUID = migUUID
	result.Created = true
	klog.InfoS("created scheduler-reserved MIG allocation", "uuid", migUUID, "gpu", gpuIndex, "profile", profile, "start", placement.Start, "size", placement.Size, "gpuInstanceID", giData.Id, "computeInstanceID", ciData.Id)
	return result, nil
}

// MarkIdle rolls back activation of a cached instance when the surrounding
// Allocate request fails after the instance was reused.
func (m *MigInstanceManager) MarkIdle(migUUID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key, ok := m.byAllocationMigUUID[migUUID]
	if !ok {
		return
	}
	inst := m.byAllocation[key]
	if inst != nil && normalizedMIGState(inst) == migInstanceActive {
		inst.State = migInstanceIdle
		inst.LastUsed = m.now()
	}
}

func (m *MigInstanceManager) compatibleIdleAllocationLocked(gpuIndex int, profile string, placement nvml.GpuInstancePlacement) (migAllocationKey, bool) {
	for key, inst := range m.byAllocation {
		if key.GPUIndex == gpuIndex && key.Start == placement.Start && key.Size == placement.Size &&
			profileSliceKey(key.Profile) == profileSliceKey(profile) && normalizedMIGState(inst) == migInstanceIdle {
			return key, true
		}
	}
	return migAllocationKey{}, false
}

func (m *MigInstanceManager) reclaimBlockingIdleLocked(gpuIndex int, placement nvml.GpuInstancePlacement) ([]string, error) {
	type candidate struct {
		key  migAllocationKey
		inst *migInstance
	}
	m.mu.Lock()
	candidates := make([]candidate, 0)
	for key, inst := range m.byAllocation {
		if key.GPUIndex != gpuIndex || !placementsOverlap(inst.Placement, placement) {
			continue
		}
		state := normalizedMIGState(inst)
		if state != migInstanceIdle && state != migInstanceError {
			m.mu.Unlock()
			return nil, fmt.Errorf("scheduler placement %+v overlaps %s MIG allocation %s", placement, state, inst.MigUUID)
		}
		candidates = append(candidates, candidate{key: key, inst: inst})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if !candidates[i].inst.LastUsed.Equal(candidates[j].inst.LastUsed) {
			return candidates[i].inst.LastUsed.Before(candidates[j].inst.LastUsed)
		}
		return candidates[i].key.Start < candidates[j].key.Start
	})
	m.mu.Unlock()

	reclaimed := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		m.mu.Lock()
		candidate.inst.State = migInstanceReclaiming
		m.mu.Unlock()
		if err := m.destroyMigInstance(gpuIndex, candidate.inst); err != nil {
			m.mu.Lock()
			candidate.inst.State = migInstanceError
			m.mu.Unlock()
			return reclaimed, err
		}
		m.mu.Lock()
		candidate.inst.State = migInstanceDeleting
		delete(m.byAllocation, candidate.key)
		delete(m.byAllocationMigUUID, candidate.inst.MigUUID)
		m.mu.Unlock()
		reclaimed = append(reclaimed, candidate.inst.MigUUID)
		klog.InfoS("reclaimed idle MIG allocation under placement pressure", "uuid", candidate.inst.MigUUID, "gpu", gpuIndex, "profile", candidate.key.Profile, "start", candidate.key.Start)
	}
	return reclaimed, nil
}

func (m *MigInstanceManager) AllocationRuntimeInfo(gpuIndex int, profile string, placement nvml.GpuInstancePlacement) (migAllocationRuntimeInfo, bool) {
	key := allocationKey(gpuIndex, profile, placement)
	m.mu.Lock()
	defer m.mu.Unlock()
	inst := m.byAllocation[key]
	if inst == nil {
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
	done, err := m.beginOperation()
	if err != nil {
		return err
	}
	defer done()

	lk := m.gpuLock(gpuIndex)
	lk.Lock()
	defer lk.Unlock()
	dev, err := m.deviceHandleByIndex(gpuIndex)
	if err != nil {
		return err
	}
	profileKey := profileSliceKey(profile)
	giProfileID, ok := profileNameToGIProfileID[profileKey]
	if !ok {
		return fmt.Errorf("unsupported MIG profile %q", profile)
	}
	ciProfileID, ok := profileNameToCIProfileID[profileKey]
	if !ok {
		return fmt.Errorf("unsupported MIG compute profile %q", profile)
	}
	profileInfo, ret := dev.GetGpuInstanceProfileInfo(giProfileID)
	if ret != nvml.SUCCESS {
		return fmt.Errorf("get GI profile %s: %s", profile, nvml.ErrorString(ret))
	}
	instances, ret := dev.GetGpuInstances(&profileInfo)
	if ret != nvml.SUCCESS {
		return fmt.Errorf("list GI profile %s: %s", profile, nvml.ErrorString(ret))
	}
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
		m.byAllocation[key] = &migInstance{Profile: profile, Placement: placement, GIID: giInfo.Id, CIID: ciData.Id, MigUUID: migUUID, State: migInstanceActive, LastUsed: m.now()}
		m.byAllocationMigUUID[migUUID] = key
		m.mu.Unlock()
		return nil
	}
	return fmt.Errorf("annotated MIG allocation %s profile=%s placement=%+v is not live", migUUID, profile, placement)
}

// ReconcileActiveAllocations preserves released instances as idle. Destruction
// is deferred until an idle instance blocks a new scheduler placement.
func (m *MigInstanceManager) ReconcileActiveAllocations(active map[migAllocationKey]struct{}) error {
	done, err := m.beginOperation()
	if err != nil {
		return err
	}
	defer done()

	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, inst := range m.byAllocation {
		if _, ok := active[key]; ok {
			if normalizedMIGState(inst) == migInstanceIdle {
				inst.State = migInstanceActive
				inst.LastUsed = now
			}
			continue
		}
		if normalizedMIGState(inst) == migInstanceActive {
			inst.State = migInstanceIdle
			inst.LastUsed = now
			klog.InfoS("cached released MIG allocation", "uuid", inst.MigUUID, "gpu", key.GPUIndex, "profile", key.Profile, "start", key.Start)
		}
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

// needsMigDisable checks current and pending hardware modes without changing
// them. A non-MIG startup needs Pod confirmation only if MIG may need disabling.
func (m *MigInstanceManager) needsMigDisable(deviceCount int) (bool, error) {
	done, err := m.beginOperation()
	if err != nil {
		return false, err
	}
	defer done()
	needsDisable := false
	for gpuIndex := 0; gpuIndex < deviceCount; gpuIndex++ {
		lk := m.gpuLock(gpuIndex)
		lk.Lock()
		enabled, err := m.migCurrentlyEnabled(gpuIndex)
		lk.Unlock()
		if err != nil {
			return false, err
		}
		needsDisable = needsDisable || enabled
	}
	return needsDisable, nil
}
