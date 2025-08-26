/*
Copyright 2024 The HAMi Authors.

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

package policy

import (
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

func TestDeviceUsageListLen(t *testing.T) {
	tests := []struct {
		name     string
		list     DeviceUsageList
		expected int
	}{
		{
			name:     "empty list",
			list:     DeviceUsageList{DeviceLists: []*DeviceListsScore{}},
			expected: 0,
		},
		{
			name: "list with items",
			list: DeviceUsageList{
				DeviceLists: []*DeviceListsScore{
					{
						Device: &device.DeviceUsage{
							ID:     "device1",
							Count:  1,
							Health: true,
						},
					},
					{
						Device: &device.DeviceUsage{
							ID:     "device2",
							Count:  2,
							Health: false,
						},
					},
				},
			},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.list.Len(); got != tt.expected {
				t.Errorf("DeviceUsageList.Len() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSwap(t *testing.T) {
	device1 := &device.DeviceUsage{
		ID:        "device1",
		Index:     1,
		Used:      50,
		Count:     100,
		Usedmem:   512,
		Totalmem:  1024,
		Totalcore: 8,
		Usedcores: 4,
		Numa:      0,
		Type:      "GPU",
		Health:    true,
	}

	device2 := &device.DeviceUsage{
		ID:        "device2",
		Index:     2,
		Used:      75,
		Count:     150,
		Usedmem:   768,
		Totalmem:  2048,
		Totalcore: 16,
		Usedcores: 8,
		Numa:      1,
		Type:      "CPU",
		Health:    false,
	}

	dul := &DeviceUsageList{
		DeviceLists: []*DeviceListsScore{
			{Device: device1, Score: 0.5},
			{Device: device2, Score: 0.75},
		},
		Policy: "some_policy",
	}

	dul.Swap(0, 1)

	expectedResult := []*DeviceListsScore{
		{Device: device2, Score: 0.75},
		{Device: device1, Score: 0.5},
	}

	for i, dls := range dul.DeviceLists {
		if dls.Device.ID != expectedResult[i].Device.ID || dls.Score != expectedResult[i].Score {
			t.Errorf("TestSwap failed: expected %v, got %v", expectedResult, dul.DeviceLists)
			break
		}
	}
}

func TestDeviceUsageList_Less(t *testing.T) {
	tests := []struct {
		name         string
		policy       string
		deviceLists  []*DeviceListsScore
		expectedLess bool
	}{
		{
			name:   "Binpack policy with same Numa",
			policy: "binpack",
			deviceLists: []*DeviceListsScore{
				{Device: &device.DeviceUsage{Numa: 0, Used: 10}, Score: 10},
				{Device: &device.DeviceUsage{Numa: 0, Used: 20}, Score: 20},
			},
			expectedLess: true,
		},
		{
			name:   "Binpack policy with different Numa true",
			policy: "binpack",
			deviceLists: []*DeviceListsScore{
				{Device: &device.DeviceUsage{Numa: 1, Used: 10}, Score: 10},
				{Device: &device.DeviceUsage{Numa: 0, Used: 20}, Score: 20},
			},
			expectedLess: true,
		},
		{
			name:   "Binpack policy with different Numa false",
			policy: "binpack",
			deviceLists: []*DeviceListsScore{
				{Device: &device.DeviceUsage{Numa: 0, Used: 10}, Score: 10},
				{Device: &device.DeviceUsage{Numa: 1, Used: 20}, Score: 20},
			},
			expectedLess: false,
		},
		{
			name:   "Spread policy with same Numa",
			policy: "spread",
			deviceLists: []*DeviceListsScore{
				{Device: &device.DeviceUsage{Numa: 0, Used: 10}, Score: 10},
				{Device: &device.DeviceUsage{Numa: 0, Used: 20}, Score: 20},
			},
			expectedLess: false,
		},
		{
			name:   "Spread policy with different Numa false",
			policy: "spread",
			deviceLists: []*DeviceListsScore{
				{Device: &device.DeviceUsage{Numa: 1, Used: 10}, Score: 10},
				{Device: &device.DeviceUsage{Numa: 0, Used: 20}, Score: 20},
			},
			expectedLess: false,
		},
		{
			name:   "Spread policy with different Numa true",
			policy: "spread",
			deviceLists: []*DeviceListsScore{
				{Device: &device.DeviceUsage{Numa: 0, Used: 10}, Score: 10},
				{Device: &device.DeviceUsage{Numa: 1, Used: 20}, Score: 20},
			},
			expectedLess: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := DeviceUsageList{
				Policy:      tt.policy,
				DeviceLists: tt.deviceLists,
			}
			i, j := 0, 1
			result := l.Less(i, j)
			if result != tt.expectedLess {
				t.Errorf("Expected %v, got %v", tt.expectedLess, result)
			}
		})
	}
}

func TestComputeScore(t *testing.T) {
	tests := []struct {
		name          string
		device        *device.DeviceUsage
		requests      device.ContainerDeviceRequests
		expectedScore float32
	}{
		{
			name: "ContainerDeviceRequests has no data",
			device: &device.DeviceUsage{
				ID:        "test-device",
				Totalcore: 4,
				Totalmem:  8192,
				Count:     10,
				Used:      2,
				Usedcores: 1,
				Usedmem:   2048,
			},
			requests:      make(device.ContainerDeviceRequests),
			expectedScore: 7,
		},
		{
			name: "ContainerDeviceRequests has  data",
			device: &device.DeviceUsage{
				ID:        "test-device",
				Totalcore: 4,
				Totalmem:  8192,
				Count:     10,
				Used:      2,
				Usedcores: 1,
				Usedmem:   2048,
			},
			requests: device.ContainerDeviceRequests{
				"container1": {
					Nums:             1,
					Type:             "type1",
					Memreq:           1024,
					MemPercentagereq: 0,
					Coresreq:         1,
				},
				"container2": {
					Nums:             2,
					Type:             "type2",
					Memreq:           0,
					MemPercentagereq: 50,
					Coresreq:         2,
				},
			},
			expectedScore: 18.75,
		},
		{
			name: "ContainerDeviceRequests has  data",
			device: &device.DeviceUsage{
				ID:        "test-device",
				Totalcore: 4,
				Totalmem:  8192,
				Count:     10,
				Used:      2,
				Usedcores: 1,
				Usedmem:   2048,
			},
			requests: device.ContainerDeviceRequests{
				"container1": {
					Nums:             1,
					Type:             "type1",
					Memreq:           1024,
					MemPercentagereq: 30,
					Coresreq:         1,
				},
				"container2": {
					Nums:             2,
					Type:             "type2",
					Memreq:           200,
					MemPercentagereq: 101,
					Coresreq:         3,
				},
			},
			expectedScore: 20.24414,
		},
		{
			name: "ContainerDeviceRequests has  data",
			device: &device.DeviceUsage{
				ID:        "test-device",
				Totalcore: 4,
				Totalmem:  8192,
				Count:     10,
				Used:      2,
				Usedcores: 1,
				Usedmem:   2048,
			},
			requests: device.ContainerDeviceRequests{
				"container1": {
					Nums:             1,
					Type:             "type1",
					Memreq:           1024,
					MemPercentagereq: 30,
					Coresreq:         1,
				},
				"container2": {
					Nums:             2,
					Type:             "type2",
					Memreq:           200,
					MemPercentagereq: 30,
					Coresreq:         3,
				},
			},
			expectedScore: 20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds := &DeviceListsScore{
				Device: tt.device,
			}

			ds.ComputeScore(tt.requests)

			if ds.Score != tt.expectedScore {
				t.Errorf("ComputeScore() = %v, want %v", ds.Score, tt.expectedScore)
			}
		})
	}
}
