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

	"github.com/Project-HAMi/HAMi/pkg/device"
)

func TestGetNodeDevices(t *testing.T) {
	MetaxResourceCount = "metax-tech.com/gpu"

	tests := []struct {
		name     string
		node     corev1.Node
		expected []*device.DeviceInfo
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
			expected: []*device.DeviceInfo{
				{
					Index:        0,
					ID:           "test-metax-0",
					Count:        100,
					Devmem:       65536,
					Devcore:      100,
					Type:         MetaxGPUDevice,
					Numa:         0,
					Health:       true,
					DeviceVendor: MetaxGPUCommonWord,
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
			name: "node type the same as device",
			args: struct {
				annos map[string]string
				d     device.DeviceUsage
				n     device.ContainerDeviceRequest
			}{
				annos: map[string]string{},
				d:     device.DeviceUsage{},
				n: device.ContainerDeviceRequest{
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
				d     device.DeviceUsage
				n     device.ContainerDeviceRequest
			}{
				annos: map[string]string{},
				d:     device.DeviceUsage{},
				n: device.ContainerDeviceRequest{
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
			result1, result2, result3 := dev.checkType(test.args.annos, test.args.d, test.args.n)
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
		want device.ContainerDeviceRequest
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
			want: device.ContainerDeviceRequest{
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
			want: device.ContainerDeviceRequest{},
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
			allocated  *device.PodDevices
			request    device.ContainerDeviceRequest
			toAllocate device.ContainerDevices
			device     *device.DeviceUsage
		}
		want bool
	}{
		{
			name: "allocated id is same as device id",
			args: struct {
				allocated  *device.PodDevices
				request    device.ContainerDeviceRequest
				toAllocate device.ContainerDevices
				device     *device.DeviceUsage
			}{
				allocated: &device.PodDevices{
					MetaxGPUDevice: device.PodSingleDevice{
						device.ContainerDevices{
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
				request:    device.ContainerDeviceRequest{},
				toAllocate: device.ContainerDevices{},
				device: &device.DeviceUsage{
					Type: MetaxGPUDevice,
					ID:   "test-0000",
				},
			},
			want: true,
		},
		{
			name: "allocated id is different from device id",
			args: struct {
				allocated  *device.PodDevices
				request    device.ContainerDeviceRequest
				toAllocate device.ContainerDevices
				device     *device.DeviceUsage
			}{
				allocated: &device.PodDevices{
					MetaxGPUDevice: device.PodSingleDevice{
						device.ContainerDevices{
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
				request:    device.ContainerDeviceRequest{},
				toAllocate: device.ContainerDevices{},
				device: &device.DeviceUsage{
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
			result := dev.customFilterRule(test.args.allocated, test.args.request, test.args.toAllocate, test.args.device)
			assert.DeepEqual(t, result, test.want)
		})
	}
}

func Test_ScoreNode(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			node       *corev1.Node
			podDevices device.PodSingleDevice
			policy     string
		}
		want float32
	}{
		{
			name: "policy is binpack",
			args: struct {
				node       *corev1.Node
				podDevices device.PodSingleDevice
				policy     string
			}{
				node: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"metax-tech.com/gpu.topology.losses": "{\"1\":100,\"2\":200}",
						},
					},
				},
				podDevices: device.PodSingleDevice{
					device.ContainerDevices{
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
				podDevices device.PodSingleDevice
				policy     string
			}{
				node: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"metax-tech.com/gpu.topology.scores": "{\"1\":100,\"2\":200}",
						},
					},
				},
				podDevices: device.PodSingleDevice{
					device.ContainerDevices{
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
			result := dev.ScoreNode(test.args.node, test.args.podDevices, []*device.DeviceUsage{}, test.args.policy)
			assert.DeepEqual(t, result, test.want)
		})
	}
}

func TestMetaxDevices_Fit(t *testing.T) {
	config := MetaxConfig{
		ResourceCountName: "metax-tech.com/gpu",
	}
	dev := InitMetaxDevice(config)

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
					Type:      MetaxGPUDevice,
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
					Type:      MetaxGPUDevice,
					Health:    true,
				},
			},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           64,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             MetaxGPUDevice,
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
				Type:      MetaxGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             MetaxGPUDevice,
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
				Type:      MetaxGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             MetaxGPUDevice,
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
				Type:      MetaxGPUDevice,
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
				Type:      MetaxGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             MetaxGPUDevice,
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
				Type:      MetaxGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         120,
				Type:             MetaxGPUDevice,
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
				Type:      MetaxGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         100,
				Type:             MetaxGPUDevice,
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
				Type:      MetaxGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         0,
				Type:             MetaxGPUDevice,
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
				Type:      MetaxGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             2,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         20,
				Type:             MetaxGPUDevice,
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
				Type:      MetaxGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           0,
				MemPercentagereq: 10,
				Coresreq:         20,
				Type:             MetaxGPUDevice,
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
				if len(result[MetaxGPUDevice]) != test.wantLen {
					t.Errorf("expected len: %d, got len %d", test.wantLen, len(result[MetaxGPUDevice]))
				}
				for idx, id := range test.wantDevIDs {
					if id != result[MetaxGPUDevice][idx].UUID {
						t.Errorf("expected device id: %s, got device id %s", id, result[MetaxGPUDevice][idx].UUID)
					}
				}
			}

			if reason != test.wantReason {
				t.Errorf("expected reason: %s, got reason: %s", test.wantReason, reason)
			}
		})
	}
}

func TestMetaxDevices_AddResourceUsage(t *testing.T) {
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
			dev := &MetaxDevices{}
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
