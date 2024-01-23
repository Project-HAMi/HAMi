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

type gdslib nvcdilib

var _ Interface = (*gdslib)(nil)

// GetAllDeviceSpecs returns the device specs for all available devices.
func (l *gdslib) GetAllDeviceSpecs() ([]specs.Device, error) {
	discoverer, err := discover.NewGDSDiscoverer(l.logger, l.driverRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to create GPUDirect Storage discoverer: %v", err)
	}
	edits, err := edits.FromDiscoverer(discoverer)
	if err != nil {
		return nil, fmt.Errorf("failed to create container edits for GPUDirect Storage: %v", err)
	}

	deviceSpec := specs.Device{
		Name:           "all",
		ContainerEdits: *edits.ContainerEdits,
	}

	return []specs.Device{deviceSpec}, nil
}

// GetCommonEdits generates a CDI specification that can be used for ANY devices
func (l *gdslib) GetCommonEdits() (*cdi.ContainerEdits, error) {
	return edits.FromDiscoverer(discover.None{})
}

// GetSpec is unsppported for the gdslib specs.
// gdslib is typically wrapped by a spec that implements GetSpec.
func (l *gdslib) GetSpec() (spec.Interface, error) {
	return nil, fmt.Errorf("GetSpec is not supported")
}

// GetGPUDeviceEdits is unsupported for the gdslib specs
func (l *gdslib) GetGPUDeviceEdits(device.Device) (*cdi.ContainerEdits, error) {
	return nil, fmt.Errorf("GetGPUDeviceEdits is not supported")
}

// GetGPUDeviceSpecs is unsupported for the gdslib specs
func (l *gdslib) GetGPUDeviceSpecs(int, device.Device) (*specs.Device, error) {
	return nil, fmt.Errorf("GetGPUDeviceSpecs is not supported")
}

// GetMIGDeviceEdits is unsupported for the gdslib specs
func (l *gdslib) GetMIGDeviceEdits(device.Device, device.MigDevice) (*cdi.ContainerEdits, error) {
	return nil, fmt.Errorf("GetMIGDeviceEdits is not supported")
}

// GetMIGDeviceSpecs is unsupported for the gdslib specs
func (l *gdslib) GetMIGDeviceSpecs(int, device.Device, int, device.MigDevice) (*specs.Device, error) {
	return nil, fmt.Errorf("GetMIGDeviceSpecs is not supported")
}
