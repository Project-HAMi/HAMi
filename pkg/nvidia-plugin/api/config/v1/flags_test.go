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
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing,
* software distributed under the License is distributed on an
* "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
* KIND, either express or implied.  See the License for the
* specific language governing permissions and limitations
* under the License.
 */

/*
* Modifications Copyright The HAMi Authors. See
* GitHub history for details.
 */

package v1

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUnmarshalFlags(t *testing.T) {
	testCases := []struct {
		input  string
		output Flags
		err    bool
	}{
		{
			input: ``,
			err:   true,
		},
		{
			input:  `{}`,
			output: Flags{},
		},
		{
			input: `{
				"gfd": {}
			}`,
			output: Flags{
				CommandLineFlags{
					GFD: &GFDCommandLineFlags{},
				},
			},
		},
		{
			input: `{
				"gfd": {
					"sleepInterval": 0
				}
			}`,
			output: Flags{
				CommandLineFlags{
					GFD: &GFDCommandLineFlags{
						SleepInterval: ptr(Duration(0)),
					},
				},
			},
		},
		{
			input: `{
				"gfd": {
					"sleepInterval": "0s"
				}
			}`,
			output: Flags{
				CommandLineFlags{
					GFD: &GFDCommandLineFlags{
						SleepInterval: ptr(Duration(0)),
					},
				},
			},
		},
		{
			input: `{
				"gfd": {
					"sleepInterval": 5
				}
			}`,
			output: Flags{
				CommandLineFlags{
					GFD: &GFDCommandLineFlags{
						SleepInterval: ptr(Duration(5)),
					},
				},
			},
		},
		{
			input: `{
				"gfd": {
					"sleepInterval": "5s"
				}
			}`,
			output: Flags{
				CommandLineFlags{
					GFD: &GFDCommandLineFlags{
						SleepInterval: ptr(Duration(5 * time.Second)),
					},
				},
			},
		},
		{
			input: `{
				"plugin": {
					"deviceListStrategy": "envvar"
				}
			}`,
			output: Flags{
				CommandLineFlags{
					Plugin: &PluginCommandLineFlags{
						DeviceListStrategy: &deviceListStrategyFlag{"envvar"},
					},
				},
			},
		},
		{
			input: `{
				"plugin": {
					"deviceListStrategy": ["envvar", "cdi-annotations"]
				}
			}`,
			output: Flags{
				CommandLineFlags{
					Plugin: &PluginCommandLineFlags{
						DeviceListStrategy: &deviceListStrategyFlag{"envvar", "cdi-annotations"},
					},
				},
			},
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("test case %d", i), func(t *testing.T) {
			var output Flags
			err := json.Unmarshal([]byte(tc.input), &output)
			if tc.err {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.output, output)
		})
	}
}

func TestMarshalFlags(t *testing.T) {
	testCases := []struct {
		input  Flags
		output string
		err    bool
	}{
		{
			input: Flags{},
			output: `{
				"migStrategy": null,
				"failOnInitError": null,
				"gdsEnabled": null,
				"mofedEnabled": null,
				"useNodeFeatureAPI": null,
				"deviceDiscoveryStrategy": null
			}`,
		},
		{
			input: Flags{
				CommandLineFlags{
					GFD: &GFDCommandLineFlags{
						SleepInterval: ptr(Duration(0)),
					},
				},
			},
			output: `{
				"migStrategy": null,
				"failOnInitError": null,
				"gdsEnabled": null,
				"mofedEnabled": null,
				"useNodeFeatureAPI": null,
				"deviceDiscoveryStrategy": null,
				"gfd": {
					"oneshot": null,
					"noTimestamp": null,
					"outputFile": null,
					"sleepInterval": "0s",
					"machineTypeFile": null
				}
			}`,
		},
		{
			input: Flags{
				CommandLineFlags{
					GFD: &GFDCommandLineFlags{
						SleepInterval: ptr(Duration(5)),
					},
				},
			},
			output: `{
				"migStrategy": null,
				"failOnInitError": null,
				"gdsEnabled": null,
				"mofedEnabled": null,
				"useNodeFeatureAPI": null,
				"deviceDiscoveryStrategy": null,
				"gfd": {
					"oneshot": null,
					"noTimestamp": null,
					"outputFile": null,
					"sleepInterval": "5ns",
					"machineTypeFile": null
				}
			}`,
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("test case %d", i), func(t *testing.T) {
			output, err := json.Marshal(tc.input)
			if tc.err {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.JSONEq(t, tc.output, string(output))
		})
	}
}
