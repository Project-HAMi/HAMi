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
	"strings"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/discover"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/edits"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/lookup/cuda"
	"github.com/NVIDIA/nvidia-container-toolkit/pkg/nvcdi/spec"
	"tags.cncf.io/container-device-interface/pkg/cdi"
	"tags.cncf.io/container-device-interface/specs-go"
)

type managementlib nvcdilib

var _ Interface = (*managementlib)(nil)

// GetAllDeviceSpecs returns all device specs for use in managemnt containers.
// A single device with the name `all` is returned.
func (m *managementlib) GetAllDeviceSpecs() ([]specs.Device, error) {
	devices, err := m.newManagementDeviceDiscoverer()
	if err != nil {
		return nil, fmt.Errorf("failed to create device discoverer: %v", err)
	}

	edits, err := edits.FromDiscoverer(devices)
	if err != nil {
		return nil, fmt.Errorf("failed to create edits from discoverer: %v", err)
	}

	if len(edits.DeviceNodes) == 0 {
		return nil, fmt.Errorf("no NVIDIA device nodes found")
	}

	device := specs.Device{
		Name:           "all",
		ContainerEdits: *edits.ContainerEdits,
	}
	return []specs.Device{device}, nil
}

// GetCommonEdits returns the common edits for use in managementlib containers.
func (m *managementlib) GetCommonEdits() (*cdi.ContainerEdits, error) {
	version, err := m.getCudaVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get CUDA version: %v", err)
	}

	driver, err := newDriverVersionDiscoverer(m.logger, m.driverRoot, m.nvidiaCTKPath, version)
	if err != nil {
		return nil, fmt.Errorf("failed to create driver library discoverer: %v", err)
	}

	edits, err := edits.FromDiscoverer(driver)
	if err != nil {
		return nil, fmt.Errorf("failed to create edits from discoverer: %v", err)
	}

	return edits, nil
}

// getCudaVersion returns the CUDA version for use in managementlib containers.
func (m *managementlib) getCudaVersion() (string, error) {
	version, err := (*nvcdilib)(m).getCudaVersion()
	if err == nil {
		return version, nil
	}

	libCudaPaths, err := cuda.New(
		cuda.WithLogger(m.logger),
		cuda.WithDriverRoot(m.driverRoot),
	).Locate(".*.*")
	if err != nil {
		return "", fmt.Errorf("failed to locate libcuda.so: %v", err)
	}

	libCudaPath := libCudaPaths[0]

	version = strings.TrimPrefix(filepath.Base(libCudaPath), "libcuda.so.")

	return version, nil
}

type managementDiscoverer struct {
	discover.Discover
}

// newManagementDeviceDiscoverer returns a discover.Discover that discovers device nodes for use in managementlib containers.
// NVML is not used to query devices and all device nodes are returned.
func (m *managementlib) newManagementDeviceDiscoverer() (discover.Discover, error) {
	deviceNodes := discover.NewCharDeviceDiscoverer(
		m.logger,
		[]string{
			"/dev/nvidia*",
			"/dev/nvidia-caps/nvidia-cap*",
			"/dev/nvidia-modeset",
			"/dev/nvidia-uvm-tools",
			"/dev/nvidia-uvm",
			"/dev/nvidiactl",
		},
		m.driverRoot,
	)

	deviceFolderPermissionHooks := newDeviceFolderPermissionHookDiscoverer(
		m.logger,
		m.driverRoot,
		m.nvidiaCTKPath,
		deviceNodes,
	)

	d := discover.Merge(
		&managementDiscoverer{deviceNodes},
		deviceFolderPermissionHooks,
	)
	return d, nil
}

func (m *managementDiscoverer) Devices() ([]discover.Device, error) {
	devices, err := m.Discover.Devices()
	if err != nil {
		return devices, err
	}

	var filteredDevices []discover.Device
	for _, device := range devices {
		if m.nodeIsBlocked(device.HostPath) {
			continue
		}
		filteredDevices = append(filteredDevices, device)
	}

	return filteredDevices, nil
}

// nodeIsBlocked returns true if the specified device node should be ignored.
func (m managementDiscoverer) nodeIsBlocked(path string) bool {
	blockedPrefixes := []string{"nvidia-fs", "nvidia-nvswitch", "nvidia-nvlink"}
	nodeName := filepath.Base(path)
	for _, prefix := range blockedPrefixes {
		if strings.HasPrefix(nodeName, prefix) {
			return true
		}
	}
	return false
}

// GetSpec is unsppported for the managementlib specs.
// managementlib is typically wrapped by a spec that implements GetSpec.
func (m *managementlib) GetSpec() (spec.Interface, error) {
	return nil, fmt.Errorf("GetSpec is not supported")
}

// GetGPUDeviceEdits is unsupported for the managementlib specs
func (m *managementlib) GetGPUDeviceEdits(device.Device) (*cdi.ContainerEdits, error) {
	return nil, fmt.Errorf("GetGPUDeviceEdits is not supported")
}

// GetGPUDeviceSpecs is unsupported for the managementlib specs
func (m *managementlib) GetGPUDeviceSpecs(int, device.Device) (*specs.Device, error) {
	return nil, fmt.Errorf("GetGPUDeviceSpecs is not supported")
}

// GetMIGDeviceEdits is unsupported for the managementlib specs
func (m *managementlib) GetMIGDeviceEdits(device.Device, device.MigDevice) (*cdi.ContainerEdits, error) {
	return nil, fmt.Errorf("GetMIGDeviceEdits is not supported")
}

// GetMIGDeviceSpecs is unsupported for the managementlib specs
func (m *managementlib) GetMIGDeviceSpecs(int, device.Device, int, device.MigDevice) (*specs.Device, error) {
	return nil, fmt.Errorf("GetMIGDeviceSpecs is not supported")
}
