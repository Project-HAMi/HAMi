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
	"fmt"
	"testing"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	nvmlmock "github.com/NVIDIA/go-nvml/pkg/nvml/mock"
	"github.com/stretchr/testify/require"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
)

// fakeGPU is a top-level GPU as buildGPUDeviceMap sees it. Only GetName and
// IsMigEnabled are called; the embedded interface is nil.
type fakeGPU struct {
	device.Device
	migModeRet nvml.Return
}

func (g fakeGPU) GetName() (string, nvml.Return) {
	return "NVIDIA A100-SXM4-40GB", nvml.SUCCESS
}

// migModeErr returns what go-nvlib's IsMigCapable and IsMigEnabled return
// when nvmlDeviceGetMigMode yields migModeRet.
func (g fakeGPU) migModeErr() error {
	switch g.migModeRet {
	case nvml.SUCCESS, nvml.ERROR_NOT_SUPPORTED:
		return nil
	default:
		return fmt.Errorf("error getting MIG mode: %v", g.migModeRet)
	}
}

func (g fakeGPU) IsMigEnabled() (bool, error) { return false, g.migModeErr() }
func (g fakeGPU) IsMigCapable() (bool, error) { return false, g.migModeErr() }
func (g fakeGPU) GetMigMode() (int, int, nvml.Return) {
	return nvml.DEVICE_MIG_DISABLE, nvml.DEVICE_MIG_DISABLE, g.migModeRet
}

// VisitMigDevices behaves as go-nvlib's: it checks MIG capability first, so it
// fails for an unusable GPU, and visits nothing for a GPU without MIG.
func (g fakeGPU) VisitMigDevices(func(int, device.MigDevice) error) error {
	if err := g.migModeErr(); err != nil {
		return fmt.Errorf("error checking if GPU is MIG capable: %v", err)
	}
	return nil
}

// fakeDeviceLib visits GPUs the way go-nvlib's devicelib does: in index
// order, stopping at the first error the callback returns. Its
// VisitMigDevices walks the GPUs itself, as go-nvlib's does, so it does not go
// through a wrapping VisitDevices.
type fakeDeviceLib struct {
	device.Interface
	gpus []fakeGPU
}

func (l fakeDeviceLib) VisitDevices(visit func(int, device.Device) error) error {
	for i, gpu := range l.gpus {
		if err := visit(i, gpu); err != nil {
			return fmt.Errorf("error visiting device: %w", err)
		}
	}
	return nil
}

func (l fakeDeviceLib) VisitMigDevices(visit func(int, device.Device, int, device.MigDevice) error) error {
	for i, gpu := range l.gpus {
		err := gpu.VisitMigDevices(func(j int, mig device.MigDevice) error { return visit(i, gpu, j, mig) })
		if err != nil {
			return fmt.Errorf("error visiting device: %w", err)
		}
	}
	return nil
}

type fakeDeviceInfo struct{ index int }

func (d fakeDeviceInfo) GetUUID() (string, error) {
	return fmt.Sprintf("GPU-%08d-0000-0000-0000-000000000000", d.index), nil
}
func (d fakeDeviceInfo) GetPaths() ([]string, error) {
	return []string{fmt.Sprintf("/dev/nvidia%d", d.index)}, nil
}
func (d fakeDeviceInfo) GetNumaNode() (bool, int, error)       { return false, 0, nil }
func (d fakeDeviceInfo) GetTotalMemory() (uint64, error)       { return 40 << 30, nil }
func (d fakeDeviceInfo) GetComputeCapability() (string, error) { return "8.0", nil }

func newFakeGPUDevice(i int, _ nvml.Device) (string, deviceInfo) {
	return fmt.Sprintf("%d", i), fakeDeviceInfo{index: i}
}

func eightGPUs(badIndex int, badRet nvml.Return) []fakeGPU {
	gpus := make([]fakeGPU, 8)
	for i := range gpus {
		gpus[i] = fakeGPU{migModeRet: nvml.ERROR_NOT_SUPPORTED}
	}
	if badIndex >= 0 {
		gpus[badIndex].migModeRet = badRet
	}
	return gpus
}

// One GPU that NVML reports as needing a reset (or as lost) must not take the
// other GPUs on the node down with it. At runtime the health checker already
// marks only that GPU unhealthy; this covers a plugin (re)start while the GPU
// is in that state, for example after a kubelet restart or a config change.
func TestBuildGPUDeviceMapToleratesOneUnusableGPU(t *testing.T) {
	resourceName := spec.ResourceName("nvidia.com/gpu")
	pattern := spec.ResourcePattern("*")
	resources := &spec.Resources{GPUs: []spec.Resource{{Pattern: pattern, Name: resourceName}}}

	testCases := []struct {
		description string
		badIndex    int
		badRet      nvml.Return
		wantHealthy int
	}{
		{description: "all GPUs healthy (baseline)", badIndex: -1, wantHealthy: 8},
		{description: "one GPU requires a reset", badIndex: 3, badRet: nvml.ERROR_RESET_REQUIRED, wantHealthy: 7},
		{description: "one GPU is lost", badIndex: 3, badRet: nvml.ERROR_GPU_IS_LOST, wantHealthy: 7},
		{description: "one GPU reports an unknown error", badIndex: 3, badRet: nvml.ERROR_UNKNOWN, wantHealthy: 7},
	}

	for _, migStrategy := range []string{spec.MigStrategyNone, spec.MigStrategySingle, spec.MigStrategyMixed} {
		for _, tc := range testCases {
			t.Run(migStrategy+"/"+tc.description, func(t *testing.T) {
				migStrategy := migStrategy
				b := deviceMapBuilder{
					Interface:    newUsableDevices(fakeDeviceLib{gpus: eightGPUs(tc.badIndex, tc.badRet)}, nil),
					migStrategy:  &migStrategy,
					resources:    resources,
					newGPUDevice: newFakeGPUDevice,
				}

				devices, err := b.buildDeviceMapFromConfigResources()
				require.NoError(t, err)

				require.Len(t, devices[resourceName], tc.wantHealthy)
				for id, dev := range devices[resourceName] {
					require.NotEqual(t, fmt.Sprintf("%d", tc.badIndex), dev.Index, "unusable GPU %s is advertised", id)
					require.Equal(t, kubeletdevicepluginv1beta1.Healthy, dev.Health, "device %s", id)
				}
			})
		}
	}
}

// An NVML error outside the unusable-GPU set still fails the device map, so a
// real problem is not hidden.
func TestBuildGPUDeviceMapStillFailsOnOtherErrors(t *testing.T) {
	resources := &spec.Resources{GPUs: []spec.Resource{{Pattern: spec.ResourcePattern("*"), Name: "nvidia.com/gpu"}}}
	migStrategy := spec.MigStrategyNone
	b := deviceMapBuilder{
		Interface:    newUsableDevices(fakeDeviceLib{gpus: eightGPUs(3, nvml.ERROR_NO_PERMISSION)}, nil),
		migStrategy:  &migStrategy,
		resources:    resources,
		newGPUDevice: newFakeGPUDevice,
	}

	_, err := b.buildDeviceMapFromConfigResources()
	require.ErrorContains(t, err, "error getting MIG mode: ERROR_NO_PERMISSION")
}

// realDeviceLib returns go-nvlib's devicelib over an NVML mock with the given
// device names. GPU lostIndex fails the call named by failAt with
// ERROR_GPU_IS_LOST, which go-nvlib's own VisitDevices cannot skip because it
// makes the call before the callback runs.
func realDeviceLib(names []string, lostIndex int, failAt string) (nvml.Interface, device.Interface) {
	devices := make([]*nvmlmock.Device, len(names))
	for i := range devices {
		i := i
		devices[i] = &nvmlmock.Device{
			GetNameFunc: func() (string, nvml.Return) {
				if i == lostIndex && failAt == "GetName" {
					return "", nvml.ERROR_GPU_IS_LOST
				}
				return names[i], nvml.SUCCESS
			},
			GetMigModeFunc: func() (int, int, nvml.Return) { return 0, 0, nvml.ERROR_NOT_SUPPORTED },
		}
	}
	lib := &nvmlmock.Interface{
		DeviceGetCountFunc: func() (int, nvml.Return) { return len(devices), nvml.SUCCESS },
		DeviceGetHandleByIndexFunc: func(i int) (nvml.Device, nvml.Return) {
			if i == lostIndex && failAt == "DeviceGetHandleByIndex" {
				return nil, nvml.ERROR_GPU_IS_LOST
			}
			return devices[i], nvml.SUCCESS
		},
	}
	return lib, device.New(lib, device.WithVerifySymbols(false))
}

func TestDeviceMapSkipsGPULostAtHandleOrName(t *testing.T) {
	resources := &spec.Resources{GPUs: []spec.Resource{{Pattern: spec.ResourcePattern("*"), Name: "nvidia.com/gpu"}}}
	migStrategy := spec.MigStrategyNone
	names := []string{"NVIDIA H100", "NVIDIA H100", "NVIDIA DGX Display", "NVIDIA H100"}

	for _, failAt := range []string{"DeviceGetHandleByIndex", "GetName"} {
		t.Run(failAt, func(t *testing.T) {
			nvmllib, devicelib := realDeviceLib(names, 1, failAt)
			b := deviceMapBuilder{
				Interface:    newUsableDevices(devicelib, nvmllib),
				migStrategy:  &migStrategy,
				resources:    resources,
				newGPUDevice: newFakeGPUDevice,
			}

			devices, err := b.buildDeviceMapFromConfigResources()
			require.NoError(t, err)

			// GPU 1 is lost and GPU 2 is excluded by model name, as go-nvlib does.
			var indices []string
			for _, dev := range devices["nvidia.com/gpu"] {
				indices = append(indices, dev.Index)
			}
			require.ElementsMatch(t, []string{"0", "3"}, indices)
		})
	}
}

// recoveringGPU fails its MIG mode query the first time only, as a GPU that
// recovers between the GPU walk and the MIG walk of one build.
type recoveringGPU struct {
	fakeGPU
	calls *int
}

func (g recoveringGPU) IsMigCapable() (bool, error) {
	*g.calls++
	if *g.calls == 1 {
		return false, fmt.Errorf("error getting MIG mode: %v", nvml.ERROR_RESET_REQUIRED)
	}
	return false, nil
}

func (g recoveringGPU) GetMigMode() (int, int, nvml.Return) {
	if *g.calls <= 1 {
		return 0, 0, nvml.ERROR_RESET_REQUIRED
	}
	return 0, 0, nvml.ERROR_NOT_SUPPORTED
}

type recoveringDeviceLib struct {
	device.Interface
	gpus []device.Device
}

func (l recoveringDeviceLib) VisitDevices(visit func(int, device.Device) error) error {
	for i, gpu := range l.gpus {
		if err := visit(i, gpu); err != nil {
			return err
		}
	}
	return nil
}

// A GPU skipped by the GPU walk stays skipped in the MIG walk of the same build.
func TestUsableDevicesKeepsSkipAcrossWalks(t *testing.T) {
	calls := 0
	healthy := fakeGPU{migModeRet: nvml.ERROR_NOT_SUPPORTED}
	u := newUsableDevices(recoveringDeviceLib{gpus: []device.Device{healthy, recoveringGPU{fakeGPU: healthy, calls: &calls}}}, nil)

	var first, second []int
	require.NoError(t, u.VisitDevices(func(i int, _ device.Device) error { first = append(first, i); return nil }))
	require.NoError(t, u.VisitDevices(func(i int, _ device.Device) error { second = append(second, i); return nil }))

	require.Equal(t, []int{0}, first)
	require.Equal(t, []int{0}, second)
}

// When every GPU is unusable there is nothing to serve, so building the device
// map fails instead of returning an empty map (the plugin would otherwise wait
// with "No devices found").
func TestDeviceMapFailsWhenNoGPUIsUsable(t *testing.T) {
	resources := &spec.Resources{GPUs: []spec.Resource{{Pattern: spec.ResourcePattern("*"), Name: "nvidia.com/gpu"}}}
	migStrategy := spec.MigStrategyNone
	nvmllib, devicelib := realDeviceLib([]string{"NVIDIA H100"}, 0, "DeviceGetHandleByIndex")
	b := deviceMapBuilder{
		Interface:    newUsableDevices(devicelib, nvmllib),
		migStrategy:  &migStrategy,
		resources:    resources,
		newGPUDevice: newFakeGPUDevice,
	}

	_, err := b.buildDeviceMapFromConfigResources()

	require.ErrorContains(t, err, "GPU at index 0 is unusable")
}

// The walk over a devicelib without NVML follows the same rule.
func TestUsableDevicesWithoutNVMLFailsWhenNoGPUIsUsable(t *testing.T) {
	lost := fakeGPU{migModeRet: nvml.ERROR_GPU_IS_LOST}
	u := newUsableDevices(fakeDeviceLib{gpus: []fakeGPU{lost, lost}}, nil)

	err := u.VisitDevices(func(int, device.Device) error { return nil })

	require.ErrorContains(t, err, "GPU at index 1 is unusable")
}
