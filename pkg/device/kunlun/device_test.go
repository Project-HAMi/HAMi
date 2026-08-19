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
	"fmt"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
)

func Test_KunlunDevices_GenerateResourceRequests(t *testing.T) {
	KunlunResourceCount = "kunlunxin.com/xpu"

	tests := []struct {
		name string
		args *corev1.Container
		want device.ContainerDeviceRequest
	}{
		{
			name: "nothing requested",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits:   corev1.ResourceList{},
					Requests: corev1.ResourceList{},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "valid positive count",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceName(KunlunResourceCount): resource.MustParse("1"),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             KunlunGPUDevice,
				Memreq:           0,
				MemPercentagereq: 100,
				Coresreq:         0,
			},
		},
		{
			name: "zero count must not silently bypass quota",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceName(KunlunResourceCount): resource.MustParse("0"),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "negative count must be rejected",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceName(KunlunResourceCount): resource.MustParse("-1"),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "max int32 count is accepted",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceName(KunlunResourceCount): resource.MustParse("2147483647"),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             int32(2147483647),
				Type:             KunlunGPUDevice,
				Memreq:           0,
				MemPercentagereq: 100,
				Coresreq:         0,
			},
		},
		{
			name: "count above max int32 is rejected",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceName(KunlunResourceCount): resource.MustParse("2147483648"),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := KunlunDevices{}
			result := dev.GenerateResourceRequests(test.args)
			assert.DeepEqual(t, result, test.want)
		})
	}
}

func TestKunlunVDevices_Fit_Mutex(t *testing.T) {
	dev := &KunlunVDevices{}
	// 8 shareable VXPU devices, each already used but memory-matching, so
	// FitVXPU would accept them for sharing.
	devices := make([]*device.DeviceUsage, 8)
	for i := range devices {
		devices[i] = &device.DeviceUsage{Index: uint(i), Used: 1, Usedmem: 24576, Totalmem: 98304, Health: true}
	}
	req := device.ContainerDeviceRequest{Nums: 1, Memreq: 24576}
	nodeInfo := &device.NodeInfo{}
	allocated := &device.PodDevices{}

	// Default policy shares a used device.
	fit, _, _ := dev.Fit(devices, req, &corev1.Pod{}, nodeInfo, allocated)
	assert.Equal(t, fit, true)

	// Mutex policy has no idle device to use, so it must be rejected.
	mutexPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Annotations: map[string]string{"hami.io/gpu-scheduler-policy": "mutex"},
	}}
	fit, _, reason := dev.Fit(devices, req, mutexPod, nodeInfo, allocated)
	assert.Equal(t, fit, false)
	assert.Equal(t, reason, "8/8 "+common.ExclusiveDeviceAllocateConflict)
}

func TestKunlunVDevices_Fit_NumaNotFit(t *testing.T) {
	dev := &KunlunVDevices{}
	devices := make([]*device.DeviceUsage, 8)
	for i := range devices {
		devices[i] = &device.DeviceUsage{Index: uint(i), Used: 0, Usedmem: 0, Totalmem: 1024}
	}
	req := device.ContainerDeviceRequest{Nums: 1, Memreq: 2048}

	fit, _, reason := dev.Fit(devices, req, &corev1.Pod{}, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, false)
	assert.Equal(t, reason, "1/8 "+common.NumaNotFit)
}

func TestKunlunDevices_Fit_NumaNotFit(t *testing.T) {
	dev := &KunlunDevices{}
	devices := make([]*device.DeviceUsage, 8)
	for i := range devices {
		devices[i] = &device.DeviceUsage{Index: uint(i), Used: 1}
	}
	req := device.ContainerDeviceRequest{Nums: 1, Type: KunlunGPUDevice}

	fit, _, reason := dev.Fit(devices, req, &corev1.Pod{}, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, false)
	assert.Equal(t, reason, "1/8 "+common.NumaNotFit)
}

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
					{Index: 0, Used: 0, Health: true},
					{Index: 1, Used: 0, Health: true},
					{Index: 2, Used: 0, Health: true},
					{Index: 3, Used: 0, Health: true},
					{Index: 4, Used: 0, Health: true},
					{Index: 5, Used: 0, Health: true},
					{Index: 6, Used: 0, Health: true},
					{Index: 7, Used: 0, Health: true},
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
					{Index: 0, Used: 0, Health: true},
					{Index: 1, Used: 0, Health: true},
					{Index: 2, Used: 0, Health: true},
					{Index: 3, Used: 0, Health: true},
					{Index: 4, Used: 0, Health: true},
					{Index: 5, Used: 1, Health: true},
					{Index: 6, Used: 0, Health: true},
					{Index: 7, Used: 0, Health: true},
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
					{Index: 0, Used: 0, Health: true},
					{Index: 1, Used: 0, Health: true},
					{Index: 2, Used: 0, Health: true},
					{Index: 3, Used: 0, Health: true},
					{Index: 4, Used: 0, Health: true},
					{Index: 5, Used: 1, Health: true},
					{Index: 6, Used: 0, Health: true},
					{Index: 7, Used: 0, Health: true},
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
					{Index: 0, Used: 0, Health: true},
					{Index: 1, Used: 0, Health: true},
					{Index: 2, Used: 0, Health: true},
					{Index: 3, Used: 0, Health: true},
					{Index: 4, Used: 0, Health: true},
					{Index: 5, Used: 1, Health: true},
					{Index: 6, Used: 0, Health: true},
					{Index: 7, Used: 0, Health: true},
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
					{Index: 0, Used: 0, Health: true},
					{Index: 1, Used: 0, Health: true},
					{Index: 2, Used: 0, Health: true},
					{Index: 3, Used: 0, Health: true},
					{Index: 4, Used: 0, Health: true},
					{Index: 5, Used: 0, Health: true},
					{Index: 6, Used: 1, Health: true},
					{Index: 7, Used: 1, Health: true},
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
					{Index: 0, Used: 1, Health: true},
					{Index: 1, Used: 1, Health: true},
					{Index: 2, Used: 1, Health: true},
					{Index: 3, Used: 0, Health: true},
					{Index: 4, Used: 1, Health: true},
					{Index: 5, Used: 1, Health: true},
					{Index: 6, Used: 1, Health: true},
					{Index: 7, Used: 0, Health: true},
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
					{Index: 0, Used: 0, Health: true},
					{Index: 1, Used: 0, Health: true},
					{Index: 2, Used: 1, Health: true},
					{Index: 3, Used: 1, Health: true},
					{Index: 4, Used: 0, Health: true},
					{Index: 5, Used: 0, Health: true},
					{Index: 6, Used: 0, Health: true},
					{Index: 7, Used: 1, Health: true},
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
					{Index: 0, Used: 0, Health: true},
					{Index: 1, Used: 0, Health: true},
					{Index: 2, Used: 0, Health: true},
					{Index: 3, Used: 1, Health: true},
					{Index: 4, Used: 0, Health: true},
					{Index: 5, Used: 0, Health: true},
					{Index: 6, Used: 1, Health: true},
					{Index: 7, Used: 0, Health: true},
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
					{Index: 0, Used: 0, Health: true},
					{Index: 1, Used: 0, Health: true},
					{Index: 2, Used: 1, Health: true},
					{Index: 3, Used: 0, Health: true},
					{Index: 4, Used: 0, Health: true},
					{Index: 5, Used: 0, Health: true},
					{Index: 6, Used: 1, Health: true},
					{Index: 7, Used: 0, Health: true},
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
					{Index: 0, Used: 0, Usedmem: 0, Totalmem: 98304, Health: true},
					{Index: 1, Used: 0, Usedmem: 0, Totalmem: 98304, Health: true},
					{Index: 2, Used: 0, Usedmem: 0, Totalmem: 98304, Health: true},
					{Index: 3, Used: 0, Usedmem: 0, Totalmem: 98304, Health: true},
					{Index: 4, Used: 0, Usedmem: 0, Totalmem: 98304, Health: true},
					{Index: 5, Used: 0, Usedmem: 0, Totalmem: 98304, Health: true},
					{Index: 6, Used: 0, Usedmem: 0, Totalmem: 98304, Health: true},
					{Index: 7, Used: 0, Usedmem: 0, Totalmem: 98304, Health: true},
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
					{Index: 0, Used: 0, Usedmem: 0, Totalmem: 98304, Health: true},
					{Index: 1, Used: 1, Usedmem: 24576, Totalmem: 98304, Health: true}, // avgMem = 24576, matches request
					{Index: 2, Used: 0, Usedmem: 0, Totalmem: 98304, Health: true},
					{Index: 3, Used: 2, Usedmem: 49152, Totalmem: 98304, Health: true}, // avgMem = 24576, matches request
					{Index: 4, Used: 0, Usedmem: 0, Totalmem: 98304, Health: true},
					{Index: 5, Used: 1, Usedmem: 49152, Totalmem: 98304, Health: true}, // avgMem = 49152, doesn't match request
					{Index: 6, Used: 0, Usedmem: 0, Totalmem: 98304, Health: true},
					{Index: 7, Used: 0, Usedmem: 0, Totalmem: 98304, Health: true},
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
					{Index: 0, Used: 0, Usedmem: 0, Totalmem: 98304, Health: true},
					{Index: 1, Used: 2, Usedmem: 49152, Totalmem: 98304, Health: true}, // avgMem = 24576, matches request
					{Index: 2, Used: 0, Usedmem: 0, Totalmem: 98304, Health: true},
					{Index: 3, Used: 1, Usedmem: 49152, Totalmem: 98304, Health: true}, // avgMem = 49152, doesn't match request
					{Index: 4, Used: 0, Usedmem: 0, Totalmem: 98304, Health: true},
					{Index: 5, Used: 1, Usedmem: 24576, Totalmem: 98304, Health: true}, // avgMem = 24576, matches request
					{Index: 6, Used: 0, Usedmem: 0, Totalmem: 98304, Health: true},
					{Index: 7, Used: 0, Usedmem: 0, Totalmem: 98304, Health: true},
				},
				c: device.ContainerDeviceRequest{Nums: 4, Memreq: 24576},
			},
			want1: []int{4, 5, 6, 7}, // select 4 devices starting from index 4
		},
		{
			name: "no suitable devices due to memory mismatch",
			args: struct {
				d []*device.DeviceUsage
				c device.ContainerDeviceRequest
			}{
				d: []*device.DeviceUsage{
					{Index: 0, Used: 1, Usedmem: 49152, Totalmem: 98304, Health: true}, // avgMem = 49152, doesn't match request
					{Index: 1, Used: 1, Usedmem: 49152, Totalmem: 98304, Health: true}, // avgMem = 49152, doesn't match request
					{Index: 2, Used: 1, Usedmem: 49152, Totalmem: 98304, Health: true}, // avgMem = 49152, doesn't match request
					{Index: 3, Used: 1, Usedmem: 49152, Totalmem: 98304, Health: true}, // avgMem = 49152, doesn't match request
					{Index: 4, Used: 1, Usedmem: 49152, Totalmem: 98304, Health: true}, // avgMem = 49152, doesn't match request
					{Index: 5, Used: 1, Usedmem: 49152, Totalmem: 98304, Health: true}, // avgMem = 49152, doesn't match request
					{Index: 6, Used: 1, Usedmem: 49152, Totalmem: 98304, Health: true}, // avgMem = 49152, doesn't match request
					{Index: 7, Used: 1, Usedmem: 49152, Totalmem: 98304, Health: true}, // avgMem = 49152, doesn't match request
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
					{Index: 0, Used: 1, Usedmem: 24576, Totalmem: 98304, Health: true}, // avgMem = 24576, matches request
					{Index: 1, Used: 1, Usedmem: 24576, Totalmem: 98304, Health: true}, // avgMem = 24576, matches request
					{Index: 2, Used: 1, Usedmem: 49152, Totalmem: 98304, Health: true}, // avgMem = 49152, doesn't match request
					{Index: 3, Used: 0, Usedmem: 0, Totalmem: 98304, Health: true},
					{Index: 4, Used: 0, Usedmem: 0, Totalmem: 98304, Health: true},
					{Index: 5, Used: 1, Usedmem: 24576, Totalmem: 98304, Health: true}, // avgMem = 24576, matches request
					{Index: 6, Used: 0, Usedmem: 0, Totalmem: 98304, Health: true},
					{Index: 7, Used: 0, Usedmem: 0, Totalmem: 98304, Health: true},
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
					{Index: 0, Used: 0, Usedmem: 0, Totalmem: 98304, Health: true},
					{Index: 1, Used: 2, Usedmem: 49152, Totalmem: 98304, Health: true}, // avgMem = 24576, matches request
					{Index: 2, Used: 1, Usedmem: 24576, Totalmem: 98304, Health: true}, // avgMem = 24576, matches request
					{Index: 3, Used: 1, Usedmem: 49152, Totalmem: 98304, Health: true}, // avgMem = 49152, doesn't match request
					{Index: 4, Used: 0, Usedmem: 0, Totalmem: 98304, Health: true},
					{Index: 5, Used: 0, Usedmem: 0, Totalmem: 98304, Health: true},
					{Index: 6, Used: 1, Usedmem: 24576, Totalmem: 98304, Health: true}, // avgMem = 24576, matches request
					{Index: 7, Used: 0, Usedmem: 0, Totalmem: 98304, Health: true},
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

// hami.io/use-kunlun-uuid and nouse-kunlun-uuid were defined but never consulted, so
// a pod asking for a specific XPU was scheduled onto any of them.
func TestKunlunDevices_Fit_UseUUID(t *testing.T) {
	dev := &KunlunDevices{}
	devices := make([]*device.DeviceUsage, 8)
	for i := range devices {
		devices[i] = &device.DeviceUsage{
			Index:     uint(i),
			ID:        fmt.Sprintf("xpu-%d", i),
			Count:     1,
			Totalmem:  KunlunMaxMemory,
			Totalcore: 100,
			Health:    true,
		}
	}
	req := device.ContainerDeviceRequest{Nums: 1, Type: KunlunGPUDevice}

	podWith := func(annos map[string]string) *corev1.Pod {
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: annos}}
	}

	// Test legacy annotation
	fit, res, _ := dev.Fit(devices, req, podWith(map[string]string{
		KunlunUseUUID: "xpu-5",
	}), &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, true)
	assert.Equal(t, len(res[KunlunGPUDevice]), 1)
	assert.Equal(t, res[KunlunGPUDevice][0].UUID, "xpu-5")

	// Test new standard annotation
	fit, res, _ = dev.Fit(devices, req, podWith(map[string]string{
		UseUUIDAnno: "xpu-3",
	}), &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, true)
	assert.Equal(t, len(res[KunlunGPUDevice]), 1)
	assert.Equal(t, res[KunlunGPUDevice][0].UUID, "xpu-3")

	// the literal key, so a rename of the constant cannot silently stop the
	// physical path from reading what vdevice.go documents.
	fit, res, _ = dev.Fit(devices, req, podWith(map[string]string{
		"hami.io/use-xpu-uuid": "xpu-6",
	}), &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, true)
	assert.Equal(t, len(res[KunlunGPUDevice]), 1)
	assert.Equal(t, res[KunlunGPUDevice][0].UUID, "xpu-6")

	// asking for a card that is not on the node must not fall back to another
	fit, _, reason := dev.Fit(devices, req, podWith(map[string]string{
		UseUUIDAnno: "xpu-99",
	}), &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, false)
	assert.Assert(t, strings.Contains(reason, common.CardUUIDMismatch))
}

func TestKunlunDevices_Fit_NoUseUUID(t *testing.T) {
	dev := &KunlunDevices{}
	devices := make([]*device.DeviceUsage, 8)
	for i := range devices {
		devices[i] = &device.DeviceUsage{
			Index:     uint(i),
			ID:        fmt.Sprintf("xpu-%d", i),
			Count:     1,
			Totalmem:  KunlunMaxMemory,
			Totalcore: 100,
			Health:    true,
		}
	}
	req := device.ContainerDeviceRequest{Nums: 1, Type: KunlunGPUDevice}

	// exclude every card and nothing should be allocatable (using legacy annotation)
	all := make([]string, 0, 8)
	for i := range devices {
		all = append(all, fmt.Sprintf("xpu-%d", i))
	}
	fit, _, reason := dev.Fit(devices, req, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			KunlunNoUseUUID: strings.Join(all, ","),
		}},
	}, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, false)
	assert.Assert(t, strings.Contains(reason, common.CardUUIDMismatch))

	// exclude every card and nothing should be allocatable (using new standard annotation)
	fit, _, reason = dev.Fit(devices, req, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			NoUseUUIDAnno: strings.Join(all, ","),
		}},
	}, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, false)
	assert.Assert(t, strings.Contains(reason, common.CardUUIDMismatch))
}

func TestKunlunVDevices_Fit_UUIDAnnotations(t *testing.T) {
	dev := &KunlunVDevices{}
	devices := make([]*device.DeviceUsage, 8)
	for i := range devices {
		devices[i] = &device.DeviceUsage{
			Index:     uint(i),
			ID:        fmt.Sprintf("xpu-%d", i),
			Count:     10,
			Totalmem:  KunlunMaxMemory,
			Totalcore: 100,
			Health:    true,
		}
	}
	req := device.ContainerDeviceRequest{Nums: 1, Type: XPUDevice, Memreq: KunlunMaxMemory}

	// Test standard hami annotation
	fit, res, _ := dev.Fit(devices, req, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{UseUUIDAnno: "xpu-3"}},
	}, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, true)
	assert.Equal(t, res[XPUDevice][0].UUID, "xpu-3")

	// Test legacy baidu annotation (backward compatibility)
	fit, res, _ = dev.Fit(devices, req, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{KunlunUseUUID: "xpu-2"}},
	}, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, true)
	assert.Equal(t, res[XPUDevice][0].UUID, "xpu-2")

	// Test standard hami nouse annotation
	fit, _, reason := dev.Fit(devices, req, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{NoUseUUIDAnno: "xpu-0,xpu-1,xpu-2,xpu-3,xpu-4,xpu-5,xpu-6,xpu-7"}},
	}, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, false)
	assert.Assert(t, strings.Contains(reason, common.CardUUIDMismatch))

	// Test legacy baidu nouse annotation
	fit, _, reason = dev.Fit(devices, req, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{KunlunNoUseUUID: "xpu-0,xpu-1,xpu-2,xpu-3,xpu-4,xpu-5,xpu-6,xpu-7"}},
	}, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, false)
	assert.Assert(t, strings.Contains(reason, common.CardUUIDMismatch))
}
