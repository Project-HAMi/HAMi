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

package metax

import (
	"flag"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/util"
)

func TestGetNodeDevices(t *testing.T) {
	MetaxResourceCount = "metax-tech.com/gpu"

	tests := []struct {
		name     string
		node     corev1.Node
		expected []*util.DeviceInfo
		err      error
	}{
		{
			name: "Test with valid node",
			node: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Status: corev1.NodeStatus{
					Capacity: corev1.ResourceList{
						corev1.ResourceName(MetaxResourceCount): *resource.NewQuantity(1, resource.DecimalSI),
					},
				},
			},
			expected: []*util.DeviceInfo{
				{
					Index:   0,
					ID:      "test-metax-0",
					Count:   100,
					Devmem:  65536,
					Devcore: 100,
					Type:    MetaxGPUDevice,
					Numa:    0,
					Health:  true,
				},
			},
			err: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dev := &MetaxDevices{}
			got, err := dev.GetNodeDevices(tt.node)
			if (err != nil) != (tt.err != nil) {
				t.Errorf("GetNodeDevices() error = %v, expected %v", err, tt.err)
				return
			}

			if len(got) != len(tt.expected) {
				t.Errorf("GetNodeDevices() got %d devices, expected %d", len(got), len(tt.expected))
				return
			}

			for i, device := range got {
				if device.Index != tt.expected[i].Index {
					t.Errorf("Expected index %d, got %d", tt.expected[i].Index, device.Index)
				}
				if device.ID != tt.expected[i].ID {
					t.Errorf("Expected id %s, got %s", tt.expected[i].ID, device.ID)
				}
				if device.Devcore != tt.expected[i].Devcore {
					t.Errorf("Expected devcore %d, got %d", tt.expected[i].Devcore, device.Devcore)
				}
				if device.Devmem != tt.expected[i].Devmem {
					t.Errorf("Expected cevmem %d, got %d", tt.expected[i].Devmem, device.Devmem)
				}
			}
		})
	}
}

func TestParseMetaxAnnos(t *testing.T) {
	tests := []struct {
		name  string
		index int
		value float32
	}{
		{
			name:  "{\"1\":0,\"2\":110,\"3\":270,\"4\":540,\"5\":580,\"6\":730,\"7\":930,\"8\":1240}",
			index: 1,
			value: 0,
		},
		{
			name:  "{\"1\":0,\"2\":110,\"3\":270,\"4\":540,\"5\":580,\"6\":730,\"7\":930,\"8\":1240}",
			index: 2,
			value: 110,
		},
		{
			name:  "{\"1\":0,\"2\":110,\"3\":270,\"4\":540,\"5\":580,\"6\":730,\"7\":930,\"8\":1240}",
			index: 3,
			value: 270,
		},
		{
			name:  "{\"1\":0,\"2\":110,\"3\":270,\"4\":540,\"5\":580,\"6\":730,\"7\":930,\"8\":1240}",
			index: 4,
			value: 540,
		},
		{
			name:  "{\"1\":0,\"2\":110,\"3\":270,\"4\":540,\"5\":580,\"6\":730,\"7\":930,\"8\":1240}",
			index: 5,
			value: 580,
		},
		{
			name:  "{\"1\":0,\"2\":110,\"3\":270,\"4\":540,\"5\":580,\"6\":730,\"7\":930,\"8\":1240}",
			index: 6,
			value: 730,
		},
		{
			name:  "{\"1\":0,\"2\":110,\"3\":270,\"4\":540,\"5\":580,\"6\":730,\"7\":930,\"8\":1240}",
			index: 7,
			value: 930,
		},
		{
			name:  "{\"1\":0,\"2\":110,\"3\":270,\"4\":540,\"5\":580,\"6\":730,\"7\":930,\"8\":1240}",
			index: 8,
			value: 1240,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := parseMetaxAnnos(tt.name, tt.index)
			if value != tt.value {
				t.Errorf("Expected index %f, got %f", tt.value, value)
			}
		})
	}
}

func Test_MutateAdmission(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			ctr *corev1.Container
			p   *corev1.Pod
		}
		want bool
	}{
		{
			name: "set to resources limits",
			args: struct {
				ctr *corev1.Container
				p   *corev1.Pod
			}{
				ctr: &corev1.Container{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"metax-tech.com/gpu": resource.MustParse("1"),
						},
					},
				},
				p: &corev1.Pod{},
			},
			want: true,
		},
		{
			name: "don't set to resources limits",
			args: struct {
				ctr *corev1.Container
				p   *corev1.Pod
			}{
				ctr: &corev1.Container{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{},
					},
				},
				p: &corev1.Pod{},
			},
			want: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := MetaxConfig{
				ResourceCountName: "metax-tech.com/gpu",
			}
			InitMetaxDevice(config)
			dev := MetaxDevices{}
			result, _ := dev.MutateAdmission(test.args.ctr, test.args.p)
			assert.Equal(t, result, test.want)
		})
	}
}

func Test_CheckType(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			annos map[string]string
			d     util.DeviceUsage
			n     util.ContainerDeviceRequest
		}
		want1 bool
		want2 bool
		want3 bool
	}{
		{
			name: "node type the same as device",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
				n     util.ContainerDeviceRequest
			}{
				annos: map[string]string{},
				d:     util.DeviceUsage{},
				n: util.ContainerDeviceRequest{
					Type: MetaxGPUDevice,
				},
			},
			want1: true,
			want2: true,
			want3: false,
		},
		{
			name: "node type the different from device",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
				n     util.ContainerDeviceRequest
			}{
				annos: map[string]string{},
				d:     util.DeviceUsage{},
				n: util.ContainerDeviceRequest{
					Type: "test",
				},
			},
			want1: false,
			want2: false,
			want3: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := MetaxDevices{}
			result1, result2, result3 := dev.CheckType(test.args.annos, test.args.d, test.args.n)
			assert.DeepEqual(t, result1, test.want1)
			assert.DeepEqual(t, result2, test.want2)
			assert.DeepEqual(t, result3, test.want3)
		})
	}
}

func Test_GenerateResourceRequests(t *testing.T) {
	tests := []struct {
		name string
		args *corev1.Container
		want util.ContainerDeviceRequest
	}{
		{
			name: "resource set to limit and request",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"metax-tech.com/gpu": resource.MustParse("1"),
					},
					Requests: corev1.ResourceList{
						"metax-tech.com/gpu": resource.MustParse("1"),
					},
				},
			},
			want: util.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             MetaxGPUDevice,
				Memreq:           0,
				MemPercentagereq: 100,
				Coresreq:         100,
			},
		},
		{
			name: "resource don't set to limit and request",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits:   corev1.ResourceList{},
					Requests: corev1.ResourceList{},
				},
			},
			want: util.ContainerDeviceRequest{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fs := flag.FlagSet{}
			ParseConfig(&fs)
			dev := MetaxDevices{}
			result := dev.GenerateResourceRequests(test.args)
			assert.DeepEqual(t, result, test.want)
		})
	}
}

func Test_CustomFilterRule(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			allocated  *util.PodDevices
			request    util.ContainerDeviceRequest
			toAllocate util.ContainerDevices
			device     *util.DeviceUsage
		}
		want bool
	}{
		{
			name: "allocated id is same as device id",
			args: struct {
				allocated  *util.PodDevices
				request    util.ContainerDeviceRequest
				toAllocate util.ContainerDevices
				device     *util.DeviceUsage
			}{
				allocated: &util.PodDevices{
					MetaxGPUDevice: util.PodSingleDevice{
						util.ContainerDevices{
							{
								Idx:       int(0),
								Type:      MetaxGPUDevice,
								UUID:      "test-0000",
								Usedcores: int32(1),
								Usedmem:   int32(1000),
							},
						},
					},
				},
				request:    util.ContainerDeviceRequest{},
				toAllocate: util.ContainerDevices{},
				device: &util.DeviceUsage{
					Type: MetaxGPUDevice,
					ID:   "test-0000",
				},
			},
			want: true,
		},
		{
			name: "allocated id is different from device id",
			args: struct {
				allocated  *util.PodDevices
				request    util.ContainerDeviceRequest
				toAllocate util.ContainerDevices
				device     *util.DeviceUsage
			}{
				allocated: &util.PodDevices{
					MetaxGPUDevice: util.PodSingleDevice{
						util.ContainerDevices{
							{
								Idx:       int(0),
								Type:      MetaxGPUDevice,
								UUID:      "test-0000",
								Usedcores: int32(1),
								Usedmem:   int32(1000),
							},
						},
					},
				},
				request:    util.ContainerDeviceRequest{},
				toAllocate: util.ContainerDevices{},
				device: &util.DeviceUsage{
					Type: MetaxGPUDevice,
					ID:   "test-1111",
				},
			},
			want: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := MetaxDevices{}
			result := dev.CustomFilterRule(test.args.allocated, test.args.request, test.args.toAllocate, test.args.device)
			assert.DeepEqual(t, result, test.want)
		})
	}
}

func Test_ScoreNode(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			node       *corev1.Node
			podDevices util.PodSingleDevice
			policy     string
		}
		want float32
	}{
		{
			name: "policy is binpack",
			args: struct {
				node       *corev1.Node
				podDevices util.PodSingleDevice
				policy     string
			}{
				node: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"metax-tech.com/gpu.topology.losses": "{\"1\":100,\"2\":200}",
						},
					},
				},
				podDevices: util.PodSingleDevice{
					util.ContainerDevices{
						{
							Idx:       int(0),
							UUID:      "test-0",
							Type:      MetaxGPUDevice,
							Usedmem:   int32(1000),
							Usedcores: int32(1),
						},
						{
							Idx:       int(1),
							UUID:      "test-1",
							Type:      MetaxGPUDevice,
							Usedmem:   int32(1000),
							Usedcores: int32(1),
						},
					},
				},
				policy: "binpack",
			},
			want: float32(1800),
		},
		{
			name: "policy is spread",
			args: struct {
				node       *corev1.Node
				podDevices util.PodSingleDevice
				policy     string
			}{
				node: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"metax-tech.com/gpu.topology.scores": "{\"1\":100,\"2\":200}",
						},
					},
				},
				podDevices: util.PodSingleDevice{
					util.ContainerDevices{
						{
							Idx:       int(0),
							UUID:      "test-0",
							Type:      MetaxGPUDevice,
							Usedmem:   int32(1000),
							Usedcores: int32(1),
						},
					},
				},
				policy: "spread",
			},
			want: float32(1900),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := MetaxDevices{}
			result := dev.ScoreNode(test.args.node, test.args.podDevices, test.args.policy)
			assert.DeepEqual(t, result, test.want)
		})
	}
}
