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

package kunlun

import (
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

func Test_graphSelect(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			d []*device.DeviceUsage
			c device.ContainerDeviceRequest
		}
		want1 []int
	}{
		{
			name: "full allocate",
			args: struct {
				d []*device.DeviceUsage
				c device.ContainerDeviceRequest
			}{
				d: []*device.DeviceUsage{
					{Index: 0, Used: 0},
					{Index: 1, Used: 0},
					{Index: 2, Used: 0},
					{Index: 3, Used: 0},
					{Index: 4, Used: 0},
					{Index: 5, Used: 0},
					{Index: 6, Used: 0},
					{Index: 7, Used: 0},
				},
				c: device.ContainerDeviceRequest{Nums: 8},
			},
			want1: []int{0, 1, 2, 3, 4, 5, 6, 7},
		},
		{
			name: "full allocate not success",
			args: struct {
				d []*device.DeviceUsage
				c device.ContainerDeviceRequest
			}{
				d: []*device.DeviceUsage{
					{Index: 0, Used: 0},
					{Index: 1, Used: 0},
					{Index: 2, Used: 0},
					{Index: 3, Used: 0},
					{Index: 4, Used: 0},
					{Index: 5, Used: 1},
					{Index: 6, Used: 0},
					{Index: 7, Used: 0},
				},
				c: device.ContainerDeviceRequest{Nums: 8},
			},
			want1: []int{},
		},
		{
			name: "allocate 2 cards",
			args: struct {
				d []*device.DeviceUsage
				c device.ContainerDeviceRequest
			}{
				d: []*device.DeviceUsage{
					{Index: 0, Used: 0},
					{Index: 1, Used: 0},
					{Index: 2, Used: 0},
					{Index: 3, Used: 0},
					{Index: 4, Used: 0},
					{Index: 5, Used: 1},
					{Index: 6, Used: 0},
					{Index: 7, Used: 0},
				},
				c: device.ContainerDeviceRequest{Nums: 2},
			},
			want1: []int{4, 6},
		},
		{
			name: "allocate 1 card",
			args: struct {
				d []*device.DeviceUsage
				c device.ContainerDeviceRequest
			}{
				d: []*device.DeviceUsage{
					{Index: 0, Used: 0},
					{Index: 1, Used: 0},
					{Index: 2, Used: 0},
					{Index: 3, Used: 0},
					{Index: 4, Used: 0},
					{Index: 5, Used: 1},
					{Index: 6, Used: 0},
					{Index: 7, Used: 0},
				},
				c: device.ContainerDeviceRequest{Nums: 1},
			},
			want1: []int{4},
		},
		{
			name: "allocate 1 card",
			args: struct {
				d []*device.DeviceUsage
				c device.ContainerDeviceRequest
			}{
				d: []*device.DeviceUsage{
					{Index: 0, Used: 0},
					{Index: 1, Used: 0},
					{Index: 2, Used: 0},
					{Index: 3, Used: 0},
					{Index: 4, Used: 0},
					{Index: 5, Used: 0},
					{Index: 6, Used: 1},
					{Index: 7, Used: 1},
				},
				c: device.ContainerDeviceRequest{Nums: 1},
			},
			want1: []int{4},
		},
		{
			name: "allocate 2 card according to interconnect",
			args: struct {
				d []*device.DeviceUsage
				c device.ContainerDeviceRequest
			}{
				d: []*device.DeviceUsage{
					{Index: 0, Used: 1},
					{Index: 1, Used: 1},
					{Index: 2, Used: 1},
					{Index: 3, Used: 0},
					{Index: 4, Used: 1},
					{Index: 5, Used: 1},
					{Index: 6, Used: 1},
					{Index: 7, Used: 0},
				},
				c: device.ContainerDeviceRequest{Nums: 2},
			},
			want1: []int{3, 7},
		},
		{
			name: "allocate 4 cards according to interconnect when have 5 cards",
			args: struct {
				d []*device.DeviceUsage
				c device.ContainerDeviceRequest
			}{
				d: []*device.DeviceUsage{
					{Index: 0, Used: 0},
					{Index: 1, Used: 0},
					{Index: 2, Used: 1},
					{Index: 3, Used: 1},
					{Index: 4, Used: 0},
					{Index: 5, Used: 0},
					{Index: 6, Used: 0},
					{Index: 7, Used: 1},
				},
				c: device.ContainerDeviceRequest{Nums: 4},
			},
			want1: []int{0, 1, 4, 5},
		},
		{
			name: "allocate 4 cards according to interconnect when have 6 cards, leave 2 cards unconnected",
			args: struct {
				d []*device.DeviceUsage
				c device.ContainerDeviceRequest
			}{
				d: []*device.DeviceUsage{
					{Index: 0, Used: 0},
					{Index: 1, Used: 0},
					{Index: 2, Used: 0},
					{Index: 3, Used: 1},
					{Index: 4, Used: 0},
					{Index: 5, Used: 0},
					{Index: 6, Used: 1},
					{Index: 7, Used: 0},
				},
				c: device.ContainerDeviceRequest{Nums: 4},
			},
			want1: []int{0, 1, 4, 5},
		},
		{
			name: "allocate 4 cards according to interconnect when have 6 cards, leave 2 cards connected",
			args: struct {
				d []*device.DeviceUsage
				c device.ContainerDeviceRequest
			}{
				d: []*device.DeviceUsage{
					{Index: 0, Used: 0},
					{Index: 1, Used: 0},
					{Index: 2, Used: 1},
					{Index: 3, Used: 0},
					{Index: 4, Used: 0},
					{Index: 5, Used: 0},
					{Index: 6, Used: 1},
					{Index: 7, Used: 0},
				},
				c: device.ContainerDeviceRequest{Nums: 4},
			},
			want1: []int{0, 1, 4, 5},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result1 := graghSelect(test.args.d, test.args.c, FitXPU)
			assert.DeepEqual(t, result1, test.want1)
		})
	}
}

func Test_graphSelectVXPU(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			d []*device.DeviceUsage
			c device.ContainerDeviceRequest
		}
		want1 []int
	}{
		{
			name: "full allocate with unused devices",
			args: struct {
				d []*device.DeviceUsage
				c device.ContainerDeviceRequest
			}{
				d: []*device.DeviceUsage{
					{Index: 0, Used: 0, Usedmem: 0},
					{Index: 1, Used: 0, Usedmem: 0},
					{Index: 2, Used: 0, Usedmem: 0},
					{Index: 3, Used: 0, Usedmem: 0},
					{Index: 4, Used: 0, Usedmem: 0},
					{Index: 5, Used: 0, Usedmem: 0},
					{Index: 6, Used: 0, Usedmem: 0},
					{Index: 7, Used: 0, Usedmem: 0},
				},
				c: device.ContainerDeviceRequest{Nums: 8, Memreq: 24576},
			},
			want1: []int{0, 1, 2, 3, 4, 5, 6, 7},
		},
		{
			name: "allocate with matching memory requirements",
			args: struct {
				d []*device.DeviceUsage
				c device.ContainerDeviceRequest
			}{
				d: []*device.DeviceUsage{
					{Index: 0, Used: 0, Usedmem: 0},
					{Index: 1, Used: 1, Usedmem: 24576}, // avgMem = 24576, matches request
					{Index: 2, Used: 0, Usedmem: 0},
					{Index: 3, Used: 2, Usedmem: 49152}, // avgMem = 24576, matches request
					{Index: 4, Used: 0, Usedmem: 0},
					{Index: 5, Used: 1, Usedmem: 49152}, // avgMem = 49152, doesn't match request
					{Index: 6, Used: 0, Usedmem: 0},
					{Index: 7, Used: 0, Usedmem: 0},
				},
				c: device.ContainerDeviceRequest{Nums: 4, Memreq: 24576},
			},
			want1: []int{0, 1, 2, 3},
		},
		{
			name: "allocate with mixed memory requirements",
			args: struct {
				d []*device.DeviceUsage
				c device.ContainerDeviceRequest
			}{
				d: []*device.DeviceUsage{
					{Index: 0, Used: 0, Usedmem: 0},
					{Index: 1, Used: 2, Usedmem: 49152}, // avgMem = 24576, matches request
					{Index: 2, Used: 0, Usedmem: 0},
					{Index: 3, Used: 1, Usedmem: 49152}, // avgMem = 49152, doesn't match request
					{Index: 4, Used: 0, Usedmem: 0},
					{Index: 5, Used: 1, Usedmem: 24576}, // avgMem = 24576, matches request
					{Index: 6, Used: 0, Usedmem: 0},
					{Index: 7, Used: 0, Usedmem: 0},
				},
				c: device.ContainerDeviceRequest{Nums: 4, Memreq: 24576},
			},
			want1: []int{4, 5, 6, 7}, // 从索引4开始选择4个设备
		},
		{
			name: "no suitable devices due to memory mismatch",
			args: struct {
				d []*device.DeviceUsage
				c device.ContainerDeviceRequest
			}{
				d: []*device.DeviceUsage{
					{Index: 0, Used: 1, Usedmem: 49152}, // avgMem = 49152, doesn't match request
					{Index: 1, Used: 1, Usedmem: 49152}, // avgMem = 49152, doesn't match request
					{Index: 2, Used: 1, Usedmem: 49152}, // avgMem = 49152, doesn't match request
					{Index: 3, Used: 1, Usedmem: 49152}, // avgMem = 49152, doesn't match request
					{Index: 4, Used: 1, Usedmem: 49152}, // avgMem = 49152, doesn't match request
					{Index: 5, Used: 1, Usedmem: 49152}, // avgMem = 49152, doesn't match request
					{Index: 6, Used: 1, Usedmem: 49152}, // avgMem = 49152, doesn't match request
					{Index: 7, Used: 1, Usedmem: 49152}, // avgMem = 49152, doesn't match request
				},
				c: device.ContainerDeviceRequest{Nums: 2, Memreq: 24576},
			},
			want1: []int{},
		},
		{
			name: "allocate 1 card with matching memory",
			args: struct {
				d []*device.DeviceUsage
				c device.ContainerDeviceRequest
			}{
				d: []*device.DeviceUsage{
					{Index: 0, Used: 1, Usedmem: 24576}, // avgMem = 24576, matches request
					{Index: 1, Used: 1, Usedmem: 24576}, // avgMem = 24576, matches request
					{Index: 2, Used: 1, Usedmem: 49152}, // avgMem = 49152, doesn't match request
					{Index: 3, Used: 0, Usedmem: 0},
					{Index: 4, Used: 0, Usedmem: 0},
					{Index: 5, Used: 1, Usedmem: 24576}, // avgMem = 24576, matches request
					{Index: 6, Used: 0, Usedmem: 0},
					{Index: 7, Used: 0, Usedmem: 0},
				},
				c: device.ContainerDeviceRequest{Nums: 1, Memreq: 24576},
			},
			want1: []int{0},
		},
		{
			name: "allocate 2 cards with different memory requirements",
			args: struct {
				d []*device.DeviceUsage
				c device.ContainerDeviceRequest
			}{
				d: []*device.DeviceUsage{
					{Index: 0, Used: 0, Usedmem: 0},
					{Index: 1, Used: 2, Usedmem: 49152}, // avgMem = 24576, matches request
					{Index: 2, Used: 1, Usedmem: 24576}, // avgMem = 24576, matches request
					{Index: 3, Used: 1, Usedmem: 49152}, // avgMem = 49152, doesn't match request
					{Index: 4, Used: 0, Usedmem: 0},
					{Index: 5, Used: 0, Usedmem: 0},
					{Index: 6, Used: 1, Usedmem: 24576}, // avgMem = 24576, matches request
					{Index: 7, Used: 0, Usedmem: 0},
				},
				c: device.ContainerDeviceRequest{Nums: 2, Memreq: 24576},
			},
			want1: []int{0, 1},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result1 := graghSelect(test.args.d, test.args.c, FitVXPU)
			assert.DeepEqual(t, result1, test.want1)
		})
	}
}

func Test_ScoreNode(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			node       *corev1.Node
			podDevices device.PodSingleDevice
			usage      []*device.DeviceUsage
			policy     string
		}
		want float32
	}{
		{
			name: "Scenario 1",
			args: struct {
				node       *corev1.Node
				podDevices device.PodSingleDevice
				usage      []*device.DeviceUsage
				policy     string
			}{
				node: &corev1.Node{},
				podDevices: device.PodSingleDevice{
					device.ContainerDevices{
						{
							Idx:  int(0),
							Type: KunlunGPUDevice,
						},
						{
							Idx:  int(1),
							Type: KunlunGPUDevice,
						},
						{
							Idx:  int(2),
							Type: KunlunGPUDevice,
						},
						{
							Idx:  int(3),
							Type: KunlunGPUDevice,
						},
						{
							Idx:  int(4),
							Type: KunlunGPUDevice,
						},
						{
							Idx:  int(5),
							Type: KunlunGPUDevice,
						},
						{
							Idx:  int(6),
							Type: KunlunGPUDevice,
						},
						{
							Idx:  int(7),
							Type: KunlunGPUDevice,
						},
					},
				},
				usage: []*device.DeviceUsage{
					{Index: 0, Used: 1, Type: KunlunGPUDevice},
					{Index: 1, Used: 1, Type: KunlunGPUDevice},
					{Index: 2, Used: 1, Type: KunlunGPUDevice},
					{Index: 3, Used: 1, Type: KunlunGPUDevice},
				},
				policy: "binpack",
			},
			want: float32(3000),
		},
		{
			name: "Scenario 2",
			args: struct {
				node       *corev1.Node
				podDevices device.PodSingleDevice
				usage      []*device.DeviceUsage
				policy     string
			}{
				node: &corev1.Node{},
				podDevices: device.PodSingleDevice{
					device.ContainerDevices{
						{
							Idx:  int(0),
							Type: KunlunGPUDevice,
						},
					},
				},
				usage:  []*device.DeviceUsage{},
				policy: "spread",
			},
			want: float32(0),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := KunlunDevices{}
			result := dev.ScoreNode(test.args.node, test.args.podDevices, test.args.usage, test.args.policy)
			assert.DeepEqual(t, result, test.want)
		})
	}
}
