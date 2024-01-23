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

	"github.com/NVIDIA/nvidia-container-toolkit/internal/discover"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/logger"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/lookup"

	"github.com/NVIDIA/go-nvlib/pkg/nvml"
)

// newCommonNVMLDiscoverer returns a discoverer for entities that are not associated with a specific CDI device.
// This includes driver libraries and meta devices, for example.
func newCommonNVMLDiscoverer(logger logger.Interface, driverRoot string, nvidiaCTKPath string, nvmllib nvml.Interface) (discover.Discover, error) {
	metaDevices := discover.NewDeviceDiscoverer(
		logger,
		lookup.NewCharDeviceLocator(
			lookup.WithLogger(logger),
			lookup.WithRoot(driverRoot),
		),
		driverRoot,
		[]string{
			"/dev/nvidia-modeset",
			"/dev/nvidia-uvm-tools",
			"/dev/nvidia-uvm",
			"/dev/nvidiactl",
		},
	)

	graphicsMounts, err := discover.NewGraphicsMountsDiscoverer(logger, driverRoot, nvidiaCTKPath)
	if err != nil {
		logger.Warningf("failed to create discoverer for graphics mounts: %v", err)
	}

	driverFiles, err := NewDriverDiscoverer(logger, driverRoot, nvidiaCTKPath, nvmllib)
	if err != nil {
		return nil, fmt.Errorf("failed to create discoverer for driver files: %v", err)
	}

	d := discover.Merge(
		metaDevices,
		graphicsMounts,
		driverFiles,
	)

	return d, nil
}
