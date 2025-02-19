/**
# Copyright (c) 2024, NVIDIA CORPORATION.  All rights reserved.
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

	"github.com/NVIDIA/go-nvlib/pkg/nvpci"
)

type vfioDevice struct {
	nvidiaPCIDevice *nvpci.NvidiaPCIDevice
}

// GetMigDevices returns the list of MIG devices configured on this device
func (d vfioDevice) GetMigDevices() ([]Device, error) {
	return nil, nil
}

// GetCudaComputeCapability is not supported for GPU devices with vfio pci driver.
func (d vfioDevice) GetCudaComputeCapability() (int, int, error) {
	return -1, -1, nil
}

// GetAttributes is only supported for MIG devices.
func (d vfioDevice) GetAttributes() (map[string]interface{}, error) {
	return nil, fmt.Errorf("GetAttributes is not supported for non-MIG devices")
}

// GetDeviceHandleFromMigDeviceHandle is only supported for MIG devices
func (d vfioDevice) GetDeviceHandleFromMigDeviceHandle() (Device, error) {
	return nil, fmt.Errorf("GetDeviceHandleFromMigDeviceHandle is not supported for non-MIG devices")
}

// GetName returns the device name / model.
func (d vfioDevice) GetName() (string, error) {
	return d.nvidiaPCIDevice.DeviceName, nil
}

// GetTotalMemoryMB returns the total memory on a device in MB
func (d vfioDevice) GetTotalMemoryMB() (uint64, error) {
	_, val := d.nvidiaPCIDevice.Resources.GetTotalAddressableMemory(true)
	return val, nil
}

func (d vfioDevice) IsMigEnabled() (bool, error) {
	return false, nil
}

func (d vfioDevice) IsMigCapable() (bool, error) {
	return false, nil
}

func (d vfioDevice) GetPCIClass() (uint32, error) {
	return d.nvidiaPCIDevice.Class, nil
}

func (d vfioDevice) IsFabricAttached() (bool, error) {
	return false, nil
}
func (d vfioDevice) GetFabricIDs() (string, string, error) {
	return "", "", fmt.Errorf("GetFabricIDs is not supported for vfio devices")
}
