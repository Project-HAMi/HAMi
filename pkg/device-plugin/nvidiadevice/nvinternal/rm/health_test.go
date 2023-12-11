/**
# Copyright (c) 2021, NVIDIA CORPORATION.  All rights reserved.
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

package rm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetAdditionalXids(t *testing.T) {
	testCases := []struct {
		description string
		input       string
		expected    []uint64
	}{
		{
			description: "Empty input",
		},
		{
			description: "Only comma",
			input:       ",",
		},
		{
			description: "Non-integer input",
			input:       "not-an-int",
		},
		{
			description: "Single integer",
			input:       "68",
			expected:    []uint64{68},
		},
		{
			description: "Negative integer",
			input:       "-68",
		},
		{
			description: "Single integer with trailing spaces",
			input:       "68  ",
			expected:    []uint64{68},
		},
		{
			description: "Single integer followed by comma without trailing number",
			input:       "68,",
			expected:    []uint64{68},
		},
		{
			description: "Comma without preceding number followed by single integer",
			input:       ",68",
			expected:    []uint64{68},
		},
		{
			description: "Two comma-separated integers",
			input:       "68,67",
			expected:    []uint64{68, 67},
		},
		{
			description: "Two integers separated by non-integer",
			input:       "68,not-an-int,67",
			expected:    []uint64{68, 67},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			xids := getAdditionalXids(tc.input)
			require.EqualValues(t, tc.expected, xids)
		})
	}
}
