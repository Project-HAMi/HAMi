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

	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

// usableDevices wraps a device.Interface so that walks over the GPUs on the
// node skip a GPU that NVML reports as unusable (see nvidia.IsUnusableGPUReturn).
// Without it, one such GPU fails every walk and the plugin cannot build a device
// map for the healthy GPUs either. GPU indices are kept, so device names do not
// change, and a skipped GPU is not advertised until the plugin restarts after it
// recovers. When every GPU is unusable the walk still fails.
//
// When nvmllib is set, VisitDevices walks NVML itself through
// nvidia.VisitUsableDevices, because go-nvlib's walk stops at the first GPU whose
// handle lookup or name query fails, before any callback runs. skipped records
// the GPUs skipped in one device map build, so the GPU walk and the MIG walk of
// that build make the same decision.
type usableDevices struct {
	device.Interface
	nvmllib nvml.Interface
	skipped map[int]struct{}
}

func newUsableDevices(devicelib device.Interface, nvmllib nvml.Interface) *usableDevices {
	return &usableDevices{Interface: devicelib, nvmllib: nvmllib, skipped: make(map[int]struct{})}
}

func (u *usableDevices) skip(i int, ret nvml.Return) {
	if _, done := u.skipped[i]; !done {
		klog.Warningf("Skipping GPU at index %d: %v; it is not advertised until it recovers and the plugin restarts", i, ret)
	}
	u.skipped[i] = struct{}{}
}

// VisitDevices visits each GPU that can be used, with its original index.
func (u *usableDevices) VisitDevices(visit func(int, device.Device) error) error {
	notSkipped := func(i int, gpu device.Device) error {
		if _, done := u.skipped[i]; done {
			return nil
		}
		return visit(i, gpu)
	}
	if u.nvmllib != nil {
		return nvidia.VisitUsableDevices(u.nvmllib, u.Interface, notSkipped, u.skip)
	}
	return u.Interface.VisitDevices(func(i int, gpu device.Device) error {
		if ret, unusable := nvidia.UnusableGPUReason(gpu); unusable {
			u.skip(i, ret)
			return nil
		}
		return notSkipped(i, gpu)
	})
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
