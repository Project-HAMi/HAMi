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

package resource

import (
	"fmt"

	"github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/pkg/cuda"
)

type cudaLib struct{}

var _ Manager = (*cudaLib)(nil)

// NewCudaManager returns an resource manger for CUDA devices
func NewCudaManager() Manager {
	return &cudaLib{}
}

// GetDevices returns the CUDA devices available on the system
func (l *cudaLib) GetDevices() ([]Device, error) {
	count, r := cuda.DeviceGetCount()
	if r != cuda.SUCCESS {
		return nil, fmt.Errorf("failed to get number of CUDA devices: %v", r)
	}

	var devices []Device
	for i := 0; i < count; i++ {
		d, r := cuda.DeviceGet(i)
		if r != cuda.SUCCESS {
			return nil, fmt.Errorf("failed to get CUDA device %v: %v", i, r)
		}
		devices = append(devices, NewCudaDevice(d))
	}

	return devices, nil
}

// GetCudaDriverVersion returns the CUDA driver version
func (l *cudaLib) GetCudaDriverVersion() (int, int, error) {
	version, r := cuda.DriverGetVersion()
	if r != cuda.SUCCESS {
		return 0, 0, fmt.Errorf("failed to get driver version: %v", r)
	}

	major := version / 1000
	minor := version % 100 / 10

	return major, minor, nil
}

// GetDriverVersion returns the driver version.
// This is currently "unknown" for Tegra systems.
func (l *cudaLib) GetDriverVersion() (string, error) {
	return "unknown.unknown.unknown", nil
}

// Init initializes the CUDA library.
func (l *cudaLib) Init() error {
	r := cuda.Init()
	if r != cuda.SUCCESS {
		return fmt.Errorf("%v", r)
	}
	return nil
}

// Shutdown shuts down the CUDA library.
func (l *cudaLib) Shutdown() (err error) {
	r := cuda.Shutdown()
	if r != cuda.SUCCESS {
		return fmt.Errorf("%v", r)
	}
	return nil
}
