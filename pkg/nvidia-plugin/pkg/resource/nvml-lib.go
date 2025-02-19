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
	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

type nvmlLib struct {
	nvml.Interface
	devicelib device.Interface
}

// NewNVMLManager creates a new manager that uses NVML to query and manage devices
func NewNVMLManager(nvmllib nvml.Interface, devicelib device.Interface) Manager {
	m := nvmlLib{
		Interface: nvmllib,
		devicelib: devicelib,
	}
	return m
}

// GetCudaDriverVersion : Return the cuda v using NVML
func (l nvmlLib) GetCudaDriverVersion() (int, int, error) {
	v, ret := l.Interface.SystemGetCudaDriverVersion()
	if ret != nvml.SUCCESS {
		return 0, 0, ret
	}
	major := v / 1000
	minor := v % 1000 / 10

	return major, minor, nil
}

// GetDevices returns the NVML devices for the manager
func (l nvmlLib) GetDevices() ([]Device, error) {
	libdevices, err := l.devicelib.GetDevices()
	if err != nil {
		return nil, err
	}

	var devices []Device
	for _, d := range libdevices {
		device := nvmlDevice{
			Device:    d,
			devicelib: l.devicelib,
		}
		devices = append(devices, device)
	}

	return devices, nil
}

// GetDriverVersion returns the driver version
func (l nvmlLib) GetDriverVersion() (string, error) {
	v, ret := l.Interface.SystemGetDriverVersion()
	if ret != nvml.SUCCESS {
		return "", ret
	}
	return v, nil
}

// Init initialises the library
func (l nvmlLib) Init() error {
	ret := l.Interface.Init()
	if ret != nvml.SUCCESS {
		return ret
	}
	return nil
}

// Shutdown shuts down the library
func (l nvmlLib) Shutdown() error {
	ret := l.Interface.Shutdown()
	if ret != nvml.SUCCESS {
		return ret
	}
	return nil
}
