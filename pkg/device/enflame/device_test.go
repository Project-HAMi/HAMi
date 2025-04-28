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

package enflame

import (
	"flag"
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
						corev1.ResourceName(CountNoSharedName):  *resource.NewQuantity(1, resource.DecimalSI),
						corev1.ResourceName(SharedResourceName): *resource.NewQuantity(6, resource.DecimalSI),
					},
				},
			},
			expected: []*util.DeviceInfo{
				{
					Index:   0,
					ID:      "test-enflame-0",
					Count:   100,
					Devmem:  100,
					Devcore: 100,
					Type:    EnflameGPUDevice,
					Numa:    0,
					Health:  true,
				},
			},
			err: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dev := &EnflameDevices{}
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
	InitEnflameDevice(EnflameConfig{})

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
				EnflameGPUDevice: util.PodSingleDevice{
					[]util.ContainerDevice{
						{
							Idx:  0,
							UUID: "k8s-gpu-enflame-0",
							Type: "Enflame",
						},
					},
				},
			},
			expected: map[string]string{
				util.SupportDevices[EnflameGPUDevice]: "k8s-gpu-enflame-0,Enflame,0,0:;",
				PodHasAssignedGCU:                     "false",
				PodAssignedGCUTime:                    strconv.FormatInt(time.Now().UnixNano(), 10),
				PodAssignedGCUID:                      "0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			annoInputCopy := make(map[string]string)
			for k, v := range tt.annoInput {
				annoInputCopy[k] = v
			}

			dev := &EnflameDevices{}
			got := dev.PatchAnnotations(&annoInputCopy, tt.podDevices)

			if len(got) != len(tt.expected) {
				t.Errorf("PatchAnnotations() got %d annotations, expected %d", len(got), len(tt.expected))
				return
			}

			for k, v := range tt.expected {
				if k == PodAssignedGCUTime {
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
			name: "EnflameResourceCount and EnflameResourcePercentage set to limits",
			args: struct {
				ctr *corev1.Container
				p   *corev1.Pod
			}{
				ctr: &corev1.Container{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"enflame.com/vgcu":            *resource.NewQuantity(1, resource.DecimalSI),
							"enflame.com/vgcu-percentage": *resource.NewQuantity(15, resource.DecimalSI),
						},
						Requests: corev1.ResourceList{},
					},
				},
				p: &corev1.Pod{},
			},
			want: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := EnflameConfig{
				ResourceCountName:      "enflame.com/vgcu",
				ResourcePercentageName: "enflame.com/vgcu-percentage",
			}
			InitEnflameDevice(config)
			dev := EnflameDevices{
				factor: 4,
			}
			result, _ := dev.MutateAdmission(test.args.ctr, test.args.p)
			assert.Equal(t, result, test.want)
			limits := test.args.ctr.Resources.Limits[corev1.ResourceName(EnflameResourcePercentage)]
			number, _ := limits.AsInt64()
			assert.Equal(t, number, int64(25))
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
					Type: EnflameGPUDevice,
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
			dev := EnflameDevices{}
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
			name: "useid is same as the device id",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{
					"enflame.com/use-gpuuuid": "test1",
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
					"enflame.com/use-gpuuuid": "test2",
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
					"enflame.com/nouse-gpuuuid": "test1",
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
					"enflame.com/nouse-gpuuuid": "test1",
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
			dev := EnflameDevices{}
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
			name: "all resources set to limits and requests",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"enflame.com/vgcu":            resource.MustParse("1"),
						"enflame.com/vgcu-percentage": resource.MustParse("15"),
					},
					Requests: corev1.ResourceList{
						"enflame.com/vgcu":            resource.MustParse("1"),
						"enflame.com/vgcu-percentage": resource.MustParse("15"),
					},
				},
			},
			want: util.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             EnflameGPUDevice,
				Memreq:           int32(15),
				MemPercentagereq: int32(0),
				Coresreq:         int32(0),
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
						"enflame.com/vgcu": resource.MustParse("1"),
					},
					Requests: corev1.ResourceList{
						"enflame.com/vgcu": resource.MustParse("1"),
					},
				},
			},
			want: util.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             EnflameGPUDevice,
				Memreq:           int32(100),
				MemPercentagereq: int32(0),
				Coresreq:         int32(0),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := EnflameDevices{factor: 4}
			fs := flag.FlagSet{}
			ParseConfig(&fs)
			result := dev.GenerateResourceRequests(test.args)
			assert.DeepEqual(t, result, test.want)
		})
	}
}
