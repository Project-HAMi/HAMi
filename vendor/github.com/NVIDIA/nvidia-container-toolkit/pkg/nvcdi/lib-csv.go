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
	"github.com/NVIDIA/nvidia-container-toolkit/internal/discover"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/edits"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/platform-support/tegra"
	"github.com/NVIDIA/nvidia-container-toolkit/pkg/nvcdi/spec"
	"tags.cncf.io/container-device-interface/pkg/cdi"
	"tags.cncf.io/container-device-interface/specs-go"
)

type csvlib nvcdilib

var _ Interface = (*csvlib)(nil)

// GetSpec should not be called for wsllib
func (l *csvlib) GetSpec() (spec.Interface, error) {
	return nil, fmt.Errorf("Unexpected call to csvlib.GetSpec()")
}

// GetAllDeviceSpecs returns the device specs for all available devices.
func (l *csvlib) GetAllDeviceSpecs() ([]specs.Device, error) {
	d, err := tegra.New(
		tegra.WithLogger(l.logger),
		tegra.WithDriverRoot(l.driverRoot),
		tegra.WithNVIDIACTKPath(l.nvidiaCTKPath),
		tegra.WithCSVFiles(l.csvFiles),
		tegra.WithLibrarySearchPaths(l.librarySearchPaths...),
		tegra.WithIngorePatterns(l.csvIgnorePatterns...),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create discoverer for CSV files: %v", err)
	}
	e, err := edits.FromDiscoverer(d)
	if err != nil {
		return nil, fmt.Errorf("failed to create container edits for CSV files: %v", err)
	}

	name, err := l.deviceNamer.GetDeviceName(0, uuidUnsupported{})
	if err != nil {
		return nil, fmt.Errorf("failed to get device name: %v", err)
	}

	deviceSpec := specs.Device{
		Name:           name,
		ContainerEdits: *e.ContainerEdits,
	}
	return []specs.Device{deviceSpec}, nil
}

// GetCommonEdits generates a CDI specification that can be used for ANY devices
func (l *csvlib) GetCommonEdits() (*cdi.ContainerEdits, error) {
	d := discover.None{}
	return edits.FromDiscoverer(d)
}

// GetGPUDeviceEdits generates a CDI specification that can be used for GPU devices
func (l *csvlib) GetGPUDeviceEdits(device.Device) (*cdi.ContainerEdits, error) {
	return nil, fmt.Errorf("GetGPUDeviceEdits is not supported for CSV files")
}

// GetGPUDeviceSpecs returns the CDI device specs for the full GPU represented by 'device'.
func (l *csvlib) GetGPUDeviceSpecs(i int, d device.Device) (*specs.Device, error) {
	return nil, fmt.Errorf("GetGPUDeviceSpecs is not supported for CSV files")
}

// GetMIGDeviceEdits generates a CDI specification that can be used for MIG devices
func (l *csvlib) GetMIGDeviceEdits(device.Device, device.MigDevice) (*cdi.ContainerEdits, error) {
	return nil, fmt.Errorf("GetMIGDeviceEdits is not supported for CSV files")
}

// GetMIGDeviceSpecs returns the CDI device specs for the full MIG represented by 'device'.
func (l *csvlib) GetMIGDeviceSpecs(int, device.Device, int, device.MigDevice) (*specs.Device, error) {
	return nil, fmt.Errorf("GetMIGDeviceSpecs is not supported for CSV files")
}
