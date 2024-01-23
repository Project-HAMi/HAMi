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
	"github.com/NVIDIA/nvidia-container-toolkit/internal/discover"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/logger"
)

const (
	dxgDeviceNode = "/dev/dxg"
)

// newDXGDeviceDiscoverer returns a Discoverer for DXG devices under WSL2.
func newDXGDeviceDiscoverer(logger logger.Interface, driverRoot string) discover.Discover {
	deviceNodes := discover.NewCharDeviceDiscoverer(
		logger,
		[]string{dxgDeviceNode},
		driverRoot,
	)

	return deviceNodes
}
