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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NVIDIA/nvidia-container-toolkit/internal/config/image"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/info/drm"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/info/proc"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/logger"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/lookup"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/lookup/cuda"
)

// NewGraphicsDiscoverer returns the discoverer for graphics tools such as Vulkan.
func NewGraphicsDiscoverer(logger logger.Interface, devices image.VisibleDevices, driverRoot string, nvidiaCTKPath string) (Discover, error) {
	mounts, err := NewGraphicsMountsDiscoverer(logger, driverRoot, nvidiaCTKPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create mounts discoverer: %v", err)
	}

	drmDeviceNodes, err := newDRMDeviceDiscoverer(logger, devices, driverRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to create DRM device discoverer: %v", err)
	}

	drmByPathSymlinks := newCreateDRMByPathSymlinks(logger, drmDeviceNodes, driverRoot, nvidiaCTKPath)

	discover := Merge(
		Merge(drmDeviceNodes, drmByPathSymlinks),
		mounts,
	)

	return discover, nil
}

// NewGraphicsMountsDiscoverer creates a discoverer for the mounts required by graphics tools such as vulkan.
func NewGraphicsMountsDiscoverer(logger logger.Interface, driverRoot string, nvidiaCTKPath string) (Discover, error) {
	locator, err := lookup.NewLibraryLocator(logger, driverRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to construct library locator: %v", err)
	}
	libraries := NewMounts(
		logger,
		locator,
		driverRoot,
		[]string{
			"libnvidia-egl-gbm.so",
		},
	)

	jsonMounts := NewMounts(
		logger,
		lookup.NewFileLocator(
			lookup.WithLogger(logger),
			lookup.WithRoot(driverRoot),
			lookup.WithSearchPaths("/etc", "/usr/share"),
		),
		driverRoot,
		[]string{
			"glvnd/egl_vendor.d/10_nvidia.json",
			"vulkan/icd.d/nvidia_icd.json",
			"vulkan/implicit_layer.d/nvidia_layers.json",
			"egl/egl_external_platform.d/15_nvidia_gbm.json",
			"egl/egl_external_platform.d/10_nvidia_wayland.json",
		},
	)

	xorg := optionalXorgDiscoverer(logger, driverRoot, nvidiaCTKPath)

	discover := Merge(
		libraries,
		jsonMounts,
		xorg,
	)

	return discover, nil
}

type drmDevicesByPath struct {
	None
	logger        logger.Interface
	nvidiaCTKPath string
	driverRoot    string
	devicesFrom   Discover
}

// newCreateDRMByPathSymlinks creates a discoverer for a hook to create the by-path symlinks for DRM devices discovered by the specified devices discoverer
func newCreateDRMByPathSymlinks(logger logger.Interface, devices Discover, driverRoot string, nvidiaCTKPath string) Discover {
	d := drmDevicesByPath{
		logger:        logger,
		nvidiaCTKPath: nvidiaCTKPath,
		driverRoot:    driverRoot,
		devicesFrom:   devices,
	}

	return &d
}

// Hooks returns a hook to create the symlinks from the required CSV files
func (d drmDevicesByPath) Hooks() ([]Hook, error) {
	devices, err := d.devicesFrom.Devices()
	if err != nil {
		return nil, fmt.Errorf("failed to discover devices for by-path symlinks: %v", err)
	}
	if len(devices) == 0 {
		return nil, nil
	}
	links, err := d.getSpecificLinkArgs(devices)
	if err != nil {
		return nil, fmt.Errorf("failed to determine specific links: %v", err)
	}
	if len(links) == 0 {
		return nil, nil
	}

	var args []string
	for _, l := range links {
		args = append(args, "--link", l)
	}

	hook := CreateNvidiaCTKHook(
		d.nvidiaCTKPath,
		"create-symlinks",
		args...,
	)

	return []Hook{hook}, nil
}

// getSpecificLinkArgs returns the required specic links that need to be created
func (d drmDevicesByPath) getSpecificLinkArgs(devices []Device) ([]string, error) {
	selectedDevices := make(map[string]bool)
	for _, d := range devices {
		selectedDevices[filepath.Base(d.HostPath)] = true
	}

	linkLocator := lookup.NewFileLocator(
		lookup.WithLogger(d.logger),
		lookup.WithRoot(d.driverRoot),
	)
	candidates, err := linkLocator.Locate("/dev/dri/by-path/pci-*-*")
	if err != nil {
		d.logger.Warningf("Failed to locate by-path links: %v; ignoring", err)
		return nil, nil
	}

	var links []string
	for _, c := range candidates {
		device, err := os.Readlink(c)
		if err != nil {
			d.logger.Warningf("Failed to evaluate symlink %v; ignoring", c)
			continue
		}

		if selectedDevices[filepath.Base(device)] {
			d.logger.Debugf("adding device symlink %v -> %v", c, device)
			links = append(links, fmt.Sprintf("%v::%v", device, c))
		}
	}

	return links, nil
}

// newDRMDeviceDiscoverer creates a discoverer for the DRM devices associated with the requested devices.
func newDRMDeviceDiscoverer(logger logger.Interface, devices image.VisibleDevices, driverRoot string) (Discover, error) {
	allDevices := NewDeviceDiscoverer(
		logger,
		lookup.NewCharDeviceLocator(
			lookup.WithLogger(logger),
			lookup.WithRoot(driverRoot),
		),
		driverRoot,
		[]string{
			"/dev/dri/card*",
			"/dev/dri/renderD*",
		},
	)

	filter, err := newDRMDeviceFilter(logger, devices, driverRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to construct DRM device filter: %v", err)
	}

	// We return a discoverer that applies the DRM device filter created above to all discovered DRM device nodes.
	d := newFilteredDisoverer(
		logger,
		allDevices,
		filter,
	)

	return d, err
}

// newDRMDeviceFilter creates a filter that matches DRM devices nodes for the visible devices.
func newDRMDeviceFilter(logger logger.Interface, devices image.VisibleDevices, driverRoot string) (Filter, error) {
	gpuInformationPaths, err := proc.GetInformationFilePaths(driverRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to read GPU information: %v", err)
	}

	var selectedBusIds []string
	for _, f := range gpuInformationPaths {
		info, err := proc.ParseGPUInformationFile(f)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %v: %v", f, err)
		}
		uuid := info[proc.GPUInfoGPUUUID]
		busID := info[proc.GPUInfoBusLocation]
		minor := info[proc.GPUInfoDeviceMinor]

		if devices.Has(minor) || devices.Has(uuid) || devices.Has(busID) {
			selectedBusIds = append(selectedBusIds, busID)
		}
	}

	filter := make(selectDeviceByPath)
	for _, busID := range selectedBusIds {
		drmDeviceNodes, err := drm.GetDeviceNodesByBusID(busID)
		if err != nil {
			return nil, fmt.Errorf("failed to determine DRM devices for %v: %v", busID, err)
		}
		for _, drmDeviceNode := range drmDeviceNodes {
			filter[drmDeviceNode] = true
		}
	}

	return filter, nil
}

type xorgHooks struct {
	libraries     Discover
	driverVersion string
	nvidiaCTKPath string
}

var _ Discover = (*xorgHooks)(nil)

// optionalXorgDiscoverer creates a discoverer for Xorg libraries.
// If the creation of the discoverer fails, a None discoverer is returned.
func optionalXorgDiscoverer(logger logger.Interface, driverRoot string, nvidiaCTKPath string) Discover {
	xorg, err := newXorgDiscoverer(logger, driverRoot, nvidiaCTKPath)
	if err != nil {
		logger.Warningf("Failed to create Xorg discoverer: %v; skipping xorg libraries", err)
		return None{}
	}
	return xorg
}

func newXorgDiscoverer(logger logger.Interface, driverRoot string, nvidiaCTKPath string) (Discover, error) {
	libCudaPaths, err := cuda.New(
		cuda.WithLogger(logger),
		cuda.WithDriverRoot(driverRoot),
	).Locate(".*.*")
	if err != nil {
		return nil, fmt.Errorf("failed to locate libcuda.so: %v", err)
	}
	libcudaPath := libCudaPaths[0]

	version := strings.TrimPrefix(filepath.Base(libcudaPath), "libcuda.so.")
	if version == "" {
		return nil, fmt.Errorf("failed to determine libcuda.so version from path: %q", libcudaPath)
	}

	libRoot := filepath.Dir(libcudaPath)
	xorgLibs := NewMounts(
		logger,
		lookup.NewFileLocator(
			lookup.WithLogger(logger),
			lookup.WithRoot(driverRoot),
			lookup.WithSearchPaths(libRoot, "/usr/lib/x86_64-linux-gnu"),
			lookup.WithCount(1),
		),
		driverRoot,
		[]string{
			"nvidia/xorg/nvidia_drv.so",
			fmt.Sprintf("nvidia/xorg/libglxserver_nvidia.so.%s", version),
		},
	)
	xorgHooks := xorgHooks{
		libraries:     xorgLibs,
		driverVersion: version,
		nvidiaCTKPath: nvidiaCTKPath,
	}

	xorgConfg := NewMounts(
		logger,
		lookup.NewFileLocator(
			lookup.WithLogger(logger),
			lookup.WithRoot(driverRoot),
			lookup.WithSearchPaths("/usr/share"),
		),
		driverRoot,
		[]string{"X11/xorg.conf.d/10-nvidia.conf"},
	)

	d := Merge(
		xorgLibs,
		xorgConfg,
		xorgHooks,
	)

	return d, nil
}

// Devices returns no devices for Xorg
func (m xorgHooks) Devices() ([]Device, error) {
	return nil, nil
}

// Hooks returns a hook to create symlinks for Xorg libraries
func (m xorgHooks) Hooks() ([]Hook, error) {
	mounts, err := m.libraries.Mounts()
	if err != nil {
		return nil, fmt.Errorf("failed to get mounts: %v", err)
	}
	if len(mounts) == 0 {
		return nil, nil
	}

	var target string
	for _, mount := range mounts {
		filename := filepath.Base(mount.HostPath)
		if filename == "libglxserver_nvidia.so."+m.driverVersion {
			target = mount.Path
		}
	}

	if target == "" {
		return nil, nil
	}

	link := strings.TrimSuffix(target, "."+m.driverVersion)
	links := []string{fmt.Sprintf("%s::%s", filepath.Base(target), link)}
	symlinkHook := CreateCreateSymlinkHook(
		m.nvidiaCTKPath,
		links,
	)

	return symlinkHook.Hooks()
}

// Mounts returns the libraries required for Xorg
func (m xorgHooks) Mounts() ([]Mount, error) {
	return nil, nil
}

// selectDeviceByPath is a filter that allows devices to be selected by the path
type selectDeviceByPath map[string]bool

var _ Filter = (*selectDeviceByPath)(nil)

// DeviceIsSelected determines whether the device's path has been selected
func (s selectDeviceByPath) DeviceIsSelected(device Device) bool {
	return s[device.Path]
}

// MountIsSelected is always true
func (s selectDeviceByPath) MountIsSelected(Mount) bool {
	return true
}

// HookIsSelected is always true
func (s selectDeviceByPath) HookIsSelected(Hook) bool {
	return true
}
