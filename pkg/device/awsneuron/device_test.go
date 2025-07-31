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

package awsneuron

import (
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
			name: "set neuron number",
			args: struct {
				ctr *corev1.Container
				p   *corev1.Pod
			}{
				ctr: &corev1.Container{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"aws.amazon.com/neuron": *resource.NewQuantity(2, resource.DecimalSI),
						},
					},
				},
				p: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{},
				},
			},
			want: true,
		},
		{
			name: "set neuron cores",
			args: struct {
				ctr *corev1.Container
				p   *corev1.Pod
			}{
				ctr: &corev1.Container{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"aws.amazon.com/neuroncore": *resource.NewQuantity(2, resource.DecimalSI),
						},
					},
				},
				p: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{},
				},
			},
			want: true,
		},
		{
			name: "no neuron devices",
			args: struct {
				ctr *corev1.Container
				p   *corev1.Pod
			}{
				ctr: &corev1.Container{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{},
					},
				},
				p: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{},
				},
			},
			want: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := AWSNeuronConfig{
				ResourceCountName: "aws.amazon.com/neuron",
				ResourceCoreName:  "aws.amazon.com/neuroncore",
			}
			dev := InitAWSNeuronDevice(config)
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
					Labels: map[string]string{
						"node.kubernetes.io/instance-type": "inf2",
					},
					Name: "test",
				},
				Status: corev1.NodeStatus{
					Capacity: corev1.ResourceList{
						"aws.amazon.com/neuron":     *resource.NewQuantity(1, resource.DecimalSI),
						"aws.amazon.com/neuroncore": *resource.NewQuantity(2, resource.DecimalSI),
					},
				},
			},
			want: []*util.DeviceInfo{
				{
					Index:      uint(0),
					ID:         "test-AWSNeuron-0",
					Count:      int32(2),
					Devmem:     int32(0),
					Devcore:    int32(3),
					Type:       AWSNeuronDevice,
					Numa:       0,
					Health:     true,
					CustomInfo: map[string]any{"AWSNodeType": string("inf2")},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := AWSNeuronConfig{
				ResourceCountName: "aws.amazon.com/neuron",
				ResourceCoreName:  "aws.amazon.com/neuroncore",
			}
			dev := InitAWSNeuronDevice(config)
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
			pod       corev1.Pod
			pd        util.PodDevices
		}
		want map[string]string
	}{
		{
			name: "neuron device",
			args: struct {
				annoinput *map[string]string
				pod       corev1.Pod
				pd        util.PodDevices
			}{
				annoinput: &map[string]string{},
				pod: corev1.Pod{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										"aws.amazon.com/neuron": resource.MustParse("2"),
									},
								},
							},
						},
					},
				},
				pd: util.PodDevices{
					AWSNeuronDevice: util.PodSingleDevice{
						util.ContainerDevices{
							{
								Idx:       0,
								UUID:      "test1",
								Type:      AWSNeuronDevice,
								Usedmem:   int32(0),
								Usedcores: int32(3),
								CustomInfo: map[string]any{
									AWSUsageInfo: 3,
								},
							},
							{
								Idx:       1,
								UUID:      "test2",
								Type:      AWSNeuronDevice,
								Usedmem:   int32(0),
								Usedcores: int32(3),
								CustomInfo: map[string]any{
									AWSUsageInfo: 3,
								},
							},
						},
					},
				},
			},
			want: map[string]string{
				util.SupportDevices[AWSNeuronDevice]: "test1,AWSNeuron,0,2:test2,AWSNeuron,0,3;",
				AWSNeuronAssignedIndex:               "0,1",
				AWSNeuronAssignedNode:                "",
			},
		},
		{
			name: "neuroncore device",
			args: struct {
				annoinput *map[string]string
				pod       corev1.Pod
				pd        util.PodDevices
			}{
				annoinput: &map[string]string{},
				pod: corev1.Pod{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										"aws.amazon.com/neuroncore": resource.MustParse("2"),
									},
								},
							},
						},
					},
				},
				pd: util.PodDevices{
					AWSNeuronDevice: util.PodSingleDevice{
						util.ContainerDevices{
							{
								Idx:       0,
								UUID:      "test1",
								Type:      AWSNeuronDevice,
								Usedmem:   int32(0),
								Usedcores: int32(3),
								CustomInfo: map[string]any{
									AWSUsageInfo: 3,
								},
							},
						},
					},
				},
			},
			want: map[string]string{
				util.SupportDevices[AWSNeuronDevice]: "test1,AWSNeuron,0,3:;",
				AWSNeuronAssignedIndex:               "0,1",
				AWSNeuronAssignedNode:                "",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := AWSNeuronConfig{
				ResourceCountName: "aws.amazon.com/neuron",
				ResourceCoreName:  "aws.amazon.com/neuroncore",
			}
			dev := InitAWSNeuronDevice(config)
			dev.coresPerAWSNeuron = 2
			result := dev.PatchAnnotations(&test.args.pod, test.args.annoinput, test.args.pd)
			assert.Equal(t, result[dev.CommonWord()], test.want[dev.CommonWord()])
			assert.Equal(t, result[AWSNeuronAssignedIndex], test.want[AWSNeuronAssignedIndex])
			assert.Equal(t, result[AWSNeuronAssignedNode], test.want[AWSNeuronAssignedNode])
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
					Type: AWSNeuronDevice,
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
			dev := AWSNeuronDevices{}
			result1, result2, result3 := dev.checkType(test.args.n)
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
					AWSNeuronUseUUID: "test1",
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
					AWSNeuronUseUUID: "test1",
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
					AWSNeuronNoUseUUID: "test1",
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
					AWSNeuronNoUseUUID: "test1",
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
			dev := AWSNeuronDevices{}
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
			name: "allocate neuron device",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"aws.amazon.com/neuron": resource.MustParse("1"),
					},
					Requests: corev1.ResourceList{
						"aws.amazon.com/neuron": resource.MustParse("1"),
					},
				},
			},
			want: util.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             AWSNeuronDevice,
				Memreq:           int32(0),
				MemPercentagereq: int32(0),
				Coresreq:         int32(2),
			},
		},
		{
			name: "allocate neuron core",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"aws.amazon.com/neuroncore": resource.MustParse("1"),
					},
					Requests: corev1.ResourceList{
						"aws.amazon.com/neuroncore": resource.MustParse("1"),
					},
				},
			},
			want: util.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             AWSNeuronDevice,
				Memreq:           int32(0),
				MemPercentagereq: int32(0),
				Coresreq:         int32(1),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := AWSNeuronConfig{
				ResourceCountName: "aws.amazon.com/neuron",
				ResourceCoreName:  "aws.amazon.com/neuroncore",
			}
			dev := InitAWSNeuronDevice(config)
			dev.coresPerAWSNeuron = 2
			result := dev.GenerateResourceRequests(test.args)
			assert.DeepEqual(t, result, test.want)
		})
	}
}

func Test_countMaskAvailable(t *testing.T) {
	tests := []struct {
		name string
		args int32
		want int32
	}{
		{
			name: "test 3",
			args: int32(3),
			want: int32(2),
		},
		{
			name: "test 2",
			args: int32(2),
			want: int32(1),
		},
		{
			name: "test 1",
			args: int32(1),
			want: int32(1),
		},
		{
			name: "test 0",
			args: int32(0),
			want: int32(0),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result1 := countMaskAvailable(test.args)
			assert.DeepEqual(t, result1, test.want)
		})
	}
}

func Test_graphSelect(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			d []*util.DeviceUsage
			c int
		}
		want1 []int
	}{
		{
			name: "full allocate",
			args: struct {
				d []*util.DeviceUsage
				c int
			}{
				d: []*util.DeviceUsage{
					{Index: 0, Used: 0, CustomInfo: map[string]any{
						AWSNodeType: "inf2",
					}},
					{Index: 1, Used: 0},
					{Index: 2, Used: 0},
					{Index: 3, Used: 0},
					{Index: 4, Used: 0},
					{Index: 5, Used: 0},
					{Index: 6, Used: 0},
					{Index: 7, Used: 0},
					{Index: 8, Used: 0},
					{Index: 9, Used: 0},
					{Index: 10, Used: 0},
					{Index: 11, Used: 0},
					{Index: 12, Used: 0},
					{Index: 13, Used: 0},
					{Index: 14, Used: 0},
					{Index: 15, Used: 0},
				},
				c: 16,
			},
			want1: []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
		},
		{
			name: "8 allocate fail",
			args: struct {
				d []*util.DeviceUsage
				c int
			}{
				d: []*util.DeviceUsage{
					{Index: 0, Used: 0, CustomInfo: map[string]any{
						AWSNodeType: "trn",
					}},
					{Index: 1, Used: 0},
					{Index: 2, Used: 0},
					{Index: 3, Used: 0},
					{Index: 4, Used: 0},
					{Index: 5, Used: 0},
					{Index: 6, Used: 0},
					{Index: 7, Used: 1},
					{Index: 8, Used: 0},
					{Index: 9, Used: 0},
					{Index: 10, Used: 0},
					{Index: 11, Used: 0},
					{Index: 12, Used: 0},
					{Index: 13, Used: 1},
					{Index: 14, Used: 0},
					{Index: 15, Used: 0},
				},
				c: 8,
			},
			want1: []int{},
		},
		{
			name: "8 allocate success",
			args: struct {
				d []*util.DeviceUsage
				c int
			}{
				d: []*util.DeviceUsage{
					{Index: 0, Used: 0, CustomInfo: map[string]any{
						AWSNodeType: "trn",
					}},
					{Index: 1, Used: 0},
					{Index: 2, Used: 0},
					{Index: 3, Used: 0},
					{Index: 4, Used: 0},
					{Index: 5, Used: 0},
					{Index: 6, Used: 0},
					{Index: 7, Used: 1},
					{Index: 8, Used: 0},
					{Index: 9, Used: 0},
					{Index: 10, Used: 0},
					{Index: 11, Used: 0},
					{Index: 12, Used: 0},
					{Index: 13, Used: 0},
					{Index: 14, Used: 0},
					{Index: 15, Used: 0},
				},
				c: 8,
			},
			want1: []int{8, 9, 10, 11, 12, 13, 14, 15},
		},
		{
			name: "8 allocate",
			args: struct {
				d []*util.DeviceUsage
				c int
			}{
				d: []*util.DeviceUsage{
					{Index: 0, Used: 0, CustomInfo: map[string]any{
						AWSNodeType: "inf",
					}},
					{Index: 1, Used: 0},
					{Index: 2, Used: 0},
					{Index: 3, Used: 0},
					{Index: 4, Used: 0},
					{Index: 5, Used: 1},
					{Index: 6, Used: 0},
					{Index: 7, Used: 0},
					{Index: 8, Used: 0},
					{Index: 9, Used: 0},
					{Index: 10, Used: 0},
					{Index: 11, Used: 0},
					{Index: 12, Used: 0},
					{Index: 13, Used: 0},
					{Index: 14, Used: 1},
					{Index: 15, Used: 0},
				},
				c: 8,
			},
			want1: []int{6, 7, 8, 9, 10, 11, 12, 13},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result1 := graphSelect(test.args.d, test.args.c)
			assert.DeepEqual(t, result1, test.want1)
		})
	}
}

func TestDevices_Fit(t *testing.T) {
	config := AWSNeuronConfig{
		ResourceCountName: "aws.amazon.com/neuron",
		ResourceCoreName:  "aws.amazon.com/neuroncore",
	}
	dev := InitAWSNeuronDevice(config)

	tests := []struct {
		name       string
		devices    []*util.DeviceUsage
		request    util.ContainerDeviceRequest
		annos      map[string]string
		wantFit    bool
		wantLen    int
		wantDevIDs []string
		wantReason string
	}{
		{
			name: "fit success",
			devices: []*util.DeviceUsage{
				{
					ID:        "dev-0",
					Index:     0,
					Used:      0,
					Count:     2,
					Usedmem:   0,
					Totalmem:  0,
					Totalcore: 3,
					Usedcores: 0,
					Numa:      0,
					Type:      AWSNeuronDevice,
					Health:    true,
					CustomInfo: map[string]any{
						AWSNodeType: "trn",
					},
				},
				{
					ID:        "dev-1",
					Index:     0,
					Used:      0,
					Count:     12,
					Usedmem:   0,
					Totalmem:  0,
					Totalcore: 3,
					Usedcores: 0,
					Numa:      0,
					Type:      AWSNeuronDevice,
					Health:    true,
					CustomInfo: map[string]any{
						AWSNodeType: "trn",
					},
				},
			},
			request: util.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           0,
				MemPercentagereq: 0,
				Coresreq:         2,
				Type:             AWSNeuronDevice,
			},
			annos:      map[string]string{},
			wantFit:    true,
			wantLen:    1,
			wantDevIDs: []string{"dev-1"},
			wantReason: "",
		},
		{
			name: "fit fail: memory not enough",
			devices: []*util.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      0,
				Count:     2,
				Usedmem:   0,
				Totalmem:  0,
				Totalcore: 3,
				Usedcores: 0,
				Numa:      0,
				Type:      AWSNeuronDevice,
				Health:    true,
				CustomInfo: map[string]any{
					AWSNodeType: "trn",
				},
			}},
			request: util.ContainerDeviceRequest{
				Nums:             2,
				Memreq:           0,
				MemPercentagereq: 0,
				Coresreq:         2,
				Type:             AWSNeuronDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 NumaNotFit",
		},
		{
			name: "fit fail: core not enough",
			devices: []*util.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      0,
				Count:     2,
				Usedmem:   0,
				Totalmem:  0,
				Totalcore: 3,
				Usedcores: 1,
				Numa:      0,
				Type:      AWSNeuronDevice,
				Health:    true,
				CustomInfo: map[string]any{
					AWSNodeType: "trn",
				},
			}},
			request: util.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           0,
				MemPercentagereq: 0,
				Coresreq:         2,
				Type:             AWSNeuronDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardInsufficientCore",
		},
		{
			name: "fit fail: type mismatch",
			devices: []*util.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      0,
				Count:     2,
				Usedmem:   0,
				Totalmem:  0,
				Totalcore: 3,
				Usedcores: 0,
				Numa:      0,
				Health:    true,
				Type:      AWSNeuronDevice,
				CustomInfo: map[string]any{
					AWSNodeType: "trn",
				},
			}},
			request: util.ContainerDeviceRequest{
				Nums:             1,
				Type:             "OtherType",
				Memreq:           0,
				MemPercentagereq: 0,
				Coresreq:         2,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardTypeMismatch",
		},
		{
			name: "fit fail: user assign use uuid mismatch",
			devices: []*util.DeviceUsage{{
				ID:        "dev-1",
				Index:     0,
				Used:      0,
				Count:     2,
				Usedmem:   0,
				Totalmem:  0,
				Totalcore: 3,
				Usedcores: 0,
				Numa:      0,
				Type:      AWSNeuronDevice,
				Health:    true,
				CustomInfo: map[string]any{
					AWSNodeType: "trn",
				},
			}},
			request: util.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           0,
				MemPercentagereq: 0,
				Coresreq:         2,
				Type:             AWSNeuronDevice,
			},
			annos:      map[string]string{"aws.amazon.com/use-neuron-uuid": "dev-0"},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardUuidMismatch",
		},
		{
			name: "fit fail: user assign no use uuid match",
			devices: []*util.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      0,
				Count:     2,
				Usedmem:   0,
				Totalmem:  0,
				Totalcore: 3,
				Usedcores: 0,
				Numa:      0,
				Type:      AWSNeuronDevice,
				Health:    true,
				CustomInfo: map[string]any{
					AWSNodeType: "trn",
				},
			}},
			request: util.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           0,
				MemPercentagereq: 0,
				Coresreq:         2,
				Type:             AWSNeuronDevice,
			},
			annos:      map[string]string{"aws.amazon.com/nouse-neuron-uuid": "dev-0"},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardUuidMismatch",
		},
		{
			name: "fit fail: card overused",
			devices: []*util.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      2,
				Count:     2,
				Usedmem:   0,
				Totalmem:  0,
				Totalcore: 3,
				Usedcores: 0,
				Numa:      0,
				Type:      AWSNeuronDevice,
				Health:    true,
				CustomInfo: map[string]any{
					AWSNodeType: "trn",
				},
			}},
			request: util.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           0,
				MemPercentagereq: 0,
				Coresreq:         2,
				Type:             AWSNeuronDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardTimeSlicingExhausted",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			allocated := &util.PodDevices{}
			fit, result, reason := dev.Fit(test.devices, test.request, test.annos, &corev1.Pod{}, &util.NodeInfo{}, allocated)
			if fit != test.wantFit {
				t.Errorf("Fit: got %v, want %v", fit, test.wantFit)
			}
			if test.wantFit {
				if len(result[AWSNeuronDevice]) != test.wantLen {
					t.Errorf("expected len: %d, got len %d", test.wantLen, len(result[AWSNeuronDevice]))
				}
				for idx, id := range test.wantDevIDs {
					if id != result[AWSNeuronDevice][idx].UUID {
						t.Errorf("expected device id: %s, got device id %s", id, result[AWSNeuronDevice][idx].UUID)
					}
				}
			}
			if reason != test.wantReason {
				t.Errorf("expected reason: %s, got reason: %s", test.wantReason, reason)
			}
		})
	}
}
