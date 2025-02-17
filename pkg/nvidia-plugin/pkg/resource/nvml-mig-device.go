/**
# Copyright (c) 2022, NVIDIA CORPORATION.  All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
**/

package resource

import (
	"fmt"
	"strings"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvlib/pkg/nvpci"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

type nvmlMigDevice struct {
	device.MigDevice
	devicelib device.Interface
}

var _ Device = (*nvmlMigDevice)(nil)

// GetAttributes is only supported for MIG devices.
func (d nvmlMigDevice) GetAttributes() (map[string]interface{}, error) {
	attributes, ret := d.MigDevice.GetAttributes()
	if ret != nvml.SUCCESS {
		return nil, ret
	}
	a := map[string]interface{}{
		"memory":          attributes.MemorySizeMB,
		"multiprocessors": attributes.MultiprocessorCount,
		"slices.gi":       attributes.GpuInstanceSliceCount,
		"slices.ci":       attributes.ComputeInstanceSliceCount,
		"engines.copy":    attributes.SharedCopyEngineCount,
		"engines.decoder": attributes.SharedDecoderCount,
		"engines.encoder": attributes.SharedEncoderCount,
		"engines.jpeg":    attributes.SharedJpegCount,
		"engines.ofa":     attributes.SharedOfaCount,
	}

	return a, nil
}

// GetDeviceHandleFromMigDeviceHandle is only supported for MIG devices
func (d nvmlMigDevice) GetDeviceHandleFromMigDeviceHandle() (Device, error) {
	p, ret := d.MigDevice.GetDeviceHandleFromMigDeviceHandle()
	if ret != nvml.SUCCESS {
		return nil, ret
	}

	device, err := d.devicelib.NewDevice(p)
	if err != nil {
		return nil, fmt.Errorf("failed to construct device: %v", err)
	}

	parent := nvmlDevice{
		Device:    device,
		devicelib: d.devicelib,
	}
	return parent, nil
}

// IsMigCapable is not supported for MIG devices
func (d nvmlMigDevice) IsMigCapable() (bool, error) {
	return false, fmt.Errorf("IsMigCapable is not supported for MIG devices")
}

// IsMigEnabled is not supported for MIG devices
func (d nvmlMigDevice) IsMigEnabled() (bool, error) {
	return false, fmt.Errorf("IsMigEnabled is not supported for MIG devices")
}

// GetMigDevices is not supported for MIG devices
func (d nvmlMigDevice) GetMigDevices() ([]Device, error) {
	return nil, fmt.Errorf("GetMigDevices is not implemented for MIG devices")
}

// GetCudaComputeCapability is not supported for MIG devices
func (d nvmlMigDevice) GetCudaComputeCapability() (int, int, error) {
	return 0, 0, fmt.Errorf("GetCudaComputeCapability is not supported for MIG devices")
}

// GetName returns the name of the nvmlMigDevice.
// This is equal to the mig profile.
func (d nvmlMigDevice) GetName() (string, error) {
	p, err := d.MigDevice.GetProfile()
	if err != nil {
		return "", fmt.Errorf("failed to get MIG profile: %v", err)
	}

	resourceName := strings.ReplaceAll(p.String(), "+", ".")
	return resourceName, nil
}

// GetTotalMemoryMB returns the total memory on a device in MB
func (d nvmlMigDevice) GetTotalMemoryMB() (uint64, error) {
	attr, err := d.GetAttributes()
	if err != nil {
		return 0, err
	}

	total, err := totalMemory(attr)
	if err != nil {
		return 0, err
	}
	return total, nil
}

func totalMemory(attr map[string]interface{}) (uint64, error) {
	totalMemory, ok := attr["memory"]
	if !ok {
		return 0, fmt.Errorf("no 'memory' attribute available")
	}

	switch totalMemory := totalMemory.(type) {
	case uint64:
		return totalMemory, nil
	case int:
		if totalMemory < 0 {
			return 0, fmt.Errorf("unexpected memory value %v", totalMemory)
		}
		//nolint:gosec  // Here we are sure that the value will fit in memory and be positive.
		return uint64(totalMemory), nil
	default:
		return 0, fmt.Errorf("unsupported attribute type %v", totalMemory)
	}
}

func (d nvmlMigDevice) GetPCIClass() (uint32, error) {
	// GPU devices that support MIG do not support switching mode between graphics and compute, so they are always in compute mode.
	return nvpci.PCI3dControllerClass, nil
}

func (d nvmlMigDevice) IsFabricAttached() (bool, error) {
	return false, fmt.Errorf("IsFabricAttached is not supported for MIG devices")
}

func (d nvmlMigDevice) GetFabricIDs() (string, string, error) {
	return "", "", fmt.Errorf("GetFabricIDs is not supported for MIG devices")
}
