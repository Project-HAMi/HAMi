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
			ok, result, _ := dev.Fit(test.devices, test.request, pod, &device.NodeInfo{}, allocated)
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
