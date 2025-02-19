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
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package v1

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func NoErrorNewResourceName(n string) ResourceName {
	rn, _ := NewResourceName(n)
	return rn
}

func TestReplicatedDeviceRef(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{
			input:    "0",
			expected: "gpuIndex",
		},
		{
			input:    "0:0",
			expected: "migIndex",
		},
		{
			input:    "GPU-4cf8db2d-06c0-7d70-1a51-e59b25b2c16c",
			expected: "uuid",
		},
		{
			input:    "MIG-3eb87630-93d5-b2b6-b8ff-9b359caf4ee2",
			expected: "uuid",
		},
		{
			input:    "MIG-GPU-662077db-fa3f-0d8f-9502-21ab0ef058a2/10/0",
			expected: "uuid",
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("test case %d", i), func(t *testing.T) {
			switch tc.expected {
			case "gpuIndex":
				require.True(t, ReplicatedDeviceRef(tc.input).IsGPUIndex())
				require.False(t, ReplicatedDeviceRef(tc.input).IsMigIndex())
				require.False(t, ReplicatedDeviceRef(tc.input).IsUUID())
			case "migIndex":
				require.False(t, ReplicatedDeviceRef(tc.input).IsGPUIndex())
				require.True(t, ReplicatedDeviceRef(tc.input).IsMigIndex())
				require.False(t, ReplicatedDeviceRef(tc.input).IsUUID())
			case "uuid":
				require.False(t, ReplicatedDeviceRef(tc.input).IsGPUIndex())
				require.False(t, ReplicatedDeviceRef(tc.input).IsMigIndex())
				require.True(t, ReplicatedDeviceRef(tc.input).IsUUID())
			}
		})
	}
}

func TestMarshalReplicatedDevices(t *testing.T) {
	testCases := []struct {
		input  ReplicatedDevices
		output string
		err    bool
	}{
		{
			input: ReplicatedDevices{},
			err:   true,
		},
		{
			input: ReplicatedDevices{
				All: true,
			},
			output: `"all"`,
		},
		{
			input: ReplicatedDevices{
				Count: 2,
			},
			output: `2`,
		},
		{
			input: ReplicatedDevices{
				List: []ReplicatedDeviceRef{"0", "0:0", "GPU-4cf8db2d-06c0-7d70-1a51-e59b25b2c16c"},
			},
			output: `["0", "0:0", "GPU-4cf8db2d-06c0-7d70-1a51-e59b25b2c16c"]`,
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("test case %d", i), func(t *testing.T) {
			output, err := tc.input.MarshalJSON()
			if tc.err {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.JSONEq(t, tc.output, string(output))
		})
	}
}

func TestUnmarshalReplicatedDevices(t *testing.T) {
	testCases := []struct {
		input  string
		output ReplicatedDevices
		err    bool
	}{
		{
			input: ``,
			err:   true,
		},
		{
			input: `"not-all"`,
			err:   true,
		},
		{
			input: `-2`,
			err:   true,
		},
		{
			input: `2.0`,
			err:   true,
		},
		{
			input: `[-1]`,
			err:   true,
		},
		{
			input: `["-1"]`,
			err:   true,
		},
		{
			input: `["invalid-UUID"]`,
			err:   true,
		},
		{
			input: `["GPU-UUID"]`,
			err:   true,
		},
		{
			input: `["MIG-UUID"]`,
			err:   true,
		},
		{
			input: `["MIG-GPU-4cf8db2d-06c0-7d70-1a51-e59b25b2c16c"]`,
			err:   true,
		},
		{
			input: `"all"`,
			output: ReplicatedDevices{
				All: true,
			},
		},
		{
			input: `2`,
			output: ReplicatedDevices{
				Count: 2,
			},
		},
		{
			input: `[0]`,
			output: ReplicatedDevices{
				List: []ReplicatedDeviceRef{"0"},
			},
		},
		{
			input: `["0"]`,
			output: ReplicatedDevices{
				List: []ReplicatedDeviceRef{"0"},
			},
		},
		{
			input: `["0:0"]`,
			output: ReplicatedDevices{
				List: []ReplicatedDeviceRef{"0:0"},
			},
		},
		{
			input: `["GPU-4cf8db2d-06c0-7d70-1a51-e59b25b2c16c"]`,
			output: ReplicatedDevices{
				List: []ReplicatedDeviceRef{"GPU-4cf8db2d-06c0-7d70-1a51-e59b25b2c16c"},
			},
		},
		{
			input: `["MIG-4cf8db2d-06c0-7d70-1a51-e59b25b2c16c"]`,
			output: ReplicatedDevices{
				List: []ReplicatedDeviceRef{"MIG-4cf8db2d-06c0-7d70-1a51-e59b25b2c16c"},
			},
		},
		{
			input: `["MIG-GPU-4cf8db2d-06c0-7d70-1a51-e59b25b2c16c/0/0"]`,
			output: ReplicatedDevices{
				List: []ReplicatedDeviceRef{"MIG-GPU-4cf8db2d-06c0-7d70-1a51-e59b25b2c16c/0/0"},
			},
		},
		{
			input: `[0, "0:0", "GPU-4cf8db2d-06c0-7d70-1a51-e59b25b2c16c"]`,
			output: ReplicatedDevices{
				List: []ReplicatedDeviceRef{"0", "0:0", "GPU-4cf8db2d-06c0-7d70-1a51-e59b25b2c16c"},
			},
		},
		{
			input: `["0", "0:0", "GPU-4cf8db2d-06c0-7d70-1a51-e59b25b2c16c"]`,
			output: ReplicatedDevices{
				List: []ReplicatedDeviceRef{"0", "0:0", "GPU-4cf8db2d-06c0-7d70-1a51-e59b25b2c16c"},
			},
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("test case %d", i), func(t *testing.T) {
			var output ReplicatedDevices
			err := output.UnmarshalJSON([]byte(tc.input))
			if tc.err {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.output, output)
		})
	}
}

func TestUnmarshalReplicatedResource(t *testing.T) {
	testCases := []struct {
		input  string
		output ReplicatedResource
		err    bool
	}{
		{
			input: ``,
			err:   true,
		},
		{
			input: `{}`,
			err:   true,
		},
		{
			input: `{
				"name": "valid",
			}`,
			err: true,
		},
		{
			input: `{
				"name": "valid",
				"devices": "all",
			}`,
			err: true,
		},
		{
			input: `{
				"name": "valid",
				"devices": "all",
				"rename": "valid-shared",
			}`,
			err: true,
		},
		{
			input: `{
				"name": "valid",
				"devices": "all",
				"replicas": 2
			}`,
			output: ReplicatedResource{
				Name:     NoErrorNewResourceName("valid"),
				Devices:  ReplicatedDevices{All: true},
				Replicas: 2,
			},
		},
		{
			input: `{
				"name": "valid",
				"devices": "all",
				"replicas": 2,
				"rename": "valid-shared"
			}`,
			output: ReplicatedResource{
				Name:     NoErrorNewResourceName("valid"),
				Devices:  ReplicatedDevices{All: true},
				Replicas: 2,
				Rename:   NoErrorNewResourceName("valid-shared"),
			},
		},
		{
			input: `{
				"name": "valid",
				"replicas": -1,
			}`,
			err: true,
		},
		{
			input: `{
				"name": "valid",
				"replicas": 0,
			}`,
			err: true,
		},
		{
			input: `{
				"name": "valid",
				"replicas": 2
			}`,
			output: ReplicatedResource{
				Name:     NoErrorNewResourceName("valid"),
				Devices:  ReplicatedDevices{All: true},
				Replicas: 2,
			},
		},
		{
			input: `{
				"name": "valid",
				"replicas": 2,
				"rename": "valid-shared"
			}`,
			output: ReplicatedResource{
				Name:     NoErrorNewResourceName("valid"),
				Devices:  ReplicatedDevices{All: true},
				Replicas: 2,
				Rename:   NoErrorNewResourceName("valid-shared"),
			},
		},
		{
			input: `{
				"name": "$invalid$",
				"replicas": 2,
				"rename": "valid-shared"
			}`,
			err: true,
		},
		{
			input: `{
				"name": "valid",
				"replicas": 2,
				"rename": "$invalid$"
			}`,
			err: true,
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("test case %d", i), func(t *testing.T) {
			var output ReplicatedResource
			err := output.UnmarshalJSON([]byte(tc.input))
			if tc.err {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.output, output)
		})
	}
}

func TestUnmarshalReplicatedResources(t *testing.T) {
	testCases := []struct {
		input  string
		output ReplicatedResources
		err    bool
	}{
		{
			input: ``,
			err:   true,
		},
		{
			input: `{}`,
			err:   true,
		},
		{
			input: `{
				"resources": []
			}`,
			err: true,
		},
		{
			input: `{
				"resources": [
					{
						"name": "valid",
						"replicas": 2
					}
				]
			}`,
			output: ReplicatedResources{
				Resources: []ReplicatedResource{
					{
						Name:     NoErrorNewResourceName("valid"),
						Devices:  ReplicatedDevices{All: true},
						Replicas: 2,
					},
				},
			},
		},
		{
			input: `{
				"resources": [
					{
						"name": "valid1",
						"replicas": 2
					},
					{
						"name": "valid2",
						"replicas": 2
					}
				]
			}`,
			output: ReplicatedResources{
				Resources: []ReplicatedResource{
					{
						Name:     NoErrorNewResourceName("valid1"),
						Devices:  ReplicatedDevices{All: true},
						Replicas: 2,
					},
					{
						Name:     NoErrorNewResourceName("valid2"),
						Devices:  ReplicatedDevices{All: true},
						Replicas: 2,
					},
				},
			},
		},
		{
			input: `{
				"resources": [
					{
						"name": "$invalid$",
						"replicas": 2
					}
				]
			}`,
			err: true,
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("test case %d", i), func(t *testing.T) {
			var output ReplicatedResources
			err := output.UnmarshalJSON([]byte(tc.input))
			if tc.err {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.output, output)
		})
	}
}
