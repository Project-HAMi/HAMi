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
	"reflect"
	"strings"
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/metax"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_addNode_ListNodes(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			nodeID   string
			nodeInfo device.NodeInfo
		}
		want map[string]*device.NodeInfo
		err  error
	}{
		{
			name: "node info is empty",
			args: struct {
				nodeID   string
				nodeInfo device.NodeInfo
			}{
				nodeID:   "node-01",
				nodeInfo: device.NodeInfo{},
			},
		},
		{
			name: "test valid info",
			args: struct {
				nodeID   string
				nodeInfo device.NodeInfo
			}{
				nodeID: "node-01",
				nodeInfo: device.NodeInfo{
					ID:   "node-01",
					Node: &corev1.Node{},
					Devices: map[string][]device.DeviceInfo{
						"vendor1": {{
							ID: "node-01",
						}},
					},
				},
			},
			want: map[string]*device.NodeInfo{
				"node-01": {
					ID:   "test123",
					Node: &corev1.Node{},
					Devices: map[string][]device.DeviceInfo{
						"vendor1": {{
							ID: "node-01",
						}},
					},
				},
			},
			err: nil,
		},
		{
			name: "test the different node id",
			args: struct {
				nodeID   string
				nodeInfo device.NodeInfo
			}{
				nodeID: "node-02",
				nodeInfo: device.NodeInfo{
					ID:   "node-02",
					Node: &corev1.Node{},
					Devices: map[string][]device.DeviceInfo{
						"vendor1": {{
							ID:      "node-02",
							Count:   int32(1),
							Devcore: int32(1),
							Devmem:  int32(2000),
						}},
					},
				},
			},
			want: map[string]*device.NodeInfo{
				"node-01": {
					ID:   "test123",
					Node: &corev1.Node{},
					Devices: map[string][]device.DeviceInfo{
						"vendor1": {{
							ID:      "GPU-0",
							Count:   int32(1),
							Devcore: int32(1),
							Devmem:  int32(2000),
						}},
					},
				},
				"node-02": {
					ID:   "node-02",
					Node: &corev1.Node{},
					Devices: map[string][]device.DeviceInfo{
						"vendor1": {{
							ID:      "node-02",
							Count:   int32(1),
							Devcore: int32(1),
							Devmem:  int32(2000),
						}},
					},
				},
			},
			err: nil,
		},
	}
	config.InitDefaultDevices()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := nodeManager{
				nodes: map[string]*device.NodeInfo{
					"node-01": {
						ID:   "test123",
						Node: &corev1.Node{},
						Devices: map[string][]device.DeviceInfo{
							"vendor1": {{
								ID:      "GPU-0",
								Count:   int32(1),
								Devcore: int32(1),
								Devmem:  int32(2000),
							}},
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
		want *device.NodeInfo
		err  error
	}{
		{
			name: "node not found",
			args: "node-1111",
			want: &device.NodeInfo{},
			err:  fmt.Errorf("node %v not found", "node-111"),
		},
		{
			name: "test valid info",
			args: "node-04",
			want: &device.NodeInfo{
				ID:   "node-04",
				Node: &corev1.Node{},
				Devices: map[string][]device.DeviceInfo{
					"vendor1": {{
						ID:      "GPU-0",
						Count:   int32(1),
						Devcore: int32(1),
						Devmem:  int32(2000),
					}},
				},
			},
			err: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := nodeManager{
				nodes: map[string]*device.NodeInfo{
					"node-04": {
						ID:   "node-04",
						Node: &corev1.Node{},
						Devices: map[string][]device.DeviceInfo{
							"vendor1": {{
								ID:      "GPU-0",
								Count:   int32(1),
								Devcore: int32(1),
								Devmem:  int32(2000),
							}},
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

func Test_rmNodeDevices(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			nodeID       string
			deviceVendor string
		}
	}{
		{
			name: "no device",
			args: struct {
				nodeID       string
				deviceVendor string
			}{
				nodeID: "node-06",
			},
		},
		{
			name: "exist device info",
			args: struct {
				nodeID       string
				deviceVendor string
			}{
				nodeID:       "node-05",
				deviceVendor: "NVIDIA",
			},
		},
		{
			name: "the different devicevendor",
			args: struct {
				nodeID       string
				deviceVendor string
			}{
				nodeID:       "node-07",
				deviceVendor: "NVIDIA",
			},
		},
		{
			name: "the same of device id no less than one",
			args: struct {
				nodeID       string
				deviceVendor string
			}{
				nodeID:       "node-08",
				deviceVendor: "NVIDIA",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := nodeManager{
				nodes: map[string]*device.NodeInfo{
					"node-05": {
						ID:   "node-05",
						Node: &corev1.Node{},
						Devices: map[string][]device.DeviceInfo{
							"NVIDIA": {{
								ID:           "GPU-0",
								Count:        int32(1),
								Devcore:      int32(1),
								Devmem:       int32(2000),
								DeviceVendor: "NVIDIA",
							}},
						},
					},
					"node-06": {
						ID:      "node-06",
						Node:    &corev1.Node{},
						Devices: map[string][]device.DeviceInfo{},
					},
					"node-07": {
						ID:   "node-17",
						Node: &corev1.Node{},
						Devices: map[string][]device.DeviceInfo{
							"test1": {{
								ID:           "GPU-0",
								Count:        int32(1),
								Devcore:      int32(1),
								Devmem:       int32(2000),
								DeviceVendor: "test",
							}},
						},
					},
					"node-08": {
						ID:   "node-08",
						Node: &corev1.Node{},
						Devices: map[string][]device.DeviceInfo{
							"NVIDIA": {{
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
								}},
						},
					},
				},
			}
			m.rmNodeDevices(test.args.nodeID, test.args.deviceVendor)
		})
	}
}

func Test_rmDeviceByNodeAnnotation(t *testing.T) {
	id1 := "60151478-4709-4242-a8c1-a944252d194b"
	id2 := "33c00a52-72ab-4b61-a7ce-43107588835b"
	type args struct {
		nodeInfo *device.NodeInfo
	}
	tests := []struct {
		name string
		args args
		want map[string][]device.DeviceInfo
	}{
		{
			name: "Test space condition",
			args: args{
				nodeInfo: &device.NodeInfo{
					Node:    &corev1.Node{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{nvidia.GPUNoUseUUID: strings.Join([]string{id1 + " ", " " + id2 + " "}, ",")}}},
					Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: []device.DeviceInfo{{DeviceVendor: nvidia.NvidiaGPUDevice, ID: id1}, {DeviceVendor: nvidia.NvidiaGPUDevice, ID: id2}}},
				},
			},
			want: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: []device.DeviceInfo{}},
		},
		{
			name: "Test remove one device",
			args: args{
				nodeInfo: &device.NodeInfo{
					Node:    &corev1.Node{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{nvidia.GPUNoUseUUID: id1}}},
					Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: []device.DeviceInfo{{DeviceVendor: nvidia.NvidiaGPUDevice, ID: id1}}},
				},
			},
			want: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: []device.DeviceInfo{}},
		},
		{
			name: "Test remove two devices",
			args: args{
				nodeInfo: &device.NodeInfo{
					Node:    &corev1.Node{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{nvidia.GPUNoUseUUID: strings.Join([]string{id1, id2}, ",")}}},
					Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: []device.DeviceInfo{{DeviceVendor: nvidia.NvidiaGPUDevice, ID: id1}, {DeviceVendor: nvidia.NvidiaGPUDevice, ID: id2}}},
				},
			},
			want: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: []device.DeviceInfo{}},
		},
		{
			name: "Test remove one device and keep one device",
			args: args{
				nodeInfo: &device.NodeInfo{
					Node:    &corev1.Node{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{nvidia.GPUNoUseUUID: strings.Join([]string{id2}, ",")}}},
					Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: []device.DeviceInfo{{DeviceVendor: nvidia.NvidiaGPUDevice, ID: id1}, {DeviceVendor: nvidia.NvidiaGPUDevice, ID: id2}}},
				},
			},
			want: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: []device.DeviceInfo{{DeviceVendor: nvidia.NvidiaGPUDevice, ID: id1}}},
		},
		{
			name: "Test no removing device, case1",
			args: args{
				nodeInfo: &device.NodeInfo{
					Node:    &corev1.Node{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"test-key": ""}}},
					Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: []device.DeviceInfo{{DeviceVendor: nvidia.NvidiaGPUDevice, ID: id1}}},
				},
			},
			want: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: []device.DeviceInfo{{DeviceVendor: nvidia.NvidiaGPUDevice, ID: id1}}},
		},
		{
			name: "Test no removing device, case2",
			args: args{
				nodeInfo: &device.NodeInfo{
					Node:    &corev1.Node{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{nvidia.GPUNoUseUUID: id2}}},
					Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: []device.DeviceInfo{{DeviceVendor: nvidia.NvidiaGPUDevice, ID: id1}}},
				},
			},
			want: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: []device.DeviceInfo{{DeviceVendor: nvidia.NvidiaGPUDevice, ID: id1}}},
		},
		{
			name: "Test removing metax device, case1",
			args: args{
				nodeInfo: &device.NodeInfo{
					Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{metax.MetaxNoUseUUID: id1}}},
					// Devices: []device.DeviceInfo{{DeviceVendor: metax.MetaxGPUDevice, ID: id1}},
					Devices: map[string][]device.DeviceInfo{metax.MetaxGPUDevice: []device.DeviceInfo{{DeviceVendor: metax.MetaxGPUDevice, ID: id1}}},
				},
			},
			want: map[string][]device.DeviceInfo{metax.MetaxGPUDevice: []device.DeviceInfo{}},
		},
		{
			name: "Test removing metax sgpu device",
			args: args{
				nodeInfo: &device.NodeInfo{
					Node:    &corev1.Node{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{metax.MetaxNoUseUUID: id1}}},
					Devices: map[string][]device.DeviceInfo{metax.MetaxSGPUDevice: {{DeviceVendor: metax.MetaxSGPUDevice, ID: id1}}},
				},
			},
			want: map[string][]device.DeviceInfo{metax.MetaxSGPUDevice: []device.DeviceInfo{}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rmDeviceByNodeAnnotation(tt.args.nodeInfo); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("rmDeviceByNodeAnnotation() = %v, want %v", got, tt.want)
			}
		})
	}
}
