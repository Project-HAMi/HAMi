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

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"k8s.io/klog/v2"
)

// usableDevices wraps a device.Interface so that walks over the GPUs on the
// node skip a GPU that NVML reports as lost (GPU_IS_LOST or UNKNOWN), needing
// a reset, lacking power or with an interrupt problem. Without it, one such GPU
// fails every walk, and the plugin cannot build a device map for the healthy
// GPUs either. GPU indices are kept, so device names do not change. The skipped
// GPU is not advertised until the plugin restarts after the GPU recovers.
//
// When nvmllib is set, VisitDevices walks NVML itself: go-nvlib's walk stops at
// the first GPU whose handle lookup or GetName fails, before any callback runs,
// so a GPU that fell off the bus could not be skipped through it. skipped
// records the GPUs skipped in one device map build, so the GPU walk and the MIG
// walk of that build make the same decision.
type usableDevices struct {
	device.Interface
	nvmllib nvml.Interface
	skipped map[int]struct{}
}

func newUsableDevices(devicelib device.Interface, nvmllib nvml.Interface) *usableDevices {
	return &usableDevices{Interface: devicelib, nvmllib: nvmllib, skipped: make(map[int]struct{})}
}

// skippedDeviceNames are the models go-nvlib's devicelib leaves out by default
// (device.New without WithSkippedDevices, as cmd/device-plugin uses it).
var skippedDeviceNames = map[string]struct{}{
	"DGX Display":        {},
	"NVIDIA DGX Display": {},
}

func isUnusableGPUReturn(ret nvml.Return) bool {
	switch ret {
	case nvml.ERROR_GPU_IS_LOST, nvml.ERROR_UNKNOWN, nvml.ERROR_RESET_REQUIRED,
		nvml.ERROR_INSUFFICIENT_POWER, nvml.ERROR_IRQ_ISSUE:
		return true
	default:
		return false
	}
}

// unusableGPUReason returns the NVML result that makes the GPU unusable, and
// false for a GPU that can be used. It only queries the MIG mode directly when
// IsMigCapable fails: go-nvlib reports the failure as text, and it checks that
// the driver has nvmlDeviceGetMigMode before it calls it.
func unusableGPUReason(gpu device.Device) (nvml.Return, bool) {
	if _, err := gpu.IsMigCapable(); err == nil {
		return nvml.SUCCESS, false
	}
	_, _, ret := gpu.GetMigMode()
	return ret, isUnusableGPUReturn(ret)
}

func (u *usableDevices) skip(i int, ret nvml.Return) {
	if _, done := u.skipped[i]; !done {
		klog.Warningf("Skipping GPU at index %d: %v; it is not advertised until it recovers and the plugin restarts", i, ret)
	}
	u.skipped[i] = struct{}{}
}

// VisitDevices visits each GPU that can be used, with its original index.
func (u *usableDevices) VisitDevices(visit func(int, device.Device) error) error {
	if u.nvmllib == nil {
		return u.Interface.VisitDevices(func(i int, gpu device.Device) error {
			return u.visitIfUsable(i, gpu, visit)
		})
	}
	count, ret := u.nvmllib.DeviceGetCount()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("error getting device count: %v", ret)
	}
	for i := 0; i < count; i++ {
		if _, done := u.skipped[i]; done {
			continue
		}
		handle, ret := u.nvmllib.DeviceGetHandleByIndex(i)
		if isUnusableGPUReturn(ret) {
			u.skip(i, ret)
			continue
		}
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting device handle for index '%v': %v", i, ret)
		}
		gpu, err := u.Interface.NewDevice(handle)
		if err != nil {
			return fmt.Errorf("error creating new device wrapper: %v", err)
		}
		name, ret := gpu.GetName()
		if isUnusableGPUReturn(ret) {
			u.skip(i, ret)
			continue
		}
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error checking whether device is skipped: error getting device name: %v", ret)
		}
		if _, excluded := skippedDeviceNames[name]; excluded {
			continue
		}
		if err := u.visitIfUsable(i, gpu, visit); err != nil {
			return fmt.Errorf("error visiting device: %v", err)
		}
	}
	return nil
}

func (u *usableDevices) visitIfUsable(i int, gpu device.Device, visit func(int, device.Device) error) error {
	if _, done := u.skipped[i]; done {
		return nil
	}
	if ret, unusable := unusableGPUReason(gpu); unusable {
		u.skip(i, ret)
		return nil
	}
	return visit(i, gpu)
}

// VisitMigDevices visits the MIG devices of each GPU that can be used. It
// replaces the go-nvlib implementation, which walks the GPUs through its own
// VisitDevices and not through the one above.
func (u *usableDevices) VisitMigDevices(visit func(int, device.Device, int, device.MigDevice) error) error {
	err := u.VisitDevices(func(i int, gpu device.Device) error {
		err := gpu.VisitMigDevices(func(j int, mig device.MigDevice) error {
			if err := visit(i, gpu, j, mig); err != nil {
				return fmt.Errorf("error visiting MIG device: %v", err)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("error visiting device: %v", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("error visiting devices: %v", err)
	}
	return nil
}
