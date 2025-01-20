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

package mthreads

import (
	"flag"
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/util"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/api/resource"
)

func Test_MutateAdmission(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			ctr *corev1.Container
			p   *corev1.Pod
		}
		want bool
		err  error
	}{
		{
			name: "set to resources limit",
			args: struct {
				ctr *corev1.Container
				p   *corev1.Pod
			}{
				ctr: &corev1.Container{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"mthreads.com/vgpu":        *resource.NewQuantity(2, resource.DecimalSI),
							"mthreads.com/sgpu-memory": *resource.NewQuantity(2, resource.DecimalSI),
							"mthreads.com/sgpu-core":   *resource.NewQuantity(1, resource.DecimalSI),
						},
					},
				},
				p: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"mthreads.com/request-gpu-num": "test123",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "don't set to count limit",
			args: struct {
				ctr *corev1.Container
				p   *corev1.Pod
			}{
				ctr: &corev1.Container{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"mthreads.com/sgpu-memory": *resource.NewQuantity(2, resource.DecimalSI),
							"mthreads.com/sgpu-core":   *resource.NewQuantity(1, resource.DecimalSI),
						},
					},
				},
				p: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"mthreads.com/request-gpu-num": "test123",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "don't set to memory limit",
			args: struct {
				ctr *corev1.Container
				p   *corev1.Pod
			}{
				ctr: &corev1.Container{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"mthreads.com/vgpu":      *resource.NewQuantity(1, resource.DecimalSI),
							"mthreads.com/sgpu-core": *resource.NewQuantity(1, resource.DecimalSI),
						},
					},
				},
				p: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"mthreads.com/request-gpu-num": "test123",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "count less than one",
			args: struct {
				ctr *corev1.Container
				p   *corev1.Pod
			}{
				ctr: &corev1.Container{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"mthreads.com/vgpu":        *resource.NewQuantity(1, resource.DecimalSI),
							"mthreads.com/sgpu-memory": *resource.NewQuantity(2, resource.DecimalSI),
							"mthreads.com/sgpu-core":   *resource.NewQuantity(1, resource.DecimalSI),
						},
					},
				},
				p: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"mthreads.com/request-gpu-num": "test123",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "memory no exist legalMemoryslices",
			args: struct {
				ctr *corev1.Container
				p   *corev1.Pod
			}{
				ctr: &corev1.Container{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"mthreads.com/vgpu":        *resource.NewQuantity(1, resource.DecimalSI),
							"mthreads.com/sgpu-memory": *resource.NewQuantity(3, resource.DecimalSI),
							"mthreads.com/sgpu-core":   *resource.NewQuantity(1, resource.DecimalSI),
						},
					},
				},
				p: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"mthreads.com/request-gpu-num": "test123",
						},
					},
				},
			},
			want: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := MthreadsConfig{
				ResourceCountName:  "mthreads.com/vgpu",
				ResourceMemoryName: "mthreads.com/sgpu-memory",
				ResourceCoreName:   "mthreads.com/sgpu-core",
			}
			InitMthreadsDevice(config)
			dev := MthreadsDevices{}
			result, _ := dev.MutateAdmission(test.args.ctr, test.args.p)
			assert.Equal(t, result, test.want)
		})
	}
}

func Test_GetNodeDevices(t *testing.T) {
	tests := []struct {
		name string
		args corev1.Node
		want []*util.DeviceInfo
	}{
		{
			name: "get node device",
			args: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Status: corev1.NodeStatus{
					Capacity: corev1.ResourceList{
						"mthreads.com/sgpu-memory": *resource.NewQuantity(1, resource.DecimalSI),
						"mthreads.com/sgpu-core":   *resource.NewQuantity(1, resource.DecimalSI),
					},
				},
			},
			want: []*util.DeviceInfo{
				{
					Index:   uint(0),
					ID:      "test-mthreads-0",
					Count:   int32(100),
					Devmem:  int32(8192),
					Devcore: int32(16),
					Type:    MthreadsGPUDevice,
					Numa:    0,
					Health:  true,
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := MthreadsDevices{}
			fs := flag.FlagSet{}
			ParseConfig(&fs)
			result, _ := dev.GetNodeDevices(test.args)
			assert.DeepEqual(t, result, test.want)
		})
	}
}

func Test_PatchAnnotations(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			annoinput *map[string]string
			pd        util.PodDevices
		}
		want map[string]string
	}{
		{
			name: "exist device",
			args: struct {
				annoinput *map[string]string
				pd        util.PodDevices
			}{
				annoinput: &map[string]string{},
				pd: util.PodDevices{
					MthreadsGPUDevice: util.PodSingleDevice{
						util.ContainerDevices{
							{
								Idx:       0,
								UUID:      "test1",
								Type:      MthreadsGPUDevice,
								Usedmem:   int32(1000),
								Usedcores: int32(1),
							},
						},
					},
				},
			},
			want: map[string]string{
				util.SupportDevices[MthreadsGPUDevice]: "test1,Mthreads,1000,1:;",
				"mthreads.com/gpu-index":               "0",
				"mthreads.com/predicate-node":          "",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := MthreadsDevices{}
			result := dev.PatchAnnotations(test.args.annoinput, test.args.pd)
			assert.Equal(t, result[dev.CommonWord()], test.want[dev.CommonWord()])
			assert.Equal(t, result["mthreads.com/gpu-index"], test.want["mthreads.com/gpu-index"])
			assert.Equal(t, result["mthreads.com/predicate-node"], test.want["mthreads.com/predicate-node"])
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
			name: "the same type",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
				n     util.ContainerDeviceRequest
			}{
				annos: map[string]string{},
				d:     util.DeviceUsage{},
				n: util.ContainerDeviceRequest{
					Type: MthreadsGPUDevice,
				},
			},
			want1: true,
			want2: true,
			want3: false,
		},
		{
			name: "the different type",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
				n     util.ContainerDeviceRequest
			}{
				annos: map[string]string{},
				d:     util.DeviceUsage{},
				n: util.ContainerDeviceRequest{
					Type: "test111",
				},
			},
			want1: false,
			want2: false,
			want3: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := MthreadsDevices{}
			result1, result2, result3 := dev.CheckType(test.args.annos, test.args.d, test.args.n)
			assert.Equal(t, result1, test.want1)
			assert.Equal(t, result2, test.want2)
			assert.Equal(t, result3, test.want3)
		})
	}
}

func Test_CheckUUID(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			annos map[string]string
			d     util.DeviceUsage
		}
		want bool
	}{
		{
			name: "no annos",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{},
				d:     util.DeviceUsage{},
			},
			want: true,
		},
		{
			name: "use id the same as device id",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{
					MthreadsUseUUID: "test1",
				},
				d: util.DeviceUsage{
					ID: "test1",
				},
			},
			want: true,
		},
		{
			name: "use id the different from device id",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{
					MthreadsUseUUID: "test1",
				},
				d: util.DeviceUsage{
					ID: "test2",
				},
			},
			want: false,
		},
		{
			name: "no use id the same as device id",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{
					MthreadsNoUseUUID: "test1",
				},
				d: util.DeviceUsage{
					ID: "test1",
				},
			},
			want: false,
		},
		{
			name: "no use id the different from device id",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{
					MthreadsNoUseUUID: "test1",
				},
				d: util.DeviceUsage{
					ID: "test2",
				},
			},
			want: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := MthreadsDevices{}
			result := dev.CheckUUID(test.args.annos, test.args.d)
			assert.Equal(t, result, test.want)
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
			name: "all resources set to limit and request",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"mthreads.com/vgpu":        resource.MustParse("1"),
						"mthreads.com/sgpu-memory": resource.MustParse("1000"),
						"mthreads.com/sgpu-core":   resource.MustParse("1"),
					},
					Requests: corev1.ResourceList{
						"mthreads.com/vgpu":        resource.MustParse("1"),
						"mthreads.com/sgpu-memory": resource.MustParse("1000"),
						"mthreads.com/sgpu-core":   resource.MustParse("1"),
					},
				},
			},
			want: util.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             MthreadsGPUDevice,
				Memreq:           int32(512000),
				MemPercentagereq: int32(0),
				Coresreq:         int32(1),
			},
		},
		{
			name: "all resources don't set to limit,count don't set to request",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{},
					Requests: corev1.ResourceList{
						"mthreads.com/sgpu-memory": resource.MustParse("1000"),
						"mthreads.com/sgpu-core":   resource.MustParse("1"),
					},
				},
			},
			want: util.ContainerDeviceRequest{},
		},
		{
			name: "all resources don't set to limit,cores and memory don't set to request",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{},
					Requests: corev1.ResourceList{
						"mthreads.com/vgpu": resource.MustParse("1"),
					},
				},
			},
			want: util.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             MthreadsGPUDevice,
				Memreq:           int32(0),
				MemPercentagereq: int32(100),
				Coresreq:         int32(0),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := MthreadsConfig{
				ResourceCountName:  "mthreads.com/vgpu",
				ResourceMemoryName: "mthreads.com/sgpu-memory",
				ResourceCoreName:   "mthreads.com/sgpu-core",
			}
			InitMthreadsDevice(config)
			dev := MthreadsDevices{}
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
			name: "allocate device",
			args: struct {
				allocated  *util.PodDevices
				request    util.ContainerDeviceRequest
				toAllocate util.ContainerDevices
				device     *util.DeviceUsage
			}{
				allocated: &util.PodDevices{
					MthreadsGPUDevice: util.PodSingleDevice{
						util.ContainerDevices{
							{
								UUID: "test123",
							},
						},
					},
				},
				request:    util.ContainerDeviceRequest{},
				toAllocate: util.ContainerDevices{},
				device: &util.DeviceUsage{
					ID:   "test123",
					Type: MthreadsGPUDevice,
				},
			},
			want: true,
		},
		{
			name: "don't allocate device",
			args: struct {
				allocated  *util.PodDevices
				request    util.ContainerDeviceRequest
				toAllocate util.ContainerDevices
				device     *util.DeviceUsage
			}{
				allocated: &util.PodDevices{
					MthreadsGPUDevice: util.PodSingleDevice{
						util.ContainerDevices{
							{
								UUID: "test456",
							},
						},
					},
				},
				request:    util.ContainerDeviceRequest{},
				toAllocate: util.ContainerDevices{},
				device: &util.DeviceUsage{
					ID:   "test123",
					Type: MthreadsGPUDevice,
				},
			},
			want: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := MthreadsDevices{}
			result := dev.CustomFilterRule(test.args.allocated, test.args.request, test.args.toAllocate, test.args.device)
			assert.Equal(t, result, test.want)
		})
	}
}
