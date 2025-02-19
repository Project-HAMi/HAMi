/**
# Copyright 2024 NVIDIA CORPORATION
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

package mps

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDevice(t *testing.T) {
	testCases := []struct {
		description            string
		input                  mpsDevice
		expectedIsAtLeastVolta bool
		expectedMaxClients     int
		expectedAssertReplicas error
	}{
		{
			description: "leading v ignored",
			input: mpsDevice{
				ComputeCapability: "v7.5",
			},
			expectedIsAtLeastVolta: true,
			expectedMaxClients:     48,
		},
		{
			description: "no-leading v supported",
			input: mpsDevice{
				ComputeCapability: "7.5",
			},
			expectedIsAtLeastVolta: true,
			expectedMaxClients:     48,
		},
		{
			description: "pre-volta clients",
			input: mpsDevice{
				ComputeCapability: "7.0",
			},
			expectedIsAtLeastVolta: false,
			expectedMaxClients:     16,
		},
		{
			description: "post-volta clients",
			input: mpsDevice{
				ComputeCapability: "9.0",
			},
			expectedIsAtLeastVolta: true,
			expectedMaxClients:     48,
		},
		{
			description: "pre-volta clients exceeded",
			input: mpsDevice{
				ComputeCapability: "7.0",
				Replicas:          29,
			},
			expectedIsAtLeastVolta: false,
			expectedMaxClients:     16,
			expectedAssertReplicas: errInvalidDevice,
		},
		{
			description: "post-volta clients exceeded",
			input: mpsDevice{
				ComputeCapability: "9.0",
				Replicas:          49,
			},
			expectedIsAtLeastVolta: true,
			expectedMaxClients:     48,
			expectedAssertReplicas: errInvalidDevice,
		},
		{
			description: "pre-volta clients max",
			input: mpsDevice{
				ComputeCapability: "7.0",
				Replicas:          16,
			},
			expectedIsAtLeastVolta: false,
			expectedMaxClients:     16,
		},
		{
			description: "post-volta clients max",
			input: mpsDevice{
				ComputeCapability: "9.0",
				Replicas:          48,
			},
			expectedIsAtLeastVolta: true,
			expectedMaxClients:     48,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			require.Equal(t, tc.expectedIsAtLeastVolta, tc.input.isAtLeastVolta())
			require.Equal(t, tc.expectedMaxClients, tc.input.maxClients())
			require.ErrorIs(t, tc.input.assertReplicas(), tc.expectedAssertReplicas)
		})
	}
}
