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

package v1

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImexUnmarshal(t *testing.T) {
	testCases := []struct {
		description   string
		input         string
		expected      Imex
		expectedError error
	}{
		{
			description: "empty json",
			input:       "{}",
			expected:    Imex{},
		},
		{
			description: "null channel ID is valid",
			input:       `{"channelIDs": null}`,
			expected:    Imex{},
		},
		{
			description: "empty channel ID is valid",
			input:       `{"channelIDs": []}`,
			expected: Imex{
				ChannelIDs: []int{},
			},
		},
		{
			description: "single 0 channel ID is valid",
			input:       `{"channelIDs": [0]}`,
			expected: Imex{
				ChannelIDs: []int{0},
			},
		},
		{
			description: "single 0 channel ID as int is valid",
			input:       `{"channelIDs": [0]}`,
			expected: Imex{
				ChannelIDs: []int{0},
			},
		},
		{
			description: "invalid cases",
			input:       `{"channelIDs": [2]}`,
			expected: Imex{
				ChannelIDs: []int{2},
			},
			expectedError: errInvalidImexConfig,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			var output Imex
			err := json.Unmarshal([]byte(tc.input), &output)
			require.ErrorIs(t, errors.Join(err, AssertChannelIDsValid(output.ChannelIDs)), tc.expectedError)
			require.Equal(t, tc.expected, output)
		})
	}
}
