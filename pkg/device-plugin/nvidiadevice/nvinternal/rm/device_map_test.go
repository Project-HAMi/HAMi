/**
# Copyright (c) 2022, NVIDIA CORPORATION.  All rights reserved.
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
