/*
 * Copyright (c) 2026, HAMi.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

package nvidia

import (
	"testing"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	nvmlmock "github.com/NVIDIA/go-nvml/pkg/nvml/mock"
	"github.com/stretchr/testify/require"
)

// mockGPUs returns an NVML mock over the named GPUs. GPU lostIndex fails the
// call named by failAt with ret.
func mockGPUs(names []string, lostIndex int, failAt string, ret nvml.Return) (nvml.Interface, device.Interface) {
	devices := make([]*nvmlmock.Device, len(names))
	for i := range devices {
		i := i
		devices[i] = &nvmlmock.Device{
			GetNameFunc: func() (string, nvml.Return) {
				if i == lostIndex && failAt == "name" {
					return "", ret
				}
				return names[i], nvml.SUCCESS
			},
			GetMigModeFunc: func() (int, int, nvml.Return) {
				if i == lostIndex && failAt == "migmode" {
					return 0, 0, ret
				}
				return 0, 0, nvml.ERROR_NOT_SUPPORTED
			},
		}
	}
	lib := &nvmlmock.Interface{
		DeviceGetCountFunc: func() (int, nvml.Return) { return len(devices), nvml.SUCCESS },
		DeviceGetHandleByIndexFunc: func(i int) (nvml.Device, nvml.Return) {
			if i == lostIndex && failAt == "handle" {
				return nil, ret
			}
			return devices[i], nvml.SUCCESS
		},
	}
	return lib, device.New(lib, device.WithVerifySymbols(false))
}

func visitedIndices(t *testing.T, nvmllib nvml.Interface, devicelib device.Interface) ([]int, []int, error) {
	t.Helper()
	var visited, skipped []int
	err := VisitUsableDevices(nvmllib, devicelib,
		func(i int, _ device.Device) error { visited = append(visited, i); return nil },
		func(i int, _ nvml.Return) { skipped = append(skipped, i) })
	return visited, skipped, err
}

func TestVisitUsableDevicesSkipsUnusableGPU(t *testing.T) {
	names := []string{"NVIDIA H100", "NVIDIA H100", "NVIDIA DGX Display", "NVIDIA H100"}
	for _, tc := range []struct {
		failAt string
		ret    nvml.Return
	}{
		{"handle", nvml.ERROR_GPU_IS_LOST},
		{"handle", nvml.ERROR_UNKNOWN},
		{"name", nvml.ERROR_GPU_IS_LOST},
		{"migmode", nvml.ERROR_RESET_REQUIRED},
	} {
		t.Run(tc.failAt+"/"+tc.ret.Error(), func(t *testing.T) {
			nvmllib, devicelib := mockGPUs(names, 1, tc.failAt, tc.ret)
			visited, skipped, err := visitedIndices(t, nvmllib, devicelib)

			require.NoError(t, err)
			// GPU 2 is excluded by model name, as go-nvlib does.
			require.Equal(t, []int{0, 3}, visited)
			require.Equal(t, []int{1}, skipped)
		})
	}
}

func TestVisitUsableDevicesStillFails(t *testing.T) {
	nvmllib, devicelib := mockGPUs([]string{"NVIDIA H100", "NVIDIA H100"}, 1, "handle", nvml.ERROR_NO_PERMISSION)
	_, _, err := visitedIndices(t, nvmllib, devicelib)
	require.ErrorContains(t, err, "ERROR_NO_PERMISSION")

	nvmllib, devicelib = mockGPUs([]string{"NVIDIA H100"}, 0, "handle", nvml.ERROR_GPU_IS_LOST)
	_, _, err = visitedIndices(t, nvmllib, devicelib)
	require.ErrorContains(t, err, "GPU at index 0 is unusable")
}
