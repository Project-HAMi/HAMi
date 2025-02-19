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
	"github.com/NVIDIA/go-nvlib/pkg/nvpci"
	"k8s.io/klog/v2"
)

type vfioLib struct {
	nvpcilib nvpci.Interface
}

// NewVfioManager returns an resource manger for devices with VFIO PCI driver
func NewVfioManager() Manager {
	nvpcilib := nvpci.New()
	manager := vfioLib{
		nvpcilib: nvpcilib,
	}
	return &manager
}

// Init is a no-op for the vfio manager
func (l *vfioLib) Init() error {
	return nil
}

// Shutdown is a no-op for the vfio manager
func (l *vfioLib) Shutdown() (err error) {
	return nil
}

// GetDevices returns the devices with VFIO PCI driver available on the system
func (l *vfioLib) GetDevices() ([]Device, error) {
	var devices []Device
	nvdevices, err := l.nvpcilib.GetGPUs()
	if err != nil {
		return nil, err
	}

	for _, dev := range nvdevices {
		if dev.Driver == "vfio-pci" {
			vfioDev := vfioDevice{dev}
			devices = append(devices, vfioDev)
		} else {
			klog.Infof("Device not bound to 'vfio-pci'; device: %s driver: '%s'", dev.Address, dev.Driver)
		}
	}
	return devices, nil
}

// GetCudaDriverVersion is not supported
func (l *vfioLib) GetCudaDriverVersion() (int, int, error) {
	return 0, 0, nil
}

// GetDriverVersion is not supported
func (l *vfioLib) GetDriverVersion() (string, error) {
	return "unknown.unknown.unknown", nil
}
