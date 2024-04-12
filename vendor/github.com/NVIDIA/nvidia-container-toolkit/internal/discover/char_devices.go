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

package discover

import (
	"github.com/NVIDIA/nvidia-container-toolkit/internal/logger"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/lookup"
)

// charDevices is a discover for a list of character devices
type charDevices mounts

var _ Discover = (*charDevices)(nil)

// NewCharDeviceDiscoverer creates a discoverer which locates the specified set of device nodes.
func NewCharDeviceDiscoverer(logger logger.Interface, devices []string, root string) Discover {
	locator := lookup.NewCharDeviceLocator(
		lookup.WithLogger(logger),
		lookup.WithRoot(root),
	)

	return NewDeviceDiscoverer(logger, locator, root, devices)
}

// NewDeviceDiscoverer creates a discoverer which locates the specified set of device nodes using the specified locator.
func NewDeviceDiscoverer(logger logger.Interface, locator lookup.Locator, root string, devices []string) Discover {
	m := NewMounts(logger, locator, root, devices).(*mounts)

	return (*charDevices)(m)
}

// Mounts returns the discovered mounts for the charDevices.
// Since this explicitly specifies a device list, the mounts are nil.
func (d *charDevices) Mounts() ([]Mount, error) {
	return nil, nil
}

// Devices returns the discovered devices for the charDevices.
// Here the device nodes are first discovered as mounts and these are converted to devices.
func (d *charDevices) Devices() ([]Device, error) {
	devicesAsMounts, err := (*mounts)(d).Mounts()
	if err != nil {
		return nil, err
	}
	var devices []Device
	for _, mount := range devicesAsMounts {
		device := Device{
			HostPath: mount.HostPath,
			Path:     mount.Path,
		}
		devices = append(devices, device)
	}

	return devices, nil
}
