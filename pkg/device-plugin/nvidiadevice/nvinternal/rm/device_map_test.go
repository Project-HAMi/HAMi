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

	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"

	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"github.com/stretchr/testify/require"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

func TestDeviceMapInsert(t *testing.T) {
	device0 := Device{Device: kubeletdevicepluginv1beta1.Device{ID: "0"}}
	device0withIndex := Device{Device: kubeletdevicepluginv1beta1.Device{ID: "0"}, Index: "index"}
	device1 := Device{Device: kubeletdevicepluginv1beta1.Device{ID: "1"}}

	testCases := []struct {
		description       string
		deviceMap         DeviceMap
		key               string
		value             *Device
		expectedDeviceMap DeviceMap
	}{
		{
			description: "insert into empty map",
			deviceMap:   make(DeviceMap),
			key:         "resource",
			value:       &device0,
			expectedDeviceMap: DeviceMap{
				"resource": Devices{
					"0": &device0,
				},
			},
		},
		{
			description: "add to existing resource",
			deviceMap: DeviceMap{
				"resource": Devices{
					"0": &device0,
				},
			},
			key:   "resource",
			value: &device1,
			expectedDeviceMap: DeviceMap{
				"resource": Devices{
					"0": &device0,
					"1": &device1,
				},
			},
		},
		{
			description: "add new resource",
			deviceMap: DeviceMap{
				"resource": Devices{
					"0": &device0,
				},
			},
			key:   "resource1",
			value: &device0,
			expectedDeviceMap: DeviceMap{
				"resource": Devices{
					"0": &device0,
				},
				"resource1": Devices{
					"0": &device0,
				},
			},
		},
		{
			description: "overwrite existing device",
			deviceMap: DeviceMap{
				"resource": Devices{
					"0": &device0,
				},
			},
			key:   "resource",
			value: &device0withIndex,
			expectedDeviceMap: DeviceMap{
				"resource": Devices{
					"0": &device0withIndex,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			tc.deviceMap.insert(spec.ResourceName(tc.key), tc.value)

			require.EqualValues(t, tc.expectedDeviceMap, tc.deviceMap)
		})
	}
}

func TestUpdateDeviceMapWithReplicas(t *testing.T) {
	device0 := Device{Device: kubeletdevicepluginv1beta1.Device{ID: "0"}, Index: "0"}
	device1 := Device{Device: kubeletdevicepluginv1beta1.Device{ID: "1"}}
	device2 := Device{Device: kubeletdevicepluginv1beta1.Device{ID: "2"}}
	device3 := Device{Device: kubeletdevicepluginv1beta1.Device{ID: "3"}}

	testCases := []struct {
		description       string
		config            *nvidia.DeviceConfig
		devices           DeviceMap
		expectedDeviceMap DeviceMap
	}{
		{
			description: "Update device map with replicas",
			config: &nvidia.DeviceConfig{
				Config: &spec.Config{
					Sharing: spec.Sharing{
						TimeSlicing: spec.ReplicatedResources{
							Resources: []spec.ReplicatedResource{
								{
									Name:     "resource1",
									Replicas: 2,
									Rename:   "replicated-resource1",
									Devices: spec.ReplicatedDevices{
										All: true,
									},
								},
								{
									Name:     "resource2",
									Replicas: 1,
									Devices: spec.ReplicatedDevices{
										All: true,
									},
								},
							},
						},
					},
				},
			},
			devices: DeviceMap{
				"resource1": Devices{
					"0": &device0,
					"1": &device1,
				},
				"resource2": Devices{
					"2": &device2,
				},
				"resource3": Devices{
					"3": &device3,
				},
			},
			expectedDeviceMap: DeviceMap{
				"replicated-resource1": Devices{
					"0::0": &Device{Device: kubeletdevicepluginv1beta1.Device{ID: "0::0"}, Index: "0", Replicas: 2},
					"0::1": &Device{Device: kubeletdevicepluginv1beta1.Device{ID: "0::1"}, Index: "0", Replicas: 2},
					"1::0": &Device{Device: kubeletdevicepluginv1beta1.Device{ID: "1::0"}, Replicas: 2},
					"1::1": &Device{Device: kubeletdevicepluginv1beta1.Device{ID: "1::1"}, Replicas: 2},
				},
				"resource2": Devices{
					"2::0": &Device{Device: kubeletdevicepluginv1beta1.Device{ID: "2::0"}, Replicas: 1},
				},
				"resource3": Devices{
					"3": &device3,
				},
			},
		},
		{
			description: "Some devices are not replicated",
			config: &nvidia.DeviceConfig{
				Config: &spec.Config{
					Sharing: spec.Sharing{
						TimeSlicing: spec.ReplicatedResources{
							Resources: []spec.ReplicatedResource{
								{
									Name:     "resource1",
									Replicas: 2,
									Rename:   "replicated-resource1",
									Devices: spec.ReplicatedDevices{
										List: []spec.ReplicatedDeviceRef{"0"}, // only replicate index 0
									},
								},
							},
						},
					},
				},
			},
			devices: DeviceMap{
				"resource1": Devices{
					"0": &device0,
					"1": &device1,
				},
			},
			expectedDeviceMap: DeviceMap{
				"replicated-resource1": Devices{
					"0::0": &Device{Device: kubeletdevicepluginv1beta1.Device{ID: "0::0"}, Index: "0", Replicas: 2},
					"0::1": &Device{Device: kubeletdevicepluginv1beta1.Device{ID: "0::1"}, Index: "0", Replicas: 2},
				},
				"resource1": Devices{
					"1": &device1,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			devices, _ := updateDeviceMapWithReplicas(&tc.config.Config.Sharing.TimeSlicing, tc.devices)
			require.EqualValues(t, tc.expectedDeviceMap, devices)
		})
	}
}

func TestDeviceMapMerge(t *testing.T) {
	device0 := Device{Device: kubeletdevicepluginv1beta1.Device{ID: "0"}}
	device1 := Device{Device: kubeletdevicepluginv1beta1.Device{ID: "1"}}
	device2 := Device{Device: kubeletdevicepluginv1beta1.Device{ID: "2"}}
	device0Updated := Device{Device: kubeletdevicepluginv1beta1.Device{ID: "0"}, Index: "updated"}

	testCases := []struct {
		description       string
		deviceMap         DeviceMap
		otherDeviceMap    DeviceMap
		expectedDeviceMap DeviceMap
	}{
		{
			description:    "merge into empty map",
			deviceMap:      make(DeviceMap),
			otherDeviceMap: DeviceMap{"resource": Devices{"0": &device0}},
			expectedDeviceMap: DeviceMap{
				"resource": Devices{
					"0": &device0,
				},
			},
		},
		{
			description: "merge from empty map",
			deviceMap: DeviceMap{
				"resource": Devices{
					"0": &device0,
				},
			},
			otherDeviceMap: make(DeviceMap),
			expectedDeviceMap: DeviceMap{
				"resource": Devices{
					"0": &device0,
				},
			},
		},
		{
			description: "merge with overlapping keys",
			deviceMap: DeviceMap{
				"resource": Devices{
					"0": &device0,
				},
			},
			otherDeviceMap: DeviceMap{
				"resource": Devices{
					"1": &device1,
				},
			},
			expectedDeviceMap: DeviceMap{
				"resource": Devices{
					"0": &device0,
					"1": &device1,
				},
			},
		},
		{
			description: "merge with device ID conflict (overwrite existing device)",
			deviceMap: DeviceMap{
				"resource": Devices{
					"0": &device0,
				},
			},
			otherDeviceMap: DeviceMap{
				"resource": Devices{
					"0": &device0Updated,
				},
			},
			expectedDeviceMap: DeviceMap{
				"resource": Devices{
					"0": &device0Updated,
				},
			},
		},
		{
			description: "merge with new resource",
			deviceMap: DeviceMap{
				"resource1": Devices{
					"0": &device0,
				},
			},
			otherDeviceMap: DeviceMap{
				"resource2": Devices{
					"1": &device1,
				},
			},
			expectedDeviceMap: DeviceMap{
				"resource1": Devices{
					"0": &device0,
				},
				"resource2": Devices{
					"1": &device1,
				},
			},
		},
		{
			description: "merge with multiple devices and resources",
			deviceMap: DeviceMap{
				"resource1": Devices{
					"0": &device0,
				},
			},
			otherDeviceMap: DeviceMap{
				"resource1": Devices{
					"1": &device1,
				},
				"resource2": Devices{
					"2": &device2,
				},
			},
			expectedDeviceMap: DeviceMap{
				"resource1": Devices{
					"0": &device0,
					"1": &device1,
				},
				"resource2": Devices{
					"2": &device2,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			tc.deviceMap.merge(tc.otherDeviceMap)

			require.EqualValues(t, tc.expectedDeviceMap, tc.deviceMap)
		})
	}
}

func TestDeviceMapIsEmpty(t *testing.T) {
	device0 := Device{Device: kubeletdevicepluginv1beta1.Device{ID: "0"}}

	testCases := []struct {
		description string
		deviceMap   DeviceMap
		expected    bool
	}{
		{
			description: "empty map",
			deviceMap:   make(DeviceMap),
			expected:    true,
		},
		{
			description: "map with empty resource",
			deviceMap: DeviceMap{
				"resource": Devices{},
			},
			expected: true,
		},
		{
			description: "map with non-empty resource",
			deviceMap: DeviceMap{
				"resource": Devices{
					"0": &device0,
				},
			},
			expected: false,
		},
		{
			description: "map with multiple empty resources",
			deviceMap: DeviceMap{
				"resource1": Devices{},
				"resource2": Devices{},
			},
			expected: true,
		},
		{
			description: "map with multiple resources, one non-empty",
			deviceMap: DeviceMap{
				"resource1": Devices{},
				"resource2": Devices{
					"0": &device0,
				},
			},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			actual := tc.deviceMap.isEmpty()

			require.Equal(t, tc.expected, actual)
		})
	}
}

func TestDeviceMapGetIDsOfDevicesToReplicate(t *testing.T) {
	device0 := Device{Device: kubeletdevicepluginv1beta1.Device{ID: "0"}, Index: "0"}
	device1 := Device{Device: kubeletdevicepluginv1beta1.Device{ID: "1"}, Index: "1"}
	device2 := Device{Device: kubeletdevicepluginv1beta1.Device{ID: "2"}, Index: "2"}
	device3 := Device{Device: kubeletdevicepluginv1beta1.Device{ID: "3"}, Index: "3"}

	deviceMap := DeviceMap{
		"resource1": Devices{
			"0": &device0,
			"1": &device1,
			"2": &device2,
			"GPU-8dcd427f-483b-b48f-d7e5-75fb19a52b76": &device3,
		},
	}

	testCases := []struct {
		description string
		deviceMap   DeviceMap
		resource    *spec.ReplicatedResource
		expectedIDs []string
		expectedErr error
	}{
		{
			description: "resource does not exist",
			deviceMap:   deviceMap,
			resource: &spec.ReplicatedResource{
				Name:    "nonexistent_resource",
				Devices: spec.ReplicatedDevices{},
			},
			expectedIDs: nil,
			expectedErr: nil,
		},
		{
			description: "replicate all devices",
			deviceMap:   deviceMap,
			resource: &spec.ReplicatedResource{
				Name: "resource1",
				Devices: spec.ReplicatedDevices{
					All: true,
				},
			},
			expectedIDs: []string{"0", "1", "2", "3"},
			expectedErr: nil,
		},
		{
			description: "replicate specific count of devices (count exceeds available)",
			deviceMap:   deviceMap,
			resource: &spec.ReplicatedResource{
				Name: "resource1",
				Devices: spec.ReplicatedDevices{
					Count: 5,
				},
			},
			expectedIDs: nil,
			expectedErr: fmt.Errorf("requested 5 devices to be replicated, but only 4 devices available"),
		},
		{
			description: "replicate specific devices by ID (valid)",
			deviceMap:   deviceMap,
			resource: &spec.ReplicatedResource{
				Name: "resource1",
				Devices: spec.ReplicatedDevices{
					List: []spec.ReplicatedDeviceRef{
						spec.ReplicatedDeviceRef("GPU-8dcd427f-483b-b48f-d7e5-75fb19a52b76"), // ref UUID
					},
				},
			},
			expectedIDs: []string{"3"},
			expectedErr: nil,
		},
		{
			description: "replicate specific devices by ID (invalid ID)",
			deviceMap:   deviceMap,
			resource: &spec.ReplicatedResource{
				Name: "resource1",
				Devices: spec.ReplicatedDevices{
					List: []spec.ReplicatedDeviceRef{
						spec.ReplicatedDeviceRef("GPU-8dcd427f-483b-b48f-d7e5-75fb19a52b77"), // Nonexistent device
					},
				},
			},
			expectedIDs: nil,
			expectedErr: fmt.Errorf("no matching device with UUID: GPU-8dcd427f-483b-b48f-d7e5-75fb19a52b77"),
		},
		{
			description: "replicate specific devices by GPU index (valid)",
			deviceMap:   deviceMap,
			resource: &spec.ReplicatedResource{
				Name: "resource1",
				Devices: spec.ReplicatedDevices{
					List: []spec.ReplicatedDeviceRef{
						spec.ReplicatedDeviceRef("0"), // Index: "0"
						spec.ReplicatedDeviceRef("1"), // Index: "1"
					},
				},
			},
			expectedIDs: []string{"0", "1"},
			expectedErr: nil,
		},
		{
			description: "replicate specific devices by GPU index (invalid)",
			deviceMap:   deviceMap,
			resource: &spec.ReplicatedResource{
				Name: "resource1",
				Devices: spec.ReplicatedDevices{
					List: []spec.ReplicatedDeviceRef{
						spec.ReplicatedDeviceRef("0"), // Index: "0"
						spec.ReplicatedDeviceRef("4"), // Nonexistent Index
					},
				},
			},
			expectedIDs: nil,
			expectedErr: fmt.Errorf("no matching device at index: 4"),
		},
		{
			description: "invalid replicated devices",
			deviceMap:   deviceMap,
			resource: &spec.ReplicatedResource{
				Name: "resource1",
				Devices: spec.ReplicatedDevices{
					List: []spec.ReplicatedDeviceRef{
						spec.ReplicatedDeviceRef("invalid_index"), // Invalid gpu
					},
				},
			},
			expectedIDs: nil,
			expectedErr: nil,
		},
		{
			description: "unexpected error (no replication criteria provided)",
			deviceMap:   deviceMap,
			resource: &spec.ReplicatedResource{
				Name:    "resource1",
				Devices: spec.ReplicatedDevices{},
			},
			expectedIDs: nil,
			expectedErr: fmt.Errorf("unexpected error"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			ids, err := tc.deviceMap.getIDsOfDevicesToReplicate(tc.resource)

			if tc.expectedErr != nil {
				require.Error(t, err)
				require.EqualError(t, err, tc.expectedErr.Error())
			} else {
				require.NoError(t, err)
			}

			require.ElementsMatch(t, tc.expectedIDs, ids)
		})
	}
}
