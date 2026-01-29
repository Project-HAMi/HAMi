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

package amd

import (
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
			name: "set amdgpu number",
			args: struct {
				ctr *corev1.Container
				p   *corev1.Pod
			}{
				ctr: &corev1.Container{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"amd.com/gpu": *resource.NewQuantity(2, resource.DecimalSI),
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
			name: "no amdgpu devices",
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
			config := AMDConfig{
				ResourceCountName: "amd.com/gpu",
			}
			dev := InitAMDGPUDevice(config)
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
					Labels: map[string]string{
						"node.kubernetes.io/instance-type": "inf2",
					},
					Name: "test",
				},
				Status: corev1.NodeStatus{
					Capacity: corev1.ResourceList{
						"amd.com/gpu": *resource.NewQuantity(1, resource.DecimalSI),
					},
				},
			},
			want: []*device.DeviceInfo{
				{
					Index:        uint(0),
					ID:           "test-AMDGPU-0",
					Count:        int32(1),
					Devmem:       int32(Mi300xMemory),
					Devcore:      int32(100),
					Type:         AMDDevice,
					Numa:         0,
					Health:       true,
					CustomInfo:   map[string]any{},
					DeviceVendor: AMDCommonWord,
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := AMDConfig{
				ResourceCountName: "amd.com/gpu",
			}
			dev := InitAMDGPUDevice(config)
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
			pd        device.PodDevices
		}
		want map[string]string
	}{
		{
			name: "amd device",
			args: struct {
				annoinput *map[string]string
				pod       corev1.Pod
				pd        device.PodDevices
			}{
				annoinput: &map[string]string{},
				pod: corev1.Pod{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										"amd.com/gpu": resource.MustParse("2"),
									},
								},
							},
						},
					},
				},
				pd: device.PodDevices{
					AMDDevice: device.PodSingleDevice{
						device.ContainerDevices{
							{
								Idx:        0,
								UUID:       "test1",
								Type:       AMDDevice,
								Usedmem:    int32(0),
								Usedcores:  int32(3),
								CustomInfo: map[string]any{},
							},
							{
								Idx:        1,
								UUID:       "test2",
								Type:       AMDDevice,
								Usedmem:    int32(0),
								Usedcores:  int32(3),
								CustomInfo: map[string]any{},
							},
						},
					},
				},
			},
			want: map[string]string{
				device.SupportDevices[AMDDevice]: "test1,AMDGPU,0,3:test2,AMDGPU,0,3:;",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := AMDConfig{
				ResourceCountName: "amd.com/gpu",
			}
			dev := InitAMDGPUDevice(config)
			result := dev.PatchAnnotations(&test.args.pod, test.args.annoinput, test.args.pd)
			assert.Equal(t, result[device.SupportDevices[AMDDevice]], test.want[device.SupportDevices[AMDDevice]])
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
					Type: AMDDevice,
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
			dev := AMDDevices{}
			result1, result2, result3 := dev.checkType(test.args.n)
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
			name: "allocate amdgpu device",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"amd.com/gpu": resource.MustParse("1"),
					},
					Requests: corev1.ResourceList{
						"amd.com/gpu": resource.MustParse("1"),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             AMDDevice,
				Memreq:           int32(Mi300xMemory),
				MemPercentagereq: int32(0),
				Coresreq:         int32(0),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := AMDConfig{
				ResourceCountName: "amd.com/gpu",
			}
			dev := InitAMDGPUDevice(config)
			result := dev.GenerateResourceRequests(test.args)
			assert.DeepEqual(t, result, test.want)
		})
	}
}

func TestDevices_Fit(t *testing.T) {
	config := AMDConfig{
		ResourceCountName: "amd.com/gpu",
	}
	dev := InitAMDGPUDevice(config)

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
					ID:         "dev-0",
					Index:      0,
					Used:       0,
					Count:      2,
					Usedmem:    0,
					Totalmem:   0,
					Totalcore:  3,
					Usedcores:  0,
					Numa:       0,
					Type:       AMDDevice,
					Health:     true,
					CustomInfo: map[string]any{},
				},
				{
					ID:         "dev-1",
					Index:      0,
					Used:       0,
					Count:      12,
					Usedmem:    0,
					Totalmem:   0,
					Totalcore:  3,
					Usedcores:  0,
					Numa:       0,
					Type:       AMDDevice,
					Health:     true,
					CustomInfo: map[string]any{},
				},
			},
			request: device.ContainerDeviceRequest{
				Nums:             2,
				Memreq:           0,
				MemPercentagereq: 0,
				Coresreq:         0,
				Type:             AMDDevice,
			},
			annos:      map[string]string{},
			wantFit:    true,
			wantLen:    2,
			wantDevIDs: []string{"dev-1", "dev-0"},
			wantReason: "",
		},
		{
			name: "fit success for multiple cards",
			devices: []*device.DeviceUsage{
				{
					ID:         "dev-0",
					Index:      0,
					Used:       0,
					Count:      2,
					Usedmem:    0,
					Totalmem:   0,
					Totalcore:  3,
					Usedcores:  0,
					Numa:       0,
					Type:       AMDDevice,
					Health:     true,
					CustomInfo: map[string]any{},
				},
				{
					ID:         "dev-1",
					Index:      0,
					Used:       0,
					Count:      12,
					Usedmem:    0,
					Totalmem:   0,
					Totalcore:  3,
					Usedcores:  0,
					Numa:       0,
					Type:       AMDDevice,
					Health:     true,
					CustomInfo: map[string]any{},
				},
			},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           0,
				MemPercentagereq: 0,
				Coresreq:         2,
				Type:             AMDDevice,
			},
			annos:      map[string]string{},
			wantFit:    true,
			wantLen:    1,
			wantDevIDs: []string{"dev-1"},
			wantReason: "",
		},
		{
			name: "fit fail: type mismatch",
			devices: []*device.DeviceUsage{{
				ID:         "dev-0",
				Index:      0,
				Used:       0,
				Count:      2,
				Usedmem:    0,
				Totalmem:   0,
				Totalcore:  3,
				Usedcores:  0,
				Numa:       0,
				Health:     true,
				Type:       AMDDevice,
				CustomInfo: map[string]any{},
			}},
			request: device.ContainerDeviceRequest{
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
			devices: []*device.DeviceUsage{{
				ID:         "dev-1",
				Index:      0,
				Used:       0,
				Count:      2,
				Usedmem:    0,
				Totalmem:   0,
				Totalcore:  3,
				Usedcores:  0,
				Numa:       0,
				Type:       AMDDevice,
				Health:     true,
				CustomInfo: map[string]any{},
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           0,
				MemPercentagereq: 0,
				Coresreq:         2,
				Type:             AMDDevice,
			},
			annos:      map[string]string{"amd.com/use-gpu-uuid": "dev-0"},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardUuidMismatch",
		},
		{
			name: "fit fail: user assign no use uuid match",
			devices: []*device.DeviceUsage{{
				ID:         "dev-0",
				Index:      0,
				Used:       0,
				Count:      2,
				Usedmem:    0,
				Totalmem:   0,
				Totalcore:  3,
				Usedcores:  0,
				Numa:       0,
				Type:       AMDDevice,
				Health:     true,
				CustomInfo: map[string]any{},
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           0,
				MemPercentagereq: 0,
				Coresreq:         2,
				Type:             AMDDevice,
			},
			annos:      map[string]string{"amd.com/nouse-gpu-uuid": "dev-0"},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardUuidMismatch",
		},
		{
			name: "fit fail: card overused",
			devices: []*device.DeviceUsage{{
				ID:         "dev-0",
				Index:      0,
				Used:       2,
				Count:      2,
				Usedmem:    0,
				Totalmem:   0,
				Totalcore:  3,
				Usedcores:  0,
				Numa:       0,
				Type:       AMDDevice,
				Health:     true,
				CustomInfo: map[string]any{},
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           0,
				MemPercentagereq: 0,
				Coresreq:         2,
				Type:             AMDDevice,
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
				if len(result[AMDDevice]) != test.wantLen {
					t.Errorf("expected len: %d, got len %d", test.wantLen, len(result[AMDDevice]))
				}
				for idx, id := range test.wantDevIDs {
					if id != result[AMDDevice][idx].UUID {
						t.Errorf("expected device id: %s, got device id %s", id, result[AMDDevice][idx].UUID)
					}
				}
			}
			if reason != test.wantReason {
				t.Errorf("expected reason: %s, got reason: %s", test.wantReason, reason)
			}
		})
	}
}
