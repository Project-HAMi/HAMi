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
	"github.com/NVIDIA/go-nvlib/pkg/nvml"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/edits"
	"github.com/NVIDIA/nvidia-container-toolkit/pkg/nvcdi/spec"
	"tags.cncf.io/container-device-interface/pkg/cdi"
	"tags.cncf.io/container-device-interface/specs-go"
)

type nvmllib nvcdilib

var _ Interface = (*nvmllib)(nil)

// GetSpec should not be called for nvmllib
func (l *nvmllib) GetSpec() (spec.Interface, error) {
	return nil, fmt.Errorf("Unexpected call to nvmllib.GetSpec()")
}

// GetAllDeviceSpecs returns the device specs for all available devices.
func (l *nvmllib) GetAllDeviceSpecs() ([]specs.Device, error) {
	var deviceSpecs []specs.Device

	if r := l.nvmllib.Init(); r != nvml.SUCCESS {
		return nil, fmt.Errorf("failed to initialize NVML: %v", r)
	}
	defer func() {
		if r := l.nvmllib.Shutdown(); r != nvml.SUCCESS {
			l.logger.Warningf("failed to shutdown NVML: %v", r)
		}
	}()

	gpuDeviceSpecs, err := l.getGPUDeviceSpecs()
	if err != nil {
		return nil, err
	}
	deviceSpecs = append(deviceSpecs, gpuDeviceSpecs...)

	migDeviceSpecs, err := l.getMigDeviceSpecs()
	if err != nil {
		return nil, err
	}
	deviceSpecs = append(deviceSpecs, migDeviceSpecs...)

	return deviceSpecs, nil
}

// GetCommonEdits generates a CDI specification that can be used for ANY devices
func (l *nvmllib) GetCommonEdits() (*cdi.ContainerEdits, error) {
	common, err := newCommonNVMLDiscoverer(l.logger, l.driverRoot, l.nvidiaCTKPath, l.nvmllib)
	if err != nil {
		return nil, fmt.Errorf("failed to create discoverer for common entities: %v", err)
	}

	return edits.FromDiscoverer(common)
}

func (l *nvmllib) getGPUDeviceSpecs() ([]specs.Device, error) {
	var deviceSpecs []specs.Device
	err := l.devicelib.VisitDevices(func(i int, d device.Device) error {
		deviceSpec, err := l.GetGPUDeviceSpecs(i, d)
		if err != nil {
			return err
		}
		deviceSpecs = append(deviceSpecs, *deviceSpec)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate CDI edits for GPU devices: %v", err)
	}
	return deviceSpecs, err
}

func (l *nvmllib) getMigDeviceSpecs() ([]specs.Device, error) {
	var deviceSpecs []specs.Device
	err := l.devicelib.VisitMigDevices(func(i int, d device.Device, j int, mig device.MigDevice) error {
		deviceSpec, err := l.GetMIGDeviceSpecs(i, d, j, mig)
		if err != nil {
			return err
		}
		deviceSpecs = append(deviceSpecs, *deviceSpec)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate CDI edits for GPU devices: %v", err)
	}
	return deviceSpecs, err
}
