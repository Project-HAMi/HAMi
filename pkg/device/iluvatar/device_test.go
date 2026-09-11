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
	"errors"
	"flag"
	"fmt"
	"maps"
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/device"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

func TestGetNodeDevices(t *testing.T) {
	dev := IluvatarDevices{
		config: IluvatarConfig{
			CommonWord:         "MR-V100",
			ChipName:           "MR-V100",
			ResourceCountName:  "iluvatar.ai/MR-V100-vgpu",
			ResourceMemoryName: "iluvatar.ai/MR-V100.vMem",
			ResourceCoreName:   "iluvatar.ai/MR-V100.vCore",
		},
	}
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
						dev.nodeRegisterAnno: "GPU-bad51c5a-ed4c-591d-91bf-c04a12e19eae,10,8192,100,MR-V100,0,true:",
					},
				},
			},
			want: []*device.DeviceInfo{
				{
					ID:           "GPU-bad51c5a-ed4c-591d-91bf-c04a12e19eae",
					Count:        int32(10),
					Devcore:      int32(100),
					Devmem:       int32(8192),
					Type:         "MR-V100",
					Numa:         0,
					Health:       true,
					DeviceVendor: dev.config.CommonWord,
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
					assert.Equal(t, v.DeviceVendor, result[k].DeviceVendor)
				}
			}
		})
	}
}

func TestPatchAnnotations(t *testing.T) {
	dev := IluvatarDevices{
		config: IluvatarConfig{
			CommonWord:         "MR-V100",
			ChipName:           "MR-V100",
			ResourceCountName:  "iluvatar.ai/MR-V100-vgpu",
			ResourceMemoryName: "iluvatar.ai/MR-V100.vMem",
			ResourceCoreName:   "iluvatar.ai/MR-V100.vCore",
		},
	}
	tests := []struct {
		name       string
		annoInput  map[string]string
		podDevices device.PodDevices
		expected   map[string]string
	}{
		{
			name:       "No devices",
			annoInput:  map[string]string{},
			podDevices: device.PodDevices{},
			expected:   map[string]string{},
		},
		{
			name:      "With devices",
			annoInput: map[string]string{},
			podDevices: device.PodDevices{
				dev.config.CommonWord: device.PodSingleDevice{
					[]device.ContainerDevice{
						{
							Idx:  0,
							UUID: "k8s-gpu-iluvatar-0",
							Type: "MR-V100",
						},
					},
				},
			},
			expected: map[string]string{
				device.InRequestDevices[dev.config.CommonWord]: "k8s-gpu-iluvatar-0,MR-V100,0,0:;",
				device.SupportDevices[dev.config.CommonWord]:   "k8s-gpu-iluvatar-0,MR-V100,0,0:;",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			annoInputCopy := make(map[string]string)
			maps.Copy(annoInputCopy, tt.annoInput)
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
							"iluvatar.ai/MR-V100-vgpu":  *resource.NewQuantity(2, resource.DecimalSI),
							"iluvatar.ai/MR-V100.vCore": *resource.NewQuantity(1, resource.DecimalSI),
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
			dev := IluvatarDevices{
				config: IluvatarConfig{
					CommonWord:         "MR-V100",
					ChipName:           "MR-V100",
					ResourceCountName:  "iluvatar.ai/MR-V100-vgpu",
					ResourceMemoryName: "iluvatar.ai/MR-V100.vMem",
					ResourceCoreName:   "iluvatar.ai/MR-V100.vCore",
				},
			}
			result, _ := dev.MutateAdmission(test.args.ctr, test.args.p)
			assert.Equal(t, result, test.want)
		})
	}
}

func Test_checkType(t *testing.T) {
	dev := IluvatarDevices{
		config: IluvatarConfig{
			CommonWord: "MR-V100",
		},
	}
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
					Type: "MR-V100",
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
			name: "all resources set to limits and requests",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"iluvatar.ai/MR-V100-vgpu":  resource.MustParse("1"),
						"iluvatar.ai/MR-V100.vMem":  resource.MustParse("1000"),
						"iluvatar.ai/MR-V100.vCore": resource.MustParse("100"),
					},
					Requests: corev1.ResourceList{
						"iluvatar.ai/MR-V100-vgpu":  resource.MustParse("1"),
						"iluvatar.ai/MR-V100.vMem":  resource.MustParse("1000"),
						"iluvatar.ai/MR-V100.vCore": resource.MustParse("100"),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             "MR-V100",
				Memreq:           int32(256000),
				MemPercentagereq: int32(0),
				Coresreq:         int32(100),
			},
		},
		{
			name: "cores 50 accepted",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"iluvatar.ai/MR-V100-vgpu":  resource.MustParse("1"),
						"iluvatar.ai/MR-V100.vMem":  resource.MustParse("1000"),
						"iluvatar.ai/MR-V100.vCore": resource.MustParse("50"),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             "MR-V100",
				Memreq:           int32(256000),
				MemPercentagereq: int32(0),
				Coresreq:         int32(50),
			},
		},
		{
			name: "cores 0 accepted",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"iluvatar.ai/MR-V100-vgpu":  resource.MustParse("1"),
						"iluvatar.ai/MR-V100.vMem":  resource.MustParse("1000"),
						"iluvatar.ai/MR-V100.vCore": resource.MustParse("0"),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             "MR-V100",
				Memreq:           int32(256000),
				MemPercentagereq: int32(0),
				Coresreq:         int32(0),
			},
		},
		{
			name: "cores 101 rejected",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"iluvatar.ai/MR-V100-vgpu":  resource.MustParse("1"),
						"iluvatar.ai/MR-V100.vCore": resource.MustParse("101"),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "cores 150 rejected",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"iluvatar.ai/MR-V100-vgpu":  resource.MustParse("1"),
						"iluvatar.ai/MR-V100.vCore": resource.MustParse("150"),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "cores 200 rejected",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"iluvatar.ai/MR-V100-vgpu":  resource.MustParse("1"),
						"iluvatar.ai/MR-V100.vCore": resource.MustParse("200"),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "negative cores -1 rejected",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"iluvatar.ai/MR-V100-vgpu":  resource.MustParse("1"),
						"iluvatar.ai/MR-V100.vCore": resource.MustParse("-1"),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "all resources don't set to limits and requests",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits:   corev1.ResourceList{},
					Requests: corev1.ResourceList{},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "resourcemem don't set to limits and requests",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"iluvatar.ai/MR-V100-vgpu": resource.MustParse("1"),
					},
					Requests: corev1.ResourceList{
						"iluvatar.ai/MR-V100-vgpu": resource.MustParse("1"),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             "MR-V100",
				Memreq:           int32(0),
				MemPercentagereq: int32(100),
				Coresreq:         int32(0),
			},
		},
		{
			name: "memory overflowing int32 is rejected, not truncated to zero",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"iluvatar.ai/MR-V100-vgpu": resource.MustParse("1"),
						"iluvatar.ai/MR-V100.vMem": resource.MustParse("16Gi"),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "decimal-form memory request is rejected, not treated as zero",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"iluvatar.ai/MR-V100-vgpu": resource.MustParse("1"),
						"iluvatar.ai/MR-V100.vMem": resource.MustParse("16.0Gi"),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "device count zero is rejected",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"iluvatar.ai/MR-V100-vgpu": resource.MustParse("0"),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "negative device count is rejected",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"iluvatar.ai/MR-V100-vgpu": resource.MustParse("-1"),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "device count above int32 max is rejected",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"iluvatar.ai/MR-V100-vgpu": resource.MustParse("2147483648"),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := IluvatarDevices{
				config: IluvatarConfig{
					CommonWord:         "MR-V100",
					ChipName:           "MR-V100",
					ResourceCountName:  "iluvatar.ai/MR-V100-vgpu",
					ResourceMemoryName: "iluvatar.ai/MR-V100.vMem",
					ResourceCoreName:   "iluvatar.ai/MR-V100.vCore",
				},
			}
			fs := flag.FlagSet{}
			ParseConfig(&fs)
			result := dev.GenerateResourceRequests(test.args)
			assert.DeepEqual(t, result, test.want)
		})
	}
}

func TestFit_MutexRejectsUsedDevice(t *testing.T) {
	dev := IluvatarDevices{
		config: IluvatarConfig{
			CommonWord:         "MR-V100",
			ChipName:           "MR-V100",
			ResourceCountName:  "iluvatar.ai/MR-V100-vgpu",
			ResourceMemoryName: "iluvatar.ai/MR-V100.vMem",
			ResourceCoreName:   "iluvatar.ai/MR-V100.vCore",
		},
	}
	devices := []*device.DeviceUsage{
		{ID: "dev-0", Index: 0, Used: 1, Count: 100, Totalmem: 128, Totalcore: 100, Numa: 0, Type: "MR-V100", Health: true},
	}
	request := device.ContainerDeviceRequest{Nums: 1, Type: "MR-V100", Memreq: 64, Coresreq: 50}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Annotations: map[string]string{"hami.io/gpu-scheduler-policy": "mutex"},
	}}

	ok, _, reason := dev.Fit(devices, request, pod, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, ok, false)
	assert.Equal(t, reason, "1/1 ExclusiveDeviceAllocateConflict")
}

func Test_Fit(t *testing.T) {

	dev := IluvatarDevices{
		config: IluvatarConfig{
			CommonWord:         "MR-V100",
			ChipName:           "MR-V100",
			ResourceCountName:  "iluvatar.ai/MR-V100-vgpu",
			ResourceMemoryName: "iluvatar.ai/MR-V100.vMem",
			ResourceCoreName:   "iluvatar.ai/MR-V100.vCore",
		},
	}
	tests := []struct {
		name       string
		devices    []*device.DeviceUsage
		request    device.ContainerDeviceRequest
		annos      map[string]string
		wantOK     bool
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
					Type:      "MR-V100",
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
					Type:      "MR-V100",
					Health:    true,
				},
			},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Type:             "MR-V100",
				Memreq:           64,
				MemPercentagereq: 0,
				Coresreq:         50,
			},
			annos:      map[string]string{},
			wantOK:     true,
			wantLen:    1,
			wantDevIDs: []string{"dev-1"},
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
				Type:      "MR-V100",
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Type:             "MR-V100",
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
			},
			annos:      map[string]string{},
			wantOK:     false,
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
				Totalmem:  128,
				Totalcore: 100,
				Usedcores: 100,
				Numa:      0,
				Type:      "MR-V100",
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Type:             "MR-V100",
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
			},
			annos:      map[string]string{},
			wantOK:     false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardInsufficientMemory",
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
				Type:      "MR-V100",
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
			wantOK:     false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardTypeMismatch",
		},
		{
			name: "mutex policy rejects used device",
			devices: []*device.DeviceUsage{
				{
					ID:        "dev-0",
					Index:     0,
					Used:      1,
					Count:     2,
					Usedmem:   0,
					Totalmem:  128,
					Totalcore: 100,
					Usedcores: 0,
					Numa:      0,
					Type:      "MR-V100",
					Health:    true,
				},
			},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Type:             "MR-V100",
				Memreq:           64,
				MemPercentagereq: 0,
				Coresreq:         50,
			},
			annos:      map[string]string{"hami.io/gpu-scheduler-policy": "mutex"},
			wantOK:     false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 ExclusiveDeviceAllocateConflict",
		},
		{
			name: "fit fail: CardNotHealth",
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
				Type:      "MR-V100",
				Health:    false,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Type:             "MR-V100",
				Memreq:           64,
				MemPercentagereq: 0,
				Coresreq:         50,
			},
			annos:      map[string]string{},
			wantOK:     false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardNotHealth",
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
			ok, result, reason := dev.Fit(test.devices, test.request, pod, &device.NodeInfo{}, allocated)
			if test.wantOK {
				if len(result["MR-V100"]) != test.wantLen {
					t.Errorf("expected %d, got %d", test.wantLen, len(result["MR-V100"]))
				}
				for idx, id := range test.wantDevIDs {
					if id != result["MR-V100"][idx].UUID {
						t.Errorf("expected %s, got %s", id, result["MR-V100"][idx].UUID)
					}
				}
				if !ok {
					t.Errorf("expected ok true, got false")
				}
			} else {
				if ok {
					t.Errorf("expected ok false, got true")
				}
				if len(result["MR-V100"]) != test.wantLen {
					t.Errorf("expected %d, got %d", test.wantLen, len(result["MR-V100"]))
				}
			}
			if reason != test.wantReason {
				t.Errorf("expected reason: %s, got reason: %s", test.wantReason, reason)
			}
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
			dev := &IluvatarDevices{
				config: IluvatarConfig{
					CommonWord:         "MR-V100",
					ChipName:           "MR-V100",
					ResourceCountName:  "iluvatar.ai/MR-V100-vgpu",
					ResourceMemoryName: "iluvatar.ai/MR-V100.vMem",
					ResourceCoreName:   "iluvatar.ai/MR-V100.vCore",
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
			dev := &IluvatarDevices{
				config: IluvatarConfig{
					CommonWord:         "MR-V100",
					ChipName:           "MR-V100",
					ResourceCountName:  "iluvatar.ai/MR-V100-vgpu",
					ResourceMemoryName: "iluvatar.ai/MR-V100.vMem",
					ResourceCoreName:   "iluvatar.ai/MR-V100.vCore",
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

func TestFit_CoresValidation(t *testing.T) {
	dev := &IluvatarDevices{
		config: IluvatarConfig{
			CommonWord: "MR-V100",
		},
	}
	devices := []*device.DeviceUsage{
		{
			ID:        "dev-0",
			Index:     0,
			Count:     10,
			Totalmem:  8000,
			Totalcore: 100,
			Used:      0,
			Usedmem:   0,
			Usedcores: 0,
			Health:    true,
			Type:      "MR-V100",
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	tests := []struct {
		name     string
		coresreq int32
		wantOk   bool
	}{
		{
			name:     "cores 0 is accepted",
			coresreq: 0,
			wantOk:   true,
		},
		{
			name:     "cores 50 is accepted",
			coresreq: 50,
			wantOk:   true,
		},
		{
			name:     "cores 100 is accepted",
			coresreq: 100,
			wantOk:   true,
		},
		{
			name:     "cores 101 is rejected",
			coresreq: 101,
			wantOk:   false,
		},
		{
			name:     "cores 150 is rejected and not converted to 100",
			coresreq: 150,
			wantOk:   false,
		},
		{
			name:     "cores 200 is rejected",
			coresreq: 200,
			wantOk:   false,
		},
		{
			name:     "negative cores -1 is rejected",
			coresreq: -1,
			wantOk:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := device.ContainerDeviceRequest{
				Nums:     1,
				Type:     "MR-V100",
				Memreq:   1000,
				Coresreq: tt.coresreq,
			}
			ok, _, _ := dev.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
			assert.Equal(t, ok, tt.wantOk)
		})
	}

	t.Run("empty devices with invalid coresreq returns out of range", func(t *testing.T) {
		req := device.ContainerDeviceRequest{
			Nums:     1,
			Type:     "MR-V100",
			Memreq:   1000,
			Coresreq: 150,
		}
		ok, _, reason := dev.Fit([]*device.DeviceUsage{}, req, pod, &device.NodeInfo{}, &device.PodDevices{})
		assert.Equal(t, ok, false)
		assert.Equal(t, reason, "core limit out of range")
	})

	t.Run("mismatched device type with invalid coresreq returns out of range", func(t *testing.T) {
		req := device.ContainerDeviceRequest{
			Nums:     1,
			Type:     "MR-V100",
			Memreq:   1000,
			Coresreq: -1,
		}
		mismatchDevs := []*device.DeviceUsage{
			{ID: "other-0", Type: "OtherType", Health: true},
		}
		ok, _, reason := dev.Fit(mismatchDevs, req, pod, &device.NodeInfo{}, &device.PodDevices{})
		assert.Equal(t, ok, false)
		assert.Equal(t, reason, "core limit out of range")
	})
}

func iluvatarTestDevice() IluvatarDevices {
	return IluvatarDevices{
		config: IluvatarConfig{
			CommonWord:         "MR-V100",
			ChipName:           "MR-V100",
			ResourceCountName:  "iluvatar.ai/MR-V100-vgpu",
			ResourceMemoryName: "iluvatar.ai/MR-V100.vMem",
			ResourceCoreName:   "iluvatar.ai/MR-V100.vCore",
		},
	}
}

func iluvatarContainer(count int64, cores *int64) *corev1.Container {
	limits := corev1.ResourceList{
		"iluvatar.ai/MR-V100-vgpu": *resource.NewQuantity(count, resource.DecimalSI),
	}
	if cores != nil {
		limits["iluvatar.ai/MR-V100.vCore"] = *resource.NewQuantity(*cores, resource.DecimalSI)
	}
	return &corev1.Container{Name: "demo", Resources: corev1.ResourceRequirements{Limits: limits}}
}

// Test_GenerateResourceRequests_MutatedMultiCard walks the admission path.
// MutateAdmission rewrites the core limit to count*100 for a multi card
// request, and GenerateResourceRequests has to read that total back as a per
// card percentage. It previously compared the total against the 0-100 range,
// so every multi card request returned an empty request and the pod was
// scheduled with no device.
func Test_GenerateResourceRequests_MutatedMultiCard(t *testing.T) {
	dev := iluvatarTestDevice()
	for _, count := range []int64{1, 2, 4, 8} {
		ctr := iluvatarContainer(count, nil)
		if _, err := dev.MutateAdmission(ctr, &corev1.Pod{}); err != nil {
			t.Fatalf("MutateAdmission(count=%d): %v", count, err)
		}
		got := dev.GenerateResourceRequests(ctr)
		if got.Nums != int32(count) {
			t.Errorf("count=%d: Nums = %d, want %d", count, got.Nums, count)
		}
		if count > 1 && got.Coresreq != 100 {
			t.Errorf("count=%d: Coresreq = %d, want 100 per card", count, got.Coresreq)
		}
	}
}

// Test_GenerateResourceRequests_CoreLimitScales pins which values are treated
// as a total and which as a per card percentage. The admission webhook is
// optional: it can be disabled, it defaults to failurePolicy Ignore, and a
// namespace or pod can carry hami.io/webhook: ignore. On those paths the limit
// was never scaled, so a per card value must reach the scheduler untouched.
func Test_GenerateResourceRequests_CoreLimitScales(t *testing.T) {
	dev := iluvatarTestDevice()
	tests := []struct {
		name     string
		count    int64
		cores    int64
		wantCore int32
		wantNums int32
	}{
		{name: "scaled by the webhook", count: 2, cores: 200, wantCore: 100, wantNums: 2},
		{name: "scaled by the webhook, eight cards", count: 8, cores: 800, wantCore: 100, wantNums: 8},
		{name: "unscaled per card value is left alone", count: 2, cores: 60, wantCore: 60, wantNums: 2},
		{name: "unscaled small value does not truncate to zero", count: 4, cores: 3, wantCore: 3, wantNums: 4},
		{name: "unscaled full share", count: 8, cores: 100, wantCore: 100, wantNums: 8},
		{name: "single card is unchanged", count: 1, cores: 50, wantCore: 50, wantNums: 1},
		{name: "a total that does not divide evenly is rejected", count: 4, cores: 150, wantCore: 0, wantNums: 0},
		{name: "a per card value above 100 is still rejected", count: 1, cores: 200, wantCore: 0, wantNums: 0},
		{name: "negative is rejected", count: 2, cores: -1, wantCore: 0, wantNums: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := dev.GenerateResourceRequests(iluvatarContainer(test.count, &test.cores))
			if got.Nums != test.wantNums {
				t.Errorf("Nums = %d, want %d", got.Nums, test.wantNums)
			}
			if got.Coresreq != test.wantCore {
				t.Errorf("Coresreq = %d, want %d", got.Coresreq, test.wantCore)
			}
		})
	}
}
