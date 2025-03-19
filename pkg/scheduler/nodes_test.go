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

package scheduler

import (
	"fmt"
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/util"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
)

func Test_addNode_ListNodes(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			nodeID   string
			nodeInfo util.NodeInfo
		}
		want map[string]*util.NodeInfo
		err  error
	}{
		{
			name: "node info is empty",
			args: struct {
				nodeID   string
				nodeInfo util.NodeInfo
			}{
				nodeID:   "node-01",
				nodeInfo: util.NodeInfo{},
			},
		},
		{
			name: "test vaild info",
			args: struct {
				nodeID   string
				nodeInfo util.NodeInfo
			}{
				nodeID: "node-01",
				nodeInfo: util.NodeInfo{
					ID:   "node-01",
					Node: &corev1.Node{},
					Devices: []util.DeviceInfo{
						{
							ID: "node-01",
						},
					},
				},
			},
			want: map[string]*util.NodeInfo{
				"node-01": {
					ID:   "test123",
					Node: &corev1.Node{},
					Devices: []util.DeviceInfo{
						{
							ID: "node-01",
						},
					},
				},
			},
			err: nil,
		},
		{
			name: "test the different node id",
			args: struct {
				nodeID   string
				nodeInfo util.NodeInfo
			}{
				nodeID: "node-02",
				nodeInfo: util.NodeInfo{
					ID:   "node-02",
					Node: &corev1.Node{},
					Devices: []util.DeviceInfo{
						{
							ID:      "node-02",
							Count:   int32(1),
							Devcore: int32(1),
							Devmem:  int32(2000),
						},
					},
				},
			},
			want: map[string]*util.NodeInfo{
				"node-01": {
					ID:   "test123",
					Node: &corev1.Node{},
					Devices: []util.DeviceInfo{
						{
							ID:      "GPU-0",
							Count:   int32(1),
							Devcore: int32(1),
							Devmem:  int32(2000),
						},
					},
				},
				"node-02": {
					ID:   "node-02",
					Node: &corev1.Node{},
					Devices: []util.DeviceInfo{
						{
							ID:      "node-02",
							Count:   int32(1),
							Devcore: int32(1),
							Devmem:  int32(2000),
						},
					},
				},
			},
			err: nil,
		},
	}
	device.InitDefaultDevices()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := nodeManager{
				nodes: map[string]*util.NodeInfo{
					"node-01": {
						ID:   "test123",
						Node: &corev1.Node{},
						Devices: []util.DeviceInfo{
							{
								ID:      "GPU-0",
								Count:   int32(1),
								Devcore: int32(1),
								Devmem:  int32(2000),
							},
						},
					},
				},
			}
			m.addNode(test.args.nodeID, &test.args.nodeInfo)
			if len(test.want) != 0 {
				result, err := m.ListNodes()
				if err == nil {
					assert.DeepEqual(t, test.want, result)
				}
			}
		})
	}
}

func Test_GetNode(t *testing.T) {
	tests := []struct {
		name string
		args string
		want *util.NodeInfo
		err  error
	}{
		{
			name: "node not found",
			args: "node-1111",
			want: &util.NodeInfo{},
			err:  fmt.Errorf("node %v not found", "node-111"),
		},
		{
			name: "test vaild info",
			args: "node-04",
			want: &util.NodeInfo{
				ID:   "node-04",
				Node: &corev1.Node{},
				Devices: []util.DeviceInfo{
					{
						ID:      "GPU-0",
						Count:   int32(1),
						Devcore: int32(1),
						Devmem:  int32(2000),
					},
				},
			},
			err: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := nodeManager{
				nodes: map[string]*util.NodeInfo{
					"node-04": {
						ID:   "node-04",
						Node: &corev1.Node{},
						Devices: []util.DeviceInfo{
							{
								ID:      "GPU-0",
								Count:   int32(1),
								Devcore: int32(1),
								Devmem:  int32(2000),
							},
						},
					},
				},
			}
			result, err := m.GetNode(test.args)
			if err != nil {
				assert.DeepEqual(t, test.want, result)
			}
		})
	}
}

func Test_rmNodeDevice(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			nodeID       string
			nodeInfo     *util.NodeInfo
			deviceVendor string
		}
	}{
		{
			name: "no device",
			args: struct {
				nodeID       string
				nodeInfo     *util.NodeInfo
				deviceVendor string
			}{
				nodeID: "node-06",
			},
		},
		{
			name: "exist device info",
			args: struct {
				nodeID       string
				nodeInfo     *util.NodeInfo
				deviceVendor string
			}{
				nodeID: "node-05",
				nodeInfo: &util.NodeInfo{
					ID:   "node-05",
					Node: &corev1.Node{},
					Devices: []util.DeviceInfo{
						{
							ID:      "GPU-0",
							Count:   int32(1),
							Devcore: int32(1),
							Devmem:  int32(2000),
						},
					},
				},
				deviceVendor: "NVIDIA",
			},
		},
		{
			name: "the different devicevendor",
			args: struct {
				nodeID       string
				nodeInfo     *util.NodeInfo
				deviceVendor string
			}{
				nodeID: "node-07",
				nodeInfo: &util.NodeInfo{
					ID:   "node-07",
					Node: &corev1.Node{},
					Devices: []util.DeviceInfo{
						{
							ID:      "GPU-0",
							Count:   int32(1),
							Devcore: int32(1),
							Devmem:  int32(2000),
						},
					},
				},
				deviceVendor: "NVIDIA",
			},
		},
		{
			name: "the same of device id no less than one",
			args: struct {
				nodeID       string
				nodeInfo     *util.NodeInfo
				deviceVendor string
			}{
				nodeID: "node-08",
				nodeInfo: &util.NodeInfo{
					ID:   "node-08",
					Node: &corev1.Node{},
					Devices: []util.DeviceInfo{
						{
							ID:      "GPU-2",
							Count:   int32(1),
							Devcore: int32(1),
							Devmem:  int32(2000),
						},
						{
							ID:      "GPU-1",
							Count:   int32(1),
							Devcore: int32(1),
							Devmem:  int32(2000),
						},
					},
				},
				deviceVendor: "NVIDIA",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := nodeManager{
				nodes: map[string]*util.NodeInfo{
					"node-05": {
						ID:   "node-05",
						Node: &corev1.Node{},
						Devices: []util.DeviceInfo{
							{
								ID:           "GPU-0",
								Count:        int32(1),
								Devcore:      int32(1),
								Devmem:       int32(2000),
								DeviceVendor: "NVIDIA",
							},
						},
					},
					"node-06": {
						ID:      "node-06",
						Node:    &corev1.Node{},
						Devices: []util.DeviceInfo{},
					},
					"node-07": {
						ID:   "node-17",
						Node: &corev1.Node{},
						Devices: []util.DeviceInfo{
							{
								ID:           "GPU-0",
								Count:        int32(1),
								Devcore:      int32(1),
								Devmem:       int32(2000),
								DeviceVendor: "test",
							},
						},
					},
					"node-08": {
						ID:   "node-08",
						Node: &corev1.Node{},
						Devices: []util.DeviceInfo{
							{
								ID:           "GPU-0",
								Count:        int32(1),
								Devcore:      int32(1),
								Devmem:       int32(2000),
								DeviceVendor: "NVIDIA",
							},
							{
								ID:           "GPU-0",
								Count:        int32(1),
								Devcore:      int32(1),
								Devmem:       int32(2000),
								DeviceVendor: "NVIDIA",
							},
						},
					},
				},
			}
			m.rmNodeDevice(test.args.nodeID, test.args.nodeInfo, test.args.deviceVendor)
		})
	}
}
