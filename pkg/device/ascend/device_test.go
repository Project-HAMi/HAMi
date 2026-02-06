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

package ascend

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v2"
	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

func Test_InitDevices(t *testing.T) {
	tests := []struct {
		name         string
		enableAscend bool
		args         []VNPUConfig
		want         []*Devices
	}{
		{
			name:         "test with valid configuration",
			enableAscend: true,
			args: []VNPUConfig{
				{
					ChipName:           "910A",
					CommonWord:         "Ascend910A",
					ResourceName:       "huawei.com/Ascend910A",
					ResourceMemoryName: "huawei.com/Ascend910A-memory",
					MemoryAllocatable:  int64(32768),
					MemoryCapacity:     int64(32768),
					AICore:             int32(30),
					Templates: []Template{
						{
							Name:   "vir02",
							Memory: int64(2184),
							AICore: int32(2),
						}, {
							Name:   "vir04",
							Memory: int64(4369),
							AICore: int32(4),
						}, {
							Name:   "vir08",
							Memory: int64(8738),
							AICore: int32(8),
						}, {
							Name:   "vir16",
							Memory: int64(17476),
							AICore: int32(16),
						},
					},
				},
			},
			want: []*Devices{
				{
					config: VNPUConfig{
						ChipName:           "910A",
						CommonWord:         "Ascend910A",
						ResourceName:       "huawei.com/Ascend910A",
						ResourceMemoryName: "huawei.com/Ascend910A-memory",
						MemoryAllocatable:  int64(32768),
						MemoryCapacity:     int64(32768),
						AICore:             int32(30),
						Templates: []Template{
							{
								Name:   "vir02",
								Memory: int64(2184),
								AICore: int32(2),
							}, {
								Name:   "vir04",
								Memory: int64(4369),
								AICore: int32(4),
							}, {
								Name:   "vir08",
								Memory: int64(8738),
								AICore: int32(8),
							}, {
								Name:   "vir16",
								Memory: int64(17476),
								AICore: int32(16),
							},
						},
					},
					nodeRegisterAnno: "hami.io/node-register-Ascend910A",
					useUUIDAnno:      "hami.io/use-Ascend910A-uuid",
					noUseUUIDAnno:    "hami.io/no-use-Ascend910A-uuid",
					handshakeAnno:    "hami.io/node-handshake-Ascend910A",
				},
			},
		},
		{
			name:         "enableAscend is false",
			enableAscend: false,
			args: []VNPUConfig{
				{
					ChipName:           "910A",
					CommonWord:         "Ascend910A",
					ResourceName:       "huawei.com/Ascend910A",
					ResourceMemoryName: "huawei.com/Ascend910A-memory",
					MemoryAllocatable:  int64(32768),
					MemoryCapacity:     int64(32768),
					AICore:             int32(30),
					Templates: []Template{
						{
							Name:   "vir02",
							Memory: int64(2184),
							AICore: int32(2),
						}, {
							Name:   "vir04",
							Memory: int64(4369),
							AICore: int32(4),
						}, {
							Name:   "vir08",
							Memory: int64(8738),
							AICore: int32(8),
						}, {
							Name:   "vir16",
							Memory: int64(17476),
							AICore: int32(16),
						},
					},
				},
			},
			want: []*Devices{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			enableAscend = test.enableAscend
			devices := InitDevices(test.args)
			assert.Equal(t, len(devices), len(test.want), "Expected length of result to match want")
			if enableAscend {
				for k, v := range devices {
					assert.Equal(t, v, devices[k], "load ascend vnpu config %s: %v", devices[k].config.CommonWord, devices[k].config)
				}
				assert.Equal(t, "hami.io/Ascend910A-devices-to-allocate", device.InRequestDevices[test.args[0].CommonWord])
				assert.Equal(t, "hami.io/Ascend910A-devices-allocated", device.SupportDevices[test.args[0].CommonWord])
				assert.Equal(t, test.want[0].handshakeAnno, util.HandshakeAnnos[test.args[0].CommonWord])
			}
		})
	}
}

func Test_GetNodeDevices(t *testing.T) {
	dev := Devices{}
	tests := []struct {
		name string
		args corev1.Node
		want []*device.DeviceInfo
		err  error
	}{
		{
			name: "exist device",
			args: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node-01",
					Annotations: map[string]string{
						dev.nodeRegisterAnno: "[{\"ID\":\"GPU-0\",\"Count\":4,\"Devmem\":8738,\"Devcore\":8,\"Type\":\"huawei.com/Ascend910\",\"Numa\":0,\"Health\":true}]",
					},
				},
			},
			want: []*device.DeviceInfo{
				{
					ID:      "GPU-0",
					Count:   int32(4),
					Devcore: int32(8),
					Devmem:  int32(8738),
					Type:    "huawei.com/Ascend910",
					Numa:    0,
					Health:  true,
				},
			},
			err: nil,
		},
		{
			name: "no device",
			args: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node-02",
					Annotations: map[string]string{
						dev.nodeRegisterAnno: "[]",
					},
				},
			},
			want: []*device.DeviceInfo{},
			err:  errors.New("no device found on node"),
		},
		{
			name: "no annotation",
			args: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node-03",
				},
			},
			want: []*device.DeviceInfo{},
			err:  fmt.Errorf("annos not found"),
		},
		{
			name: "failed to unmarshal node devices",
			args: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node-04",
					Annotations: map[string]string{
						dev.nodeRegisterAnno: "",
					},
				},
			},
			want: []*device.DeviceInfo{},
			err:  fmt.Errorf("failed to unmarshal node devices"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := dev.GetNodeDevices(test.args)
			if (err != nil) != (test.err != nil) {
				klog.ErrorS(err, "failed to unmarshal node devices", "node", test.args.Name)
			}
			if len(result) != len(test.want) {
				t.Errorf("GetNodeDevices got %d devices, want %d", len(result), len(test.want))
				return
			}
			if err == nil && len(result) != 0 {
				for k, v := range test.want {
					assert.Equal(t, v.Index, result[k].Index)
					assert.Equal(t, v.ID, result[k].ID)
					assert.Equal(t, v.Count, result[k].Count)
					assert.Equal(t, v.Devcore, result[k].Devcore)
					assert.Equal(t, v.Devmem, result[k].Devmem)
					assert.Equal(t, v.Type, result[k].Type)
					assert.Equal(t, v.Numa, result[k].Numa)
					assert.Equal(t, v.Health, result[k].Health)
				}
			}
		})
	}
}

func Test_PatchAnnotations(t *testing.T) {
	dev := Devices{
		config: VNPUConfig{
			CommonWord:     "Ascend910A",
			MemoryCapacity: int64(1024),
			Templates: []Template{
				{
					Name:   "vir02",
					Memory: int64(2184),
					AICore: int32(2),
				}, {
					Name:   "vir04",
					Memory: int64(4369),
					AICore: int32(4),
				}, {
					Name:   "vir08",
					Memory: int64(8738),
					AICore: int32(8),
				}, {
					Name:   "vir16",
					Memory: int64(17476),
					AICore: int32(16),
				},
			},
		},
	}
	tests := []struct {
		name string
		args struct {
			annoinput map[string]string
			pd        device.PodDevices
		}
		want map[string]string
	}{
		{
			name: "exist device",
			args: struct {
				annoinput map[string]string
				pd        device.PodDevices
			}{
				annoinput: map[string]string{},
				pd: device.PodDevices{
					dev.config.CommonWord: device.PodSingleDevice{
						[]device.ContainerDevice{
							{
								Idx:       0,
								UUID:      "device-0",
								Type:      "Ascend",
								Usedcores: 1,
								Usedmem:   8738,
							},
						},
					},
				},
			},
			want: map[string]string{
				device.InRequestDevices[dev.config.CommonWord]: "device-0,Ascend,8738,1:;",
				device.SupportDevices[dev.config.CommonWord]:   "device-0,Ascend,8738,1:;",
				"predicate-time":        strconv.FormatInt(time.Now().Unix(), 10),
				"huawei.com/Ascend910A": "[{\"UUID\":\"device-0\",\"temp\":\"vir08\"}]",
			},
		},
		{
			name: "no device",
			args: struct {
				annoinput map[string]string
				pd        device.PodDevices
			}{
				annoinput: map[string]string{},
				pd:        device.PodDevices{},
			},
			want: map[string]string{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := dev.PatchAnnotations(&corev1.Pod{}, &test.args.annoinput, test.args.pd)

			assert.Equal(t, len(test.want), len(result), "Expected length of result to match want")
			for k, v := range test.want {
				assert.Equal(t, v, result[k], "pod add annotation key [%s], values is [%s]", k, result[k])
			}
		})
	}
}

func Test_checkType(t *testing.T) {
	dev := Devices{
		config: VNPUConfig{
			CommonWord: "Ascend910A",
		},
	}
	tests := []struct {
		name string
		args struct {
			annos map[string]string
			d     device.DeviceUsage
			n     device.ContainerDeviceRequest
		}
		want bool
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
					Type: "Ascend910A",
				},
			},
			want: true,
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
					Type: "Ascend910B",
				},
			},
			want: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, result, _ := dev.checkType(test.args.annos, test.args.d, test.args.n)
			assert.Equal(t, result, test.want)
		})
	}
}

func Test_CheckHealth(t *testing.T) {
	dev := Devices{}
	tests := []struct {
		name string
		args struct {
			devType string
			n       corev1.Node
		}
		want1 bool
		want2 bool
	}{
		{
			name: "Requesting state",
			args: struct {
				devType string
				n       corev1.Node
			}{
				devType: "huawei.com/Ascend910",
				n: corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							util.HandshakeAnnos["huawei.com/Ascend910"]: "Requesting_2128-12-02 00:00:00",
						},
					},
				},
			},
			want1: true,
			want2: false,
		},
		{
			name: "Deleted state",
			args: struct {
				devType string
				n       corev1.Node
			}{
				devType: "huawei.com/Ascend910",
				n: corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							util.HandshakeAnnos["huawei.com/Ascend910"]: "Deleted",
						},
					},
				},
			},
			want1: true,
			want2: false,
		},
		{
			name: "Unknown state",
			args: struct {
				devType string
				n       corev1.Node
			}{
				devType: "huawei.com/Ascend910",
				n: corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							util.HandshakeAnnos["huawei.com/Ascend910"]: "Unknown",
						},
					},
				},
			},
			want1: true,
			want2: true,
		},
		{
			name: "Requesting state expired",
			args: struct {
				devType string
				n       corev1.Node
			}{
				devType: "huawei.com/Ascend910",
				n: corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							util.HandshakeAnnos["huawei.com/Ascend910"]: "Requesting_2024-01-02 00:00:00",
						},
					},
				},
			},
			want1: false,
			want2: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result1, result2 := dev.CheckHealth(test.args.devType, &test.args.n)
			assert.Equal(t, result1, test.want1)
			assert.Equal(t, result2, test.want2)
		})
	}
}

func Test_MutateAdmission(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			ctr corev1.Container
			pod corev1.Pod
		}
		want bool
	}{
		{
			name: "no set to resources limits",
			args: struct {
				ctr corev1.Container
				pod corev1.Pod
			}{
				ctr: corev1.Container{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{},
					},
				},
				pod: corev1.Pod{},
			},
			want: false,
		},
		{
			name: "resourcename and resourcememoryname set to resources limits",
			args: struct {
				ctr corev1.Container
				pod corev1.Pod
			}{
				ctr: corev1.Container{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"huawei.com/Ascend910A":        resource.MustParse("2"),
							"huawei.com/Ascend910A-memory": resource.MustParse("8738"),
						},
					},
				},
				pod: corev1.Pod{},
			},
			want: true,
		},
		{
			name: "resourcememoryname is invalid",
			args: struct {
				ctr corev1.Container
				pod corev1.Pod
			}{
				ctr: corev1.Container{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"huawei.com/Ascend910A":        resource.MustParse("2"),
							"huawei.com/Ascend910A-memory": resource.MustParse("40000"),
						},
					},
				},
				pod: corev1.Pod{},
			},
			want: false,
		},
		{
			name: "resourcememoryname not within the template scope，but smaller than MemoryCapacity",
			args: struct {
				ctr corev1.Container
				pod corev1.Pod
			}{
				ctr: corev1.Container{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"huawei.com/Ascend910A":        resource.MustParse("1"),
							"huawei.com/Ascend910A-memory": resource.MustParse("20000"),
						},
						Requests: corev1.ResourceList{
							"huawei.com/Ascend910A-memory": resource.MustParse("20000"),
						},
					},
				},
				pod: corev1.Pod{},
			},
			want: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := Devices{
				config: VNPUConfig{
					ResourceName:       "huawei.com/Ascend910A",
					ResourceMemoryName: "huawei.com/Ascend910A-memory",
					MemoryAllocatable:  int64(32768),
					MemoryCapacity:     int64(32768),
					Templates: []Template{
						{
							Name:   "vir02",
							Memory: int64(2184),
							AICore: int32(2),
						}, {
							Name:   "vir04",
							Memory: int64(4369),
							AICore: int32(4),
						}, {
							Name:   "vir08",
							Memory: int64(8738),
							AICore: int32(8),
						}, {
							Name:   "vir16",
							Memory: int64(17476),
							AICore: int32(16),
						},
					},
				},
			}
			result, _ := dev.MutateAdmission(&test.args.ctr, &test.args.pod)

			if result != test.want {
				t.Fatalf("exec MutateAdmission method expect return is %+v, but got is %+v", test.want, result)
			}

		})
	}
}

func Test_MutateAdmission910C(t *testing.T) {
	tests := []struct {
		name   string
		devCfg VNPUConfig
		args   struct {
			ctr corev1.Container
			pod corev1.Pod
		}
		want      bool
		wantErr   bool
		wantCount int64
	}{
		{
			name: "910C: request 1 → auto adjust to 2",
			devCfg: VNPUConfig{
				CommonWord:        "Ascend910C",
				ResourceName:      "huawei.com/Ascend910C",
				MemoryAllocatable: 65536,
				MemoryCapacity:    65536,
			},
			args: struct {
				ctr corev1.Container
				pod corev1.Pod
			}{
				ctr: corev1.Container{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"huawei.com/Ascend910C": resource.MustParse("1"),
						},
						Requests: corev1.ResourceList{
							"huawei.com/Ascend910C": resource.MustParse("1"),
						},
					},
				},
				pod: corev1.Pod{},
			},
			want:      true,
			wantCount: 2,
		},
		{
			name: "910C: request 3 → reject (odd number)",
			devCfg: VNPUConfig{
				CommonWord:        "Ascend910C",
				ResourceName:      "huawei.com/Ascend910C",
				MemoryAllocatable: 65536,
				MemoryCapacity:    65536,
			},
			args: struct {
				ctr corev1.Container
				pod corev1.Pod
			}{
				ctr: corev1.Container{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"huawei.com/Ascend910C": resource.MustParse("3"),
						},
						Requests: corev1.ResourceList{
							"huawei.com/Ascend910C": resource.MustParse("3"),
						},
					},
				},
				pod: corev1.Pod{},
			},
			want:    false,
			wantErr: true,
		},
		{
			name: "910C: request 4 → valid even number",
			devCfg: VNPUConfig{
				CommonWord:        "Ascend910C",
				ResourceName:      "huawei.com/Ascend910C",
				MemoryAllocatable: 65536,
				MemoryCapacity:    65536,
			},
			args: struct {
				ctr corev1.Container
				pod corev1.Pod
			}{
				ctr: corev1.Container{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"huawei.com/Ascend910C": resource.MustParse("4"),
						},
						Requests: corev1.ResourceList{
							"huawei.com/Ascend910C": resource.MustParse("4"),
						},
					},
				},
				pod: corev1.Pod{},
			},
			want:      true,
			wantCount: 4,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := Devices{config: test.devCfg}
			result, err := dev.MutateAdmission(&test.args.ctr, &test.args.pod)

			if result != test.want {
				t.Errorf("expected return bool: %v, got: %v", test.want, result)
			}

			if test.wantErr {
				assert.Assert(t, err != nil, "expected error but got nil")
			} else {
				assert.NilError(t, err)
			}

			if test.wantCount > 0 {
				limitQty := test.args.ctr.Resources.Limits[corev1.ResourceName(test.devCfg.ResourceName)]
				gotCount, ok := limitQty.AsInt64()
				assert.Assert(t, ok, "limit quantity should be convertible to int64")
				assert.Equal(t, gotCount, test.wantCount, "device count should be adjusted")

				if reqQty, exists := test.args.ctr.Resources.Requests[corev1.ResourceName(test.devCfg.ResourceName)]; exists {
					reqVal, ok := reqQty.AsInt64()
					assert.Assert(t, ok, "request quantity should be convertible to int64")
					assert.Equal(t, reqVal, test.wantCount, "requests should also be adjusted")
				}
			}
		})
	}
}

func Test_GenerateResourceRequests(t *testing.T) {
	tests := []struct {
		name string
		args corev1.Container
		want device.ContainerDeviceRequest
	}{
		{
			name: "don't set to limits and request",
			args: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits:   corev1.ResourceList{},
					Requests: corev1.ResourceList{},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "resourcename and resourcememoryname set to limits and request",
			args: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"huawei.com/Ascend910A":        resource.MustParse("2"),
						"huawei.com/Ascend910A-memory": resource.MustParse("8738"),
					},
					Requests: corev1.ResourceList{
						"huawei.com/Ascend910A":        resource.MustParse("2"),
						"huawei.com/Ascend910A-memory": resource.MustParse("8738"),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             int32(2),
				Type:             "Ascend910A",
				Memreq:           int32(8738),
				MemPercentagereq: int32(0),
				Coresreq:         int32(0),
			},
		},
		{
			name: "resourcememoryname don't set to limits and requests",
			args: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"huawei.com/Ascend910A": resource.MustParse("2"),
					},
					Requests: corev1.ResourceList{
						"huawei.com/Ascend910A": resource.MustParse("2"),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             int32(2),
				Type:             "Ascend910A",
				Memreq:           int32(0),
				MemPercentagereq: int32(100),
				Coresreq:         int32(0),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := Devices{
				config: VNPUConfig{
					CommonWord:         "Ascend910A",
					ResourceName:       "huawei.com/Ascend910A",
					ResourceMemoryName: "huawei.com/Ascend910A-memory",
					MemoryAllocatable:  int64(32768),
					MemoryCapacity:     int64(32768),
					Templates: []Template{
						{
							Name:   "vir02",
							Memory: int64(2184),
							AICore: int32(2),
						}, {
							Name:   "vir04",
							Memory: int64(4369),
							AICore: int32(4),
						}, {
							Name:   "vir08",
							Memory: int64(8738),
							AICore: int32(8),
						}, {
							Name:   "vir16",
							Memory: int64(17476),
							AICore: int32(16),
						},
					},
				},
			}
			result := dev.GenerateResourceRequests(&test.args)

			assert.Equal(t, result, test.want)
		})
	}
}

func Test_GenerateResourceRequestsFactor(t *testing.T) {
	req := corev1.Container{
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				"huawei.com/Ascend910A":        resource.MustParse("1"),
				"huawei.com/Ascend910A-memory": resource.MustParse("128"),
			},
			Requests: corev1.ResourceList{
				"huawei.com/Ascend910A":        resource.MustParse("1"),
				"huawei.com/Ascend910A-memory": resource.MustParse("128"),
			},
		},
	}
	tests := []struct {
		name string
		dev  Devices
		want device.ContainerDeviceRequest
	}{
		{
			name: "factor 10",
			dev: Devices{
				config: VNPUConfig{
					CommonWord:         "Ascend910A",
					ResourceName:       "huawei.com/Ascend910A",
					ResourceMemoryName: "huawei.com/Ascend910A-memory",
					MemoryAllocatable:  int64(32768),
					MemoryCapacity:     int64(32768),
					MemoryFactor:       int32(10),
					Templates: []Template{
						{
							Name:   "vir02",
							Memory: int64(2184),
							AICore: int32(2),
						}, {
							Name:   "vir04",
							Memory: int64(4369),
							AICore: int32(4),
						}, {
							Name:   "vir08",
							Memory: int64(8738),
							AICore: int32(8),
						}, {
							Name:   "vir16",
							Memory: int64(17476),
							AICore: int32(16),
						},
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             "Ascend910A",
				Memreq:           int32(2184),
				MemPercentagereq: int32(0),
				Coresreq:         int32(0),
			},
		},
		{
			name: "factor 100",
			dev: Devices{
				config: VNPUConfig{
					CommonWord:         "Ascend910A",
					ResourceName:       "huawei.com/Ascend910A",
					ResourceMemoryName: "huawei.com/Ascend910A-memory",
					MemoryAllocatable:  int64(32768),
					MemoryCapacity:     int64(32768),
					MemoryFactor:       int32(100),
					Templates: []Template{
						{
							Name:   "vir02",
							Memory: int64(2184),
							AICore: int32(2),
						}, {
							Name:   "vir04",
							Memory: int64(4369),
							AICore: int32(4),
						}, {
							Name:   "vir08",
							Memory: int64(8738),
							AICore: int32(8),
						}, {
							Name:   "vir16",
							Memory: int64(17476),
							AICore: int32(16),
						},
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             "Ascend910A",
				Memreq:           int32(17476),
				MemPercentagereq: int32(0),
				Coresreq:         int32(0),
			},
		},
		{
			name: "factor 0",
			dev: Devices{
				config: VNPUConfig{
					CommonWord:         "Ascend910A",
					ResourceName:       "huawei.com/Ascend910A",
					ResourceMemoryName: "huawei.com/Ascend910A-memory",
					MemoryAllocatable:  int64(32768),
					MemoryCapacity:     int64(32768),
					MemoryFactor:       int32(0),
					Templates: []Template{
						{
							Name:   "vir02",
							Memory: int64(2184),
							AICore: int32(2),
						}, {
							Name:   "vir04",
							Memory: int64(4369),
							AICore: int32(4),
						}, {
							Name:   "vir08",
							Memory: int64(8738),
							AICore: int32(8),
						}, {
							Name:   "vir16",
							Memory: int64(17476),
							AICore: int32(16),
						},
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             "Ascend910A",
				Memreq:           int32(2184),
				MemPercentagereq: int32(0),
				Coresreq:         int32(0),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := test.dev.GenerateResourceRequests(&req)
			assert.Equal(t, result, test.want)
		})
	}
}

func TestDevices_LockNode(t *testing.T) {
	tests := []struct {
		name        string
		node        *corev1.Node
		pod         *corev1.Pod
		expectError bool
	}{
		{
			name:        "Test with no containers",
			node:        &corev1.Node{},
			pod:         &corev1.Pod{Spec: corev1.PodSpec{}},
			expectError: false,
		},
		{
			name:        "Test with non-zero resource requests",
			node:        &corev1.Node{},
			pod:         &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{}}}}}},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dev := &Devices{
				config: VNPUConfig{
					CommonWord:         "Ascend310P",
					ResourceName:       "huawei.com/Ascend310P",
					ResourceMemoryName: "huawei.com/Ascend310P-memory",
				},
			}
			err := dev.LockNode(tt.node, tt.pod)
			if tt.expectError {
				assert.Equal(t, err != nil, true)
			} else {
				assert.NilError(t, err)
			}
		})
	}
}

func TestDevices_ReleaseNodeLock(t *testing.T) {
	tests := []struct {
		name        string
		node        *corev1.Node
		pod         *corev1.Pod
		expectError bool
	}{
		{
			name:        "Test with no containers",
			node:        &corev1.Node{},
			pod:         &corev1.Pod{Spec: corev1.PodSpec{}},
			expectError: false,
		},
		{
			name:        "Test with non-zero resource requests",
			node:        &corev1.Node{},
			pod:         &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{}}}}}},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dev := &Devices{
				config: VNPUConfig{
					CommonWord:         "Ascend310P",
					ResourceName:       "huawei.com/Ascend310P",
					ResourceMemoryName: "huawei.com/Ascend310P-memory",
				},
			}
			err := dev.ReleaseNodeLock(tt.node, tt.pod)
			if tt.expectError {
				assert.Equal(t, err != nil, true)
			} else {
				assert.NilError(t, err)
			}
		})
	}
}

func TestDevices_Fit(t *testing.T) {
	configStr := `- chipName: 910A
  commonWord: Ascend910A
  resourceName: huawei.com/Ascend910A
  resourceMemoryName: huawei.com/Ascend910A-memory
  memoryAllocatable: 32768
  memoryCapacity: 32768
  aiCore: 30
  templates:
    - name: vir02
      memory: 2184
      aiCore: 2
    - name: vir04
      memory: 4369
      aiCore: 4
    - name: vir08
      memory: 8738
      aiCore: 8
    - name: vir16
      memory: 17476
      aiCore: 16
- chipName: 910B2
  commonWord: Ascend910B2
  resourceName: huawei.com/Ascend910B2
  resourceMemoryName: huawei.com/Ascend910B2-memory
  memoryAllocatable: 65536
  memoryCapacity: 65536
  aiCore: 24
  aiCPU: 6
  topologyPairs:
    - 1,2,3,4,5,6,7
    - 0,2,3,4,5,6,7
    - 0,1,3,4,5,6,7
    - 0,1,2,4,5,6,7
    - 0,1,2,3,5,6,7
    - 0,1,2,3,4,6,7
    - 0,1,2,3,4,5,7
    - 0,1,2,3,4,5,6
  templates:
    - name: vir03_1c_8g
      memory: 8192
      aiCore: 3
      aiCPU: 1
    - name: vir06_1c_16g
      memory: 16384
      aiCore: 6
      aiCPU: 1
    - name: vir12_3c_32g
      memory: 32768
      aiCore: 12
      aiCPU: 3
- chipName: 910B3
  commonWord: Ascend910B3
  resourceName: huawei.com/Ascend910B3
  resourceMemoryName: huawei.com/Ascend910B3-memory
  memoryAllocatable: 65536
  memoryCapacity: 65536
  aiCore: 20
  aiCPU: 7
  topologyPairs:
    - 1,2,3,4,5,6,7
    - 0,2,3,4,5,6,7
    - 0,1,3,4,5,6,7
    - 0,1,2,4,5,6,7
    - 0,1,2,3,5,6,7
    - 0,1,2,3,4,6,7
    - 0,1,2,3,4,5,7
    - 0,1,2,3,4,5,6
  templates:
    - name: vir05_1c_16g
      memory: 16384
      aiCore: 5
      aiCPU: 1
    - name: vir10_3c_32g
      memory: 32768
      aiCore: 10
      aiCPU: 3
- chipName: 910B4
  commonWord: Ascend910B4
  resourceName: huawei.com/Ascend910B4
  resourceMemoryName: huawei.com/Ascend910B4-memory
  memoryAllocatable: 32768
  memoryCapacity: 32768
  aiCore: 20
  aiCPU: 7
  templates:
    - name: vir05_1c_8g
      memory: 8192
      aiCore: 5
      aiCPU: 1
    - name: vir10_3c_16g
      memory: 16384
      aiCore: 10
      aiCPU: 3
- chipName: 910B4-1
  commonWord: Ascend910B4
  resourceName: huawei.com/Ascend910B4
  resourceMemoryName: huawei.com/Ascend910B4-memory
  memoryAllocatable: 65536
  memoryCapacity: 65536
  aiCore: 20
  aiCPU: 7
  templates:
    - name: vir05_1c_8g
      memory: 8192
      aiCore: 5
      aiCPU: 1
    - name: vir10_3c_16g
      memory: 16384
      aiCore: 10
      aiCPU: 3
- chipName: 310P3
  commonWord: Ascend310P
  resourceName: huawei.com/Ascend310P
  resourceMemoryName: huawei.com/Ascend310P-memory
  memoryAllocatable: 21527
  memoryCapacity: 24576
  aiCore: 8
  aiCPU: 7
  templates:
    - name: vir01
      memory: 3072
      aiCore: 1
      aiCPU: 1
    - name: vir02
      memory: 6144
      aiCore: 2
      aiCPU: 2
    - name: vir04
      memory: 12288
      aiCore: 4
      aiCPU: 4
`

	var config []VNPUConfig
	if err := yaml.Unmarshal([]byte(configStr), &config); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}
	enableAscend = true
	devs := InitDevices(config)

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
					Health:    true,
				},
			},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           64,
				MemPercentagereq: 0,
				Coresreq:         50,
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
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
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
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
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
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             2,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
			},
			annos:      map[string]string{"hami.io/use-Ascend910B2-uuid": "dev-0"},
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
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             2,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
			},
			annos:      map[string]string{"hami.io/no-use-Ascend910B2-uuid": "dev-0"},
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
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
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
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         120,
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
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         100,
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
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         0,
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
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             2,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         20,
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
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           0,
				MemPercentagereq: 10,
				Coresreq:         20,
			},
			annos:      map[string]string{},
			wantFit:    true,
			wantLen:    1,
			wantDevIDs: []string{"dev-0"},
			wantReason: "",
		},
		{
			name: "fit success. schedule by NetworkID",
			devices: []*device.DeviceUsage{
				{
					ID:         "dev-0",
					Index:      0,
					Used:       0,
					Count:      100,
					Usedmem:    0,
					Totalmem:   128,
					Totalcore:  100,
					Usedcores:  0,
					Numa:       0,
					Health:     true,
					CustomInfo: map[string]any{"NetworkID": float64(0)},
				},
				{
					ID:         "dev-1",
					Index:      0,
					Used:       0,
					Count:      100,
					Usedmem:    0,
					Totalmem:   128,
					Totalcore:  100,
					Usedcores:  0,
					Numa:       0,
					Health:     true,
					CustomInfo: map[string]any{"NetworkID": float64(1)},
				},
				{
					ID:         "dev-2",
					Index:      0,
					Used:       0,
					Count:      100,
					Usedmem:    0,
					Totalmem:   128,
					Totalcore:  100,
					Usedcores:  0,
					Numa:       0,
					Health:     true,
					CustomInfo: map[string]any{"NetworkID": float64(1)},
				},
			},
			request: device.ContainerDeviceRequest{
				Nums:             2,
				Memreq:           64,
				MemPercentagereq: 0,
				Coresreq:         50,
			},
			annos:      map[string]string{},
			wantFit:    true,
			wantLen:    2,
			wantDevIDs: []string{"dev-2", "dev-1"},
			wantReason: "",
		},
	}

	for _, dev := range devs {
		for _, test := range tests {
			if !strings.Contains(test.name, "type mismatch") {
				test.request.Type = dev.config.CommonWord
			}
			if strings.Contains(test.name, "user assign use uuid mismatch") {
				test.annos["hami.io/use-"+dev.config.CommonWord+"-uuid"] = "dev-0"
			}
			if strings.Contains(test.name, "user assign no use uuid match") {
				test.annos["hami.io/no-use-"+dev.config.CommonWord+"-uuid"] = "dev-0"
			}
			for _, d := range test.devices {
				d.Type = dev.config.CommonWord
			}

			t.Run(fmt.Sprintf("%s:%s", dev.config.CommonWord, test.name), func(t *testing.T) {
				allocated := &device.PodDevices{}
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: test.annos,
					},
				}
				nodeInfo := &device.NodeInfo{
					ID: "node1",
					Devices: map[string][]device.DeviceInfo{
						dev.config.CommonWord: {
							{
								ID:         "dev-0",
								Index:      0,
								Health:     true,
								CustomInfo: map[string]any{"NetworkID": float64(0)},
							},
							{
								ID:         "dev-1",
								Index:      0,
								Numa:       0,
								Health:     true,
								CustomInfo: map[string]any{"NetworkID": float64(1)},
							},
							{
								ID:         "dev-2",
								Index:      0,
								Health:     true,
								CustomInfo: map[string]any{"NetworkID": float64(1)},
							},
						},
					},
				}
				fit, result, reason := dev.Fit(test.devices, test.request, pod, nodeInfo, allocated)
				if fit != test.wantFit {
					t.Errorf("Fit: got %v, want %v", fit, test.wantFit)
				}
				if test.wantFit {
					if len(result[dev.config.CommonWord]) != test.wantLen {
						t.Errorf("expected len: %d, got len %d", test.wantLen, len(result[dev.config.CommonWord]))
					}
					for idx, id := range test.wantDevIDs {
						if id != result[dev.config.CommonWord][idx].UUID {
							t.Errorf("expected device id: %s, got device id %s", id, result[dev.config.CommonWord][idx].UUID)
						}
					}
				}

				if reason != test.wantReason {
					t.Errorf("expected reason: %s, got reason: %s", test.wantReason, reason)
				}
			})
		}
	}
}

func TestDevices_Fit_910C(t *testing.T) {
	configStr := `- chipName: Ascend910
  commonWord: Ascend910C
  resourceName: huawei.com/Ascend910C
  resourceMemoryName: huawei.com/Ascend910C-memory
  memoryAllocatable: 65536
  memoryCapacity: 65536
  aiCore: 20
  aiCPU: 7
`

	var config []VNPUConfig
	if err := yaml.Unmarshal([]byte(configStr), &config); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}
	enableAscend = true
	devs := InitDevices(config)

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
			name: "fit success: Ascend910C topology-aware allocation (full modules only)",
			devices: []*device.DeviceUsage{
				{
					ID:         "dev-0",
					Index:      0,
					Used:       0,
					Count:      100,
					Usedmem:    0,
					Totalmem:   128,
					Totalcore:  100,
					Usedcores:  0,
					Numa:       0,
					Health:     true,
					CustomInfo: map[string]any{"NetworkID": float64(0)},
				},
				{
					ID:         "dev-1",
					Index:      1,
					Used:       0,
					Count:      100,
					Usedmem:    0,
					Totalmem:   128,
					Totalcore:  100,
					Usedcores:  0,
					Numa:       0,
					Health:     true,
					CustomInfo: map[string]any{"NetworkID": float64(0)},
				},
				{
					ID:         "dev-2",
					Index:      2,
					Used:       0,
					Count:      100,
					Usedmem:    0,
					Totalmem:   128,
					Totalcore:  100,
					Usedcores:  0,
					Numa:       0,
					Health:     true,
					CustomInfo: map[string]any{"NetworkID": float64(0)},
				},
			},
			request: device.ContainerDeviceRequest{
				Nums:             2,
				Memreq:           128,
				MemPercentagereq: 0,
				Coresreq:         100,
			},
			annos:      map[string]string{},
			wantFit:    true,
			wantLen:    2,
			wantDevIDs: []string{"dev-1", "dev-0"},
			wantReason: "",
		},
	}

	for _, dev := range devs {
		for _, test := range tests {
			if !strings.Contains(test.name, "type mismatch") {
				test.request.Type = dev.config.CommonWord
			}

			for _, d := range test.devices {
				d.Type = dev.config.CommonWord
			}

			t.Run(fmt.Sprintf("%s:%s", dev.config.CommonWord, test.name), func(t *testing.T) {
				allocated := &device.PodDevices{}
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: test.annos,
					},
				}
				nodeInfo := &device.NodeInfo{
					ID: "node1",
					Devices: map[string][]device.DeviceInfo{
						dev.config.CommonWord: {
							{
								ID:         "dev-0",
								Index:      0,
								Health:     true,
								CustomInfo: map[string]any{"NetworkID": float64(0)},
							},
							{
								ID:         "dev-1",
								Index:      0,
								Numa:       0,
								Health:     true,
								CustomInfo: map[string]any{"NetworkID": float64(0)},
							},
							{
								ID:         "dev-2",
								Index:      0,
								Health:     true,
								CustomInfo: map[string]any{"NetworkID": float64(0)},
							},
						},
					},
				}
				fit, result, reason := dev.Fit(test.devices, test.request, pod, nodeInfo, allocated)
				klog.Infof("Result>>>> %d Ascend device plugins: %+v", len(result), result)
				if fit != test.wantFit {
					t.Errorf("Fit: got %v, want %v", fit, test.wantFit)
				}
				if test.wantFit {
					if len(result[dev.config.CommonWord]) != test.wantLen {
						t.Errorf("expected len: %d, got len %d", test.wantLen, len(result[dev.config.CommonWord]))
					}
					for idx, id := range test.wantDevIDs {
						if id != result[dev.config.CommonWord][idx].UUID {
							t.Errorf("expected device id: %s, got device id %s", id, result[dev.config.CommonWord][idx].UUID)
						}
					}
				}

				if reason != test.wantReason {
					t.Errorf("expected reason: %s, got reason: %s", test.wantReason, reason)
				}
			})
		}
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
			dev := &Devices{}
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
