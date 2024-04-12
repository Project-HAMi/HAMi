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

type gdsDeviceDiscoverer struct {
	None
	logger  logger.Interface
	devices Discover
	mounts  Discover
}

// NewGDSDiscoverer creates a discoverer for GPUDirect Storage devices and mounts.
func NewGDSDiscoverer(logger logger.Interface, root string) (Discover, error) {
	devices := NewCharDeviceDiscoverer(
		logger,
		[]string{"/dev/nvidia-fs*"},
		root,
	)

	udev := NewMounts(
		logger,
		lookup.NewDirectoryLocator(lookup.WithLogger(logger), lookup.WithRoot(root)),
		root,
		[]string{"/run/udev"},
	)

	cufile := NewMounts(
		logger,
		lookup.NewFileLocator(
			lookup.WithLogger(logger),
			lookup.WithRoot(root),
		),
		root,
		[]string{"/etc/cufile.json"},
	)

	d := gdsDeviceDiscoverer{
		logger:  logger,
		devices: devices,
		mounts:  Merge(udev, cufile),
	}

	return &d, nil
}

// Devices discovers the nvidia-fs device nodes for use with GPUDirect Storage
func (d *gdsDeviceDiscoverer) Devices() ([]Device, error) {
	return d.devices.Devices()
}

// Mounts discovers the required mounts for GPUDirect Storage.
// If no devices are discovered the discovered mounts are empty
func (d *gdsDeviceDiscoverer) Mounts() ([]Mount, error) {
	devices, err := d.Devices()
	if err != nil || len(devices) == 0 {
		d.logger.Debugf("No nvidia-fs devices detected; skipping detection of mounts")
		return nil, nil
	}

	return d.mounts.Mounts()
}
