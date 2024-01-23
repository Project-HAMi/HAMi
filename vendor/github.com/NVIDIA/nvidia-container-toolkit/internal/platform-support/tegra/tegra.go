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

package tegra

import (
	"fmt"

	"github.com/NVIDIA/nvidia-container-toolkit/internal/discover"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/logger"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/lookup"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/lookup/symlinks"
)

type tegraOptions struct {
	logger             logger.Interface
	csvFiles           []string
	driverRoot         string
	nvidiaCTKPath      string
	librarySearchPaths []string
	ignorePatterns     ignoreMountSpecPatterns

	// The following can be overridden for testing
	symlinkLocator      lookup.Locator
	symlinkChainLocator lookup.Locator
	// TODO: This should be replaced by a regular mock
	resolveSymlink func(string) (string, error)
}

// Option defines a functional option for configuring a Tegra discoverer.
type Option func(*tegraOptions)

// New creates a new tegra discoverer using the supplied options.
func New(opts ...Option) (discover.Discover, error) {
	o := &tegraOptions{}
	for _, opt := range opts {
		opt(o)
	}

	if o.symlinkLocator == nil {
		o.symlinkLocator = lookup.NewSymlinkLocator(
			lookup.WithLogger(o.logger),
			lookup.WithRoot(o.driverRoot),
			lookup.WithSearchPaths(append(o.librarySearchPaths, "/")...),
		)
	}

	if o.symlinkChainLocator == nil {
		o.symlinkChainLocator = lookup.NewSymlinkChainLocator(
			lookup.WithLogger(o.logger),
			lookup.WithRoot(o.driverRoot),
		)
	}

	if o.resolveSymlink == nil {
		o.resolveSymlink = symlinks.Resolve
	}

	csvDiscoverer, err := o.newDiscovererFromCSVFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to create CSV discoverer: %v", err)
	}

	ldcacheUpdateHook, err := discover.NewLDCacheUpdateHook(o.logger, csvDiscoverer, o.nvidiaCTKPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create ldcach update hook discoverer: %v", err)
	}

	tegraSystemMounts := discover.NewMounts(
		o.logger,
		lookup.NewFileLocator(lookup.WithLogger(o.logger)),
		"",
		[]string{
			"/etc/nv_tegra_release",
		},
	)

	d := discover.Merge(
		csvDiscoverer,
		// The ldcacheUpdateHook is added last to ensure that the created symlinks are included
		ldcacheUpdateHook,
		tegraSystemMounts,
	)

	return d, nil
}

// WithLogger sets the logger for the discoverer.
func WithLogger(logger logger.Interface) Option {
	return func(o *tegraOptions) {
		o.logger = logger
	}
}

// WithDriverRoot sets the driver root for the discoverer.
func WithDriverRoot(driverRoot string) Option {
	return func(o *tegraOptions) {
		o.driverRoot = driverRoot
	}
}

// WithCSVFiles sets the CSV files for the discoverer.
func WithCSVFiles(csvFiles []string) Option {
	return func(o *tegraOptions) {
		o.csvFiles = csvFiles
	}
}

// WithNVIDIACTKPath sets the path to the nvidia-container-toolkit binary.
func WithNVIDIACTKPath(nvidiaCTKPath string) Option {
	return func(o *tegraOptions) {
		o.nvidiaCTKPath = nvidiaCTKPath
	}
}

// WithLibrarySearchPaths sets the library search paths for the discoverer.
func WithLibrarySearchPaths(librarySearchPaths ...string) Option {
	return func(o *tegraOptions) {
		o.librarySearchPaths = librarySearchPaths
	}
}

// WithIngorePatterns sets patterns to ignore in the CSV files
func WithIngorePatterns(ignorePatterns ...string) Option {
	return func(o *tegraOptions) {
		o.ignorePatterns = ignoreMountSpecPatterns(ignorePatterns)
	}
}
