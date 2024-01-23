/*
 * Copyright (c) 2022, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package device

import (
	"fmt"

	"github.com/NVIDIA/go-nvlib/pkg/nvml"
)

// MigDevice defines the set of extended functions associated with a MIG device
type MigDevice interface {
	nvml.Device
	GetProfile() (MigProfile, error)
}

type migdevice struct {
	nvml.Device
	lib     *devicelib
	profile MigProfile
}

var _ MigDevice = &migdevice{}

// NewMigDevice builds a new MigDevice from an nvml.Device
func (d *devicelib) NewMigDevice(handle nvml.Device) (MigDevice, error) {
	isMig, ret := handle.IsMigDeviceHandle()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error checking if device is a MIG device: %v", ret)
	}
	if !isMig {
		return nil, fmt.Errorf("not a MIG device")
	}
	return &migdevice{handle, d, nil}, nil
}

// NewMigDeviceByUUID builds a new MigDevice from a UUID
func (d *devicelib) NewMigDeviceByUUID(uuid string) (MigDevice, error) {
	dev, ret := d.nvml.DeviceGetHandleByUUID(uuid)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting device handle for uuid '%v': %v", uuid, ret)
	}
	return d.NewMigDevice(dev)
}

// GetProfile returns the MIG profile associated with a MIG device
func (m *migdevice) GetProfile() (MigProfile, error) {
	if m.profile != nil {
		return m.profile, nil
	}

	parent, ret := m.Device.GetDeviceHandleFromMigDeviceHandle()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting parent device handle: %v", ret)
	}

	parentMemoryInfo, ret := parent.GetMemoryInfo()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting parent memory info: %v", ret)
	}

	attributes, ret := m.Device.GetAttributes()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting MIG device attributes: %v", ret)
	}

	giID, ret := m.Device.GetGpuInstanceId()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting MIG device GPU Instance ID: %v", ret)
	}

	ciID, ret := m.Device.GetComputeInstanceId()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting MIG device Compute Instance ID: %v", ret)
	}

	gi, ret := parent.GetGpuInstanceById(giID)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting GPU Instance: %v", ret)
	}

	ci, ret := gi.GetComputeInstanceById(ciID)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting Compute Instance: %v", ret)
	}

	giInfo, ret := gi.GetInfo()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting GPU Instance info: %v", ret)
	}

	ciInfo, ret := ci.GetInfo()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting Compute Instance info: %v", ret)
	}

	for i := 0; i < nvml.GPU_INSTANCE_PROFILE_COUNT; i++ {
		giProfileInfo, ret := parent.GetGpuInstanceProfileInfo(i)
		if ret == nvml.ERROR_NOT_SUPPORTED {
			continue
		}
		if ret == nvml.ERROR_INVALID_ARGUMENT {
			continue
		}
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("error getting GPU Instance profile info: %v", ret)
		}

		if giProfileInfo.Id != giInfo.ProfileId {
			continue
		}

		for j := 0; j < nvml.COMPUTE_INSTANCE_PROFILE_COUNT; j++ {
			for k := 0; k < nvml.COMPUTE_INSTANCE_ENGINE_PROFILE_COUNT; k++ {
				ciProfileInfo, ret := gi.GetComputeInstanceProfileInfo(j, k)
				if ret == nvml.ERROR_NOT_SUPPORTED {
					continue
				}
				if ret == nvml.ERROR_INVALID_ARGUMENT {
					continue
				}
				if ret != nvml.SUCCESS {
					return nil, fmt.Errorf("error getting Compute Instance profile info: %v", ret)

				}

				if ciProfileInfo.Id != ciInfo.ProfileId {
					continue
				}

				p, err := m.lib.NewMigProfile(i, j, k, attributes.MemorySizeMB, parentMemoryInfo.Total)
				if err != nil {
					return nil, fmt.Errorf("error creating MIG profile: %v", err)
				}

				m.profile = p
				return p, nil
			}
		}
	}

	return nil, fmt.Errorf("no matching profile IDs found")
}
