/*
Copyright 2025 The HAMi Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package metax

import (
	"reflect"
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

func TestConvertMetaxSDeviceToHAMIDevice(t *testing.T) {
	for _, ts := range []struct {
		name  string
		metax []*MetaxSDeviceInfo

		expected []*device.DeviceInfo
	}{
		{
			name: "base test",
			metax: []*MetaxSDeviceInfo{
				{
					UUID:              "GPU-a16ac188-0592-5c8f-2b6e-8bd8e7a604a9",
					BDF:               "0000:0a:00.0",
					Model:             "native",
					TotalDevCount:     16,
					TotalCompute:      100,
					TotalVRam:         64 * 1024,
					AvailableDevCount: 6,
					AvailableCompute:  50,
					AvailableVRam:     32 * 1024,
					Numa:              0,
					Healthy:           true,
					QosPolicy:         BestEffort,
					LinkZone:          1,
				},
				{
					UUID:              "GPU-a16ac188-0592-5c8f-2b6e-8bd8e7a604a8",
					BDF:               "0000:09:00.0",
					Model:             "sgpu",
					TotalDevCount:     8,
					TotalCompute:      100,
					TotalVRam:         32 * 1024,
					AvailableDevCount: 6,
					AvailableCompute:  50,
					AvailableVRam:     32 * 1024,
					Numa:              -1,
					Healthy:           false,
					QosPolicy:         BurstShare,
					LinkZone:          2,
				},
			},
			expected: []*device.DeviceInfo{
				{
					ID:           "GPU-a16ac188-0592-5c8f-2b6e-8bd8e7a604a9",
					Index:        0,
					Count:        16,
					Devmem:       64 * 1024,
					Devcore:      100,
					Type:         MetaxSGPUDevice,
					Numa:         0,
					Mode:         "",
					MIGTemplate:  []device.Geometry{},
					Health:       true,
					DeviceVendor: MetaxSGPUDevice,
					CustomInfo: map[string]any{
						"QosPolicy": BestEffort,
						"Model":     "native",
						"LinkZone":  int32(1),
					},
				},
				{
					ID:           "GPU-a16ac188-0592-5c8f-2b6e-8bd8e7a604a8",
					Index:        1,
					Count:        8,
					Devmem:       32 * 1024,
					Devcore:      100,
					Type:         MetaxSGPUDevice,
					Numa:         -1,
					Mode:         "",
					MIGTemplate:  []device.Geometry{},
					Health:       false,
					DeviceVendor: MetaxSGPUDevice,
					CustomInfo: map[string]any{
						"QosPolicy": BurstShare,
						"Model":     "sgpu",
						"LinkZone":  int32(2),
					},
				},
			},
		},
	} {
		t.Run(ts.name, func(t *testing.T) {
			result := convertMetaxSDeviceToHAMIDevice(ts.metax)

			if !reflect.DeepEqual(ts.expected, result) {
				t.Errorf("convertMetaxSDeviceToHAMIDevice failed: result %v, expected %v",
					result, ts.expected)
			}
		})
	}
}

func TestConvertHAMIPodDeviceToMetaxPodDevice(t *testing.T) {
	for _, ts := range []struct {
		name string
		hami device.PodSingleDevice

		expected PodMetaxSDevice
	}{
		{
			name: "base test",
			hami: device.PodSingleDevice{
				{
					{
						Idx:       0,
						UUID:      "GPU-a16ac188-0592-5c8f-2b6e-8bd8e7a604a0",
						Type:      MetaxGPUDevice,
						Usedmem:   10,
						Usedcores: 50,
					},
					{
						Idx:       1,
						UUID:      "GPU-a16ac188-0592-5c8f-2b6e-8bd8e7a604a1",
						Type:      MetaxGPUDevice,
						Usedmem:   1024,
						Usedcores: 30,
					},
				},
				{
					{
						Idx:       3,
						UUID:      "GPU-a16ac188-0592-5c8f-2b6e-8bd8e7a604a3",
						Type:      MetaxGPUDevice,
						Usedmem:   10 * 1024,
						Usedcores: 20,
					},
					{
						Idx:       7,
						UUID:      "GPU-a16ac188-0592-5c8f-2b6e-8bd8e7a604a7",
						Type:      MetaxGPUDevice,
						Usedmem:   64 * 1024,
						Usedcores: 80,
					},
				},
			},
			expected: PodMetaxSDevice{
				{
					{
						UUID:    "GPU-a16ac188-0592-5c8f-2b6e-8bd8e7a604a0",
						Compute: 50,
						VRam:    10,
					},
					{
						UUID:    "GPU-a16ac188-0592-5c8f-2b6e-8bd8e7a604a1",
						Compute: 30,
						VRam:    1024,
					},
				},
				{
					{
						UUID:    "GPU-a16ac188-0592-5c8f-2b6e-8bd8e7a604a3",
						Compute: 20,
						VRam:    10 * 1024,
					},
					{
						UUID:    "GPU-a16ac188-0592-5c8f-2b6e-8bd8e7a604a7",
						Compute: 80,
						VRam:    64 * 1024,
					},
				},
			},
		},
	} {
		t.Run(ts.name, func(t *testing.T) {
			result := convertHAMIPodDeviceToMetaxPodDevice(ts.hami)

			if !reflect.DeepEqual(ts.expected, result) {
				t.Errorf("convertMetaxSDeviceToHAMIDevice failed: result %v, expected %v",
					result, ts.expected)
			}
		})
	}
}
