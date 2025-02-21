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

package testing

import (
	"fmt"

	"github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/pkg/resource"
)

// DeviceMock provides an alias that allows for additional functions to be defined.
type DeviceMock struct {
	resource.DeviceMock
}

// NewFullGPU creates a device that can be treated as a full GPU for testing
func NewFullGPU() resource.Device {
	return NewDeviceMock(false)
}

// NewMigEnabledDevice creates a GPU with MIG enabled and the specified MIG devices
func NewMigEnabledDevice(migs ...*resource.DeviceMock) resource.Device {
	return NewDeviceMock(true).WithMigDevices(migs...)
}

// NewDeviceMock creates a devices for testing which can have MIG enabled or disabled.
func NewDeviceMock(migEnabled bool) *DeviceMock {
	d := DeviceMock{resource.DeviceMock{
		GetNameFunc: func() (string, error) { return "MOCKMODEL", nil },
		GetCudaComputeCapabilityFunc: func() (int, int, error) {
			if migEnabled {
				return 0, 0, nil
			}
			return 8, 0, nil
		},
		GetTotalMemoryMBFunc: func() (uint64, error) { return uint64(300), nil },
		IsFabricAttachedFunc: func() (bool, error) { return false, nil },
		IsMigEnabledFunc:     func() (bool, error) { return migEnabled, nil },
		IsMigCapableFunc:     func() (bool, error) { return migEnabled, nil },
		GetMigDevicesFunc:    func() ([]resource.Device, error) { return nil, nil },
		GetPCIClassFunc:      func() (uint32, error) { return 0, nil },
	}}
	return &d
}

func NewDeviceWithPCIClassMock(pciClass uint32) *DeviceMock {
	d := DeviceMock{resource.DeviceMock{
		GetPCIClassFunc: func() (uint32, error) { return pciClass, nil },
	}}
	return &d
}

// NewMigDevice creates a MIG devices with the specified attributes for testing
func NewMigDevice(gi int, ci int, gb uint64, attributes ...map[string]interface{}) *resource.DeviceMock {

	defaultAttributes := map[string]interface{}{
		"memory":          gb,
		"multiprocessors": 0,
		"slices.gi":       gi,
		"slices.ci":       ci,
		"engines.copy":    0,
		"engines.decoder": 0,
		"engines.encoder": 0,
		"engines.jpeg":    0,
		"engines.ofa":     0,
	}
	for _, attr := range attributes {
		for a, v := range attr {
			defaultAttributes[a] = v
		}
	}

	return &resource.DeviceMock{
		GetNameFunc:       func() (string, error) { return fmt.Sprintf("%dg.%dgb", gi, gb), nil },
		GetAttributesFunc: func() (map[string]interface{}, error) { return defaultAttributes, nil },
	}
}

// WithMigDevices adds the specified MIG devices to the mocked device
func (d *DeviceMock) WithMigDevices(migs ...*resource.DeviceMock) *DeviceMock {
	for _, m := range migs {
		m.GetDeviceHandleFromMigDeviceHandleFunc = func() (resource.Device, error) {
			return d, nil
		}
	}
	d.GetMigDevicesFunc = func() ([]resource.Device, error) {
		var devices []resource.Device
		for _, m := range migs {
			devices = append(devices, m)
		}
		return devices, nil
	}

	return d
}

// ManagerMock provides an alias that allows for additional functions to be defined.
type ManagerMock struct {
	resource.ManagerMock
}

// NewManagerMockWithDevices creates a mocked manager with the specified devices
func NewManagerMockWithDevices(devices ...resource.Device) *ManagerMock {
	manager := ManagerMock{resource.ManagerMock{
		InitFunc:     func() error { return nil },
		ShutdownFunc: func() error { return nil },
		GetDriverVersionFunc: func() (string, error) {
			return "400.300", nil
		},
		GetDevicesFunc: func() ([]resource.Device, error) {
			return devices, nil
		},
		GetCudaDriverVersionFunc: func() (int, int, error) {
			return 8, 0, nil
		},
	}}
	return &manager
}

// WithErrorOnInit sets the Init function for the ManagerMock to error if called.
func (m *ManagerMock) WithErrorOnInit(err error) *ManagerMock {
	m.InitFunc = func() error {
		fmt.Printf("returning error = %v", err)
		return err
	}
	return m
}
