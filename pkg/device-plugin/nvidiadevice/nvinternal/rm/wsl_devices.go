/*
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
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
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

// GetTotalMemory returns the total memory available on the device.
func (d wslDevice) GetTotalMemory() (uint64, error) {
	return nvmlDevice(d).GetTotalMemory()
}

// GetComputeCapability returns the CUDA compute capability for the device.
func (d wslDevice) GetComputeCapability() (string, error) {
	return nvmlDevice(d).GetComputeCapability()
}
