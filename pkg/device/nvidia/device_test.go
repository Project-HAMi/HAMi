/*/*
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

package nvidia

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"
)

func Test_DefaultResourceNum(t *testing.T) {
	v := *resource.NewQuantity(1, resource.BinarySI)
	vv, ok := v.AsInt64()
	assert.Equal(t, ok, true)
	assert.Equal(t, vv, int64(1))
}

func Test_MutateAdmission(t *testing.T) {
	tests := []struct {
		name string
		args *corev1.Container
		want bool
	}{
		{
			name: "having ResourceName set to resource limits.",
			args: &corev1.Container{
				Name: "test",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu": *resource.NewQuantity(1, resource.BinarySI),
					},
				},
			},
			want: true,
		},
		{
			name: "don't having ResourceName, but having ResourceCores set to resource limits",
			args: &corev1.Container{
				Name: "test",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpucores": *resource.NewQuantity(1, resource.BinarySI),
					},
				},
			},
			want: true,
		},
		{
			name: "don't having ResourceName, but having ResourceMem set to resource limits",
			args: &corev1.Container{
				Name: "test",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpumem": *resource.NewQuantity(1, resource.BinarySI),
					},
				},
			},
			want: true,
		},
		{
			name: "don't having ResourceName, but having ResourceMemPercentage set to resource limits",
			args: &corev1.Container{
				Name: "test",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpumem-percentage": *resource.NewQuantity(1, resource.BinarySI),
					},
				},
			},
			want: true,
		},
		{
			name: "don't having math resources.",
			args: &corev1.Container{
				Name: "test",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{},
				},
			},
			want: false,
		},
	}

	gpuDevices := &NvidiaGPUDevices{
		config: NvidiaConfig{
			ResourceCountName:            "nvidia.com/gpu",
			ResourceMemoryName:           "nvidia.com/gpumem",
			ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
			ResourceCoreName:             "nvidia.com/gpucores",
			DefaultGPUNum:                int32(1),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, _ := gpuDevices.MutateAdmission(test.args, &corev1.Pod{})
			if test.want != got {
				t.Fatalf("exec MutateAdmission method expect return is %+v, but got is %+v", test.want, got)
			}
		})
	}
}

func TestMutateAdmissionDefaultsExclusiveCore(t *testing.T) {
	ptr := func(v int64) *int64 { return &v }
	clone := func(in corev1.ResourceList) corev1.ResourceList {
		if in == nil {
			return nil
		}
		out := corev1.ResourceList{}
		for k, v := range in {
			out[k] = v.DeepCopy()
		}
		return out
	}

	defaultConfig := NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
		ResourceCoreName:             "nvidia.com/gpucores",
		DefaultGPUNum:                1,
	}

	tests := []struct {
		name             string
		config           NvidiaConfig
		limits           corev1.ResourceList
		requests         corev1.ResourceList
		wantCore         bool
		expectCore       int64
		requestCoreValue *int64
	}{
		{
			name:   "exclusive via percentage",
			config: defaultConfig,
			limits: corev1.ResourceList{
				"nvidia.com/gpu":               resource.MustParse("1"),
				"nvidia.com/gpumem-percentage": resource.MustParse("100"),
			},
			wantCore:   true,
			expectCore: 100,
		},
		{
			name:   "exclusive via percentage in requests only",
			config: defaultConfig,
			requests: corev1.ResourceList{
				"nvidia.com/gpu":               resource.MustParse("1"),
				"nvidia.com/gpumem-percentage": resource.MustParse("100"),
			},
			wantCore:   true,
			expectCore: 100,
		},
		{
			name:   "non-exclusive percentage",
			config: defaultConfig,
			limits: corev1.ResourceList{
				"nvidia.com/gpu":               resource.MustParse("1"),
				"nvidia.com/gpumem-percentage": resource.MustParse("50"),
			},
			wantCore: false,
		},
		{
			name:   "no memory fields defaults to exclusive",
			config: defaultConfig,
			limits: corev1.ResourceList{
				"nvidia.com/gpu": resource.MustParse("1"),
			},
			wantCore:   true,
			expectCore: 100,
		},
		{
			name:   "explicit cores remains unchanged",
			config: defaultConfig,
			limits: corev1.ResourceList{
				"nvidia.com/gpu":      resource.MustParse("1"),
				"nvidia.com/gpucores": resource.MustParse("70"),
			},
			wantCore:   true,
			expectCore: 70,
		},
		{
			name:   "explicit cores in requests remains unchanged",
			config: defaultConfig,
			requests: corev1.ResourceList{
				"nvidia.com/gpu":      resource.MustParse("1"),
				"nvidia.com/gpucores": resource.MustParse("55"),
			},
			requestCoreValue: ptr(55),
		},
		{
			name:   "memory size present treated as shareable",
			config: defaultConfig,
			limits: corev1.ResourceList{
				"nvidia.com/gpu":    resource.MustParse("1"),
				"nvidia.com/gpumem": resource.MustParse("8192"),
			},
			wantCore: false,
		},
		{
			name: "memory name empty treated as exclusive",
			config: NvidiaConfig{
				ResourceCountName:            "nvidia.com/gpu",
				ResourceMemoryName:           "",
				ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
				ResourceCoreName:             "nvidia.com/gpucores",
				DefaultGPUNum:                1,
			},
			limits: corev1.ResourceList{
				"nvidia.com/gpu": resource.MustParse("1"),
			},
			wantCore:   true,
			expectCore: 100,
		},
		{
			name: "custom resource names",
			config: NvidiaConfig{
				ResourceCountName:            "hami.io/gpu",
				ResourceMemoryName:           "hami.io/gpumem",
				ResourceMemoryPercentageName: "hami.io/gpumem-percentage",
				ResourceCoreName:             "hami.io/gpucores",
				DefaultGPUNum:                1,
			},
			limits: corev1.ResourceList{
				"hami.io/gpu":               resource.MustParse("1"),
				"hami.io/gpumem-percentage": resource.MustParse("100"),
			},
			wantCore:   true,
			expectCore: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctr := &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits:   clone(tt.limits),
					Requests: clone(tt.requests),
				},
			}
			dev := &NvidiaGPUDevices{config: tt.config}
			dev.MutateAdmission(ctr, &corev1.Pod{})

			coreName := corev1.ResourceName(tt.config.ResourceCoreName)
			qty, exists := ctr.Resources.Limits[coreName]
			if tt.wantCore != exists {
				t.Fatalf("expected core presence %v, got %v", tt.wantCore, exists)
			}
			if tt.wantCore && qty.Value() != tt.expectCore {
				t.Fatalf("expected core value %d, got %d", tt.expectCore, qty.Value())
			}

			if tt.requestCoreValue != nil {
				reqQty, reqExists := ctr.Resources.Requests[coreName]
				if !reqExists {
					t.Fatalf("expected core request presence true, got false")
				}
				if reqQty.Value() != *tt.requestCoreValue {
					t.Fatalf("expected core request value %d, got %d", *tt.requestCoreValue, reqQty.Value())
				}
			}
		})
	}
}

func Test_checkType(t *testing.T) {
	gpuDevices := &NvidiaGPUDevices{
		config: NvidiaConfig{
			ResourceCountName:            "nvidia.com/gpu",
			ResourceMemoryName:           "nvidia.com/gpumem",
			ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
			ResourceCoreName:             "nvidia.com/gpucores",
			DefaultGPUNum:                int32(1),
		},
	}
	tests := []struct {
		name string
		args struct {
			annos map[string]string
			d     device.DeviceUsage
		}
		want bool
	}{
		{
			name: "use set GPUInUse don't set GPUNoUse annotation,device match",
			args: struct {
				annos map[string]string
				d     device.DeviceUsage
			}{
				annos: map[string]string{
					GPUInUse: "A10",
				},
				d: device.DeviceUsage{
					Type: "NVIDIA A100",
				},
			},
			want: true,
		},
		{
			name: "use set GPUInUse set GPUNoUse annotation,device don't match",
			args: struct {
				annos map[string]string
				d     device.DeviceUsage
			}{
				annos: map[string]string{
					GPUInUse: "A10",
					GPUNoUse: "A100",
				},
				d: device.DeviceUsage{
					Type: "NVIDIA A100",
				},
			},
			want: false,
		},
		{
			name: "use set GPUInUse set GPUNoUse annotation,device match",
			args: struct {
				annos map[string]string
				d     device.DeviceUsage
			}{
				annos: map[string]string{
					GPUInUse: "A10",
					GPUNoUse: "A100",
				},
				d: device.DeviceUsage{
					Type: "NVIDIA A10",
				},
			},
			want: true,
		},
	}
	req := device.ContainerDeviceRequest{
		Type: NvidiaGPUDevice,
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, _ := gpuDevices.checkType(test.args.annos, test.args.d, req)
			assert.Equal(t, test.want, got)
		})
	}
}

func Test_FilterDeviceToRegister(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			uuid string
			idx  string
			*FilterDevice
		}
		want bool
	}{
		{
			name: "filter is nil",
			args: struct {
				uuid string
				idx  string
				*FilterDevice
			}{
				uuid:         "GPU-8dcd427f-483b-b48f-d7e5-75fb19a52b76",
				idx:          "0",
				FilterDevice: nil,
			},
			want: false,
		},
		{
			name: "uuid is empty",
			args: struct {
				uuid string
				idx  string
				*FilterDevice
			}{
				uuid: "",
				idx:  "0",
				FilterDevice: &FilterDevice{
					UUID: []string{"GPU-8dcd427f-483b-b48f-d7e5-75fb19a52b76"},
				},
			},
			want: false,
		},
		{
			name: "uuid is not in filter",
			args: struct {
				uuid string
				idx  string
				*FilterDevice
			}{
				uuid: "GPU-8dcd427f-483b-b48f-d7e5-75fb19a52b76",
				idx:  "0",
				FilterDevice: &FilterDevice{
					UUID: []string{"GPU-8dcd427f-483b-b48f-d7e5-75fb19a52b77"},
				},
			},
			want: false,
		},
		{
			name: "uuid is in filter",
			args: struct {
				uuid string
				idx  string
				*FilterDevice
			}{
				uuid: "GPU-8dcd427f-483b-b48f-d7e5-75fb19a52b76",
				idx:  "0",
				FilterDevice: &FilterDevice{
					UUID: []string{"GPU-8dcd427f-483b-b48f-d7e5-75fb19a52b76"},
				},
			},
			want: true,
		},
		{
			name: "idx is empty",
			args: struct {
				uuid string
				idx  string
				*FilterDevice
			}{
				uuid: "GPU-8dcd427f-483b-b48f-d7e5-75fb19a52b76",
				idx:  "",
				FilterDevice: &FilterDevice{
					Index: []uint{0},
				},
			},
			want: false,
		},
		{
			name: "idx is not in filter",
			args: struct {
				uuid string
				idx  string
				*FilterDevice
			}{
				uuid: "GPU-8dcd427f-483b-b48f-d7e5-75fb19a52b76",
				idx:  "0",
				FilterDevice: &FilterDevice{
					Index: []uint{1},
				},
			},
			want: false,
		},
		{
			name: "idx is in filter",
			args: struct {
				uuid string
				idx  string
				*FilterDevice
			}{
				uuid: "GPU-8dcd427f-483b-b48f-d7e5-75fb19a52b76",
				idx:  "0",
				FilterDevice: &FilterDevice{
					Index: []uint{0},
				},
			},
			want: true,
		},
		{
			name: "idx is invalid",
			args: struct {
				uuid string
				idx  string
				*FilterDevice
			}{
				uuid: "GPU-8dcd427f-483b-b48f-d7e5-75fb19a52b76",
				idx:  "a",
				FilterDevice: &FilterDevice{
					Index: []uint{0},
				},
			},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			DevicePluginFilterDevice = test.args.FilterDevice
			got := FilterDeviceToRegister(test.args.uuid, test.args.idx)
			assert.DeepEqual(t, test.want, got)
		})
	}
}

func Test_InitNvidiaDevice(t *testing.T) {
	tests := []struct {
		name string
		args NvidiaConfig
		want *NvidiaGPUDevices
	}{
		{
			name: "test with valid configuration",
			args: NvidiaConfig{
				ResourceCountName:  "nvidia.com/gpu",
				ResourceMemoryName: "nvidia.com/gpumem",
				DefaultGPUNum:      int32(1),
			},
			want: &NvidiaGPUDevices{
				config: NvidiaConfig{
					ResourceCountName:  "nvidia.com/gpu",
					ResourceMemoryName: "nvidia.com/gpumem",
					DefaultGPUNum:      int32(1),
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			devices := InitNvidiaDevice(test.args)
			assert.DeepEqual(t, test.want.config, devices.config)
			assert.Equal(t, "hami.io/vgpu-devices-to-allocate", device.InRequestDevices[NvidiaGPUDevice], "Expected InRequestDevices to be set")
			assert.Equal(t, "hami.io/vgpu-devices-allocated", device.SupportDevices[NvidiaGPUDevice], "Expected SupportDevices to be set")
			assert.Equal(t, HandshakeAnnos, util.HandshakeAnnos[NvidiaGPUDevice], "Expected HandshakeAnnos to be set")
		})
	}
}

func Test_PatchAnnotations(t *testing.T) {
	InitNvidiaDevice(NvidiaConfig{})

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
					NvidiaGPUDevice: device.PodSingleDevice{
						[]device.ContainerDevice{
							{
								Idx:       0,
								UUID:      "nvidia-device-0",
								Type:      "NVIDIA",
								Usedmem:   2000,
								Usedcores: 1,
							},
						},
					},
				},
			},
			want: map[string]string{
				device.InRequestDevices[NvidiaGPUDevice]: "nvidia-device-0,NVIDIA,2000,1:;",
				device.SupportDevices[NvidiaGPUDevice]:   "nvidia-device-0,NVIDIA,2000,1:;",
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
			gpuDevices := &NvidiaGPUDevices{}
			result := gpuDevices.PatchAnnotations(&corev1.Pod{}, &test.args.annoinput, test.args.pd)

			assert.Equal(t, len(test.want), len(result), "Expected length of result to match want")
			for k, v := range test.want {
				assert.Equal(t, v, result[k], "pod add annotation key [%s], values is [%s]", k, result[k])
			}
		})
	}

}

func Test_GetNodeDevices(t *testing.T) {
	tests := []struct {
		name string
		args corev1.Node
		want []*device.DeviceInfo
		err  error
	}{
		{
			name: "exist gpu devices",
			args: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node-01",
					Annotations: map[string]string{
						RegisterAnnos: `[{"id":"GPU-0","count":5,"devmem":8192,"devcore":100,"type":"NVIDIA-Tesla P4","numa":0,"health":true,"index":0}]`,
					},
				},
			},
			want: []*device.DeviceInfo{
				{
					ID:           "GPU-0",
					Count:        5,
					Devmem:       8192,
					Devcore:      100,
					Type:         "NVIDIA-Tesla P4",
					Numa:         0,
					Health:       true,
					Index:        0,
					DeviceVendor: NvidiaGPUDevice,
				},
			},
			err: nil,
		},
		{
			name: "no gpu devices",
			args: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node-02",
					Annotations: map[string]string{
						RegisterAnnos: "[]",
					},
				},
			},
			want: []*device.DeviceInfo{},
			err:  errors.New("no gpu found on node"),
		},
		{
			name: "no annotation",
			args: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "node-03",
					Annotations: map[string]string{},
				},
			},
			want: []*device.DeviceInfo{},
			err:  errors.New("annos not found " + RegisterAnnos),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gpuDevices := &NvidiaGPUDevices{}
			result, err := gpuDevices.GetNodeDevices(test.args)
			if (err != nil) != (test.err != nil) {
				t.Errorf("GetNodeDevices error = %v, want %v", err, test.err)
				return
			}
			if err != nil && test.err != nil {
				if err.Error() != test.err.Error() {
					t.Errorf("GetNodeDevices error message = %v, want %v", err.Error(), test.err.Error())
					return
				}
			}
			if len(result) != len(test.want) {
				t.Errorf("GetNodeDevices got %d devices, want %d", len(result), len(test.want))
				return
			}
			if err == nil && len(result) != 0 {
				for k, v := range test.want {
					assert.Equal(t, v.Index, result[k].Index)
					assert.Equal(t, v.ID, result[k].ID)
					assert.Equal(t, v.Devcore, result[k].Devcore)
					assert.Equal(t, v.Health, result[k].Health)
					assert.Equal(t, v.Numa, result[k].Numa)
					assert.Equal(t, v.Type, result[k].Type)
					assert.Equal(t, v.Count, result[k].Count)
					assert.Equal(t, v.DeviceVendor, result[k].DeviceVendor)
				}
			}
		})
	}
}

func TestDevices_Fit(t *testing.T) {
	config := NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpumem",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
	}
	dev := InitNvidiaDevice(config)

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
					Type:      NvidiaGPUDevice,
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
					Type:      NvidiaGPUDevice,
					Health:    true,
				},
			},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           64,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             NvidiaGPUDevice,
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
				Type:      NvidiaGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             NvidiaGPUDevice,
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
				Type:      NvidiaGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             NvidiaGPUDevice,
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
				Type:      NvidiaGPUDevice,
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
				Type:      NvidiaGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             2,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             NvidiaGPUDevice,
			},
			annos:      map[string]string{GPUUseUUID: "dev-0"},
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
				Type:      NvidiaGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             2,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             NvidiaGPUDevice,
			},
			annos:      map[string]string{GPUNoUseUUID: "dev-0"},
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
				Type:      NvidiaGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             NvidiaGPUDevice,
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
				Type:      NvidiaGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         120,
				Type:             NvidiaGPUDevice,
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
				Type:      NvidiaGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         100,
				Type:             NvidiaGPUDevice,
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
				Type:      NvidiaGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         0,
				Type:             NvidiaGPUDevice,
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
				Type:      NvidiaGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             2,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         20,
				Type:             NvidiaGPUDevice,
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
				Type:      NvidiaGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           0,
				MemPercentagereq: 10,
				Coresreq:         20,
				Type:             NvidiaGPUDevice,
			},
			annos:      map[string]string{},
			wantFit:    true,
			wantLen:    1,
			wantDevIDs: []string{"dev-0"},
			wantReason: "",
		},
		{
			name: "fit fail:  CardNotHealth",
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
				Type:      NvidiaGPUDevice,
				Health:    false,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           0,
				MemPercentagereq: 10,
				Coresreq:         20,
				Type:             NvidiaGPUDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
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
			fit, result, reason := dev.Fit(test.devices, test.request, pod, &device.NodeInfo{}, allocated)
			if fit != test.wantFit {
				t.Errorf("Fit: got %v, want %v", fit, test.wantFit)
			}
			if test.wantFit {
				if len(result[NvidiaGPUDevice]) != test.wantLen {
					t.Errorf("expected len: %d, got len %d", test.wantLen, len(result[NvidiaGPUDevice]))
				}
				for idx, id := range test.wantDevIDs {
					if id != result[NvidiaGPUDevice][idx].UUID {
						t.Errorf("expected device id: %s, got device id %s", id, result[NvidiaGPUDevice][idx].UUID)
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
		checkMig    bool
		wantMigIdx  int32
		wantUUID    string
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
			wantErr:  false,
			checkMig: false,
		},
		{
			name: "test MIG mode with migNeedsReset true - first template matches",
			deviceUsage: &device.DeviceUsage{
				ID:        "dev-0",
				Used:      0,
				Usedcores: 0,
				Usedmem:   0,
				Mode:      MigMode,
				MigTemplate: []device.Geometry{
					{
						{Name: "1g.5gb", Core: 25, Memory: 5120, Count: 1},
					},
					{
						{Name: "1g.5gb", Core: 25, Memory: 5120, Count: 1},
						{Name: "2g.10gb", Core: 50, Memory: 10240, Count: 1},
					},
				},
				MigUsage: device.MigInUse{
					UsageList: make(device.MIGS, 0),
				},
			},
			ctr: &device.ContainerDevice{
				UUID:    "dev-0",
				Usedmem: 6000,
			},
			wantUsage: &device.DeviceUsage{
				Used:      1,
				Usedcores: 50,
				Usedmem:   10240,
			},
			wantErr:    false,
			checkMig:   true,
			wantMigIdx: 1,
			wantUUID:   "dev-0[1-1]",
		},
		{
			name: "test MIG mode with migNeedsReset true - second template matches with correct idx",
			deviceUsage: &device.DeviceUsage{
				ID:        "dev-1",
				Used:      0,
				Usedcores: 0,
				Usedmem:   0,
				Mode:      MigMode,
				MigTemplate: []device.Geometry{
					{
						{Name: "1g.3gb", Core: 25, Memory: 3072, Count: 1},
					},
					{
						{Name: "1g.5gb", Core: 25, Memory: 5120, Count: 1},
						{Name: "2g.10gb", Core: 50, Memory: 10240, Count: 1},
					},
				},
				MigUsage: device.MigInUse{
					UsageList: make(device.MIGS, 0),
				},
			},
			ctr: &device.ContainerDevice{
				UUID:    "dev-1",
				Usedmem: 8000,
			},
			wantUsage: &device.DeviceUsage{
				Used:      1,
				Usedcores: 50,
				Usedmem:   10240,
			},
			wantErr:    false,
			checkMig:   true,
			wantMigIdx: 1,
			wantUUID:   "dev-1[1-1]",
		},
		{
			name: "test MIG mode with migNeedsReset true - verify outer loop break",
			deviceUsage: &device.DeviceUsage{
				ID:        "dev-2",
				Used:      0,
				Usedcores: 0,
				Usedmem:   0,
				Mode:      MigMode,
				MigTemplate: []device.Geometry{
					{
						{Name: "1g.5gb", Core: 25, Memory: 5120, Count: 1},
						{Name: "2g.10gb", Core: 50, Memory: 10240, Count: 1},
					},
					{
						{Name: "3g.20gb", Core: 100, Memory: 20480, Count: 1},
					},
				},
				MigUsage: device.MigInUse{
					UsageList: make(device.MIGS, 0),
				},
			},
			ctr: &device.ContainerDevice{
				UUID:    "dev-2",
				Usedmem: 6000,
			},
			wantUsage: &device.DeviceUsage{
				Used:      1,
				Usedcores: 50,
				Usedmem:   10240,
			},
			wantErr:    false,
			checkMig:   true,
			wantMigIdx: 0,
			wantUUID:   "dev-2[0-1]",
		},
		{
			name: "test MIG mode with migNeedsReset true - template with Count > 1",
			deviceUsage: &device.DeviceUsage{
				ID:        "dev-3",
				Used:      0,
				Usedcores: 0,
				Usedmem:   0,
				Mode:      MigMode,
				MigTemplate: []device.Geometry{
					{
						// Template index 0: first template has Count=2, second template has Count=1
						{Name: "1g.5gb", Core: 50, Memory: 5120, Count: 2},
						{Name: "2g.10gb", Core: 100, Memory: 10240, Count: 1},
					},
				},
				MigUsage: device.MigInUse{
					UsageList: make(device.MIGS, 0),
				},
			},
			ctr: &device.ContainerDevice{
				UUID:    "dev-3",
				Usedmem: 8000, // Requires 8GB, matches second template (idx=1) which should be at UsageList[2]
			},
			wantUsage: &device.DeviceUsage{
				Used:      1,
				Usedcores: 100,
				Usedmem:   10240, // Should be set to the matched template's memory
			},
			wantErr:    false,
			checkMig:   true,
			wantMigIdx: 0,
			wantUUID:   "dev-3[0-1]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dev := &NvidiaGPUDevices{}
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
				if tt.checkMig {
					// Verify MIG-related fields
					if tt.deviceUsage.MigUsage.Index != tt.wantMigIdx {
						t.Errorf("expected MigUsage.Index: %d, got %d", tt.wantMigIdx, tt.deviceUsage.MigUsage.Index)
					}
					if tt.ctr.UUID != tt.wantUUID {
						t.Errorf("expected UUID: %s, got %s", tt.wantUUID, tt.ctr.UUID)
					}
					// Verify that the entry at the corresponding index in UsageList is marked as InUse
					// According to the modified code, should calculate usageListIdx by summing Count of all templates before idx
					expectedUsageListIdx := -1
					if strings.Contains(tt.wantUUID, "[") {
						parts := strings.Split(strings.TrimSuffix(strings.Split(tt.wantUUID, "[")[1], "]"), "-")
						if len(parts) == 2 {
							if tidx, err1 := strconv.Atoi(parts[0]); err1 == nil {
								if idx, err2 := strconv.Atoi(parts[1]); err2 == nil {
									// Calculate usageListIdx by summing Count of all templates before idx
									if tidx >= 0 && tidx < len(tt.deviceUsage.MigTemplate) {
										expectedUsageListIdx = 0
										for i := 0; i < idx && i < len(tt.deviceUsage.MigTemplate[tidx]); i++ {
											expectedUsageListIdx += int(tt.deviceUsage.MigTemplate[tidx][i].Count)
										}
									}
								}
							}
						}
					}
					if expectedUsageListIdx >= 0 && expectedUsageListIdx < len(tt.deviceUsage.MigUsage.UsageList) {
						if !tt.deviceUsage.MigUsage.UsageList[expectedUsageListIdx].InUse {
							t.Errorf("expected UsageList[%d].InUse to be true, got false", expectedUsageListIdx)
						}
						if tt.deviceUsage.MigUsage.UsageList[expectedUsageListIdx].Memory != tt.ctr.Usedmem {
							t.Errorf("expected UsageList[%d].Memory: %d, got %d", expectedUsageListIdx, tt.ctr.Usedmem, tt.deviceUsage.MigUsage.UsageList[expectedUsageListIdx].Memory)
						}
					}
				}
			}
		})
	}
}

func TestFitQuota(t *testing.T) {
	NvidiaGPUDevice := "NVIDIA"
	config := NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
		MemoryFactor:                 1,
	}
	dev := InitNvidiaDevice(config)
	device.DevicesMap = make(map[string]device.Devices)
	device.DevicesMap[NvidiaGPUDevice] = dev

	qm := device.NewQuotaManager()
	qm.AddQuota(&corev1.ResourceQuota{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ResourceQuota",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "basic-quota",
			Namespace: "default",
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceName("limits.nvidia.com/gpumem"): resource.MustParse("2048"),
			},
		},
	})

	tests := []struct {
		name           string
		tmpDevs        map[string]device.ContainerDevices
		allocated      *device.PodDevices
		ns             string
		memreq         int64
		coresreq       int64
		expectedResult bool
	}{
		{
			name:           "no tmp and no allocated",
			tmpDevs:        map[string]device.ContainerDevices{},
			allocated:      nil,
			ns:             "default",
			memreq:         100,
			coresreq:       1,
			expectedResult: true,
		},
		{
			name:           "request exceed quota",
			tmpDevs:        map[string]device.ContainerDevices{},
			allocated:      nil,
			ns:             "default",
			memreq:         3000,
			coresreq:       1,
			expectedResult: false,
		},
		{
			name: "tmpdev",
			tmpDevs: map[string]device.ContainerDevices{
				NvidiaGPUDevice: {
					{UUID: "gpu-1", Type: NvidiaGPUDevice, Usedmem: 1024, Usedcores: 5},
				},
			},
			allocated:      nil,
			ns:             "default",
			memreq:         100,
			coresreq:       1,
			expectedResult: true,
		},
		{
			name: "tmpdev exceed quota",
			tmpDevs: map[string]device.ContainerDevices{
				NvidiaGPUDevice: {
					{UUID: "gpu-1", Type: NvidiaGPUDevice, Usedmem: 1024, Usedcores: 5},
				},
			},
			allocated:      nil,
			ns:             "default",
			memreq:         2000,
			coresreq:       1,
			expectedResult: false,
		},
		{
			name:    "allocated devs",
			tmpDevs: map[string]device.ContainerDevices{},
			allocated: &device.PodDevices{
				NvidiaGPUDevice: device.PodSingleDevice{
					device.ContainerDevices{
						{UUID: "gpu-0", Type: NvidiaGPUDevice, Usedmem: 1024, Usedcores: 2},
					},
				},
			},
			ns:             "default",
			memreq:         100,
			coresreq:       1,
			expectedResult: true,
		},
		{
			name:    "allocated devs exceed quota",
			tmpDevs: map[string]device.ContainerDevices{},
			allocated: &device.PodDevices{
				NvidiaGPUDevice: device.PodSingleDevice{
					device.ContainerDevices{
						{UUID: "gpu-0", Type: NvidiaGPUDevice, Usedmem: 1024, Usedcores: 2},
					},
				},
			},
			ns:             "default",
			memreq:         2000,
			coresreq:       1,
			expectedResult: false,
		},
		{
			name: "exceed quota",
			tmpDevs: map[string]device.ContainerDevices{
				NvidiaGPUDevice: {
					{UUID: "gpu-1", Type: NvidiaGPUDevice, Usedmem: 1024, Usedcores: 5},
				},
			},
			allocated: &device.PodDevices{
				NvidiaGPUDevice: device.PodSingleDevice{
					device.ContainerDevices{
						{UUID: "gpu-0", Type: NvidiaGPUDevice, Usedmem: 1024, Usedcores: 2},
					},
				},
			},
			ns:             "default",
			memreq:         100,
			coresreq:       1,
			expectedResult: false,
		},
		{
			name: "fit",
			tmpDevs: map[string]device.ContainerDevices{
				NvidiaGPUDevice: {
					{UUID: "gpu-1", Type: NvidiaGPUDevice, Usedmem: 100, Usedcores: 1},
				},
			},
			allocated: &device.PodDevices{
				NvidiaGPUDevice: device.PodSingleDevice{
					device.ContainerDevices{
						{UUID: "gpu-0", Type: NvidiaGPUDevice, Usedmem: 100, Usedcores: 2},
					},
				},
			},
			ns:             "default",
			memreq:         100,
			coresreq:       1,
			expectedResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fitQuota(tt.tmpDevs, tt.allocated, tt.ns, tt.memreq, tt.coresreq)
			assert.Equal(t, tt.expectedResult, result, tt.name)
		})
	}
}

func TestParseConfig(t *testing.T) {
	// ParseConfig is intentionally a no-op; just verify it is callable.
	ParseConfig(nil)
}

func TestScoreNode(t *testing.T) {
	dev := InitNvidiaDevice(NvidiaConfig{ResourceCountName: "nvidia.com/gpu"})
	score := dev.ScoreNode(nil, nil, nil, "")
	assert.Equal(t, score, float32(0))
}

func TestAssertNuma(t *testing.T) {
	tests := []struct {
		name  string
		annos map[string]string
		want  bool
	}{
		{
			name:  "NumaBind=true enforces numa",
			annos: map[string]string{NumaBind: "true"},
			want:  true,
		},
		{
			name:  "NumaBind=false does not enforce",
			annos: map[string]string{NumaBind: "false"},
			want:  false,
		},
		{
			name:  "NumaBind invalid value",
			annos: map[string]string{NumaBind: "notabool"},
			want:  false,
		},
		{
			name:  "NumaBind key absent",
			annos: map[string]string{},
			want:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, assertNuma(tt.annos), tt.want)
		})
	}
}

func TestCheckHealth(t *testing.T) {
	config := NvidiaConfig{ResourceCountName: "nvidia.com/gpu"}

	util.HandshakeAnnos = make(map[string]string)
	util.HandshakeAnnos[NvidiaGPUDevice] = "hami.io/node-handshake" // Adjust annotation key if different

	pastTime := time.Now().Add(-2 * time.Hour).Format(time.DateTime)

	tests := []struct {
		name                string
		current             int64
		reported            int64
		handshakeAnnotation string
		wantHealthy         bool
		wantNeedReset       bool
	}{
		{
			name:                "current=0 reported=0: no devices registered yet",
			current:             0,
			reported:            0,
			handshakeAnnotation: "Deleted_",
			wantHealthy:         true,
			wantNeedReset:       false,
		},
		{
			name:                "current=0 reported>0: devices disappeared",
			current:             0,
			reported:            2,
			handshakeAnnotation: "Deleted_",
			wantHealthy:         false,
			wantNeedReset:       false,
		},
		{
			name:                "current>0 reported differs: count changed",
			current:             4,
			reported:            2,
			handshakeAnnotation: "Deleted_",
			wantHealthy:         true,
			wantNeedReset:       true,
		},
		{
			name:                "current>0 reported same: stable",
			current:             4,
			reported:            4,
			handshakeAnnotation: "Deleted_",
			wantHealthy:         true,
			wantNeedReset:       false,
		},
		{
			name:                "Kernel 6.17 Bug: current=0 reported=0 but handshake pending",
			current:             0,
			reported:            0,
			handshakeAnnotation: "Requesting_" + time.Now().Format(time.DateTime),
			wantHealthy:         true,
			wantNeedReset:       false,
		},
		{
			name:                "Kernel 6.17 Bug: current=0 reported=0 but handshake expired",
			current:             0,
			reported:            0,
			handshakeAnnotation: "Requesting_" + pastTime,
			wantHealthy:         false,
			wantNeedReset:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dev := InitNvidiaDevice(config)

			allocatable := corev1.ResourceList{}
			if tt.current > 0 {
				allocatable[corev1.ResourceName(config.ResourceCountName)] = *resource.NewQuantity(tt.current, resource.DecimalSI)
			}

			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Annotations: map[string]string{
						util.HandshakeAnnos[NvidiaGPUDevice]: tt.handshakeAnnotation,
					},
				},
				Status: corev1.NodeStatus{Allocatable: allocatable},
			}

			if tt.reported > 0 {
				dev.ReportedGPUNum["test-node"] = tt.reported
			}

			healthy, needReset := dev.CheckHealth("NVIDIA", node)
			assert.Equal(t, tt.wantHealthy, healthy, "Healthy status mismatch")
			assert.Equal(t, tt.wantNeedReset, needReset, "Reset status mismatch")
		})
	}
}

func TestGenerateResourceRequests(t *testing.T) {
	config := NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
		MemoryFactor:                 1,
	}
	dev := InitNvidiaDevice(config)

	tests := []struct {
		name string
		ctr  *corev1.Container
		want device.ContainerDeviceRequest
	}{
		{
			name: "gpu count only — defaults to 100% memory",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu": *resource.NewQuantity(1, resource.BinarySI),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             1,
				Type:             NvidiaGPUDevice,
				Memreq:           0,
				MemPercentagereq: 100,
				Coresreq:         0,
			},
		},
		{
			name: "gpu count + explicit memory",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu":    *resource.NewQuantity(2, resource.BinarySI),
						"nvidia.com/gpumem": *resource.NewQuantity(4096, resource.DecimalSI),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             2,
				Type:             NvidiaGPUDevice,
				Memreq:           4096,
				MemPercentagereq: 101,
				Coresreq:         0,
			},
		},
		{
			name: "gpu count + memory percentage",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu":               *resource.NewQuantity(1, resource.BinarySI),
						"nvidia.com/gpumem-percentage": *resource.NewQuantity(50, resource.DecimalSI),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             1,
				Type:             NvidiaGPUDevice,
				Memreq:           0,
				MemPercentagereq: 50,
				Coresreq:         0,
			},
		},
		{
			name: "gpu count + explicit cores",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu":      *resource.NewQuantity(1, resource.BinarySI),
						"nvidia.com/gpucores": *resource.NewQuantity(50, resource.DecimalSI),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             1,
				Type:             NvidiaGPUDevice,
				Memreq:           0,
				MemPercentagereq: 100,
				Coresreq:         50,
			},
		},
		{
			name: "no gpu resource — returns empty request",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "gpu in Requests (not Limits) — falls back to Requests",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						"nvidia.com/gpu": *resource.NewQuantity(1, resource.BinarySI),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             1,
				Type:             NvidiaGPUDevice,
				Memreq:           0,
				MemPercentagereq: 100,
				Coresreq:         0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := dev.GenerateResourceRequests(tt.ctr)
			assert.DeepEqual(t, result, tt.want)
		})
	}
}

func TestGenerateResourceRequests_MemoryFactor(t *testing.T) {
	config := NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
		MemoryFactor:                 2,
	}
	dev := InitNvidiaDevice(config)
	ctr := &corev1.Container{
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				"nvidia.com/gpu":    *resource.NewQuantity(1, resource.BinarySI),
				"nvidia.com/gpumem": *resource.NewQuantity(1024, resource.DecimalSI),
			},
		},
	}
	result := dev.GenerateResourceRequests(ctr)
	assert.Equal(t, result.Memreq, int32(2048))
}

func TestGenerateResourceRequests_DefaultMemory(t *testing.T) {
	config := NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
		DefaultMemory:                512,
		MemoryFactor:                 1,
	}
	dev := InitNvidiaDevice(config)
	ctr := &corev1.Container{
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				"nvidia.com/gpu": *resource.NewQuantity(1, resource.BinarySI),
			},
		},
	}
	result := dev.GenerateResourceRequests(ctr)
	assert.Equal(t, result.Memreq, int32(512))
	assert.Equal(t, result.MemPercentagereq, int32(101))
}

func TestMigNeedsReset(t *testing.T) {
	dev := InitNvidiaDevice(NvidiaConfig{})

	tests := []struct {
		name      string
		usage     *device.DeviceUsage
		want      bool
		wantEmpty bool
	}{
		{
			name:  "empty UsageList — needs reset",
			usage: &device.DeviceUsage{MigUsage: device.MigInUse{UsageList: device.MIGS{}}},
			want:  true,
		},
		{
			name: "all entries not InUse — resets and clears list",
			usage: &device.DeviceUsage{
				MigUsage: device.MigInUse{
					UsageList: device.MIGS{
						{Memory: 10, InUse: false},
						{Memory: 20, InUse: false},
					},
				},
			},
			want:      true,
			wantEmpty: true,
		},
		{
			name: "one entry InUse=true — no reset",
			usage: &device.DeviceUsage{
				MigUsage: device.MigInUse{
					UsageList: device.MIGS{
						{Memory: 10, InUse: true},
						{Memory: 20, InUse: false},
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := dev.migNeedsReset(tt.usage)
			assert.Equal(t, result, tt.want)
			if tt.wantEmpty {
				assert.Equal(t, len(tt.usage.MigUsage.UsageList), 0)
			}
		})
	}
}

func TestGenerateCombinations(t *testing.T) {
	devs := device.ContainerDevices{
		{UUID: "gpu0"},
		{UUID: "gpu1"},
		{UUID: "gpu2"},
		{UUID: "gpu3"},
	}
	tmpDevs := map[string]device.ContainerDevices{NvidiaGPUDevice: devs}

	tests := []struct {
		name    string
		nums    int32
		wantLen int
	}{
		{name: "C(4,1)=4", nums: 1, wantLen: 4},
		{name: "C(4,2)=6", nums: 2, wantLen: 6},
		{name: "C(4,3)=4", nums: 3, wantLen: 4},
		{name: "C(4,4)=1", nums: 4, wantLen: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := device.ContainerDeviceRequest{Nums: tt.nums, Type: NvidiaGPUDevice}
			result := generateCombinations(req, tmpDevs)
			assert.Equal(t, len(result), tt.wantLen)
			for _, combo := range result {
				assert.Equal(t, len(combo), int(tt.nums))
			}
		})
	}
}

func TestGetDevicePairScoreMap(t *testing.T) {
	nodeInfo := &device.NodeInfo{
		Devices: map[string][]device.DeviceInfo{
			NvidiaGPUDevice: {
				{
					ID: "gpu0",
					DevicePairScore: device.DevicePairScore{
						ID:     "gpu0",
						Scores: map[string]int{"gpu1": 100, "gpu2": 200},
					},
				},
				{
					ID: "gpu1",
					DevicePairScore: device.DevicePairScore{
						ID:     "gpu1",
						Scores: map[string]int{"gpu0": 100, "gpu2": 150},
					},
				},
			},
		},
	}

	result := getDevicePairScoreMap(nodeInfo)
	assert.Equal(t, len(result), 2)
	assert.Equal(t, result["gpu0"].Scores["gpu1"], 100)
	assert.Equal(t, result["gpu1"].Scores["gpu2"], 150)
}

// gpu0 total=300, gpu1 total=250, gpu2 total=350 — worst is gpu1.
func TestComputeWorstSingleCard(t *testing.T) {
	nodeInfo := &device.NodeInfo{
		Devices: map[string][]device.DeviceInfo{
			NvidiaGPUDevice: {
				{ID: "gpu0", DevicePairScore: device.DevicePairScore{Scores: map[string]int{"gpu1": 100, "gpu2": 200}}},
				{ID: "gpu1", DevicePairScore: device.DevicePairScore{Scores: map[string]int{"gpu0": 100, "gpu2": 150}}},
				{ID: "gpu2", DevicePairScore: device.DevicePairScore{Scores: map[string]int{"gpu0": 200, "gpu1": 150}}},
			},
		},
	}
	tmpDevs := map[string]device.ContainerDevices{
		NvidiaGPUDevice: {{UUID: "gpu0"}, {UUID: "gpu1"}, {UUID: "gpu2"}},
	}
	req := device.ContainerDeviceRequest{Type: NvidiaGPUDevice}
	result := computeWorstSingleCard(nodeInfo, req, tmpDevs)
	assert.Equal(t, len(result), 1)
	assert.Equal(t, result[0].UUID, "gpu1")
}

// [gpu0,gpu1]=100, [gpu0,gpu2]=200, [gpu1,gpu2]=150 — best is [gpu0,gpu2].
func TestComputeBestCombination(t *testing.T) {
	nodeInfo := &device.NodeInfo{
		Devices: map[string][]device.DeviceInfo{
			NvidiaGPUDevice: {
				{ID: "gpu0", DevicePairScore: device.DevicePairScore{Scores: map[string]int{"gpu1": 100, "gpu2": 200}}},
				{ID: "gpu1", DevicePairScore: device.DevicePairScore{Scores: map[string]int{"gpu0": 100, "gpu2": 150}}},
				{ID: "gpu2", DevicePairScore: device.DevicePairScore{Scores: map[string]int{"gpu0": 200, "gpu1": 150}}},
			},
		},
	}
	combinations := []device.ContainerDevices{
		{{UUID: "gpu0"}, {UUID: "gpu1"}},
		{{UUID: "gpu0"}, {UUID: "gpu2"}},
		{{UUID: "gpu1"}, {UUID: "gpu2"}},
	}
	result := computeBestCombination(nodeInfo, combinations)
	assert.Equal(t, len(result), 2)
	assert.Equal(t, result[0].UUID, "gpu0")
	assert.Equal(t, result[1].UUID, "gpu2")
}

func TestCustomFilterRule_NonMig(t *testing.T) {
	dev := InitNvidiaDevice(NvidiaConfig{})
	devusage := &device.DeviceUsage{Mode: ""}
	result := dev.CustomFilterRule(nil, device.ContainerDeviceRequest{}, nil, devusage)
	assert.Equal(t, result, true)
}

func TestCustomFilterRule_Mig(t *testing.T) {
	dev := InitNvidiaDevice(NvidiaConfig{})

	tests := []struct {
		name       string
		usageList  device.MIGS
		toAllocate device.ContainerDevices
		memreq     int32
		want       bool
	}{
		{
			name: "slot available after allocating toAllocate",
			usageList: device.MIGS{
				{Memory: 1000, InUse: false},
				{Memory: 1000, InUse: false},
			},
			toAllocate: device.ContainerDevices{{Usedmem: 500}},
			memreq:     500,
			want:       true,
		},
		{
			name: "no remaining slot after allocating toAllocate",
			usageList: device.MIGS{
				{Memory: 1000, InUse: false},
			},
			toAllocate: device.ContainerDevices{{Usedmem: 500}},
			memreq:     500,
			want:       false,
		},
		{
			name: "toAllocate does not fit any slot",
			usageList: device.MIGS{
				{Memory: 100, InUse: false},
			},
			toAllocate: device.ContainerDevices{{Usedmem: 500}},
			memreq:     500,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			devusage := &device.DeviceUsage{
				Mode:     MigMode,
				MigUsage: device.MigInUse{UsageList: tt.usageList},
			}
			req := device.ContainerDeviceRequest{Memreq: tt.memreq}
			result := dev.CustomFilterRule(nil, req, tt.toAllocate, devusage)
			assert.Equal(t, result, tt.want)
		})
	}
}

func TestNodeCleanUp(t *testing.T) {
	client.KubeClient = fake.NewClientset()
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-node",
			Annotations: map[string]string{HandshakeAnnos: "ready"},
		},
	}
	_, err := client.KubeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
	assert.NilError(t, err)

	dev := InitNvidiaDevice(NvidiaConfig{ResourceCountName: "nvidia.com/gpu"})
	err = dev.NodeCleanUp("test-node")
	assert.NilError(t, err)

	updated, err := client.KubeClient.CoreV1().Nodes().Get(context.Background(), "test-node", metav1.GetOptions{})
	assert.NilError(t, err)
	anno := updated.Annotations[HandshakeAnnos]
	assert.Assert(t, strings.HasPrefix(anno, "Deleted_"), "expected annotation to start with 'Deleted_', got %q", anno)
}

func TestLockNode(t *testing.T) {
	config := NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
	}

	tests := []struct {
		name    string
		pod     *corev1.Pod
		hasLock bool
	}{
		{
			name: "no GPU containers — skip lock",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-no-gpu", Namespace: "default"},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}},
			},
			hasLock: false,
		},
		{
			name: "has GPU container — acquires lock",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-gpu", Namespace: "default"},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "gpu-app",
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								"nvidia.com/gpu": *resource.NewQuantity(1, resource.BinarySI),
							},
						},
					}},
				},
			},
			hasLock: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client.KubeClient = fake.NewClientset()
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-node",
					Annotations: map[string]string{},
				},
			}
			_, err := client.KubeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
			assert.NilError(t, err)

			dev := InitNvidiaDevice(config)
			err = dev.LockNode(node, tt.pod)
			assert.NilError(t, err)

			updated, err := client.KubeClient.CoreV1().Nodes().Get(context.Background(), "test-node", metav1.GetOptions{})
			assert.NilError(t, err)
			_, ok := updated.Annotations[nodelock.NodeLockKey]
			assert.Equal(t, ok, tt.hasLock)
		})
	}
}

func TestMutateAdmission_Priority(t *testing.T) {
	dev := &NvidiaGPUDevices{
		config: NvidiaConfig{
			ResourceCountName:            "nvidia.com/gpu",
			ResourceMemoryName:           "nvidia.com/gpumem",
			ResourceCoreName:             "nvidia.com/gpucores",
			ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
			ResourcePriority:             "nvidia.com/priority",
			DefaultGPUNum:                1,
		},
	}
	ctr := &corev1.Container{
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				"nvidia.com/gpu":      *resource.NewQuantity(1, resource.BinarySI),
				"nvidia.com/priority": *resource.NewQuantity(5, resource.DecimalSI),
			},
		},
	}
	got, err := dev.MutateAdmission(ctr, &corev1.Pod{})
	assert.NilError(t, err)
	assert.Equal(t, got, true)
	found := false
	for _, env := range ctr.Env {
		if env.Name == util.TaskPriority {
			assert.Equal(t, env.Value, "5")
			found = true
		}
	}
	assert.Assert(t, found, "expected TaskPriority env to be set")
}

func TestMutateAdmission_CorePolicy(t *testing.T) {
	dev := &NvidiaGPUDevices{
		config: NvidiaConfig{
			ResourceCountName:            "nvidia.com/gpu",
			ResourceMemoryName:           "nvidia.com/gpumem",
			ResourceCoreName:             "nvidia.com/gpucores",
			ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
			DefaultGPUNum:                1,
			GPUCorePolicy:                "force",
		},
	}
	ctr := &corev1.Container{
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				"nvidia.com/gpu": *resource.NewQuantity(1, resource.BinarySI),
			},
		},
	}
	dev.MutateAdmission(ctr, &corev1.Pod{})
	found := false
	for _, env := range ctr.Env {
		if env.Name == util.CoreLimitSwitch {
			assert.Equal(t, env.Value, "force")
			found = true
		}
	}
	assert.Assert(t, found, "expected CoreLimitSwitch env to be set")
}

func TestMutateAdmission_RuntimeClassName(t *testing.T) {
	dev := &NvidiaGPUDevices{
		config: NvidiaConfig{
			ResourceCountName:            "nvidia.com/gpu",
			ResourceMemoryName:           "nvidia.com/gpumem",
			ResourceCoreName:             "nvidia.com/gpucores",
			ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
			DefaultGPUNum:                1,
			RuntimeClassName:             "nvidia",
		},
	}
	ctr := &corev1.Container{
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				"nvidia.com/gpu": *resource.NewQuantity(1, resource.BinarySI),
			},
		},
	}
	pod := &corev1.Pod{}
	dev.MutateAdmission(ctr, pod)
	assert.Assert(t, pod.Spec.RuntimeClassName != nil)
	assert.Equal(t, *pod.Spec.RuntimeClassName, "nvidia")
}

func TestMutateAdmission_OverwriteEnv(t *testing.T) {
	dev := &NvidiaGPUDevices{
		config: NvidiaConfig{
			ResourceCountName:            "nvidia.com/gpu",
			ResourceMemoryName:           "nvidia.com/gpumem",
			ResourceCoreName:             "nvidia.com/gpucores",
			ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
			OverwriteEnv:                 true,
		},
	}
	ctr := &corev1.Container{
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{},
		},
	}
	got, _ := dev.MutateAdmission(ctr, &corev1.Pod{})
	assert.Equal(t, got, false)
	found := false
	for _, env := range ctr.Env {
		if env.Name == "NVIDIA_VISIBLE_DEVICES" && env.Value == "none" {
			found = true
		}
	}
	assert.Assert(t, found, "expected NVIDIA_VISIBLE_DEVICES=none env")
}

func TestDefaultExclusiveCoreIfNeeded_NilContainer(t *testing.T) {
	dev := &NvidiaGPUDevices{config: NvidiaConfig{ResourceCountName: "nvidia.com/gpu", ResourceCoreName: "nvidia.com/gpucores"}}
	assert.Equal(t, dev.defaultExclusiveCoreIfNeeded(nil), false)
}

func TestResourceValue_NilAndEmpty(t *testing.T) {
	v, ok := resourceValue(nil, "nvidia.com/gpu")
	assert.Equal(t, v, int64(0))
	assert.Equal(t, ok, false)

	v, ok = resourceValue(&corev1.Container{}, "")
	assert.Equal(t, v, int64(0))
	assert.Equal(t, ok, false)
}

func TestResourcePresent_NilAndEmpty(t *testing.T) {
	assert.Equal(t, resourcePresent(nil, "nvidia.com/gpu"), false)
	assert.Equal(t, resourcePresent(&corev1.Container{}, ""), false)
}

func TestCheckGPUtype_NoUse(t *testing.T) {
	annos := map[string]string{
		GPUNoUse: "A100",
	}
	assert.Equal(t, checkGPUtype(annos, "NVIDIA-A100"), false)
	assert.Equal(t, checkGPUtype(annos, "NVIDIA-V100"), true)
}

func TestCheckType_AllocateMode(t *testing.T) {
	dev := &NvidiaGPUDevices{}
	req := device.ContainerDeviceRequest{Type: NvidiaGPUDevice}

	// AllocateMode does not contain device mode → typeCheck false
	annos := map[string]string{AllocateMode: "mps"}
	d := device.DeviceUsage{Type: "NVIDIA-A100", Mode: "hami-core"}
	ok, _ := dev.checkType(annos, d, req)
	assert.Equal(t, ok, false)

	// AllocateMode contains device mode → typeCheck true
	annos2 := map[string]string{AllocateMode: "hami-core"}
	ok2, _ := dev.checkType(annos2, d, req)
	assert.Equal(t, ok2, true)
}

func TestGetNodeDevices_InvalidJSON(t *testing.T) {
	dev := &NvidiaGPUDevices{}
	node := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "node-bad",
			Annotations: map[string]string{RegisterAnnos: "not-valid-json"},
		},
	}
	_, err := dev.GetNodeDevices(node)
	assert.Assert(t, err != nil)
}

func TestGetNodeDevices_MigTemplate(t *testing.T) {
	dev := &NvidiaGPUDevices{
		config: NvidiaConfig{
			MigGeometriesList: []device.AllowedMigGeometries{
				{
					Models: []string{"A100"},
					Geometries: []device.Geometry{
						{
							{Name: "1g.5gb", Memory: 5120, Core: 14, Count: 7},
						},
					},
				},
			},
		},
	}
	node := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-mig",
			Annotations: map[string]string{
				RegisterAnnos: `[{"id":"GPU-0","count":7,"devmem":40960,"devcore":100,"type":"NVIDIA-A100-SXM4-40GB","numa":0,"health":true,"mode":"mig"}]`,
			},
		},
	}
	result, err := dev.GetNodeDevices(node)
	assert.NilError(t, err)
	assert.Equal(t, len(result), 1)
	assert.Equal(t, len(result[0].MIGTemplate), 1)
	assert.Equal(t, result[0].MIGTemplate[0][0].Name, "1g.5gb")
}

func TestGetNodeDevices_PairScores(t *testing.T) {
	dev := &NvidiaGPUDevices{}
	node := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-topo",
			Annotations: map[string]string{
				RegisterAnnos:        `[{"id":"GPU-0","count":1,"devmem":8192,"devcore":100,"type":"NVIDIA-V100","health":true},{"id":"GPU-1","count":1,"devmem":8192,"devcore":100,"type":"NVIDIA-V100","health":true}]`,
				RegisterGPUPairScore: `[{"uuid":"GPU-0","score":{"GPU-1":100}},{"uuid":"GPU-1","score":{"GPU-0":100}}]`,
			},
		},
	}
	result, err := dev.GetNodeDevices(node)
	assert.NilError(t, err)
	assert.Equal(t, len(result), 2)
	assert.Equal(t, result[0].DevicePairScore.Scores["GPU-1"], 100)
}

func TestGetNodeDevices_InvalidPairScores(t *testing.T) {
	dev := &NvidiaGPUDevices{}
	node := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-bad-scores",
			Annotations: map[string]string{
				RegisterAnnos:        `[{"id":"GPU-0","count":1,"devmem":8192,"devcore":100,"type":"V100","health":true}]`,
				RegisterGPUPairScore: `not-valid-json`,
			},
		},
	}
	_, err := dev.GetNodeDevices(node)
	assert.Assert(t, err != nil)
}

func TestAddResourceUsage_MigNonReset(t *testing.T) {
	dev := InitNvidiaDevice(NvidiaConfig{})
	usage := &device.DeviceUsage{
		Mode: MigMode,
		MigUsage: device.MigInUse{
			Index: 0,
			UsageList: device.MIGS{
				{Name: "1g.5gb", Memory: 5120, Core: 14, InUse: true},
				{Name: "1g.5gb", Memory: 5120, Core: 14, InUse: false},
			},
		},
	}
	ctr := &device.ContainerDevice{UUID: "GPU-0", Usedmem: 4096, Usedcores: 10}
	err := dev.AddResourceUsage(&corev1.Pod{}, usage, ctr)
	assert.NilError(t, err)
	assert.Assert(t, usage.MigUsage.UsageList[1].InUse)
	assert.Equal(t, ctr.Usedmem, int32(5120))
	assert.Assert(t, strings.Contains(ctr.UUID, "["))
}

func TestAddResourceUsage_MigNonResetNoSlot(t *testing.T) {
	dev := InitNvidiaDevice(NvidiaConfig{})
	usage := &device.DeviceUsage{
		Mode: MigMode,
		MigUsage: device.MigInUse{
			UsageList: device.MIGS{
				{Name: "1g.5gb", Memory: 5120, Core: 14, InUse: true},
			},
		},
	}
	ctr := &device.ContainerDevice{UUID: "GPU-0", Usedmem: 4096}
	err := dev.AddResourceUsage(&corev1.Pod{}, usage, ctr)
	assert.Assert(t, err != nil)
	assert.Assert(t, strings.Contains(err.Error(), "mig template allocate resource fail"))
}

func TestCustomFilterRule_MigEmptyUsageWithTemplate(t *testing.T) {
	dev := InitNvidiaDevice(NvidiaConfig{})
	devusage := &device.DeviceUsage{
		Mode: MigMode,
		MigUsage: device.MigInUse{
			UsageList: device.MIGS{},
		},
		MigTemplate: []device.Geometry{
			{
				{Name: "1g.5gb", Memory: 5120, Core: 14, Count: 2},
			},
		},
	}
	req := device.ContainerDeviceRequest{Memreq: 4096}
	result := dev.CustomFilterRule(nil, req, nil, devusage)
	assert.Equal(t, result, true)
}

func TestCustomFilterRule_MigEmptyUsageNoFitTemplate(t *testing.T) {
	dev := InitNvidiaDevice(NvidiaConfig{})
	devusage := &device.DeviceUsage{
		Mode: MigMode,
		MigUsage: device.MigInUse{
			UsageList: device.MIGS{},
		},
		MigTemplate: []device.Geometry{
			{
				{Name: "1g.5gb", Memory: 100, Core: 14, Count: 1},
			},
		},
	}
	req := device.ContainerDeviceRequest{Memreq: 4096}
	result := dev.CustomFilterRule(nil, req, nil, devusage)
	assert.Equal(t, result, false)
}

func TestReleaseNodeLock(t *testing.T) {
	config := NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
	}

	tests := []struct {
		name    string
		pod     *corev1.Pod
		hasLock bool
	}{
		{
			name: "no GPU containers — skip release, lock remains",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-no-gpu", Namespace: "default"},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}},
			},
			hasLock: true,
		},
		{
			name: "has GPU container — releases lock",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-gpu", Namespace: "default"},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "gpu-app",
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								"nvidia.com/gpu": *resource.NewQuantity(1, resource.BinarySI),
							},
						},
					}},
				},
			},
			hasLock: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client.KubeClient = fake.NewClientset()
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Annotations: map[string]string{
						nodelock.NodeLockKey: "lock-values,default,pod-gpu",
					},
				},
			}
			_, err := client.KubeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
			assert.NilError(t, err)

			dev := InitNvidiaDevice(config)
			err = dev.ReleaseNodeLock(node, tt.pod)
			assert.NilError(t, err)

			updated, err := client.KubeClient.CoreV1().Nodes().Get(context.Background(), "test-node", metav1.GetOptions{})
			assert.NilError(t, err)
			_, ok := updated.Annotations[nodelock.NodeLockKey]
			assert.Equal(t, ok, tt.hasLock)
		})
	}
}

func TestCheckGPUtype_InUseMismatch(t *testing.T) {
	annos := map[string]string{GPUInUse: "A100"}
	assert.Equal(t, checkGPUtype(annos, "NVIDIA-V100"), false)
}

func TestFit_NumaSwitching(t *testing.T) {
	config := NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
	}
	nv := InitNvidiaDevice(config)

	// Iterated in reverse: dev-2(NUMA 0) → dev-1(NUMA 1) → dev-0(NUMA 1)
	// dev-2 collected first, then NUMA switch at dev-1 resets progress.
	devices := []*device.DeviceUsage{
		{ID: "dev-0", Index: 0, Used: 0, Count: 10, Totalmem: 8192, Totalcore: 100, Type: NvidiaGPUDevice, Health: true, Numa: 1},
		{ID: "dev-1", Index: 1, Used: 0, Count: 10, Totalmem: 8192, Totalcore: 100, Type: NvidiaGPUDevice, Health: true, Numa: 1},
		{ID: "dev-2", Index: 2, Used: 0, Count: 10, Totalmem: 8192, Totalcore: 100, Type: NvidiaGPUDevice, Health: true, Numa: 0},
	}
	req := device.ContainerDeviceRequest{Nums: 2, Memreq: 100, Coresreq: 10, Type: NvidiaGPUDevice}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{NumaBind: "true"},
		},
	}
	fit, result, _ := nv.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, true)
	assert.Equal(t, len(result[NvidiaGPUDevice]), 2)
	assert.Equal(t, result[NvidiaGPUDevice][0].UUID, "dev-1")
	assert.Equal(t, result[NvidiaGPUDevice][1].UUID, "dev-0")
}

func TestFit_TopologyExactMatch(t *testing.T) {
	config := NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
	}
	nv := InitNvidiaDevice(config)
	devices := []*device.DeviceUsage{
		{ID: "dev-0", Index: 0, Used: 0, Count: 10, Totalmem: 8192, Totalcore: 100, Type: NvidiaGPUDevice, Health: true},
		{ID: "dev-1", Index: 1, Used: 0, Count: 10, Totalmem: 8192, Totalcore: 100, Type: NvidiaGPUDevice, Health: true},
	}
	req := device.ContainerDeviceRequest{Nums: 2, Memreq: 100, Coresreq: 10, Type: NvidiaGPUDevice}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{util.GPUSchedulerPolicyAnnotationKey: util.GPUSchedulerPolicyTopology.String()},
		},
	}
	fit, result, _ := nv.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, true)
	assert.Equal(t, len(result[NvidiaGPUDevice]), 2)
}

func TestFit_TopologyWorstSingleCard(t *testing.T) {
	config := NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
	}
	nv := InitNvidiaDevice(config)
	devices := []*device.DeviceUsage{
		{ID: "dev-0", Index: 0, Used: 0, Count: 10, Totalmem: 8192, Totalcore: 100, Type: NvidiaGPUDevice, Health: true},
		{ID: "dev-1", Index: 1, Used: 0, Count: 10, Totalmem: 8192, Totalcore: 100, Type: NvidiaGPUDevice, Health: true},
		{ID: "dev-2", Index: 2, Used: 0, Count: 10, Totalmem: 8192, Totalcore: 100, Type: NvidiaGPUDevice, Health: true},
	}
	nodeInfo := &device.NodeInfo{
		Devices: map[string][]device.DeviceInfo{
			NvidiaGPUDevice: {
				{ID: "dev-0", DevicePairScore: device.DevicePairScore{Scores: map[string]int{"dev-1": 100, "dev-2": 200}}},
				{ID: "dev-1", DevicePairScore: device.DevicePairScore{Scores: map[string]int{"dev-0": 100, "dev-2": 150}}},
				{ID: "dev-2", DevicePairScore: device.DevicePairScore{Scores: map[string]int{"dev-0": 200, "dev-1": 150}}},
			},
		},
	}
	req := device.ContainerDeviceRequest{Nums: 1, Memreq: 100, Coresreq: 10, Type: NvidiaGPUDevice}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{util.GPUSchedulerPolicyAnnotationKey: util.GPUSchedulerPolicyTopology.String()},
		},
	}
	fit, result, _ := nv.Fit(devices, req, pod, nodeInfo, &device.PodDevices{})
	assert.Equal(t, fit, true)
	assert.Equal(t, len(result[NvidiaGPUDevice]), 1)
	assert.Equal(t, result[NvidiaGPUDevice][0].UUID, "dev-1")
}

func TestFit_TopologyBestCombination(t *testing.T) {
	config := NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
	}
	nv := InitNvidiaDevice(config)
	devices := []*device.DeviceUsage{
		{ID: "dev-0", Index: 0, Used: 0, Count: 10, Totalmem: 8192, Totalcore: 100, Type: NvidiaGPUDevice, Health: true},
		{ID: "dev-1", Index: 1, Used: 0, Count: 10, Totalmem: 8192, Totalcore: 100, Type: NvidiaGPUDevice, Health: true},
		{ID: "dev-2", Index: 2, Used: 0, Count: 10, Totalmem: 8192, Totalcore: 100, Type: NvidiaGPUDevice, Health: true},
	}
	nodeInfo := &device.NodeInfo{
		Devices: map[string][]device.DeviceInfo{
			NvidiaGPUDevice: {
				{ID: "dev-0", DevicePairScore: device.DevicePairScore{Scores: map[string]int{"dev-1": 100, "dev-2": 200}}},
				{ID: "dev-1", DevicePairScore: device.DevicePairScore{Scores: map[string]int{"dev-0": 100, "dev-2": 150}}},
				{ID: "dev-2", DevicePairScore: device.DevicePairScore{Scores: map[string]int{"dev-0": 200, "dev-1": 150}}},
			},
		},
	}
	req := device.ContainerDeviceRequest{Nums: 2, Memreq: 100, Coresreq: 10, Type: NvidiaGPUDevice}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{util.GPUSchedulerPolicyAnnotationKey: util.GPUSchedulerPolicyTopology.String()},
		},
	}
	fit, result, _ := nv.Fit(devices, req, pod, nodeInfo, &device.PodDevices{})
	assert.Equal(t, fit, true)
	assert.Equal(t, len(result[NvidiaGPUDevice]), 2)
	uuids := map[string]bool{}
	for _, d := range result[NvidiaGPUDevice] {
		uuids[d.UUID] = true
	}
	assert.Assert(t, uuids["dev-0"])
	assert.Assert(t, uuids["dev-2"])
}
