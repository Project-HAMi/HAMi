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
	"path/filepath"

	"github.com/NVIDIA/nvidia-container-toolkit/internal/discover"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/logger"
)

type deviceFolderPermissions struct {
	logger        logger.Interface
	driverRoot    string
	nvidiaCTKPath string
	devices       discover.Discover
}

var _ discover.Discover = (*deviceFolderPermissions)(nil)

// newDeviceFolderPermissionHookDiscoverer creates a discoverer that can be used to update the permissions for the parent folders of nested device nodes from the specified set of device specs.
// This works around an issue with rootless podman when using crun as a low-level runtime.
// See https://github.com/containers/crun/issues/1047
// The nested devices that are applicable to the NVIDIA GPU devices are:
//   - DRM devices at /dev/dri/*
//   - NVIDIA Caps devices at /dev/nvidia-caps/*
func newDeviceFolderPermissionHookDiscoverer(logger logger.Interface, driverRoot string, nvidiaCTKPath string, devices discover.Discover) discover.Discover {
	d := &deviceFolderPermissions{
		logger:        logger,
		driverRoot:    driverRoot,
		nvidiaCTKPath: nvidiaCTKPath,
		devices:       devices,
	}

	return d
}

// Devices are empty for this discoverer
func (d *deviceFolderPermissions) Devices() ([]discover.Device, error) {
	return nil, nil
}

// Hooks returns a set of hooks that sets the file mode to 755 of parent folders for nested device nodes.
func (d *deviceFolderPermissions) Hooks() ([]discover.Hook, error) {
	folders, err := d.getDeviceSubfolders()
	if err != nil {
		return nil, fmt.Errorf("failed to get device subfolders: %v", err)
	}
	if len(folders) == 0 {
		return nil, nil
	}

	args := []string{"--mode", "755"}
	for _, folder := range folders {
		args = append(args, "--path", folder)
	}

	hook := discover.CreateNvidiaCTKHook(
		d.nvidiaCTKPath,
		"chmod",
		args...,
	)

	return []discover.Hook{hook}, nil
}

func (d *deviceFolderPermissions) getDeviceSubfolders() ([]string, error) {
	// For now we only consider the following special case paths
	allowedPaths := map[string]bool{
		"/dev/dri":         true,
		"/dev/nvidia-caps": true,
	}

	devices, err := d.devices.Devices()
	if err != nil {
		return nil, fmt.Errorf("failed to get devices: %v", err)
	}

	var folders []string
	seen := make(map[string]bool)
	for _, device := range devices {
		df := filepath.Dir(device.Path)
		if seen[df] {
			continue
		}
		// We only consider the special case paths
		if !allowedPaths[df] {
			continue
		}
		folders = append(folders, df)
		seen[df] = true
		if len(folders) == len(allowedPaths) {
			break
		}
	}

	return folders, nil
}

// Mounts are empty for this discoverer
func (d *deviceFolderPermissions) Mounts() ([]discover.Mount, error) {
	return nil, nil
}
