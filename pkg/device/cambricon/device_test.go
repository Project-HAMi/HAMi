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

package cambricon

import (
	"flag"
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/util"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/api/resource"
)

func Test_GetNodeDevices(t *testing.T) {
	config := CambriconConfig{
		ResourceMemoryName: "cambricon.com/mlu.smlu.vmemory",
		ResourceCoreName:   "cambricon.com/mlu.smlu.vcore",
	}
	InitMLUDevice(config)
	tests := []struct {
		name string
		args corev1.Node
		want []*util.DeviceInfo
	}{
		{
			name: "test with vaild configuration",
			args: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Status: corev1.NodeStatus{
					Capacity: corev1.ResourceList{
						"cambricon.com/mlu.smlu.vcore":   *resource.NewQuantity(1, resource.DecimalSI),
						"cambricon.com/mlu.smlu.vmemory": *resource.NewQuantity(1, resource.DecimalSI),
					},
				},
			},
			want: []*util.DeviceInfo{
				{
					Index:   0,
					ID:      "test-cambricon-mlu-0",
					Count:   int32(100),
					Devmem:  int32(25600),
					Devcore: int32(100),
					Type:    CambriconMLUDevice,
					Numa:    0,
					Health:  true,
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := CambriconDevices{}
			result, err := dev.GetNodeDevices(test.args)
			if err != nil {
				assert.NoError(t, err)
			}
			for k, v := range test.want {
				assert.Equal(t, v, result[k])
			}
		})
	}
}

func Test_MutateAdmission(t *testing.T) {
	config := CambriconConfig{
		ResourceMemoryName: "cambricon.com/mlu.smlu.vmemory",
		ResourceCoreName:   "cambricon.com/mlu.smlu.vcore",
		ResourceCountName:  "cambricon.com/mlu",
	}
	InitMLUDevice(config)
	tests := []struct {
		name string
		args struct {
			ctr corev1.Container
			pod corev1.Pod
		}
		want bool
		err  error
	}{
		{
			name: "set to resources limits",
			args: struct {
				ctr corev1.Container
				pod corev1.Pod
			}{
				ctr: corev1.Container{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"cambricon.com/mlu": resource.MustParse("1"),
						},
					},
				},
				pod: corev1.Pod{},
			},
			want: true,
			err:  nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := CambriconDevices{}
			result, _ := dev.MutateAdmission(&test.args.ctr, &test.args.pod)
			assert.Equal(t, result, test.want)
		})
	}
}

func Test_ParseConfig(t *testing.T) {
	tests := []struct {
		name string
		args flag.FlagSet
	}{
		{
			name: "test",
			args: flag.FlagSet{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ParseConfig(&test.args)
		})
	}
}

func Test_CheckType(t *testing.T) {
	dev := CambriconDevices{}
	tests := []struct {
		name string
		args struct {
			annos map[string]string
			d     util.DeviceUsage
			n     util.ContainerDeviceRequest
		}
		want1 bool
		want2 bool
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
					Type: dev.CommonWord(),
				},
			},
			want1: true,
			want2: true,
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
					Type: "TEST",
				},
			},
			want1: false,
			want2: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result1, result2, _ := dev.CheckType(test.args.annos, test.args.d, test.args.n)
			assert.Equal(t, result1, test.want1)
			assert.Equal(t, result2, test.want2)
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
			name: "don't set UserUUID,NoUserUUID and annotation",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{annos: map[string]string{},
				d: util.DeviceUsage{},
			},
			want: true,
		},
		{
			name: "set UserUUID and annotation, don't set NoUserUUID",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{
					"cambricon.com/use-gpuuuid": "test123,111",
				},
				d: util.DeviceUsage{
					ID: "test123",
				},
			},
			want: true,
		},
		{
			name: "don't set UserUUID, set NoUserUUID and annotation",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{
					"cambricon.com/nouse-gpuuuid": "test123,111",
				},
				d: util.DeviceUsage{
					ID: "test123",
				},
			},
			want: false,
		},
		{
			name: "set UserUUID, don't set NoUserUUID,annotation and device not match",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{
					"cambricon.com/nouse-gpuuuid": "test123,111",
				},
				d: util.DeviceUsage{
					ID: "test456",
				},
			},
			want: true,
		},
		{
			name: "don't set UserUUID,set NoUserUUID,annotation and device not match",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{
					"cambricon.com/use-gpuuuid": "test123,111",
				},
				d: util.DeviceUsage{
					ID: "test456",
				},
			},
			want: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := CambriconDevices{}
			result := dev.CheckUUID(test.args.annos, test.args.d)
			assert.Equal(t, result, test.want)
		})
	}
}

func Test_GenerateResourceRequests(t *testing.T) {
	tests := []struct {
		name string
		args corev1.Container
		want util.ContainerDeviceRequest
	}{
		{
			name: "don't set to limits and request",
			args: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits:   corev1.ResourceList{},
					Requests: corev1.ResourceList{},
				},
			},
			want: util.ContainerDeviceRequest{
				Nums: 0,
			},
		},
		{
			name: "resourcecoresname,resourcecountname and resourcememoryname set to limits and request",
			args: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"cambricon.com/mlu":              resource.MustParse("1"),
						"cambricon.com/mlu.smlu.vmemory": resource.MustParse("1000"),
						"cambricon.com/mlu.smlu.vcore":   resource.MustParse("2"),
					},
					Requests: corev1.ResourceList{
						"cambricon.com/mlu":              resource.MustParse("1"),
						"cambricon.com/mlu.smlu.vmemory": resource.MustParse("1000"),
						"cambricon.com/mlu.smlu.vcore":   resource.MustParse("2"),
					},
				},
			},
			want: util.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             CambriconMLUDevice,
				Memreq:           int32(256000),
				MemPercentagereq: int32(0),
				Coresreq:         int32(2),
			},
		},
		{
			name: "resourcememoryname don't set to limits and requests",
			args: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"cambricon.com/mlu":              resource.MustParse("1"),
						"cambricon.com/mlu.smlu.vmemory": resource.MustParse("1000"),
					},
					Requests: corev1.ResourceList{
						"cambricon.com/mlu":              resource.MustParse("1"),
						"cambricon.com/mlu.smlu.vmemory": resource.MustParse("1000"),
					},
				},
			},
			want: util.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             CambriconMLUDevice,
				Memreq:           int32(256000),
				MemPercentagereq: int32(0),
				Coresreq:         int32(100),
			},
		},
		{
			name: "resourcecoresname don't set to limits and requests",
			args: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"cambricon.com/mlu":            resource.MustParse("1"),
						"cambricon.com/mlu.smlu.vcore": resource.MustParse("2"),
					},
					Requests: corev1.ResourceList{
						"cambricon.com/mlu":            resource.MustParse("1"),
						"cambricon.com/mlu.smlu.vcore": resource.MustParse("2"),
					},
				},
			},
			want: util.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             CambriconMLUDevice,
				Memreq:           int32(0),
				MemPercentagereq: int32(100),
				Coresreq:         int32(2),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := CambriconConfig{
				ResourceMemoryName: "cambricon.com/mlu.smlu.vmemory",
				ResourceCoreName:   "cambricon.com/mlu.smlu.vcore",
				ResourceCountName:  "cambricon.com/mlu",
			}
			InitMLUDevice(config)
			dev := CambriconDevices{}
			result := dev.GenerateResourceRequests(&test.args)
			assert.Equal(t, test.want, result)
		})
	}
}

func Test_PatchAnnotations(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			annoinput map[string]string
			pd        util.PodDevices
		}
		want map[string]string
	}{
		{
			name: "exist device",
			args: struct {
				annoinput map[string]string
				pd        util.PodDevices
			}{
				annoinput: map[string]string{},
				pd: util.PodDevices{
					CambriconMLUDevice: util.PodSingleDevice{
						[]util.ContainerDevice{
							{
								Idx:       0,
								UUID:      "device-0",
								Type:      "MLU",
								Usedcores: 1,
								Usedmem:   256000,
							},
						},
					},
				},
			},
			want: map[string]string{
				"CAMBRICON_DSMLU_ASSIGHED": "false",
				"CAMBRICON_DSMLU_PROFILE":  "0_1_1000",
			},
		},
		{
			name: "no device",
			args: struct {
				annoinput map[string]string
				pd        util.PodDevices
			}{
				annoinput: map[string]string{},
				pd:        util.PodDevices{},
			},
			want: map[string]string{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := CambriconDevices{}
			result := dev.PatchAnnotations(&test.args.annoinput, test.args.pd)
			assert.Equal(t, len(test.want), len(result), "Expected length of result to match want")
			for k, v := range test.want {
				assert.Equal(t, v, result[k], "pod add annotation key [%s], values is [%s]", k, result[k])
			}
		})
	}
}
