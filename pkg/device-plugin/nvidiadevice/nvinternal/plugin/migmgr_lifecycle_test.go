/*
 * Copyright (c) 2026, HAMi. All rights reserved.
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
 */

package plugin

import (
	"sync"
	"testing"
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	nvmlmock "github.com/NVIDIA/go-nvml/pkg/nvml/mock"
)

func TestMigManagerLifecycle(t *testing.T) {
	lib := &nvmlmock.Interface{
		InitFunc:           func() nvml.Return { return nvml.SUCCESS },
		ShutdownFunc:       func() nvml.Return { return nvml.SUCCESS },
		DeviceGetCountFunc: func() (int, nvml.Return) { return 0, nvml.SUCCESS },
	}
	m := newMigInstanceManager(lib)
	if len(lib.InitCalls()) != 0 {
		t.Fatal("construction initialized NVML")
	}
	if _, _, err := m.deviceInventory(); err == nil {
		t.Fatal("query before Init succeeded")
	}
	m.Shutdown()
	for cycle := 1; cycle <= 2; cycle++ {
		for i := 0; i < 2; i++ {
			if err := m.Init(); err != nil {
				t.Fatal(err)
			}
		}
		for i := 0; i < 3; i++ {
			if _, _, err := m.deviceInventory(); err != nil {
				t.Fatal(err)
			}
			if _, err := m.nvmlBusyGPUs(); err != nil {
				t.Fatal(err)
			}
			if err := m.Release("unknown"); err != nil {
				t.Fatal(err)
			}
		}
		if len(lib.InitCalls()) != cycle || len(lib.ShutdownCalls()) != cycle-1 {
			t.Fatal("operations changed NVML session ownership")
		}
		m.Shutdown()
		m.Shutdown()
		if len(lib.ShutdownCalls()) != cycle {
			t.Fatal("shutdown was not paired exactly once")
		}
		if err := m.Release("unknown"); err == nil {
			t.Fatal("operation after Shutdown succeeded")
		}
	}
}

func TestMigManagerInitFailureCanRetry(t *testing.T) {
	attempt := 0
	lib := &nvmlmock.Interface{
		InitFunc: func() nvml.Return {
			attempt++
			if attempt == 1 {
				return nvml.ERROR_LIBRARY_NOT_FOUND
			}
			return nvml.SUCCESS
		},
		ShutdownFunc: func() nvml.Return { return nvml.SUCCESS },
	}
	m := newMigInstanceManager(lib)
	if err := m.Init(); err == nil {
		t.Fatal("expected init failure")
	}
	m.Shutdown()
	if len(lib.ShutdownCalls()) != 0 {
		t.Fatal("shutdown after failed Init")
	}
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}
	m.Shutdown()
	if len(lib.ShutdownCalls()) != 1 {
		t.Fatal("retry did not acquire a session")
	}
}

func TestMigManagerShutdownDrainsQuery(t *testing.T) {
	entered, unblock := make(chan struct{}), make(chan struct{})
	var unblockOnce sync.Once
	release := func() { unblockOnce.Do(func() { close(unblock) }) }
	defer release()
	lib := &nvmlmock.Interface{
		InitFunc:     func() nvml.Return { return nvml.SUCCESS },
		ShutdownFunc: func() nvml.Return { return nvml.SUCCESS },
		DeviceGetCountFunc: func() (int, nvml.Return) {
			close(entered)
			<-unblock
			return 0, nvml.SUCCESS
		},
	}
	m := newMigInstanceManager(lib)
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}
	queryDone := make(chan error, 1)
	go func() { _, _, err := m.deviceInventory(); queryDone <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("query did not enter NVML")
	}
	closed := make(chan struct{})
	go func() { m.Shutdown(); close(closed) }()
	deadline := time.After(time.Second)
	for {
		m.sessionMu.Lock()
		closing := m.closing != nil
		m.sessionMu.Unlock()
		if closing {
			break
		}
		select {
		case <-deadline:
			t.Fatal("shutdown did not close admission")
		case <-time.After(time.Millisecond):
		}
	}
	if err := m.Release("unknown"); err == nil {
		t.Fatal("closing manager admitted new work")
	}
	if err := m.Init(); err == nil {
		t.Fatal("Init succeeded while shutting down")
	}
	if len(lib.ShutdownCalls()) != 0 {
		t.Fatal("NVML closed while query was active")
	}
	second := make(chan struct{})
	go func() { m.Shutdown(); close(second) }()
	release()
	if err := <-queryDone; err != nil {
		t.Fatal(err)
	}
	for _, done := range []chan struct{}{closed, second} {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("Shutdown did not finish")
		}
	}
	if len(lib.ShutdownCalls()) != 1 {
		t.Fatal("concurrent Shutdown released twice")
	}
}

// Exercise actual GI/CI creation, cache hits, adoption, and release through
// the borrowed library, rather than only counting empty operations.
func TestMigManagerAllocationUsesOneSession(t *testing.T) {
	placement := nvml.GpuInstancePlacement{Start: 0, Size: 1}
	ci := &nvmlmock.ComputeInstance{
		GetInfoFunc: func() (nvml.ComputeInstanceInfo, nvml.Return) { return nvml.ComputeInstanceInfo{Id: 2}, nvml.SUCCESS },
		DestroyFunc: func() nvml.Return { return nvml.SUCCESS },
	}
	gi := &nvmlmock.GpuInstance{
		GetInfoFunc: func() (nvml.GpuInstanceInfo, nvml.Return) {
			return nvml.GpuInstanceInfo{Id: 1, Placement: placement}, nvml.SUCCESS
		},
		GetComputeInstanceProfileInfoFunc: func(int, int) (nvml.ComputeInstanceProfileInfo, nvml.Return) {
			return nvml.ComputeInstanceProfileInfo{}, nvml.SUCCESS
		},
		CreateComputeInstanceFunc:  func(*nvml.ComputeInstanceProfileInfo) (nvml.ComputeInstance, nvml.Return) { return ci, nvml.SUCCESS },
		GetComputeInstanceByIdFunc: func(int) (nvml.ComputeInstance, nvml.Return) { return ci, nvml.SUCCESS },
		GetComputeInstancesFunc: func(*nvml.ComputeInstanceProfileInfo) ([]nvml.ComputeInstance, nvml.Return) {
			return []nvml.ComputeInstance{ci}, nvml.SUCCESS
		},
		DestroyFunc: func() nvml.Return { return nvml.SUCCESS },
	}
	migDev := &nvmlmock.Device{
		GetGpuInstanceIdFunc: func() (int, nvml.Return) { return 1, nvml.SUCCESS },
		GetUUIDFunc:          func() (string, nvml.Return) { return "MIG-test", nvml.SUCCESS },
	}
	dev := &nvmlmock.Device{
		GetIndexFunc:   func() (int, nvml.Return) { return 0, nvml.SUCCESS },
		GetMigModeFunc: func() (int, int, nvml.Return) { return nvml.DEVICE_MIG_ENABLE, nvml.DEVICE_MIG_ENABLE, nvml.SUCCESS },
		GetGpuInstanceProfileInfoFunc: func(int) (nvml.GpuInstanceProfileInfo, nvml.Return) {
			return nvml.GpuInstanceProfileInfo{}, nvml.SUCCESS
		},
		GetGpuInstancePossiblePlacementsFunc: func(*nvml.GpuInstanceProfileInfo) ([]nvml.GpuInstancePlacement, nvml.Return) {
			return []nvml.GpuInstancePlacement{placement}, nvml.SUCCESS
		},
		CreateGpuInstanceWithPlacementFunc: func(*nvml.GpuInstanceProfileInfo, *nvml.GpuInstancePlacement) (nvml.GpuInstance, nvml.Return) {
			return gi, nvml.SUCCESS
		},
		GetMaxMigDeviceCountFunc:      func() (int, nvml.Return) { return 1, nvml.SUCCESS },
		GetMigDeviceHandleByIndexFunc: func(int) (nvml.Device, nvml.Return) { return migDev, nvml.SUCCESS },
		GetGpuInstanceByIdFunc:        func(int) (nvml.GpuInstance, nvml.Return) { return gi, nvml.SUCCESS },
		GetGpuInstancesFunc: func(*nvml.GpuInstanceProfileInfo) ([]nvml.GpuInstance, nvml.Return) {
			return []nvml.GpuInstance{gi}, nvml.SUCCESS
		},
	}
	lib := &nvmlmock.Interface{
		InitFunc:                   func() nvml.Return { return nvml.SUCCESS },
		ShutdownFunc:               func() nvml.Return { return nvml.SUCCESS },
		DeviceGetHandleByIndexFunc: func(int) (nvml.Device, nvml.Return) { return dev, nvml.SUCCESS },
		DeviceGetHandleByUUIDFunc:  func(string) (nvml.Device, nvml.Return) { return dev, nvml.SUCCESS },
	}
	m := newMigInstanceManager(lib)
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}
	defer m.Shutdown()
	if index, ok := m.gpuUUIDToIndex("GPU-test"); !ok || index != 0 {
		t.Fatal("UUID resolution failed")
	}
	for i := 0; i < 2; i++ {
		uuid, created, err := m.EnsureAllocation(0, "1g.5gb", placement)
		if err != nil || uuid != "MIG-test" || created != (i == 0) {
			t.Fatalf("EnsureAllocation = %q, %v, %v", uuid, created, err)
		}
	}
	if err := m.AdoptAllocation(0, "1g.5gb", "MIG-test", placement, 1, 2); err != nil {
		t.Fatal(err)
	}
	if err := m.ReconcileActiveAllocations(map[migAllocationKey]struct{}{allocationKey(0, "1g.5gb", placement): {}}); err != nil {
		t.Fatal(err)
	}
	if err := m.Release("MIG-test"); err != nil {
		t.Fatal(err)
	}
	if len(gi.DestroyCalls()) != 1 || len(ci.DestroyCalls()) != 1 {
		t.Fatal("release did not destroy GI and CI")
	}
	if len(lib.InitCalls()) != 1 || len(lib.ShutdownCalls()) != 0 {
		t.Fatal("allocation operations changed session ownership")
	}
	// Closing the session must never destroy an active allocation.
	if _, _, err := m.EnsureAllocation(0, "1g.5gb", placement); err != nil {
		t.Fatal(err)
	}
	m.Shutdown()
	if len(gi.DestroyCalls()) != 1 || len(ci.DestroyCalls()) != 1 {
		t.Fatal("Shutdown destroyed an allocation")
	}
	if len(lib.ShutdownCalls()) != 1 {
		t.Fatal("NVML was not shut down once")
	}
}
