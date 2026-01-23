/*
<<<<<<< HEAD
 * SPDX-License-Identifier: Apache-2.0
 *
 * The HAMi Contributors require contributions made to
 * this file be licensed under the Apache-2.0 license or a
 * compatible open source license.
 */

/*
 * Licensed to NVIDIA CORPORATION under one or more contributor
 * license agreements. See the NOTICE file distributed with
 * this work for additional information regarding copyright
 * ownership. NVIDIA CORPORATION licenses this file to you under
 * the Apache License, Version 2.0 (the "License"); you may
 * not use this file except in compliance with the License.
=======
 * Copyright (c) 2022, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
<<<<<<< HEAD
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/*
 * Modifications Copyright The HAMi Authors. See
 * GitHub history for details.
=======
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY Type, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
 */

package rm

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"

<<<<<<< HEAD
<<<<<<< HEAD
	"github.com/NVIDIA/go-nvml/pkg/nvml"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/mig"
=======
	"gitlab.com/nvidia/cloud-native/go-nvlib/pkg/nvlib/info"
	"gitlab.com/nvidia/cloud-native/go-nvlib/pkg/nvml"
=======
	"github.com/NVIDIA/go-nvlib/pkg/nvlib/info"
	"github.com/NVIDIA/go-nvlib/pkg/nvml"
>>>>>>> c7a3893 (Remake this repo to HAMi)

	"4pd.io/k8s-vgpu/pkg/device-plugin/nvidiadevice/nvinternal/mig"
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
)

const (
	nvidiaProcDriverPath   = "/proc/driver/nvidia"
	nvidiaCapabilitiesPath = nvidiaProcDriverPath + "/capabilities"
)

// nvmlDevice wraps an nvml.Device with more functions.
type nvmlDevice struct {
	nvml.Device
}

// nvmlMigDevice allows for specific functions of nvmlDevice to be overridden.
type nvmlMigDevice nvmlDevice

var _ deviceInfo = (*nvmlDevice)(nil)
var _ deviceInfo = (*nvmlMigDevice)(nil)

<<<<<<< HEAD
func newNvmlGPUDevice(i int, gpu nvml.Device) (string, deviceInfo) {
	index := fmt.Sprintf("%v", i)
	return index, nvmlDevice{gpu}
}

func newWslGPUDevice(i int, gpu nvml.Device) (string, deviceInfo) {
	index := fmt.Sprintf("%v", i)
	return index, wslDevice{gpu}
=======
func newGPUDevice(i int, gpu nvml.Device) (string, deviceInfo) {
	index := fmt.Sprintf("%v", i)
	isWsl, _ := info.New().HasDXCore()
	if isWsl {
		return index, wslDevice{gpu}
	}

	return index, nvmlDevice{gpu}
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
}

func newMigDevice(i int, j int, mig nvml.Device) (string, nvmlMigDevice) {
	return fmt.Sprintf("%v:%v", i, j), nvmlMigDevice{mig}
}

// GetUUID returns the UUID of the device
func (d nvmlDevice) GetUUID() (string, error) {
	uuid, ret := d.Device.GetUUID()
	if ret != nvml.SUCCESS {
		return "", ret
	}
	return uuid, nil
}

// GetUUID returns the UUID of the device
func (d nvmlMigDevice) GetUUID() (string, error) {
	return nvmlDevice(d).GetUUID()
}

// GetPaths returns the paths for a GPU device
func (d nvmlDevice) GetPaths() ([]string, error) {
	minor, ret := d.GetMinorNumber()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting GPU device minor number: %v", ret)
	}
	path := fmt.Sprintf("/dev/nvidia%d", minor)

	return []string{path}, nil
}

<<<<<<< HEAD
// GetComputeCapability returns the CUDA Compute Capability for the device.
func (d nvmlDevice) GetComputeCapability() (string, error) {
	major, minor, ret := d.GetCudaComputeCapability()
	if ret != nvml.SUCCESS {
		return "", ret
	}
	return fmt.Sprintf("%d.%d", major, minor), nil
}

// GetComputeCapability returns the CUDA Compute Capability for the device.
func (d nvmlMigDevice) GetComputeCapability() (string, error) {
	parent, ret := d.GetDeviceHandleFromMigDeviceHandle()
	if ret != nvml.SUCCESS {
		return "", fmt.Errorf("failed to get parent device: %w", ret)
	}
	return nvmlDevice{parent}.GetComputeCapability()
}

=======
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
// GetPaths returns the paths for a MIG device
func (d nvmlMigDevice) GetPaths() ([]string, error) {
	capDevicePaths, err := mig.GetMigCapabilityDevicePaths()
	if err != nil {
		return nil, fmt.Errorf("error getting MIG capability device paths: %v", err)
	}

	gi, ret := d.GetGpuInstanceId()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting GPU Instance ID: %v", ret)
	}

	ci, ret := d.GetComputeInstanceId()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting Compute Instance ID: %v", ret)
	}

	parent, ret := d.GetDeviceHandleFromMigDeviceHandle()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting parent device: %v", ret)
	}
	minor, ret := parent.GetMinorNumber()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting GPU device minor number: %v", ret)
	}
	parentPath := fmt.Sprintf("/dev/nvidia%d", minor)

	giCapPath := fmt.Sprintf(nvidiaCapabilitiesPath+"/gpu%d/mig/gi%d/access", minor, gi)
	if _, exists := capDevicePaths[giCapPath]; !exists {
		return nil, fmt.Errorf("missing MIG GPU instance capability path: %v", giCapPath)
	}

	ciCapPath := fmt.Sprintf(nvidiaCapabilitiesPath+"/gpu%d/mig/gi%d/ci%d/access", minor, gi, ci)
	if _, exists := capDevicePaths[ciCapPath]; !exists {
		return nil, fmt.Errorf("missing MIG GPU instance capability path: %v", giCapPath)
	}

	devicePaths := []string{
		parentPath,
		capDevicePaths[giCapPath],
		capDevicePaths[ciCapPath],
	}

	return devicePaths, nil
}

// GetNumaNode returns the NUMA node associated with the GPU device
func (d nvmlDevice) GetNumaNode() (bool, int, error) {
	info, ret := d.GetPciInfo()
	if ret != nvml.SUCCESS {
		return false, 0, fmt.Errorf("error getting PCI Bus Info of device: %v", ret)
	}

	// Discard leading zeros.
<<<<<<< HEAD
	busID := strings.ToLower(strings.TrimPrefix(uint8Slice(info.BusId[:]).String(), "0000"))
=======
	busID := strings.ToLower(strings.TrimPrefix(int8Slice(info.BusId[:]).String(), "0000"))
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)

	b, err := os.ReadFile(fmt.Sprintf("/sys/bus/pci/devices/%s/numa_node", busID))
	if err != nil {
		return false, 0, nil
	}

	node, err := strconv.Atoi(string(bytes.TrimSpace(b)))
	if err != nil {
<<<<<<< HEAD
		return false, 0, fmt.Errorf("error parsing value for NUMA node: %v", err)
=======
		return false, 0, fmt.Errorf("eror parsing value for NUMA node: %v", err)
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
	}

	if node < 0 {
		return false, 0, nil
	}

	return true, node, nil
}

// GetNumaNode for a MIG device is the NUMA node of the parent device.
func (d nvmlMigDevice) GetNumaNode() (bool, int, error) {
	parent, ret := d.GetDeviceHandleFromMigDeviceHandle()
	if ret != nvml.SUCCESS {
		return false, 0, fmt.Errorf("error getting parent GPU device from MIG device: %v", ret)
	}

	return nvmlDevice{parent}.GetNumaNode()
}
<<<<<<< HEAD

// GetTotalMemory returns the total memory available on the device.
func (d nvmlDevice) GetTotalMemory() (uint64, error) {
	info, ret := d.GetMemoryInfo()
	if ret != nvml.SUCCESS {
		return 0, ret
	}
	return info.Total, nil
}

// GetTotalMemory returns the total memory available on the device.
func (d nvmlMigDevice) GetTotalMemory() (uint64, error) {
	info, ret := d.GetMemoryInfo()
	if ret != nvml.SUCCESS {
		return 0, ret
	}
	return info.Total, nil
}
=======
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
