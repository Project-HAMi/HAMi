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

	"github.com/Project-HAMi/HAMi/pkg/util"
)

func Test_graphSelect(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			d []*util.DeviceUsage
			c int
		}
		want1 []int
	}{
		{
			name: "full allocate",
			args: struct {
				d []*util.DeviceUsage
				c int
			}{
				d: []*util.DeviceUsage{
					{Index: 0, Used: 0},
					{Index: 1, Used: 0},
					{Index: 2, Used: 0},
					{Index: 3, Used: 0},
					{Index: 4, Used: 0},
					{Index: 5, Used: 0},
					{Index: 6, Used: 0},
					{Index: 7, Used: 0},
				},
				c: 8,
			},
			want1: []int{0, 1, 2, 3, 4, 5, 6, 7},
		},
		{
			name: "full allocate not success",
			args: struct {
				d []*util.DeviceUsage
				c int
			}{
				d: []*util.DeviceUsage{
					{Index: 0, Used: 0},
					{Index: 1, Used: 0},
					{Index: 2, Used: 0},
					{Index: 3, Used: 0},
					{Index: 4, Used: 0},
					{Index: 5, Used: 1},
					{Index: 6, Used: 0},
					{Index: 7, Used: 0},
				},
				c: 8,
			},
			want1: []int{},
		},
		{
			name: "allocate 2 cards",
			args: struct {
				d []*util.DeviceUsage
				c int
			}{
				d: []*util.DeviceUsage{
					{Index: 0, Used: 0},
					{Index: 1, Used: 0},
					{Index: 2, Used: 0},
					{Index: 3, Used: 0},
					{Index: 4, Used: 0},
					{Index: 5, Used: 1},
					{Index: 6, Used: 0},
					{Index: 7, Used: 0},
				},
				c: 2,
			},
			want1: []int{4, 6},
		},
		{
			name: "allocate 1 card",
			args: struct {
				d []*util.DeviceUsage
				c int
			}{
				d: []*util.DeviceUsage{
					{Index: 0, Used: 0},
					{Index: 1, Used: 0},
					{Index: 2, Used: 0},
					{Index: 3, Used: 0},
					{Index: 4, Used: 0},
					{Index: 5, Used: 1},
					{Index: 6, Used: 0},
					{Index: 7, Used: 0},
				},
				c: 1,
			},
			want1: []int{4},
		},
		{
			name: "allocate 1 card",
			args: struct {
				d []*util.DeviceUsage
				c int
			}{
				d: []*util.DeviceUsage{
					{Index: 0, Used: 0},
					{Index: 1, Used: 0},
					{Index: 2, Used: 1},
					{Index: 3, Used: 1},
					{Index: 4, Used: 0},
					{Index: 5, Used: 0},
					{Index: 6, Used: 6},
					{Index: 7, Used: 0},
				},
				c: 1,
			},
			want1: []int{4},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result1 := graghSelect(test.args.d, test.args.c)
			assert.DeepEqual(t, result1, test.want1)
		})
	}
}

func Test_ScoreNode(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			node       *corev1.Node
			podDevices util.PodSingleDevice
			usage      []*util.DeviceUsage
			policy     string
		}
		want float32
	}{
		{
			name: "Scenario 1",
			args: struct {
				node       *corev1.Node
				podDevices util.PodSingleDevice
				usage      []*util.DeviceUsage
				policy     string
			}{
				node: &corev1.Node{},
				podDevices: util.PodSingleDevice{
					util.ContainerDevices{
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
				usage: []*util.DeviceUsage{
					{Index: 0, Used: 1, Type: KunlunGPUDevice},
					{Index: 1, Used: 1, Type: KunlunGPUDevice},
					{Index: 2, Used: 1, Type: KunlunGPUDevice},
					{Index: 3, Used: 1, Type: KunlunGPUDevice},
				},
				policy: "binpack",
			},
			want: float32(2000),
		},
		{
			name: "Scenario 2",
			args: struct {
				node       *corev1.Node
				podDevices util.PodSingleDevice
				usage      []*util.DeviceUsage
				policy     string
			}{
				node: &corev1.Node{},
				podDevices: util.PodSingleDevice{
					util.ContainerDevices{
						{
							Idx:  int(0),
							Type: KunlunGPUDevice,
						},
					},
				},
				usage:  []*util.DeviceUsage{},
				policy: "spread",
			},
			want: float32(-1000),
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
