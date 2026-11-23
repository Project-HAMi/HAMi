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

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

// IsUnusableGPUReturn reports an NVML result that means the GPU cannot be used
// until it recovers or is reset: it fell off the bus (Xid 79, reported as
// GPU_IS_LOST or often as UNKNOWN), needs a reset after a MIG mode change that
// could not be activated, lacks power, or has an interrupt problem.
func IsUnusableGPUReturn(ret nvml.Return) bool {
	switch ret {
	case nvml.ERROR_GPU_IS_LOST, nvml.ERROR_UNKNOWN, nvml.ERROR_RESET_REQUIRED,
		nvml.ERROR_INSUFFICIENT_POWER, nvml.ERROR_IRQ_ISSUE:
		return true
	default:
		return false
	}
}

// skippedDeviceNames are the models go-nvlib's devicelib leaves out by default
// (device.New without WithSkippedDevices, as the device plugin uses it).
var skippedDeviceNames = map[string]struct{}{
	"DGX Display":        {},
	"NVIDIA DGX Display": {},
}

// UnusableGPUReason returns the NVML result that makes the GPU unusable. It only
// queries the MIG mode directly when IsMigCapable fails: go-nvlib reports that
// failure as text, and it checks that the driver has nvmlDeviceGetMigMode first.
func UnusableGPUReason(gpu device.Device) (nvml.Return, bool) {
	if _, err := gpu.IsMigCapable(); err == nil {
		return nvml.SUCCESS, false
	}
	_, _, ret := gpu.GetMigMode()
	return ret, IsUnusableGPUReturn(ret)
}

// VisitUsableDevices visits the GPUs like go-nvlib's VisitDevices, with the same
// indices and model exclusions, but skips a GPU that NVML reports as unusable
// and calls onSkip for it instead of failing the walk. It walks NVML itself,
// because go-nvlib's VisitDevices stops at a GPU whose handle lookup or name
// query fails before the callback runs. Other NVML errors still fail the walk,
// and so does a walk in which every GPU is unusable.
func VisitUsableDevices(nvmllib nvml.Interface, devicelib device.Interface, visit func(int, device.Device) error, onSkip func(int, nvml.Return)) error {
	count, ret := nvmllib.DeviceGetCount()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("error getting device count: %v", ret)
	}

	usable := 0
	var lastErr error
	skip := func(i int, ret nvml.Return) {
		lastErr = fmt.Errorf("GPU at index %d is unusable: %v", i, ret)
		if onSkip != nil {
			onSkip(i, ret)
		}
	}

	for i := 0; i < count; i++ {
		handle, ret := nvmllib.DeviceGetHandleByIndex(i)
		if IsUnusableGPUReturn(ret) {
			skip(i, ret)
			continue
		}
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting device handle for index '%v': %v", i, ret)
		}
		gpu, err := devicelib.NewDevice(handle)
		if err != nil {
			return fmt.Errorf("error creating new device wrapper: %v", err)
		}
		name, ret := gpu.GetName()
		if IsUnusableGPUReturn(ret) {
			skip(i, ret)
			continue
		}
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting device name for index '%v': %v", i, ret)
		}
		if _, excluded := skippedDeviceNames[name]; excluded {
			continue
		}
		if ret, unusable := UnusableGPUReason(gpu); unusable {
			skip(i, ret)
			continue
		}
		usable++
		if err := visit(i, gpu); err != nil {
			return fmt.Errorf("error visiting device: %v", err)
		}
	}

	if usable == 0 && lastErr != nil {
		return lastErr
	}
	return nil
}
