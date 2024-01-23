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
	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/nvidia-container-toolkit/pkg/nvcdi/spec"
	"tags.cncf.io/container-device-interface/pkg/cdi"
	"tags.cncf.io/container-device-interface/specs-go"
)

const (
	// ModeAuto configures the CDI spec generator to automatically detect the system configuration
	ModeAuto = "auto"
	// ModeNvml configures the CDI spec generator to use the NVML library.
	ModeNvml = "nvml"
	// ModeWsl configures the CDI spec generator to generate a WSL spec.
	ModeWsl = "wsl"
	// ModeManagement configures the CDI spec generator to generate a management spec.
	ModeManagement = "management"
	// ModeGds configures the CDI spec generator to generate a GDS spec.
	ModeGds = "gds"
	// ModeMofed configures the CDI spec generator to generate a MOFED spec.
	ModeMofed = "mofed"
	// ModeCSV configures the CDI spec generator to generate a spec based on the contents of CSV
	// mountspec files.
	ModeCSV = "csv"
)

// Interface defines the API for the nvcdi package
type Interface interface {
	GetSpec() (spec.Interface, error)
	GetCommonEdits() (*cdi.ContainerEdits, error)
	GetAllDeviceSpecs() ([]specs.Device, error)
	GetGPUDeviceEdits(device.Device) (*cdi.ContainerEdits, error)
	GetGPUDeviceSpecs(int, device.Device) (*specs.Device, error)
	GetMIGDeviceEdits(device.Device, device.MigDevice) (*cdi.ContainerEdits, error)
	GetMIGDeviceSpecs(int, device.Device, int, device.MigDevice) (*specs.Device, error)
}
