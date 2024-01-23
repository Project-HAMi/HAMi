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

package discover

import (
	"github.com/NVIDIA/nvidia-container-toolkit/internal/logger"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/lookup"
)

type ipcMounts mounts

// NewIPCDiscoverer creats a discoverer for NVIDIA IPC sockets.
func NewIPCDiscoverer(logger logger.Interface, driverRoot string) (Discover, error) {
	sockets := newMounts(
		logger,
		lookup.NewFileLocator(
			lookup.WithLogger(logger),
			lookup.WithRoot(driverRoot),
			lookup.WithSearchPaths("/run", "/var/run"),
			lookup.WithCount(1),
		),
		driverRoot,
		[]string{
			"/nvidia-persistenced/socket",
			"/nvidia-fabricmanager/socket",
		},
	)

	mps := newMounts(
		logger,
		lookup.NewFileLocator(
			lookup.WithLogger(logger),
			lookup.WithRoot(driverRoot),
			lookup.WithCount(1),
		),
		driverRoot,
		[]string{
			"/tmp/nvidia-mps",
		},
	)

	d := Merge(
		(*ipcMounts)(sockets),
		(*ipcMounts)(mps),
	)
	return d, nil
}

// Mounts returns the discovered mounts with "noexec" added to the mount options.
func (d *ipcMounts) Mounts() ([]Mount, error) {
	mounts, err := (*mounts)(d).Mounts()
	if err != nil {
		return nil, err
	}

	var modifiedMounts []Mount
	for _, m := range mounts {
		mount := m
		mount.Options = append(mount.Options, "noexec")
		modifiedMounts = append(modifiedMounts, mount)
	}

	return modifiedMounts, nil
}
