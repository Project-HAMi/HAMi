/*
 * Copyright (c) 2026, HAMi.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

package rm

import (
	"testing"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvlib/pkg/nvlib/info"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	nvmlmock "github.com/NVIDIA/go-nvml/pkg/nvml/mock"
	"github.com/stretchr/testify/require"

	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"

	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

type nvmlInfo struct{ info.Interface }

func (nvmlInfo) HasNvml() (bool, string) { return true, "" }

func (nvmlInfo) ResolvePlatform() info.Platform { return info.PlatformNVML }

// migCapableLib returns MIG-capable GPUs that each support only the 1g.10gb
// profile. The GPU at lostIndex fails the handle lookup with ret.
func migCapableLib(count, lostIndex int, ret nvml.Return) (nvml.Interface, device.Interface) {
	devices := make([]nvml.Device, count)
	for i := range devices {
		devices[i] = &nvmlmock.Device{
			GetNameFunc:    func() (string, nvml.Return) { return "NVIDIA A100-SXM4-80GB", nvml.SUCCESS },
			GetMigModeFunc: func() (int, int, nvml.Return) { return nvml.DEVICE_MIG_ENABLE, nvml.DEVICE_MIG_ENABLE, nvml.SUCCESS },
			GetMemoryInfoFunc: func() (nvml.Memory, nvml.Return) {
				return nvml.Memory{Total: 80 * 1024 * 1024 * 1024}, nvml.SUCCESS
			},
			GetGpuInstanceProfileInfoFunc: func(profile int) (nvml.GpuInstanceProfileInfo, nvml.Return) {
				if profile != nvml.GPU_INSTANCE_PROFILE_1_SLICE {
					return nvml.GpuInstanceProfileInfo{}, nvml.ERROR_NOT_SUPPORTED
				}
				return nvml.GpuInstanceProfileInfo{MemorySizeMB: 9856}, nvml.SUCCESS
			},
		}
	}
	lib := &nvmlmock.Interface{
		InitFunc:           func() nvml.Return { return nvml.SUCCESS },
		ShutdownFunc:       func() nvml.Return { return nvml.SUCCESS },
		DeviceGetCountFunc: func() (int, nvml.Return) { return count, nvml.SUCCESS },
		DeviceGetHandleByIndexFunc: func(i int) (nvml.Device, nvml.Return) {
			if i == lostIndex {
				return nil, ret
			}
			return devices[i], nvml.SUCCESS
		},
	}
	return lib, device.New(lib, device.WithVerifySymbols(false))
}

func mixedConfig() *nvidia.DeviceConfig {
	migStrategy := spec.MigStrategyMixed
	failOnInitError := true
	resourceName := "nvidia.com/gpu"
	return &nvidia.DeviceConfig{
		Config: &spec.Config{Flags: spec.Flags{CommandLineFlags: spec.CommandLineFlags{
			MigStrategy:     &migStrategy,
			FailOnInitError: &failOnInitError,
		}}},
		ResourceName: &resourceName,
	}
}

// With --mig-strategy=mixed, one GPU that NVML cannot use must not stop the
// plugin from collecting the MIG profiles of the other GPUs.
func TestAddDefaultResourcesMixedSkipsUnusableGPU(t *testing.T) {
	for _, ret := range []nvml.Return{nvml.ERROR_GPU_IS_LOST, nvml.ERROR_UNKNOWN} {
		t.Run(nvml.ErrorString(ret), func(t *testing.T) {
			nvmllib, devicelib := migCapableLib(4, 1, ret)
			config := mixedConfig()

			err := AddDefaultResourcesToConfig(nvmlInfo{}, nvmllib, devicelib, config)

			require.NoError(t, err)
			var names []string
			for _, r := range config.Resources.MIGs {
				names = append(names, string(r.Name))
			}
			require.Equal(t, []string{"nvidia.com/mig-1g.10gb"}, names)
		})
	}
}

func TestAddDefaultResourcesMixedStillFails(t *testing.T) {
	t.Run("other error", func(t *testing.T) {
		nvmllib, devicelib := migCapableLib(4, 1, nvml.ERROR_NO_PERMISSION)
		err := AddDefaultResourcesToConfig(nvmlInfo{}, nvmllib, devicelib, mixedConfig())
		require.ErrorContains(t, err, "ERROR_NO_PERMISSION")
	})
	t.Run("no usable GPU", func(t *testing.T) {
		nvmllib, devicelib := migCapableLib(1, 0, nvml.ERROR_GPU_IS_LOST)
		err := AddDefaultResourcesToConfig(nvmlInfo{}, nvmllib, devicelib, mixedConfig())
		require.ErrorContains(t, err, "GPU at index 0 is unusable")
	})
}

// When no GPU is usable, building the resource managers fails. It must not
// return an empty set, which would make the plugin log "No devices found.
// Waiting indefinitely." and keep running.
func TestNewNVMLResourceManagersFailsWhenNoGPUIsUsable(t *testing.T) {
	for _, failAt := range []string{"DeviceGetHandleByIndex", "GetName"} {
		t.Run(failAt, func(t *testing.T) {
			nvmllib, devicelib := realDeviceLib([]string{"NVIDIA H100", "NVIDIA H100"}, -1, "")
			lib := nvmllib.(*nvmlmock.Interface)
			lib.InitFunc = func() nvml.Return { return nvml.SUCCESS }
			lib.ShutdownFunc = func() nvml.Return { return nvml.SUCCESS }
			getHandle := lib.DeviceGetHandleByIndexFunc
			lib.DeviceGetHandleByIndexFunc = func(i int) (nvml.Device, nvml.Return) {
				if failAt == "DeviceGetHandleByIndex" {
					return nil, nvml.ERROR_GPU_IS_LOST
				}
				d, ret := getHandle(i)
				d.(*nvmlmock.Device).GetNameFunc = func() (string, nvml.Return) { return "", nvml.ERROR_GPU_IS_LOST }
				return d, ret
			}
			migStrategy := spec.MigStrategyNone
			config := &spec.Config{
				Flags:     spec.Flags{CommandLineFlags: spec.CommandLineFlags{MigStrategy: &migStrategy}},
				Resources: spec.Resources{GPUs: []spec.Resource{{Pattern: "*", Name: "nvidia.com/gpu"}}},
			}

			rms, err := NewNVMLResourceManagers(nvmlInfo{}, nvmllib, devicelib, config)

			require.ErrorContains(t, err, "GPU at index 1 is unusable")
			require.Empty(t, rms)
		})
	}
}
