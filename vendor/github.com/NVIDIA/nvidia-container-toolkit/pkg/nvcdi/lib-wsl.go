/**
# Copyright (c) NVIDIA CORPORATION.  All rights reserved.
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

package nvcdi

import (
	"fmt"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/edits"
	"github.com/NVIDIA/nvidia-container-toolkit/pkg/nvcdi/spec"
	"tags.cncf.io/container-device-interface/pkg/cdi"
	"tags.cncf.io/container-device-interface/specs-go"
)

type wsllib nvcdilib

var _ Interface = (*wsllib)(nil)

// GetSpec should not be called for wsllib
func (l *wsllib) GetSpec() (spec.Interface, error) {
	return nil, fmt.Errorf("Unexpected call to wsllib.GetSpec()")
}

// GetAllDeviceSpecs returns the device specs for all available devices.
func (l *wsllib) GetAllDeviceSpecs() ([]specs.Device, error) {
	device := newDXGDeviceDiscoverer(l.logger, l.driverRoot)
	deviceEdits, err := edits.FromDiscoverer(device)
	if err != nil {
		return nil, fmt.Errorf("failed to create container edits for DXG device: %v", err)
	}

	deviceSpec := specs.Device{
		Name:           "all",
		ContainerEdits: *deviceEdits.ContainerEdits,
	}

	return []specs.Device{deviceSpec}, nil
}

// GetCommonEdits generates a CDI specification that can be used for ANY devices
func (l *wsllib) GetCommonEdits() (*cdi.ContainerEdits, error) {
	driver, err := newWSLDriverDiscoverer(l.logger, l.driverRoot, l.nvidiaCTKPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create discoverer for WSL driver: %v", err)
	}

	return edits.FromDiscoverer(driver)
}

// GetGPUDeviceEdits generates a CDI specification that can be used for GPU devices
func (l *wsllib) GetGPUDeviceEdits(device.Device) (*cdi.ContainerEdits, error) {
	return nil, fmt.Errorf("GetGPUDeviceEdits is not supported on WSL")
}

// GetGPUDeviceSpecs returns the CDI device specs for the full GPU represented by 'device'.
func (l *wsllib) GetGPUDeviceSpecs(i int, d device.Device) (*specs.Device, error) {
	return nil, fmt.Errorf("GetGPUDeviceSpecs is not supported on WSL")
}

// GetMIGDeviceEdits generates a CDI specification that can be used for MIG devices
func (l *wsllib) GetMIGDeviceEdits(device.Device, device.MigDevice) (*cdi.ContainerEdits, error) {
	return nil, fmt.Errorf("GetMIGDeviceEdits is not supported on WSL")
}

// GetMIGDeviceSpecs returns the CDI device specs for the full MIG represented by 'device'.
func (l *wsllib) GetMIGDeviceSpecs(int, device.Device, int, device.MigDevice) (*specs.Device, error) {
	return nil, fmt.Errorf("GetMIGDeviceSpecs is not supported on WSL")
}
