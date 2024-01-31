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

package tegra

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/NVIDIA/nvidia-container-toolkit/internal/discover"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/logger"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/lookup"
)

type symlinkHook struct {
	discover.None
	logger        logger.Interface
	nvidiaCTKPath string
	targets       []string
	mountsFrom    discover.Discover

	// The following can be overridden for testing
	symlinkChainLocator lookup.Locator
	resolveSymlink      func(string) (string, error)
}

// createCSVSymlinkHooks creates a discoverer for a hook that creates required symlinks in the container
func (o tegraOptions) createCSVSymlinkHooks(targets []string, mounts discover.Discover) discover.Discover {
	return symlinkHook{
		logger:              o.logger,
		nvidiaCTKPath:       o.nvidiaCTKPath,
		targets:             targets,
		mountsFrom:          mounts,
		symlinkChainLocator: o.symlinkChainLocator,
		resolveSymlink:      o.resolveSymlink,
	}
}

// Hooks returns a hook to create the symlinks from the required CSV files
func (d symlinkHook) Hooks() ([]discover.Hook, error) {
	specificLinks, err := d.getSpecificLinks()
	if err != nil {
		return nil, fmt.Errorf("failed to determine specific links: %v", err)
	}

	csvSymlinks := d.getCSVFileSymlinks()

	return discover.CreateCreateSymlinkHook(
		d.nvidiaCTKPath,
		append(csvSymlinks, specificLinks...),
	).Hooks()
}

// getSpecificLinks returns the required specic links that need to be created
func (d symlinkHook) getSpecificLinks() ([]string, error) {
	mounts, err := d.mountsFrom.Mounts()
	if err != nil {
		return nil, fmt.Errorf("failed to discover mounts for ldcache update: %v", err)
	}

	linkProcessed := make(map[string]bool)
	var links []string
	for _, m := range mounts {
		var target string
		var link string

		lib := filepath.Base(m.Path)

		switch {
		case strings.HasPrefix(lib, "libcuda.so"):
			// XXX Many applications wrongly assume that libcuda.so exists (e.g. with dlopen).
			target = "libcuda.so.1"
			link = "libcuda.so"
		case strings.HasPrefix(lib, "libGLX_nvidia.so"):
			// XXX GLVND requires this symlink for indirect GLX support.
			target = lib
			link = "libGLX_indirect.so.0"
		case strings.HasPrefix(lib, "libnvidia-opticalflow.so"):
			// XXX Fix missing symlink for libnvidia-opticalflow.so.
			target = "libnvidia-opticalflow.so.1"
			link = "libnvidia-opticalflow.so"
		default:
			continue
		}
		if linkProcessed[link] {
			continue
		}
		linkProcessed[link] = true

		linkPath := filepath.Join(filepath.Dir(m.Path), link)
		links = append(links, fmt.Sprintf("%v::%v", target, linkPath))
	}

	return links, nil
}

// getSymlinkCandidates returns a list of symlinks that are candidates for being created.
func (d symlinkHook) getSymlinkCandidates() []string {
	var candidates []string
	for _, target := range d.targets {
		reslovedSymlinkChain, err := d.symlinkChainLocator.Locate(target)
		if err != nil {
			d.logger.Warningf("Failed to locate symlink %v", target)
			continue
		}
		candidates = append(candidates, reslovedSymlinkChain...)
	}
	return candidates
}

func (d symlinkHook) getCSVFileSymlinks() []string {
	var links []string
	created := make(map[string]bool)
	// candidates is a list of absolute paths to symlinks in a chain, or the final target of the chain.
	for _, candidate := range d.getSymlinkCandidates() {
		target, err := d.resolveSymlink(candidate)
		if err != nil {
			d.logger.Debugf("Skipping invalid link: %v", err)
			continue
		} else if target == candidate {
			d.logger.Debugf("%v is not a symlink", candidate)
			continue
		}

		link := fmt.Sprintf("%v::%v", target, candidate)
		if created[link] {
			d.logger.Debugf("skipping duplicate link: %v", link)
			continue
		}
		created[link] = true

		links = append(links, link)
	}

	return links
}
