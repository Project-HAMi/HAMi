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

	"github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/pkg/cuda"
)

type cudaDevice cuda.Device

var _ Device = (*cudaDevice)(nil)

// NewCudaDevice constructs a new CUDA device
func NewCudaDevice(d cuda.Device) Device {
	device := cudaDevice(d)
	return &device
}

// GetAttributes is unsupported for CUDA devices
func (d *cudaDevice) GetAttributes() (map[string]interface{}, error) {
	return nil, fmt.Errorf("GetAttributes is not supported for CUDA devices")
}

// GetCudaComputeCapability returns the CUDA Compute Capability major and minor versions.
// If the device is a MIG device (i.e. a compute instance) these are 0
func (d *cudaDevice) GetCudaComputeCapability() (int, int, error) {
	major, r := cuda.Device(*d).GetAttribute(cuda.COMPUTE_CAPABILITY_MAJOR)
	if r != cuda.SUCCESS {
		return 0, 0, fmt.Errorf("failed to get CUDA compute capability major for device: result=%v", r)
	}

	minor, r := cuda.Device(*d).GetAttribute(cuda.COMPUTE_CAPABILITY_MINOR)
	if r != cuda.SUCCESS {
		return 0, 0, fmt.Errorf("failed to get CUDA compute capability minor for device: result=%v", r)
	}

	return major, minor, nil
}

// GetDeviceHandleFromMigDeviceHandle is unsupported for CUDA devices
func (d *cudaDevice) GetDeviceHandleFromMigDeviceHandle() (Device, error) {
	return nil, fmt.Errorf("GetDeviceHandleFromMigDeviceHandle is unsupported for CUDA devices")
}

// GetTotalMemoryMB returns the total memory for a device
func (d *cudaDevice) GetTotalMemoryMB() (uint64, error) {
	total, r := cuda.Device(*d).TotalMem()
	if r != cuda.SUCCESS {
		return 0, fmt.Errorf("failed to get memory info for device: %v", r)
	}
	return total / (1024 * 1024), nil
}

// GetMigDevices is unsupported for CUDA devices
func (d *cudaDevice) GetMigDevices() ([]Device, error) {
	return nil, fmt.Errorf("GetMigDevices is unsupported for CUDA devices")
}

// GetName returns the device name / model.
func (d *cudaDevice) GetName() (string, error) {
	name, r := cuda.Device(*d).GetName()
	if r != cuda.SUCCESS {
		return "", fmt.Errorf("failed to get device name: %v", r)
	}

	return name, nil
}

// GetUUID is unsupported for CUDA devices
func (d *cudaDevice) GetUUID() (string, error) {
	return "", fmt.Errorf("GetUUID is unsupported for CUDA devices")
}

// IsMigCapable always returns false for CUDA devices
func (d *cudaDevice) IsMigCapable() (bool, error) {
	return false, nil
}

// IsMigEnabled always returns false for CUDA devices
func (d *cudaDevice) IsMigEnabled() (bool, error) {
	return false, nil
}

func (d *cudaDevice) GetPCIClass() (uint32, error) {
	return 0, nil
}

func (d *cudaDevice) IsFabricAttached() (bool, error) {
	return false, nil
}

func (d *cudaDevice) GetFabricIDs() (string, string, error) {
	return "", "", fmt.Errorf("GetFabricIDs is not supported for CUDA devices")
}
