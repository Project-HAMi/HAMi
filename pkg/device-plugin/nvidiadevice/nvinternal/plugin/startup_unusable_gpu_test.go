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
	"testing"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	nvmlmock "github.com/NVIDIA/go-nvml/pkg/nvml/mock"
	"github.com/stretchr/testify/require"
)

// startupManager returns an initialized MigInstanceManager over four GPUs
// without MIG. GPU 1 fails: its handle lookup returns handleRet, or its MIG
// mode query returns migModeRet. NVML documents NVML_ERROR_GPU_IS_LOST for a
// GPU that has fallen off the bus, and "GPU requires reset" is what
// nvmlDeviceGetMigMode returns after a MIG mode change that the platform could
// not activate.
func startupManager(t *testing.T, handleRet, migModeRet nvml.Return) *MigInstanceManager {
	t.Helper()
	devices := make([]*nvmlmock.Device, 4)
	for i := range devices {
		ret := nvml.SUCCESS
		if i == 1 {
			ret = migModeRet
		}
		devices[i] = &nvmlmock.Device{
			GetNameFunc: func() (string, nvml.Return) { return "NVIDIA A100-SXM4-80GB", nvml.SUCCESS },
			GetMigModeFunc: func() (int, int, nvml.Return) {
				return nvml.DEVICE_MIG_DISABLE, nvml.DEVICE_MIG_DISABLE, ret
			},
		}
	}
	manager := newMigInstanceManager(&nvmlmock.Interface{
		InitFunc:           func() nvml.Return { return nvml.SUCCESS },
		ShutdownFunc:       func() nvml.Return { return nvml.SUCCESS },
		DeviceGetCountFunc: func() (int, nvml.Return) { return len(devices), nvml.SUCCESS },
		DeviceGetHandleByIndexFunc: func(i int) (nvml.Device, nvml.Return) {
			if i == 1 && handleRet != nvml.SUCCESS {
				return nil, handleRet
			}
			return devices[i], nvml.SUCCESS
		},
	})
	require.NoError(t, manager.Init())
	t.Cleanup(manager.Shutdown)
	return manager
}

// Start calls deviceInventory in every operating mode before it serves. One
// lost GPU must not keep the plugin from serving the healthy GPUs.
func TestDeviceInventoryToleratesOneLostGPU(t *testing.T) {
	// A GPU that fell off the bus is often reported as ERROR_UNKNOWN.
	for _, ret := range []nvml.Return{nvml.ERROR_GPU_IS_LOST, nvml.ERROR_UNKNOWN} {
		t.Run(ret.Error(), func(t *testing.T) {
			manager := startupManager(t, ret, nvml.SUCCESS)

			count, names, err := manager.deviceInventory()

			require.NoError(t, err)
			require.Equal(t, 4, count)
			require.Equal(t, "", names[1])
		})
	}
}

// Other NVML errors still fail the start, so a real problem is not hidden.
func TestDeviceInventoryStillFailsOnOtherErrors(t *testing.T) {
	manager := startupManager(t, nvml.ERROR_NO_PERMISSION, nvml.SUCCESS)

	_, _, err := manager.deviceInventory()

	require.ErrorContains(t, err, "get device 1")
}

// In the non-MIG operating modes, Start calls needsMigDisable on every start.
// One GPU whose MIG mode cannot be read must not keep the plugin from serving
// the healthy GPUs, none of which has MIG enabled.
func TestNeedsMigDisableToleratesOneUnusableGPU(t *testing.T) {
	for _, ret := range []nvml.Return{nvml.ERROR_RESET_REQUIRED, nvml.ERROR_GPU_IS_LOST, nvml.ERROR_UNKNOWN} {
		t.Run(ret.Error(), func(t *testing.T) {
			manager := startupManager(t, nvml.SUCCESS, ret)

			needsDisable, err := manager.needsMigDisable(4)

			require.NoError(t, err)
			require.False(t, needsDisable)
		})
	}
}

// Baseline: with every GPU healthy, both startup walks succeed.
func TestStartupWalksSucceedWithHealthyGPUs(t *testing.T) {
	manager := startupManager(t, nvml.SUCCESS, nvml.SUCCESS)

	count, names, err := manager.deviceInventory()
	require.NoError(t, err)
	require.Equal(t, 4, count)
	require.Len(t, names, 4)

	needsDisable, err := manager.needsMigDisable(4)
	require.NoError(t, err)
	require.False(t, needsDisable)
}

// DisableIdleGPUs treats a GPU that nvmlBusyGPUs could not query as idle. When
// that GPU is lost, the pass must still cover the healthy GPUs.
func TestDisableIdleGPUsSkipsLostGPU(t *testing.T) {
	manager := startupManager(t, nvml.ERROR_GPU_IS_LOST, nvml.SUCCESS)

	disabled, err := manager.DisableIdleGPUs(4, map[int]struct{}{})

	require.NoError(t, err)
	require.Equal(t, []int{0, 2, 3}, disabled)
}

// In MIG mode, recovery must skip a lost GPU instead of failing for all GPUs.
func TestRestoreAllocationsSkipsLostGPU(t *testing.T) {
	manager := startupManager(t, nvml.ERROR_GPU_IS_LOST, nvml.SUCCESS)
	busy := map[int]struct{}{0: {}, 2: {}, 3: {}}

	err := manager.RestoreAllocations(4, busy, nil, false)

	require.NoError(t, err)
}

// startupManagerAllUnusable returns a manager whose every GPU fails with ret,
// on the handle lookup when onHandle is true and on GetMigMode otherwise.
func startupManagerAllUnusable(t *testing.T, ret nvml.Return, onHandle bool) *MigInstanceManager {
	t.Helper()
	dev := &nvmlmock.Device{
		GetNameFunc:    func() (string, nvml.Return) { return "NVIDIA A100-SXM4-80GB", nvml.SUCCESS },
		GetMigModeFunc: func() (int, int, nvml.Return) { return 0, 0, ret },
	}
	manager := newMigInstanceManager(&nvmlmock.Interface{
		InitFunc:           func() nvml.Return { return nvml.SUCCESS },
		ShutdownFunc:       func() nvml.Return { return nvml.SUCCESS },
		DeviceGetCountFunc: func() (int, nvml.Return) { return 2, nvml.SUCCESS },
		DeviceGetHandleByIndexFunc: func(int) (nvml.Device, nvml.Return) {
			if onHandle {
				return nil, ret
			}
			return dev, nvml.SUCCESS
		},
	})
	require.NoError(t, manager.Init())
	t.Cleanup(manager.Shutdown)
	return manager
}

// When no GPU is usable there is nothing to serve, so the start still fails.
func TestStartupWalksFailWhenNoGPUIsUsable(t *testing.T) {
	_, _, err := startupManagerAllUnusable(t, nvml.ERROR_GPU_IS_LOST, true).deviceInventory()
	require.ErrorContains(t, err, "ERROR_GPU_IS_LOST")

	_, err = startupManagerAllUnusable(t, nvml.ERROR_UNKNOWN, false).needsMigDisable(2)
	require.ErrorContains(t, err, "get mig mode")
}

// When the GPU fails the read that verifies a MIG mode change, it is treated as
// unusable, so the startup walks skip it like any other unusable GPU.
func TestMigModeVerificationFailureIsUnusable(t *testing.T) {
	calls := 0
	dev := &nvmlmock.Device{
		GetMigModeFunc: func() (int, int, nvml.Return) {
			calls++
			if calls == 1 {
				return nvml.DEVICE_MIG_DISABLE, nvml.DEVICE_MIG_DISABLE, nvml.SUCCESS
			}
			return 0, 0, nvml.ERROR_UNKNOWN
		},
		SetMigModeFunc: func(int) (nvml.Return, nvml.Return) { return nvml.SUCCESS, nvml.SUCCESS },
	}
	manager := newMigInstanceManager(&nvmlmock.Interface{
		InitFunc:                   func() nvml.Return { return nvml.SUCCESS },
		ShutdownFunc:               func() nvml.Return { return nvml.SUCCESS },
		DeviceGetHandleByIndexFunc: func(int) (nvml.Device, nvml.Return) { return dev, nvml.SUCCESS },
	})
	require.NoError(t, manager.Init())
	t.Cleanup(manager.Shutdown)

	err := manager.ensureMigModeEnabled(0)

	require.ErrorContains(t, err, "verify mig mode after set")
	require.True(t, isUnusableGPU(err))
}
