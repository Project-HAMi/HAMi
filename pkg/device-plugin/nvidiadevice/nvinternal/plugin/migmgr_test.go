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
	"sync"
	"testing"
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/NVIDIA/go-nvml/pkg/nvml/mock"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/cdi"
)

func newMockNVML() nvml.Interface {
	return &mock.Interface{
		DeviceGetHandleByIndexFunc: func(i int) (nvml.Device, nvml.Return) {
			return &mock.Device{
				GetGpuInstanceByIdFunc: func(id int) (nvml.GpuInstance, nvml.Return) {
					return &mock.GpuInstance{
						GetComputeInstanceByIdFunc: func(cid int) (nvml.ComputeInstance, nvml.Return) {
							return &mock.ComputeInstance{
								DestroyFunc: func() nvml.Return { return nvml.SUCCESS },
							}, nvml.SUCCESS
						},
						DestroyFunc: func() nvml.Return { return nvml.SUCCESS },
					}, nvml.SUCCESS
				},
			}, nvml.SUCCESS
		},
	}
}

func TestMigInstanceManagerLazyReclamation(t *testing.T) {
	mgr := NewMigInstanceManager(newMockNVML())
	cdiMock := &cdi.InterfaceMock{
		CreateMigSpecFileFunc: func(migUUID string, devicePath string, caps map[string]string) error {
			return nil
		},
		DeleteMigSpecFileFunc: func(migUUID string) error {
			return nil
		},
	}
	mgr.SetCDIHandler(cdiMock)

	gpuIndex := 0
	profile := "1g.10gb"
	placement := nvml.GpuInstancePlacement{Start: 0, Size: 1}
	key := allocationKey(gpuIndex, profile, placement)

	// Manually seed an active instance
	inst := &migInstance{
		Profile:   profile,
		Placement: placement,
		GIID:      1,
		CIID:      0,
		MigUUID:   "MIG-TEST-1234",
		State:     StateActive,
		LastUsed:  time.Now(),
	}

	mgr.mu.Lock()
	mgr.byAllocation[key] = inst
	mgr.byAllocationMigUUID["MIG-TEST-1234"] = key
	mgr.mu.Unlock()

	// 1. Release should transition state to Idle instead of deleting it
	err := mgr.Release("MIG-TEST-1234")
	if err != nil {
		t.Fatalf("Release failed: %v", err)
	}

	mgr.mu.Lock()
	savedInst := mgr.byAllocation[key]
	mgr.mu.Unlock()

	if savedInst == nil {
		t.Fatalf("expected instance to remain in byAllocation, but was nil")
	}
	if savedInst.State != StateIdle {
		t.Errorf("expected StateIdle after Release, got %s", savedInst.State)
	}

	// 2. EnsureAllocation with matching profile & placement should reuse the Idle instance
	reusedUUID, created, err := mgr.EnsureAllocation(gpuIndex, profile, placement)
	if err != nil {
		t.Fatalf("EnsureAllocation failed: %v", err)
	}
	if created {
		t.Errorf("expected created=false for reused Idle instance")
	}
	if reusedUUID != "MIG-TEST-1234" {
		t.Errorf("expected reused UUID 'MIG-TEST-1234', got %s", reusedUUID)
	}

	mgr.mu.Lock()
	reusedInst := mgr.byAllocation[key]
	mgr.mu.Unlock()

	if reusedInst.State != StateActive {
		t.Errorf("expected StateActive after reuse, got %s", reusedInst.State)
	}
}

func TestMigInstanceManagerExpiredTTLReclamation(t *testing.T) {
	mgr := NewMigInstanceManager(newMockNVML())
	deleteCalled := false
	cdiMock := &cdi.InterfaceMock{
		DeleteMigSpecFileFunc: func(migUUID string) error {
			deleteCalled = true
			return nil
		},
	}
	mgr.SetCDIHandler(cdiMock)

	gpuIndex := 0
	profile := "1g.10gb"
	placement := nvml.GpuInstancePlacement{Start: 0, Size: 1}
	key := allocationKey(gpuIndex, profile, placement)

	// Seed an idle instance with an old LastUsed timestamp
	inst := &migInstance{
		Profile:   profile,
		Placement: placement,
		GIID:      1,
		CIID:      0,
		MigUUID:   "MIG-EXPIRED-5678",
		State:     StateIdle,
		LastUsed:  time.Now().Add(-10 * time.Minute),
	}

	mgr.mu.Lock()
	mgr.byAllocation[key] = inst
	mgr.byAllocationMigUUID["MIG-EXPIRED-5678"] = key
	mgr.mu.Unlock()

	// Reclaim expired instances with 5 minute TTL
	err := mgr.ReclaimExpiredIdleInstances(5 * time.Minute)
	if err != nil {
		t.Fatalf("ReclaimExpiredIdleInstances failed: %v", err)
	}

	mgr.mu.Lock()
	reclaimedInst := mgr.byAllocation[key]
	mgr.mu.Unlock()

	if reclaimedInst != nil {
		t.Errorf("expected expired instance to be reclaimed, but still exists")
	}
	if !deleteCalled {
		t.Errorf("expected DeleteMigSpecFile to be called for expired instance")
	}
}

func TestMigInstanceManagerConflictingIdleEviction(t *testing.T) {
	mgr := NewMigInstanceManager(newMockNVML())
	deletedUUIDs := make(map[string]bool)
	cdiMock := &cdi.InterfaceMock{
		DeleteMigSpecFileFunc: func(migUUID string) error {
			deletedUUIDs[migUUID] = true
			return nil
		},
		CreateMigSpecFileFunc: func(migUUID string, devicePath string, caps map[string]string) error {
			return nil
		},
	}
	mgr.SetCDIHandler(cdiMock)

	gpuIndex := 0
	// Seed an idle 1g instance at Start=0, Size=1
	p1 := nvml.GpuInstancePlacement{Start: 0, Size: 1}
	k1 := allocationKey(gpuIndex, "1g.10gb", p1)
	inst1 := &migInstance{
		Profile:   "1g.10gb",
		Placement: p1,
		GIID:      1,
		CIID:      0,
		MigUUID:   "MIG-IDLE-CONFLICT-1",
		State:     StateIdle,
		LastUsed:  time.Now(),
	}

	mgr.mu.Lock()
	mgr.byAllocation[k1] = inst1
	mgr.byAllocationMigUUID["MIG-IDLE-CONFLICT-1"] = k1
	mgr.mu.Unlock()

	// Requesting an overlapping 2g placement at Start=0, Size=2 should trigger eviction
	// of the conflicting idle instance
	p2 := nvml.GpuInstancePlacement{Start: 0, Size: 2}
	k2 := allocationKey(gpuIndex, "2g.20gb", p2)

	// Manually invoke eviction logic check
	mgr.mu.Lock()
	var conflictingKeys []migAllocationKey
	for k, inst := range mgr.byAllocation {
		if k.GPUIndex == gpuIndex && inst.State == StateIdle && placementsOverlap(k.Placement(), p2) {
			conflictingKeys = append(conflictingKeys, k)
		}
	}
	mgr.mu.Unlock()

	for _, cKey := range conflictingKeys {
		mgr.mu.Lock()
		cInst := mgr.byAllocation[cKey]
		mgr.mu.Unlock()
		if cInst != nil && cInst.State == StateIdle {
			_ = mgr.destroyAndRemoveInstanceLocked(gpuIndex, cKey, cInst)
		}
	}

	mgr.mu.Lock()
	remaining := mgr.byAllocation[k1]
	mgr.mu.Unlock()

	if remaining != nil {
		t.Errorf("expected conflicting idle instance to be evicted, but still present")
	}
	if !deletedUUIDs["MIG-IDLE-CONFLICT-1"] {
		t.Errorf("expected DeleteMigSpecFile to be called for evicted idle instance")
	}

	// Verify k2 placement key
	if k2.Size != 2 {
		t.Errorf("expected k2 size=2")
	}
}

func TestPlacementsOverlap(t *testing.T) {
	p1 := nvml.GpuInstancePlacement{Start: 0, Size: 2}
	p2 := nvml.GpuInstancePlacement{Start: 1, Size: 2}
	p3 := nvml.GpuInstancePlacement{Start: 2, Size: 2}

	if !placementsOverlap(p1, p2) {
		t.Errorf("expected p1 (0..2) and p2 (1..3) to overlap")
	}
	if placementsOverlap(p1, p3) {
		t.Errorf("expected p1 (0..2) and p3 (2..4) NOT to overlap")
	}
}

func TestMigInstanceManagerConcurrentReclaimAndRelease(t *testing.T) {
	mgr := NewMigInstanceManager(newMockNVML())
	cdiMock := &cdi.InterfaceMock{
		DeleteMigSpecFileFunc: func(migUUID string) error {
			return nil
		},
	}
	mgr.SetCDIHandler(cdiMock)

	for i := 0; i < 20; i++ {
		key := allocationKey(i, "1g.10gb", nvml.GpuInstancePlacement{Start: 0, Size: 1})
		uuid := "MIG-CONC-" + string(rune('A'+i))
		inst := &migInstance{
			Profile:   "1g.10gb",
			Placement: nvml.GpuInstancePlacement{Start: 0, Size: 1},
			GIID:      uint32(i + 1),
			CIID:      0,
			MigUUID:   uuid,
			State:     StateActive,
			LastUsed:  time.Now(),
		}
		mgr.mu.Lock()
		mgr.byAllocation[key] = inst
		mgr.byAllocationMigUUID[uuid] = key
		mgr.mu.Unlock()
	}

	var wg sync.WaitGroup
	// Concurrently release
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			uuid := "MIG-CONC-" + string(rune('A'+idx))
			_ = mgr.Release(uuid)
		}(i)
	}

	// Concurrently scan and reclaim
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = mgr.ReclaimExpiredIdleInstances(0)
		}()
	}

	wg.Wait()
}
