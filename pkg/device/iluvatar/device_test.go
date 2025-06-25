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

package iluvatar

import (
	"flag"
	"maps"
	"strconv"
	"testing"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/util"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetNodeDevices(t *testing.T) {
	IluvatarResourceCores = "iluvatar.ai/MR-V100.vCore"
	IluvatarResourceMemory = "iluvatar.ai/MR-V100.vMem"

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
						corev1.ResourceName(IluvatarResourceCores):  *resource.NewQuantity(100, resource.DecimalSI),
						corev1.ResourceName(IluvatarResourceMemory): *resource.NewQuantity(128, resource.DecimalSI),
					},
				},
			},
			expected: []*util.DeviceInfo{
				{
					Index:   0,
					ID:      "test-iluvatar-0",
					Count:   100,
					Devmem:  32768,
					Devcore: 100,
					Type:    IluvatarGPUDevice,
					Numa:    0,
					Health:  true,
				},
			},
			err: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dev := &IluvatarDevices{}
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

func TestPatchAnnotations(t *testing.T) {
	InitIluvatarDevice(IluvatarConfig{})

	tests := []struct {
		name       string
		annoInput  map[string]string
		podDevices util.PodDevices
		expected   map[string]string
	}{
		{
			name:       "No devices",
			annoInput:  map[string]string{},
			podDevices: util.PodDevices{},
			expected:   map[string]string{},
		},
		{
			name:      "With devices",
			annoInput: map[string]string{},
			podDevices: util.PodDevices{
				IluvatarGPUDevice: util.PodSingleDevice{
					[]util.ContainerDevice{
						{
							Idx:  0,
							UUID: "k8s-gpu-iluvatar-0",
							Type: "Iluvatar",
						},
					},
				},
			},
			expected: map[string]string{
				util.InRequestDevices[IluvatarGPUDevice]: "k8s-gpu-iluvatar-0,Iluvatar,0,0:;",
				util.SupportDevices[IluvatarGPUDevice]:   "k8s-gpu-iluvatar-0,Iluvatar,0,0:;",
				"iluvatar.ai/gpu-assigned":               "false",
				"iluvatar.ai/predicate-time":             strconv.FormatInt(time.Now().UnixNano(), 10),
				IluvatarDeviceSelection + "0":            "0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			annoInputCopy := make(map[string]string)
			maps.Copy(annoInputCopy, tt.annoInput)

			dev := &IluvatarDevices{}
			got := dev.PatchAnnotations(&corev1.Pod{}, &annoInputCopy, tt.podDevices)

			if len(got) != len(tt.expected) {
				t.Errorf("PatchAnnotations() got %d annotations, expected %d", len(got), len(tt.expected))
				return
			}

			for k, v := range tt.expected {
				if k == "iluvatar.ai/predicate-time" {
					if len(got[k]) != len(v) {
						t.Errorf("Expected %s %s, got %s", k, v, got[k])
					}
					continue
				}

				if got[k] != v {
					t.Errorf("Expected %s %s, got %s", k, v, got[k])
				}
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
		err  error
	}{
		{
			name: "IluvatarResourceCount and IluvatarResourceCores set to limits",
			args: struct {
				ctr *corev1.Container
				p   *corev1.Pod
			}{
				ctr: &corev1.Container{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"iluvatar.ai/vgpu":       *resource.NewQuantity(2, resource.DecimalSI),
							"iluvatar.ai/vcuda-core": *resource.NewQuantity(1, resource.DecimalSI),
						},
					},
				},
				p: &corev1.Pod{},
			},
			want: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := IluvatarConfig{
				ResourceCountName:  "iluvatar.ai/vgpu",
				ResourceCoreName:   "iluvatar.ai/vcuda-core",
				ResourceMemoryName: "iluvatar.ai/vcuda-memory",
			}
			InitIluvatarDevice(config)
			dev := IluvatarDevices{}
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
					Type: IluvatarGPUDevice,
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
					Type: "test123",
				},
			},
			want1: false,
			want2: false,
			want3: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := IluvatarDevices{}
			result1, result2, result3 := dev.checkType(test.args.annos, test.args.d, test.args.n)
			assert.Equal(t, result1, test.want1)
			assert.Equal(t, result2, test.want2)
			assert.Equal(t, result3, test.want3)
		})
	}
}

func Test_checkUUID(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			annos map[string]string
			d     util.DeviceUsage
		}
		want bool
	}{
		{
			name: "useid is same as the device id",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{
					"iluvatar.ai/use-gpuuuid": "test1",
				},
				d: util.DeviceUsage{
					ID: "test1",
				},
			},
			want: true,
		},
		{
			name: "useid is different from the device id",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{
					"iluvatar.ai/use-gpuuuid": "test2",
				},
				d: util.DeviceUsage{
					ID: "test1",
				},
			},
			want: false,
		},
		{
			name: "no annos",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{},
				d: util.DeviceUsage{
					ID: "test3",
				},
			},
			want: true,
		},
		{
			name: "nouseid is same as the device id",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{
					"iluvatar.ai/nouse-gpuuuid": "test1",
				},
				d: util.DeviceUsage{
					ID: "test1",
				},
			},
			want: false,
		},
		{
			name: "nouseid is different from the device id",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{
					"iluvatar.ai/nouse-gpuuuid": "test1",
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
			dev := IluvatarDevices{}
			result := dev.checkUUID(test.args.annos, test.args.d)
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
			name: "all resources set to limits and requests",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"iluvatar.ai/vgpu":         resource.MustParse("1"),
						"iluvatar.ai/vcuda-memory": resource.MustParse("1000"),
						"iluvatar.ai/vcuda-core":   resource.MustParse("100"),
					},
					Requests: corev1.ResourceList{
						"iluvatar.ai/vgpu":         resource.MustParse("1"),
						"iluvatar.ai/vcuda-memory": resource.MustParse("1000"),
						"iluvatar.ai/vcuda-core":   resource.MustParse("100"),
					},
				},
			},
			want: util.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             IluvatarGPUDevice,
				Memreq:           int32(256000),
				MemPercentagereq: int32(0),
				Coresreq:         int32(100),
			},
		},
		{
			name: "all resources don't set to limits and requests",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits:   corev1.ResourceList{},
					Requests: corev1.ResourceList{},
				},
			},
			want: util.ContainerDeviceRequest{},
		},
		{
			name: "resourcemem don't set to limits and requests",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"iluvatar.ai/vgpu": resource.MustParse("1"),
					},
					Requests: corev1.ResourceList{
						"iluvatar.ai/vgpu": resource.MustParse("1"),
					},
				},
			},
			want: util.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             IluvatarGPUDevice,
				Memreq:           int32(0),
				MemPercentagereq: int32(100),
				Coresreq:         int32(0),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := IluvatarDevices{}
			fs := flag.FlagSet{}
			ParseConfig(&fs)
			result := dev.GenerateResourceRequests(test.args)
			assert.DeepEqual(t, result, test.want)
		})
	}
}

func Test_Fit(t *testing.T) {
	config := IluvatarConfig{
		ResourceCountName:  "iluvatar.ai/vgpu",
		ResourceCoreName:   "iluvatar.ai/MR-V100.vCore",
		ResourceMemoryName: "iluvatar.ai/MR-V100.vMem",
	}
	dev := InitIluvatarDevice(config)

	tests := []struct {
		name       string
		devices    []*util.DeviceUsage
		request    util.ContainerDeviceRequest
		annos      map[string]string
		wantOK     bool
		wantLen    int
		wantDevIDs []string
	}{
		{
			name: "fit success",
			devices: []*util.DeviceUsage{
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
					Type:      IluvatarGPUDevice,
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
					Type:      IluvatarGPUDevice,
					Health:    true,
				},
			},
			request: util.ContainerDeviceRequest{
				Nums:             1,
				Type:             IluvatarGPUDevice,
				Memreq:           64,
				MemPercentagereq: 0,
				Coresreq:         50,
			},
			annos:      map[string]string{},
			wantOK:     true,
			wantLen:    1,
			wantDevIDs: []string{"dev-0"},
		},
		{
			name: "fit fail: memory not enough",
			devices: []*util.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      0,
				Count:     100,
				Usedmem:   0,
				Totalmem:  128,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Type:      IluvatarGPUDevice,
				Health:    true,
			}},
			request: util.ContainerDeviceRequest{
				Nums:             1,
				Type:             IluvatarGPUDevice,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
			},
			annos:      map[string]string{},
			wantOK:     false,
			wantLen:    0,
			wantDevIDs: []string{},
		},
		{
			name: "fit fail: core not enough",
			devices: []*util.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      0,
				Count:     100,
				Usedmem:   0,
				Totalmem:  128,
				Totalcore: 100,
				Usedcores: 100,
				Numa:      0,
				Type:      IluvatarGPUDevice,
				Health:    true,
			}},
			request: util.ContainerDeviceRequest{
				Nums:             1,
				Type:             IluvatarGPUDevice,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
			},
			annos:      map[string]string{},
			wantOK:     false,
			wantLen:    0,
			wantDevIDs: []string{},
		},
		{
			name: "fit fail: type mismatch",
			devices: []*util.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      0,
				Count:     100,
				Usedmem:   0,
				Totalmem:  128,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Type:      IluvatarGPUDevice,
				Health:    true,
			}},
			request: util.ContainerDeviceRequest{
				Nums:             1,
				Type:             "OtherType",
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
			},
			annos:      map[string]string{},
			wantOK:     false,
			wantLen:    0,
			wantDevIDs: []string{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			allocated := &util.PodDevices{}
			ok, result, _ := dev.Fit(test.devices, test.request, test.annos, &corev1.Pod{}, allocated)
			if test.wantOK {
				if len(result[IluvatarGPUDevice]) != test.wantLen {
					t.Errorf("expected %d, got %d", test.wantLen, len(result[IluvatarGPUDevice]))
				}
				for idx, id := range test.wantDevIDs {
					if id != result[IluvatarGPUDevice][idx].UUID {
						t.Errorf("expected %s, got %s", id, result[IluvatarGPUDevice][idx].UUID)
					}
				}
				if !ok {
					t.Errorf("expected ok true, got false")
				}
			} else {
				if ok {
					t.Errorf("expected ok false, got true")
				}
				if len(result[IluvatarGPUDevice]) != test.wantLen {
					t.Errorf("expected %d, got %d", test.wantLen, len(result[IluvatarGPUDevice]))
				}
			}
		})
	}
}
