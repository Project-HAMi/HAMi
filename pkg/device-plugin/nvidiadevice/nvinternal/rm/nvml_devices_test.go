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

func TestNewNvmlGPUDevice(t *testing.T) {
	index, dev := newNvmlGPUDevice(3, &mock.Device{})
	require.Equal(t, "3", index)
	require.IsType(t, nvmlDevice{}, dev)
}

func TestNewWslGPUDevice(t *testing.T) {
	index, dev := newWslGPUDevice(2, &mock.Device{})
	require.Equal(t, "2", index)
	require.IsType(t, wslDevice{}, dev)
}

func TestNewMigDevice(t *testing.T) {
	index, dev := newMigDevice(1, 4, &mock.Device{})
	require.Equal(t, "1:4", index)
	require.IsType(t, nvmlMigDevice{}, dev)
}

func TestNvmlMigDevice_GetUUID(t *testing.T) {
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
					return "MIG-12345", nvml.SUCCESS
				},
			},
			expectedUUID: "MIG-12345",
		},
		{
			description: "Error retrieving UUID",
			nvmlDevice: &mock.Device{
				GetUUIDFunc: func() (string, nvml.Return) {
					return "", nvml.ERROR_UNKNOWN
				},
			},
			expectedError: nvml.ERROR_UNKNOWN,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			device := nvmlMigDevice{Device: tc.nvmlDevice}
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

func TestNvmlDevice_GetComputeCapability(t *testing.T) {
	testCases := []struct {
		description   string
		nvmlDevice    nvml.Device
		expectedCC    string
		expectedError error
	}{
		{
			description: "Successful compute capability retrieval",
			nvmlDevice: &mock.Device{
				GetCudaComputeCapabilityFunc: func() (int, int, nvml.Return) {
					return 8, 0, nvml.SUCCESS
				},
			},
			expectedCC: "8.0",
		},
		{
			description: "Error retrieving compute capability",
			nvmlDevice: &mock.Device{
				GetCudaComputeCapabilityFunc: func() (int, int, nvml.Return) {
					return 0, 0, nvml.ERROR_UNKNOWN
				},
			},
			expectedError: nvml.ERROR_UNKNOWN,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			device := nvmlDevice{Device: tc.nvmlDevice}
			cc, err := device.GetComputeCapability()

			if tc.expectedError == nil {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tc.expectedError.Error())
			}
			require.Equal(t, tc.expectedCC, cc)
		})
	}
}

func TestNvmlMigDevice_GetComputeCapability(t *testing.T) {
	t.Run("delegates to parent device", func(t *testing.T) {
		parent := &mock.Device{
			GetCudaComputeCapabilityFunc: func() (int, int, nvml.Return) {
				return 9, 0, nvml.SUCCESS
			},
		}
		device := nvmlMigDevice{Device: &mock.Device{
			GetDeviceHandleFromMigDeviceHandleFunc: func() (nvml.Device, nvml.Return) {
				return parent, nvml.SUCCESS
			},
		}}
		cc, err := device.GetComputeCapability()
		require.NoError(t, err)
		require.Equal(t, "9.0", cc)
	})

	t.Run("fails to resolve parent device", func(t *testing.T) {
		device := nvmlMigDevice{Device: &mock.Device{
			GetDeviceHandleFromMigDeviceHandleFunc: func() (nvml.Device, nvml.Return) {
				return nil, nvml.ERROR_UNKNOWN
			},
		}}
		_, err := device.GetComputeCapability()
		require.ErrorContains(t, err, "failed to get parent device")
	})
}

func TestNvmlMigDevice_GetPaths(t *testing.T) {
	testCases := []struct {
		description string
		nvmlDevice  nvml.Device
		errContains string
	}{
		{
			description: "fails to get GPU instance ID",
			nvmlDevice: &mock.Device{
				GetGpuInstanceIdFunc: func() (int, nvml.Return) {
					return 0, nvml.ERROR_UNKNOWN
				},
			},
			errContains: "error getting GPU Instance ID",
		},
		{
			description: "fails to get compute instance ID",
			nvmlDevice: &mock.Device{
				GetGpuInstanceIdFunc: func() (int, nvml.Return) {
					return 0, nvml.SUCCESS
				},
				GetComputeInstanceIdFunc: func() (int, nvml.Return) {
					return 0, nvml.ERROR_UNKNOWN
				},
			},
			errContains: "error getting Compute Instance ID",
		},
		{
			description: "fails to resolve parent device",
			nvmlDevice: &mock.Device{
				GetGpuInstanceIdFunc: func() (int, nvml.Return) {
					return 0, nvml.SUCCESS
				},
				GetComputeInstanceIdFunc: func() (int, nvml.Return) {
					return 0, nvml.SUCCESS
				},
				GetDeviceHandleFromMigDeviceHandleFunc: func() (nvml.Device, nvml.Return) {
					return nil, nvml.ERROR_UNKNOWN
				},
			},
			errContains: "error getting parent device",
		},
		{
			description: "fails to get parent minor number",
			nvmlDevice: &mock.Device{
				GetGpuInstanceIdFunc: func() (int, nvml.Return) {
					return 0, nvml.SUCCESS
				},
				GetComputeInstanceIdFunc: func() (int, nvml.Return) {
					return 0, nvml.SUCCESS
				},
				GetDeviceHandleFromMigDeviceHandleFunc: func() (nvml.Device, nvml.Return) {
					return &mock.Device{
						GetMinorNumberFunc: func() (int, nvml.Return) {
							return 0, nvml.ERROR_UNKNOWN
						},
					}, nvml.SUCCESS
				},
			},
			errContains: "error getting GPU device minor number",
		},
		{
			description: "missing MIG capability device paths (no MIG hardware present)",
			nvmlDevice: &mock.Device{
				GetGpuInstanceIdFunc: func() (int, nvml.Return) {
					return 0, nvml.SUCCESS
				},
				GetComputeInstanceIdFunc: func() (int, nvml.Return) {
					return 0, nvml.SUCCESS
				},
				GetDeviceHandleFromMigDeviceHandleFunc: func() (nvml.Device, nvml.Return) {
					return &mock.Device{
						GetMinorNumberFunc: func() (int, nvml.Return) {
							return 0, nvml.SUCCESS
						},
					}, nvml.SUCCESS
				},
			},
			errContains: "missing MIG GPU instance capability path",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			device := nvmlMigDevice{Device: tc.nvmlDevice}
			paths, err := device.GetPaths()
			require.Nil(t, paths)
			require.ErrorContains(t, err, tc.errContains)
		})
	}
}

func TestNvmlMigDevice_GetNumaNode(t *testing.T) {
	t.Run("delegates to parent device", func(t *testing.T) {
		parent := &mock.Device{
			GetPciInfoFunc: func() (nvml.PciInfo, nvml.Return) {
				return nvml.PciInfo{BusId: [32]int8{'0', '0', '0', '0', ':', 'f', 'f', ':', '0', '0', '.', '0', 0}}, nvml.SUCCESS
			},
		}
		device := nvmlMigDevice{Device: &mock.Device{
			GetDeviceHandleFromMigDeviceHandleFunc: func() (nvml.Device, nvml.Return) {
				return parent, nvml.SUCCESS
			},
		}}
		hasNode, node, err := device.GetNumaNode()
		require.NoError(t, err)
		require.False(t, hasNode)
		require.Equal(t, 0, node)
	})

	t.Run("fails to resolve parent device", func(t *testing.T) {
		device := nvmlMigDevice{Device: &mock.Device{
			GetDeviceHandleFromMigDeviceHandleFunc: func() (nvml.Device, nvml.Return) {
				return nil, nvml.ERROR_UNKNOWN
			},
		}}
		_, _, err := device.GetNumaNode()
		require.ErrorContains(t, err, "error getting parent GPU device from MIG device")
	})
}

func TestNvmlDevice_GetTotalMemory(t *testing.T) {
	testCases := []struct {
		description   string
		nvmlDevice    nvml.Device
		expectedTotal uint64
		expectedError error
	}{
		{
			description: "Successful memory retrieval",
			nvmlDevice: &mock.Device{
				GetMemoryInfoFunc: func() (nvml.Memory, nvml.Return) {
					return nvml.Memory{Total: 16 * 1024 * 1024 * 1024}, nvml.SUCCESS
				},
			},
			expectedTotal: 16 * 1024 * 1024 * 1024,
		},
		{
			description: "Error retrieving memory",
			nvmlDevice: &mock.Device{
				GetMemoryInfoFunc: func() (nvml.Memory, nvml.Return) {
					return nvml.Memory{}, nvml.ERROR_UNKNOWN
				},
			},
			expectedError: nvml.ERROR_UNKNOWN,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			device := nvmlDevice{Device: tc.nvmlDevice}
			total, err := device.GetTotalMemory()

			if tc.expectedError == nil {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tc.expectedError.Error())
			}
			require.Equal(t, tc.expectedTotal, total)
		})
	}
}

func TestNvmlMigDevice_GetTotalMemory(t *testing.T) {
	testCases := []struct {
		description   string
		nvmlDevice    nvml.Device
		expectedTotal uint64
		expectedError error
	}{
		{
			description: "Successful memory retrieval",
			nvmlDevice: &mock.Device{
				GetMemoryInfoFunc: func() (nvml.Memory, nvml.Return) {
					return nvml.Memory{Total: 5 * 1024 * 1024 * 1024}, nvml.SUCCESS
				},
			},
			expectedTotal: 5 * 1024 * 1024 * 1024,
		},
		{
			description: "Error retrieving memory",
			nvmlDevice: &mock.Device{
				GetMemoryInfoFunc: func() (nvml.Memory, nvml.Return) {
					return nvml.Memory{}, nvml.ERROR_UNKNOWN
				},
			},
			expectedError: nvml.ERROR_UNKNOWN,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			device := nvmlMigDevice{Device: tc.nvmlDevice}
			total, err := device.GetTotalMemory()

			if tc.expectedError == nil {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tc.expectedError.Error())
			}
			require.Equal(t, tc.expectedTotal, total)
		})
	}
}
