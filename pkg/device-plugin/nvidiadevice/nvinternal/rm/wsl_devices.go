/*
 * Copyright (c) 2022, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY Type, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package rm

type wslDevice nvmlDevice

var _ deviceInfo = (*wslDevice)(nil)

// GetUUID returns the UUID of the device
func (d wslDevice) GetUUID() (string, error) {
	return nvmlDevice(d).GetUUID()
}

// GetPaths returns the paths for a tegra device.
func (d wslDevice) GetPaths() ([]string, error) {
	return []string{"/dev/dxg"}, nil
}

// GetNumaNode returns the NUMA node associated with the GPU device
func (d wslDevice) GetNumaNode() (bool, int, error) {
	return nvmlDevice(d).GetNumaNode()
}
