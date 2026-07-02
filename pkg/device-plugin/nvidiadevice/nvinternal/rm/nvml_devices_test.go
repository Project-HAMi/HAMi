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

import (
	"fmt"
	"testing"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	mock "github.com/NVIDIA/go-nvml/pkg/nvml/mock"

	//"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/rm"
	"github.com/stretchr/testify/require"
)

// Test GetUUID for nvmlDevice
func TestNvmlDevice_GetUUID(t *testing.T) {
	testCases := []struct {
		description   string
		nvmlDevice    nvml.Device
		expectedUUID  string
		expectedError error
	}{
		{
			description: "Successful UUID retrieval",
			nvmlDevice: &mock.Device{
				GetUUIDFunc: func() (string, nvml.Return) {
					return "GPU-12345", nvml.SUCCESS
				},
			},
			expectedUUID:  "GPU-12345",
			expectedError: nil,
		},
		{
			description: "Error retrieving UUID",
			nvmlDevice: &mock.Device{
				GetUUIDFunc: func() (string, nvml.Return) {
					return "GPU-12345", nvml.ERROR_UNKNOWN
				},
			},
			expectedUUID:  "",
			expectedError: nvml.ERROR_UNKNOWN,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			device := nvmlDevice{Device: tc.nvmlDevice}
			uuid, err := device.GetUUID()

			if tc.expectedError == nil {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tc.expectedError.Error())
			}
			require.Equal(t, tc.expectedUUID, uuid)
		})
	}
}

func TestNvmlDevice_GetPaths(t *testing.T) {
	testCases := []struct {
		description   string
		nvmlDevice    nvml.Device
		expectedPaths []string
		expectedError error
	}{
		{
			description: "Successful path retrieval",
			nvmlDevice: &mock.Device{
				GetMinorNumberFunc: func() (int, nvml.Return) {
					return 0, nvml.SUCCESS
				},
			},
			expectedPaths: []string{"/dev/nvidia0"},
			expectedError: nil,
		},
		{
			description: "Error retrieving UUID",
			nvmlDevice: &mock.Device{
				GetMinorNumberFunc: func() (int, nvml.Return) {
					return 0, nvml.ERROR_UNKNOWN
				},
			},
			expectedPaths: nil,
			expectedError: fmt.Errorf("error getting GPU device minor number: %v", nvml.ERROR_UNKNOWN),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			device := nvmlDevice{Device: tc.nvmlDevice}
			paths, err := device.GetPaths()

			if tc.expectedError == nil {
				require.NoError(t, err)
			} else {
				require.Contains(t, err.Error(), nvml.ERROR_UNKNOWN.Error())
			}
			require.Equal(t, tc.expectedPaths, paths)
		})
	}
}

func TestNvmlDevice_GetNumaNode(t *testing.T) {
	testCases := []struct {
		description     string
		nvmlDevice      nvml.Device
		expectedHasNode bool
		expectedNode    int
		expectedError   error
	}{
		{
			description: "No NUMA node",
			nvmlDevice: &mock.Device{
				GetPciInfoFunc: func() (nvml.PciInfo, nvml.Return) {
					return nvml.PciInfo{BusId: [32]int8{'0', '0', '0', '0', ':', '0', '2', ':', '0', '0', '.', '0', 0, 0, 0, 0}}, nvml.SUCCESS
				},
			},
			expectedHasNode: false,
			expectedNode:    0,
			expectedError:   nil,
		},
		{
			description: "Error getting PCI info",
			nvmlDevice: &mock.Device{
				GetPciInfoFunc: func() (nvml.PciInfo, nvml.Return) {
					return nvml.PciInfo{}, nvml.ERROR_UNKNOWN
				},
			},
			expectedHasNode: false,
			expectedNode:    0,
			expectedError:   nvml.ERROR_UNKNOWN,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			device := nvmlDevice{Device: tc.nvmlDevice}
			hasNode, node, err := device.GetNumaNode()

			if tc.expectedError == nil {
				require.NoError(t, err)
			} else {
				require.Contains(t, err.Error(), tc.expectedError.Error())
			}
			require.Equal(t, tc.expectedHasNode, hasNode)
			require.Equal(t, tc.expectedNode, node)
		})
	}
}
