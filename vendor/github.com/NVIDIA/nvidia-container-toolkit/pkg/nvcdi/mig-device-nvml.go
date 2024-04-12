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

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvlib/pkg/nvml"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/discover"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/edits"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/logger"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/nvcaps"
	"tags.cncf.io/container-device-interface/pkg/cdi"
	"tags.cncf.io/container-device-interface/specs-go"
)

// GetMIGDeviceSpecs returns the CDI device specs for the full GPU represented by 'device'.
func (l *nvmllib) GetMIGDeviceSpecs(i int, d device.Device, j int, mig device.MigDevice) (*specs.Device, error) {
	edits, err := l.GetMIGDeviceEdits(d, mig)
	if err != nil {
		return nil, fmt.Errorf("failed to get edits for device: %v", err)
	}

	name, err := l.deviceNamer.GetMigDeviceName(i, convert{d}, j, convert{mig})
	if err != nil {
		return nil, fmt.Errorf("failed to get device name: %v", err)
	}

	spec := specs.Device{
		Name:           name,
		ContainerEdits: *edits.ContainerEdits,
	}

	return &spec, nil
}

// GetMIGDeviceEdits returns the CDI edits for the MIG device represented by 'mig' on 'parent'.
func (l *nvmllib) GetMIGDeviceEdits(parent device.Device, mig device.MigDevice) (*cdi.ContainerEdits, error) {
	gpu, ret := parent.GetMinorNumber()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting GPU minor: %v", ret)
	}

	gi, ret := mig.GetGpuInstanceId()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting GPU Instance ID: %v", ret)
	}

	ci, ret := mig.GetComputeInstanceId()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting Compute Instance ID: %v", ret)
	}

	editsForDevice, err := GetEditsForComputeInstance(l.logger, l.driverRoot, gpu, gi, ci)
	if err != nil {
		return nil, fmt.Errorf("failed to create container edits for MIG device: %v", err)
	}

	return editsForDevice, nil
}

// GetEditsForComputeInstance returns the CDI edits for a particular compute instance defined by the (gpu, gi, ci) tuple
func GetEditsForComputeInstance(logger logger.Interface, driverRoot string, gpu int, gi int, ci int) (*cdi.ContainerEdits, error) {
	computeInstance, err := newComputeInstanceDiscoverer(logger, driverRoot, gpu, gi, ci)
	if err != nil {
		return nil, fmt.Errorf("failed to create discoverer for Compute Instance: %v", err)
	}

	editsForDevice, err := edits.FromDiscoverer(computeInstance)
	if err != nil {
		return nil, fmt.Errorf("failed to create container edits for Compute Instance: %v", err)
	}

	return editsForDevice, nil
}

// newComputeInstanceDiscoverer returns a discoverer for the specified compute instance
func newComputeInstanceDiscoverer(logger logger.Interface, driverRoot string, gpu int, gi int, ci int) (discover.Discover, error) {
	parentPath := fmt.Sprintf("/dev/nvidia%d", gpu)

	migCaps, err := nvcaps.NewMigCaps()
	if err != nil {
		return nil, fmt.Errorf("error getting MIG capability device paths: %v", err)
	}

	giCap := nvcaps.NewGPUInstanceCap(gpu, gi)
	giCapDevicePath, err := migCaps.GetCapDevicePath(giCap)
	if err != nil {
		return nil, fmt.Errorf("failed to get GI cap device path: %v", err)
	}

	ciCap := nvcaps.NewComputeInstanceCap(gpu, gi, ci)
	ciCapDevicePath, err := migCaps.GetCapDevicePath(ciCap)
	if err != nil {
		return nil, fmt.Errorf("failed to get CI cap device path: %v", err)
	}

	deviceNodes := discover.NewCharDeviceDiscoverer(
		logger,
		[]string{
			parentPath,
			giCapDevicePath,
			ciCapDevicePath,
		},
		driverRoot,
	)

	return deviceNodes, nil
}
