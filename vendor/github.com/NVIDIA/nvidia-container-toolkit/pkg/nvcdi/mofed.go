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
	"github.com/NVIDIA/nvidia-container-toolkit/pkg/nvcdi/spec"
	"tags.cncf.io/container-device-interface/pkg/cdi"
	"tags.cncf.io/container-device-interface/specs-go"
)

type mofedlib nvcdilib

var _ Interface = (*mofedlib)(nil)

// GetAllDeviceSpecs returns the device specs for all available devices.
func (l *mofedlib) GetAllDeviceSpecs() ([]specs.Device, error) {
	discoverer, err := discover.NewMOFEDDiscoverer(l.logger, l.driverRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to create MOFED discoverer: %v", err)
	}
	edits, err := edits.FromDiscoverer(discoverer)
	if err != nil {
		return nil, fmt.Errorf("failed to create container edits for MOFED devices: %v", err)
	}

	deviceSpec := specs.Device{
		Name:           "all",
		ContainerEdits: *edits.ContainerEdits,
	}

	return []specs.Device{deviceSpec}, nil
}

// GetCommonEdits generates a CDI specification that can be used for ANY devices
func (l *mofedlib) GetCommonEdits() (*cdi.ContainerEdits, error) {
	return edits.FromDiscoverer(discover.None{})
}

// GetSpec is unsppported for the mofedlib specs.
// mofedlib is typically wrapped by a spec that implements GetSpec.
func (l *mofedlib) GetSpec() (spec.Interface, error) {
	return nil, fmt.Errorf("GetSpec is not supported")
}

// GetGPUDeviceEdits is unsupported for the mofedlib specs
func (l *mofedlib) GetGPUDeviceEdits(device.Device) (*cdi.ContainerEdits, error) {
	return nil, fmt.Errorf("GetGPUDeviceEdits is not supported")
}

// GetGPUDeviceSpecs is unsupported for the mofedlib specs
func (l *mofedlib) GetGPUDeviceSpecs(int, device.Device) (*specs.Device, error) {
	return nil, fmt.Errorf("GetGPUDeviceSpecs is not supported")
}

// GetMIGDeviceEdits is unsupported for the mofedlib specs
func (l *mofedlib) GetMIGDeviceEdits(device.Device, device.MigDevice) (*cdi.ContainerEdits, error) {
	return nil, fmt.Errorf("GetMIGDeviceEdits is not supported")
}

// GetMIGDeviceSpecs is unsupported for the mofedlib specs
func (l *mofedlib) GetMIGDeviceSpecs(int, device.Device, int, device.MigDevice) (*specs.Device, error) {
	return nil, fmt.Errorf("GetMIGDeviceSpecs is not supported")
}
