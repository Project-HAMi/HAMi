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

	"github.com/Project-HAMi/HAMi/pkg/device"

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
		want []*device.DeviceInfo
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
			want: []*device.DeviceInfo{
				{
					Index:        uint(0),
					ID:           "test-mthreads-0",
					Count:        int32(100),
					Devmem:       int32(8192),
					Devcore:      int32(16),
					Type:         MthreadsGPUDevice,
					Numa:         0,
					Health:       true,
					DeviceVendor: MthreadsGPUCommonWord,
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
			pd        device.PodDevices
		}
		want map[string]string
	}{
		{
			name: "exist device",
			args: struct {
				annoinput *map[string]string
				pd        device.PodDevices
			}{
				annoinput: &map[string]string{},
				pd: device.PodDevices{
					MthreadsGPUDevice: device.PodSingleDevice{
						device.ContainerDevices{
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
				device.SupportDevices[MthreadsGPUDevice]: "test1,Mthreads,1000,1:;",
				"mthreads.com/gpu-index":                 "0",
				"mthreads.com/predicate-node":            "",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := MthreadsDevices{}
			result := dev.PatchAnnotations(&corev1.Pod{}, test.args.annoinput, test.args.pd)
			assert.Equal(t, result[dev.CommonWord()], test.want[dev.CommonWord()])
			assert.Equal(t, result["mthreads.com/gpu-index"], test.want["mthreads.com/gpu-index"])
			assert.Equal(t, result["mthreads.com/predicate-node"], test.want["mthreads.com/predicate-node"])
		})
	}
}

func Test_checkType(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			annos map[string]string
			d     device.DeviceUsage
			n     device.ContainerDeviceRequest
		}
		want1 bool
		want2 bool
		want3 bool
	}{
		{
			name: "the same type",
			args: struct {
				annos map[string]string
				d     device.DeviceUsage
				n     device.ContainerDeviceRequest
			}{
				annos: map[string]string{},
				d:     device.DeviceUsage{},
				n: device.ContainerDeviceRequest{
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
				d     device.DeviceUsage
				n     device.ContainerDeviceRequest
			}{
				annos: map[string]string{},
				d:     device.DeviceUsage{},
				n: device.ContainerDeviceRequest{
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
			result1, result2, result3 := dev.checkType(test.args.annos, test.args.d, test.args.n)
			assert.Equal(t, result1, test.want1)
			assert.Equal(t, result2, test.want2)
			assert.Equal(t, result3, test.want3)
		})
	}
}

func Test_GenerateResourceRequests(t *testing.T) {
	tests := []struct {
		name string
		args *corev1.Container
		want device.ContainerDeviceRequest
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
			want: device.ContainerDeviceRequest{
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
			want: device.ContainerDeviceRequest{},
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
			want: device.ContainerDeviceRequest{
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
			allocated  *device.PodDevices
			request    device.ContainerDeviceRequest
			toAllocate device.ContainerDevices
			device     *device.DeviceUsage
		}
		want bool
	}{
		{
			name: "allocate device",
			args: struct {
				allocated  *device.PodDevices
				request    device.ContainerDeviceRequest
				toAllocate device.ContainerDevices
				device     *device.DeviceUsage
			}{
				allocated: &device.PodDevices{
					MthreadsGPUDevice: device.PodSingleDevice{
						device.ContainerDevices{
							{
								UUID: "test123",
							},
						},
					},
				},
				request:    device.ContainerDeviceRequest{},
				toAllocate: device.ContainerDevices{},
				device: &device.DeviceUsage{
					ID:   "test123",
					Type: MthreadsGPUDevice,
				},
			},
			want: true,
		},
		{
			name: "don't allocate device",
			args: struct {
				allocated  *device.PodDevices
				request    device.ContainerDeviceRequest
				toAllocate device.ContainerDevices
				device     *device.DeviceUsage
			}{
				allocated: &device.PodDevices{
					MthreadsGPUDevice: device.PodSingleDevice{
						device.ContainerDevices{
							{
								UUID: "test456",
							},
						},
					},
				},
				request:    device.ContainerDeviceRequest{},
				toAllocate: device.ContainerDevices{},
				device: &device.DeviceUsage{
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
			result := dev.customFilterRule(test.args.allocated, test.args.request, test.args.toAllocate, test.args.device)
			assert.Equal(t, result, test.want)
		})
	}
}

func TestDevices_Fit(t *testing.T) {
	config := MthreadsConfig{
		ResourceCountName:  "mthreads.com/vgpu",
		ResourceMemoryName: "mthreads.com/sgpu-memory",
		ResourceCoreName:   "mthreads.com/sgpu-core",
	}
	dev := InitMthreadsDevice(config)

	tests := []struct {
		name       string
		devices    []*device.DeviceUsage
		request    device.ContainerDeviceRequest
		annos      map[string]string
		wantFit    bool
		wantLen    int
		wantDevIDs []string
		wantReason string
	}{
		{
			name: "fit success",
			devices: []*device.DeviceUsage{
				{
					ID:        "dev-0",
					Index:     0,
					Used:      0,
					Count:     100,
					Usedmem:   0,
					Totalmem:  128,
					Totalcore: 100,
					Usedcores: 0,
					Numa:      0,
					Type:      MthreadsGPUDevice,
					Health:    true,
				},
				{
					ID:        "dev-1",
					Index:     0,
					Used:      0,
					Count:     100,
					Usedmem:   0,
					Totalmem:  128,
					Totalcore: 100,
					Usedcores: 0,
					Numa:      0,
					Type:      MthreadsGPUDevice,
					Health:    true,
				},
			},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           64,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             MthreadsGPUDevice,
			},
			annos:      map[string]string{},
			wantFit:    true,
			wantLen:    1,
			wantDevIDs: []string{"dev-1"},
			wantReason: "",
		},
		{
			name: "fit fail: memory not enough",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      0,
				Count:     100,
				Usedmem:   0,
				Totalmem:  128,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Type:      MthreadsGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             MthreadsGPUDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardInsufficientMemory",
		},
		{
			name: "fit fail: core not enough",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      0,
				Count:     100,
				Usedmem:   0,
				Totalmem:  1024,
				Totalcore: 100,
				Usedcores: 100,
				Numa:      0,
				Type:      MthreadsGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             MthreadsGPUDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardInsufficientCore",
		},
		{
			name: "fit fail: type mismatch",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      0,
				Count:     100,
				Usedmem:   0,
				Totalmem:  128,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Health:    true,
				Type:      MthreadsGPUDevice,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Type:             "OtherType",
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardTypeMismatch",
		},
		{
			name: "fit fail: user assign use uuid mismatch",
			devices: []*device.DeviceUsage{{
				ID:        "dev-1",
				Index:     0,
				Used:      0,
				Count:     100,
				Usedmem:   0,
				Totalmem:  1280,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Type:      MthreadsGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             2,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             MthreadsGPUDevice,
			},
			annos:      map[string]string{MthreadsUseUUID: "dev-0"},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardUuidMismatch",
		},
		{
			name: "fit fail: user assign no use uuid match",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      0,
				Count:     100,
				Usedmem:   0,
				Totalmem:  1280,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Type:      MthreadsGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             2,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             MthreadsGPUDevice,
			},
			annos:      map[string]string{MthreadsNoUseUUID: "dev-0"},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardUuidMismatch",
		},
		{
			name: "fit fail: card overused",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      100,
				Count:     100,
				Usedmem:   0,
				Totalmem:  1280,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Type:      MthreadsGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             MthreadsGPUDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardTimeSlicingExhausted",
		},
		{
			name: "fit success: but core limit can't exceed 100",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      0,
				Count:     100,
				Usedmem:   0,
				Totalmem:  1280,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Type:      MthreadsGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         120,
				Type:             MthreadsGPUDevice,
			},
			annos:      map[string]string{},
			wantFit:    true,
			wantLen:    1,
			wantDevIDs: []string{"dev-0"},
			wantReason: "",
		},
		{
			name: "fit fail:  card exclusively",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      20,
				Count:     100,
				Usedmem:   0,
				Totalmem:  1280,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Type:      MthreadsGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         100,
				Type:             MthreadsGPUDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 ExclusiveDeviceAllocateConflict",
		},
		{
			name: "fit fail:  CardComputeUnitsExhausted",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      20,
				Count:     100,
				Usedmem:   0,
				Totalmem:  1280,
				Totalcore: 100,
				Usedcores: 100,
				Numa:      0,
				Type:      MthreadsGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         0,
				Type:             MthreadsGPUDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardComputeUnitsExhausted",
		},
		{
			name: "fit fail:  AllocatedCardsInsufficientRequest",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      20,
				Count:     100,
				Usedmem:   0,
				Totalmem:  1280,
				Totalcore: 100,
				Usedcores: 10,
				Numa:      0,
				Type:      MthreadsGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             2,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         20,
				Type:             MthreadsGPUDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 AllocatedCardsInsufficientRequest",
		},
		{
			name: "fit success:  memory percentage",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      20,
				Count:     100,
				Usedmem:   0,
				Totalmem:  1280,
				Totalcore: 100,
				Usedcores: 10,
				Numa:      0,
				Type:      MthreadsGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           0,
				MemPercentagereq: 10,
				Coresreq:         20,
				Type:             MthreadsGPUDevice,
			},
			annos:      map[string]string{},
			wantFit:    true,
			wantLen:    1,
			wantDevIDs: []string{"dev-0"},
			wantReason: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			allocated := &device.PodDevices{}
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: test.annos,
				},
			}
			fit, result, reason := dev.Fit(test.devices, test.request, pod, &device.NodeInfo{}, allocated)
			if fit != test.wantFit {
				t.Errorf("Fit: got %v, want %v", fit, test.wantFit)
			}
			if test.wantFit {
				if len(result[MthreadsGPUDevice]) != test.wantLen {
					t.Errorf("expected len: %d, got len %d", test.wantLen, len(result[MthreadsGPUDevice]))
				}
				for idx, id := range test.wantDevIDs {
					if id != result[MthreadsGPUDevice][idx].UUID {
						t.Errorf("expected device id: %s, got device id %s", id, result[MthreadsGPUDevice][idx].UUID)
					}
				}
			}

			if reason != test.wantReason {
				t.Errorf("expected reason: %s, got reason: %s", test.wantReason, reason)
			}
		})
	}
}

func TestDevices_AddResourceUsage(t *testing.T) {
	tests := []struct {
		name        string
		deviceUsage *device.DeviceUsage
		ctr         *device.ContainerDevice
		wantErr     bool
		wantUsage   *device.DeviceUsage
	}{
		{
			name: "test add resource usage",
			deviceUsage: &device.DeviceUsage{
				ID:        "dev-0",
				Used:      0,
				Usedcores: 15,
				Usedmem:   2000,
			},
			ctr: &device.ContainerDevice{
				UUID:      "dev-0",
				Usedcores: 50,
				Usedmem:   1024,
			},
			wantUsage: &device.DeviceUsage{
				ID:        "dev-0",
				Used:      1,
				Usedcores: 65,
				Usedmem:   3024,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dev := &MthreadsDevices{}
			if err := dev.AddResourceUsage(&corev1.Pod{}, tt.deviceUsage, tt.ctr); (err != nil) != tt.wantErr {
				t.Errorf("AddResourceUsage() error=%v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if tt.deviceUsage.Usedcores != tt.wantUsage.Usedcores {
					t.Errorf("expected used cores: %d, got used cores %d", tt.wantUsage.Usedcores, tt.deviceUsage.Usedcores)
				}
				if tt.deviceUsage.Usedmem != tt.wantUsage.Usedmem {
					t.Errorf("expected used mem: %d, got used mem %d", tt.wantUsage.Usedmem, tt.deviceUsage.Usedmem)
				}
				if tt.deviceUsage.Used != tt.wantUsage.Used {
					t.Errorf("expected used: %d, got used %d", tt.wantUsage.Used, tt.deviceUsage.Used)
				}
			}
		})
	}
}
