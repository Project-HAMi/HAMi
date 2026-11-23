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
	"fmt"
	"testing"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	nvmlmock "github.com/NVIDIA/go-nvml/pkg/nvml/mock"
	"github.com/stretchr/testify/require"
)

func gpuUUID(i int) string { return fmt.Sprintf("GPU-%d", i) }

// topologyLib returns count GPUs under one CPU without NVLink. The GPU at
// lostIndex fails the handle lookup with ret.
func topologyLib(count, lostIndex int, ret nvml.Return) nvml.Interface {
	devices := make([]*nvmlmock.Device, count)
	for i := range devices {
		i := i
		devices[i] = &nvmlmock.Device{
			GetNameFunc:    func() (string, nvml.Return) { return "NVIDIA H100", nvml.SUCCESS },
			GetMigModeFunc: func() (int, int, nvml.Return) { return 0, 0, nvml.ERROR_NOT_SUPPORTED },
			GetUUIDFunc:    func() (string, nvml.Return) { return gpuUUID(i), nvml.SUCCESS },
			GetPciInfoFunc: func() (nvml.PciInfo, nvml.Return) {
				var info nvml.PciInfo
				for k, c := range fmt.Sprintf("00000000:%02X:00.0", i+1) {
					info.BusId[k] = int8(c)
				}
				return info, nvml.SUCCESS
			},
			GetTopologyCommonAncestorFunc: func(nvml.Device) (nvml.GpuTopologyLevel, nvml.Return) {
				return nvml.TOPOLOGY_NODE, nvml.SUCCESS
			},
			GetNvLinkStateFunc: func(int) (nvml.EnableState, nvml.Return) { return 0, nvml.ERROR_NOT_SUPPORTED },
		}
	}
	return &nvmlmock.Interface{
		// CalculateGPUScoreWithNVML checks NVML symbols; report all of them present.
		ExtensionsFunc: func() nvml.ExtendedInterface {
			return &nvmlmock.ExtendedInterface{LookupSymbolFunc: func(string) error { return nil }}
		},
		DeviceGetCountFunc: func() (int, nvml.Return) { return count, nvml.SUCCESS },
		DeviceGetHandleByIndexFunc: func(i int) (nvml.Device, nvml.Return) {
			if i == lostIndex {
				return nil, ret
			}
			return devices[i], nvml.SUCCESS
		},
	}
}

// With ENABLE_TOPOLOGY_SCORE=true, one GPU that NVML cannot use must not stop
// the scores of the other GPUs from being computed. Otherwise the node
// annotation is never registered.
func TestCalculateGPUScoreSkipsUnusableGPU(t *testing.T) {
	for _, ret := range []nvml.Return{nvml.ERROR_GPU_IS_LOST, nvml.ERROR_UNKNOWN} {
		t.Run(nvml.ErrorString(ret), func(t *testing.T) {
			lib := topologyLib(4, 1, ret)

			scores, asymmetric, err := CalculateGPUScoreWithNVML(lib, []string{gpuUUID(0), gpuUUID(2), gpuUUID(3)})

			require.NoError(t, err)
			require.False(t, asymmetric)
			got := map[string]map[string]int{}
			for _, s := range scores {
				got[s.UUID] = s.Score
			}
			require.Equal(t, map[string]map[string]int{
				gpuUUID(0): {gpuUUID(2): 20, gpuUUID(3): 20},
				gpuUUID(2): {gpuUUID(0): 20, gpuUUID(3): 20},
				gpuUUID(3): {gpuUUID(0): 20, gpuUUID(2): 20},
			}, got)
		})
	}
}

// The devices keep their NVML index, and links are keyed by it.
func TestDeviceListKeepsNVMLIndices(t *testing.T) {
	lib := topologyLib(4, 1, nvml.ERROR_GPU_IS_LOST)
	b := &deviceListBuilder{nvmllib: lib, devicelib: device.New(lib, device.WithVerifySymbols(false))}

	devices, err := b.buildWithSession()

	require.NoError(t, err)
	var indices []int
	for _, d := range devices {
		indices = append(indices, d.Index)
	}
	require.Equal(t, []int{0, 2, 3}, indices)
	require.Contains(t, devices[0].Links, 3)
	require.NotContains(t, devices[0].Links, 1)
}

func TestCalculateGPUScoreStillFails(t *testing.T) {
	t.Run("other error", func(t *testing.T) {
		_, _, err := CalculateGPUScoreWithNVML(topologyLib(4, 1, nvml.ERROR_NO_PERMISSION), []string{gpuUUID(0)})
		require.ErrorContains(t, err, "ERROR_NO_PERMISSION")
	})
	t.Run("no usable GPU", func(t *testing.T) {
		_, _, err := CalculateGPUScoreWithNVML(topologyLib(1, 0, nvml.ERROR_GPU_IS_LOST), []string{gpuUUID(0)})
		require.ErrorContains(t, err, "GPU at index 0 is unusable")
	})
}
