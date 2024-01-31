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
	"github.com/NVIDIA/nvidia-container-toolkit/internal/dxcore"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/logger"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/lookup"
)

var requiredDriverStoreFiles = []string{
	"libcuda.so.1.1",                /* Core library for cuda support */
	"libcuda_loader.so",             /* Core library for cuda support on WSL */
	"libnvidia-ptxjitcompiler.so.1", /* Core library for PTX Jit support */
	"libnvidia-ml.so.1",             /* Core library for nvml */
	"libnvidia-ml_loader.so",        /* Core library for nvml on WSL */
	"libdxcore.so",                  /* Core library for dxcore support */
	"nvcubins.bin",                  /* Binary containing GPU code for cuda */
	"nvidia-smi",                    /* nvidia-smi binary*/
}

// newWSLDriverDiscoverer returns a Discoverer for WSL2 drivers.
func newWSLDriverDiscoverer(logger logger.Interface, driverRoot string, nvidiaCTKPath string) (discover.Discover, error) {
	err := dxcore.Init()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize dxcore: %v", err)
	}
	defer func() {
		if err := dxcore.Shutdown(); err != nil {
			logger.Warningf("failed to shutdown dxcore: %v", err)
		}
	}()

	driverStorePaths := dxcore.GetDriverStorePaths()
	if len(driverStorePaths) == 0 {
		return nil, fmt.Errorf("no driver store paths found")
	}
	logger.Infof("Using WSL driver store paths: %v", driverStorePaths)

	return newWSLDriverStoreDiscoverer(logger, driverRoot, nvidiaCTKPath, driverStorePaths)
}

// newWSLDriverStoreDiscoverer returns a Discoverer for WSL2 drivers in the driver store associated with a dxcore adapter.
func newWSLDriverStoreDiscoverer(logger logger.Interface, driverRoot string, nvidiaCTKPath string, driverStorePaths []string) (discover.Discover, error) {
	var searchPaths []string
	seen := make(map[string]bool)
	for _, path := range driverStorePaths {
		if seen[path] {
			continue
		}
		searchPaths = append(searchPaths, path)
	}
	if len(searchPaths) > 1 {
		logger.Warningf("Found multiple driver store paths: %v", searchPaths)
	}
	searchPaths = append(searchPaths, "/usr/lib/wsl/lib")

	libraries := discover.NewMounts(
		logger,
		lookup.NewFileLocator(
			lookup.WithLogger(logger),
			lookup.WithSearchPaths(
				searchPaths...,
			),
			lookup.WithCount(1),
		),
		driverRoot,
		requiredDriverStoreFiles,
	)

	symlinkHook := nvidiaSMISimlinkHook{
		logger:        logger,
		mountsFrom:    libraries,
		nvidiaCTKPath: nvidiaCTKPath,
	}

	ldcacheHook, _ := discover.NewLDCacheUpdateHook(logger, libraries, nvidiaCTKPath)

	d := discover.Merge(
		libraries,
		symlinkHook,
		ldcacheHook,
	)

	return d, nil
}

type nvidiaSMISimlinkHook struct {
	discover.None
	logger        logger.Interface
	mountsFrom    discover.Discover
	nvidiaCTKPath string
}

// Hooks returns a hook that creates a symlink to nvidia-smi in the driver store.
// On WSL2 the driver store location is used unchanged, for this reason we need
// to create a symlink from /usr/bin/nvidia-smi to the nvidia-smi binary in the
// driver store.
func (m nvidiaSMISimlinkHook) Hooks() ([]discover.Hook, error) {
	mounts, err := m.mountsFrom.Mounts()
	if err != nil {
		return nil, fmt.Errorf("failed to discover mounts: %w", err)
	}

	var target string
	for _, mount := range mounts {
		if filepath.Base(mount.Path) == "nvidia-smi" {
			target = mount.Path
			break
		}
	}

	if target == "" {
		m.logger.Warningf("Failed to find nvidia-smi in mounts: %v", mounts)
		return nil, nil
	}
	link := "/usr/bin/nvidia-smi"
	links := []string{fmt.Sprintf("%s::%s", target, link)}
	symlinkHook := discover.CreateCreateSymlinkHook(m.nvidiaCTKPath, links)

	return symlinkHook.Hooks()
}
