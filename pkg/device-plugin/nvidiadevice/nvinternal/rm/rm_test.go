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
	"testing"

	"github.com/stretchr/testify/require"

	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
)

func TestValidateRequest(t *testing.T) {
	testCases := []struct {
		description       string
		devices           Devices
		sharing           spec.Sharing
		requestDevicesIDs []string

		expectedError error
	}{
		{
			description: "valid device IDs -- no sharing",
			devices: Devices{
				"device0": nil,
				"device1": nil,
			},
			requestDevicesIDs: []string{"device1"},
		},
		{
			description: "invalid device IDs -- no sharing",
			devices: Devices{
				"device0": nil,
				"device1": nil,
			},
			requestDevicesIDs: []string{"device1", "device2"},
			expectedError:     errInvalidRequest,
		},
		{
			description: "timeslicing with single device",
			sharing: spec.Sharing{
				TimeSlicing: spec.ReplicatedResources{
					Resources: []spec.ReplicatedResource{
						{
							Name:     "nvidia.com/gpu",
							Replicas: 2,
						},
					},
				},
			},
			devices: Devices{
				"device0::0": nil,
				"device0::1": nil,
				"device1::0": nil,
				"device1::1": nil,
			},
			requestDevicesIDs: []string{"device0::1"},
		},
		{
			description: "timeslicing with two devices",
			sharing: spec.Sharing{
				TimeSlicing: spec.ReplicatedResources{
					Resources: []spec.ReplicatedResource{
						{
							Name:     "nvidia.com/gpu",
							Replicas: 2,
						},
					},
				},
			},
			devices: Devices{
				"device0::0": nil,
				"device0::1": nil,
				"device1::0": nil,
				"device1::1": nil,
			},
			requestDevicesIDs: []string{"device0::1", "device1::0"},
		},
		{
			description: "timeslicing with two devices -- failRequestsGreaterThanOne",
			sharing: spec.Sharing{
				TimeSlicing: spec.ReplicatedResources{
					FailRequestsGreaterThanOne: true,
					Resources: []spec.ReplicatedResource{
						{
							Name:     "nvidia.com/gpu",
							Replicas: 2,
						},
					},
				},
			},
			devices: Devices{
				"device0::0": nil,
				"device0::1": nil,
				"device1::0": nil,
				"device1::1": nil,
			},
			requestDevicesIDs: []string{"device0::1", "device1::0"},
			expectedError:     errInvalidRequest,
		},
		{
			description: "MPS with single device",
			sharing: spec.Sharing{
				MPS: &spec.ReplicatedResources{
					Resources: []spec.ReplicatedResource{
						{
							Name:     "nvidia.com/gpu",
							Replicas: 2,
						},
					},
				},
			},
			devices: Devices{
				"device0::0": nil,
				"device0::1": nil,
				"device1::0": nil,
				"device1::1": nil,
			},
			requestDevicesIDs: []string{"device0::1"},
		},
		{
			description: "MPS with two devices",
			sharing: spec.Sharing{
				MPS: &spec.ReplicatedResources{
					Resources: []spec.ReplicatedResource{
						{
							Name:     "nvidia.com/gpu",
							Replicas: 2,
						},
					},
				},
			},
			devices: Devices{
				"device0::0": nil,
				"device0::1": nil,
				"device1::0": nil,
				"device1::1": nil,
			},
			requestDevicesIDs: []string{"device0::1", "device1::0"},
			expectedError:     errInvalidRequest,
		},
		{
			description: "MPS with two devices -- failRequestsGreaterThanOne",
			sharing: spec.Sharing{
				MPS: &spec.ReplicatedResources{
					FailRequestsGreaterThanOne: true,
					Resources: []spec.ReplicatedResource{
						{
							Name:     "nvidia.com/gpu",
							Replicas: 2,
						},
					},
				},
			},
			devices: Devices{
				"device0::0": nil,
				"device0::1": nil,
				"device1::0": nil,
				"device1::1": nil,
			},
			requestDevicesIDs: []string{"device0::1", "device1::0"},
			expectedError:     errInvalidRequest,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			r := resourceManager{
				config: &spec.Config{
					Sharing: tc.sharing,
				},
				devices: tc.devices,
			}
			err := r.ValidateRequest(tc.requestDevicesIDs)
			require.ErrorIs(t, err, tc.expectedError)
		})
	}
}
