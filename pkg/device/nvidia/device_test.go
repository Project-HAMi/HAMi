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
	"errors"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/util"
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

func Test_checkUUID(t *testing.T) {
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
			d     util.DeviceUsage
		}
		want bool
	}{
		{
			name: "don't set GPUUseUUID and GPUNoUseUUID annotation",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: make(map[string]string),
				d:     util.DeviceUsage{},
			},
			want: true,
		},
		{
			name: "use set GPUUseUUID don't set GPUNoUseUUID annotation,device match",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{
					GPUUseUUID: "abc,123",
				},
				d: util.DeviceUsage{
					ID: "abc",
				},
			},
			want: true,
		},
		{
			name: "use set GPUUseUUID don't set GPUNoUseUUID annotation,device don't match",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{
					GPUUseUUID: "abc,123",
				},
				d: util.DeviceUsage{
					ID: "1abc",
				},
			},
			want: false,
		},
		{
			name: "use don't set GPUUseUUID set GPUNoUseUUID annotation,device match",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{
					GPUNoUseUUID: "abc,123",
				},
				d: util.DeviceUsage{
					ID: "abc",
				},
			},
			want: false,
		},
		{
			name: "use don't set GPUUseUUID set GPUNoUseUUID annotation,device  don't match",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{
					GPUNoUseUUID: "abc,123",
				},
				d: util.DeviceUsage{
					ID: "1abc",
				},
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := gpuDevices.checkUUID(test.args.annos, test.args.d)
			assert.Equal(t, test.want, got)
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
			d     util.DeviceUsage
		}
		want bool
	}{
		{
			name: "use set GPUInUse don't set GPUNoUse annotation,device match",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{
					GPUInUse: "A10",
				},
				d: util.DeviceUsage{
					Type: "NVIDIA A100",
				},
			},
			want: true,
		},
		{
			name: "use set GPUInUse set GPUNoUse annotation,device don't match",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{
					GPUInUse: "A10",
					GPUNoUse: "A100",
				},
				d: util.DeviceUsage{
					Type: "NVIDIA A100",
				},
			},
			want: false,
		},
		{
			name: "use set GPUInUse set GPUNoUse annotation,device match",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{
					GPUInUse: "A10",
					GPUNoUse: "A100",
				},
				d: util.DeviceUsage{
					Type: "NVIDIA A10",
				},
			},
			want: true,
		},
	}
	req := util.ContainerDeviceRequest{
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
			name: "test with vaild configuration",
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
			if devices == nil {
				t.Fatalf("Expected devices to be initialized")
			}
			assert.DeepEqual(t, test.want.config, devices.config)
			assert.Equal(t, "hami.io/vgpu-devices-to-allocate", util.InRequestDevices[NvidiaGPUDevice], "Expected InRequestDevices to be set")
			assert.Equal(t, "hami.io/vgpu-devices-allocated", util.SupportDevices[NvidiaGPUDevice], "Expected SupportDevices to be set")
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
					NvidiaGPUDevice: util.PodSingleDevice{
						[]util.ContainerDevice{
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
				util.InRequestDevices[NvidiaGPUDevice]: "nvidia-device-0,NVIDIA,2000,1:;",
				util.SupportDevices[NvidiaGPUDevice]:   "nvidia-device-0,NVIDIA,2000,1:;",
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
		want []*util.DeviceInfo
		err  error
	}{
		{
			name: "exist gpu devices",
			args: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node-01",
					Annotations: map[string]string{
						RegisterAnnos: "GPU-0,5,8192,100,NVIDIA-Tesla P4,0,true:",
					},
				},
			},
			want: []*util.DeviceInfo{
				{
					ID:      "GPU-0",
					Count:   5,
					Devmem:  8192,
					Devcore: 100,
					Type:    "NVIDIA-Tesla P4",
					Numa:    0,
					Health:  true,
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
						RegisterAnnos: "",
					},
				},
			},
			want: []*util.DeviceInfo{},
			err:  errors.New("failed to decode node devices"),
		},
		{
			name: "no annotation",
			args: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "node-03",
					Annotations: map[string]string{},
				},
			},
			want: []*util.DeviceInfo{},
			err:  errors.New("annos not found " + RegisterAnnos),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gpuDevices := &NvidiaGPUDevices{}
			result, err := gpuDevices.GetNodeDevices(test.args)
			if (err != nil) != (test.err != nil) {
				t.Errorf("GetNodeDevices error = %v, want %v", err, test.err)
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
			request: util.ContainerDeviceRequest{
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
				Type:      NvidiaGPUDevice,
				Health:    true,
			}},
			request: util.ContainerDeviceRequest{
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
			devices: []*util.DeviceUsage{{
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
			request: util.ContainerDeviceRequest{
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
				Health:    true,
				Type:      NvidiaGPUDevice,
			}},
			request: util.ContainerDeviceRequest{
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
			devices: []*util.DeviceUsage{{
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
			request: util.ContainerDeviceRequest{
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
			devices: []*util.DeviceUsage{{
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
			request: util.ContainerDeviceRequest{
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
			devices: []*util.DeviceUsage{{
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
			request: util.ContainerDeviceRequest{
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
			devices: []*util.DeviceUsage{{
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
			request: util.ContainerDeviceRequest{
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
			devices: []*util.DeviceUsage{{
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
			request: util.ContainerDeviceRequest{
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
			devices: []*util.DeviceUsage{{
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
			request: util.ContainerDeviceRequest{
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
			devices: []*util.DeviceUsage{{
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
			request: util.ContainerDeviceRequest{
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
			devices: []*util.DeviceUsage{{
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
			request: util.ContainerDeviceRequest{
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
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			allocated := &util.PodDevices{}
			fit, result, reason := dev.Fit(test.devices, test.request, test.annos, &corev1.Pod{}, &util.NodeInfo{}, allocated)
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
		deviceUsage *util.DeviceUsage
		ctr         *util.ContainerDevice
		wantErr     bool
		wantUsage   *util.DeviceUsage
	}{
		{
			name: "test add resource usage",
			deviceUsage: &util.DeviceUsage{
				ID:        "dev-0",
				Used:      0,
				Usedcores: 15,
				Usedmem:   2000,
			},
			ctr: &util.ContainerDevice{
				UUID:      "dev-0",
				Usedcores: 50,
				Usedmem:   1024,
			},
			wantUsage: &util.DeviceUsage{
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
			}
		})
	}
}
