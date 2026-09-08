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

func TestResourceQuantityAsInt64(t *testing.T) {
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

func Test_MutateAdmission_MemoryPercentageValidation(t *testing.T) {
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
		name    string
		pct     int64
		wantErr bool
	}{
		{
			name:    "percentage of 0 is accepted",
			pct:     0,
			wantErr: false,
		},
		{
			name:    "percentage of 100 is accepted",
			pct:     100,
			wantErr: false,
		},
		{
			name:    "percentage of 101 is rejected",
			pct:     101,
			wantErr: true,
		},
		{
			name:    "percentage above 100 is rejected",
			pct:     150,
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctr := &corev1.Container{
				Name: "test",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu":               *resource.NewQuantity(1, resource.BinarySI),
						"nvidia.com/gpumem-percentage": *resource.NewQuantity(test.pct, resource.DecimalSI),
					},
				},
			}
			_, err := gpuDevices.MutateAdmission(ctr, &corev1.Pod{})
			if test.wantErr && err == nil {
				t.Fatalf("expected MutateAdmission to reject percentage %d, but got no error", test.pct)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("expected MutateAdmission to accept percentage %d, but got error: %v", test.pct, err)
			}
		})
	}
}

func Test_MutateAdmission_CoresValidation(t *testing.T) {
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
		name    string
		cores   int64
		wantErr bool
	}{
		{
			name:    "cores of 0 is accepted",
			cores:   0,
			wantErr: false,
		},
		{
			name:    "cores of 50 is accepted",
			cores:   50,
			wantErr: false,
		},
		{
			name:    "cores of 100 is accepted",
			cores:   100,
			wantErr: false,
		},
		{
			name:    "cores of 101 is rejected",
			cores:   101,
			wantErr: true,
		},
		{
			name:    "cores of 150 is rejected",
			cores:   150,
			wantErr: true,
		},
		{
			name:    "cores of 200 is rejected",
			cores:   200,
			wantErr: true,
		},
		{
			name:    "negative cores is rejected",
			cores:   -1,
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctr := &corev1.Container{
				Name: "test",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu":      *resource.NewQuantity(1, resource.BinarySI),
						"nvidia.com/gpucores": *resource.NewQuantity(test.cores, resource.DecimalSI),
					},
				},
			}
			_, err := gpuDevices.MutateAdmission(ctr, &corev1.Pod{})
			if test.wantErr && err == nil {
				t.Fatalf("expected MutateAdmission to reject cores %d, but got no error", test.cores)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("expected MutateAdmission to accept cores %d, but got error: %v", test.cores, err)
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
			name: "fit fail: core limit out of range",
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
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardInsufficientCore",
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
			name: "fit fail: partial allocation AllocatedCardsInsufficientRequest for multiple cards",
			devices: []*device.DeviceUsage{
				{
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
				},
				{
					ID:        "dev-1",
					Index:     1,
					Used:      0,
					Count:     100,
					Usedmem:   0,
					Totalmem:  1280,
					Totalcore: 100,
					Usedcores: 0,
					Numa:      0,
					Type:      NvidiaGPUDevice,
					Health:    true,
				},
			},
			request: device.ContainerDeviceRequest{
				Nums:             3,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         20,
				Type:             NvidiaGPUDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "2/2 AllocatedCardsInsufficientRequest",
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

func TestFit_DeviceCordon(t *testing.T) {
	config := NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpumem",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
	}
	dev := InitNvidiaDevice(config)

	newDevices := func() []*device.DeviceUsage {
		return []*device.DeviceUsage{
			{ID: "dev-0", Count: 100, Totalmem: 128, Totalcore: 100, Type: NvidiaGPUDevice, Health: true},
			{ID: "dev-1", Count: 100, Totalmem: 128, Totalcore: 100, Type: NvidiaGPUDevice, Health: true},
		}
	}
	request := device.ContainerDeviceRequest{Nums: 1, Memreq: 64, Coresreq: 50, Type: NvidiaGPUDevice}
	pod := &corev1.Pod{}

	nodeWithCordon := func(uuids string) *device.NodeInfo {
		return &device.NodeInfo{Node: &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{DeviceCordonAnnotation: uuids}},
		}}
	}

	t.Run("cordoned device is skipped, healthy sibling still fits", func(t *testing.T) {
		fit, result, reason := dev.Fit(newDevices(), request, pod, nodeWithCordon("dev-1, "), &device.PodDevices{})
		if !fit {
			t.Fatalf("expected fit, got reason: %s", reason)
		}
		if got := result[NvidiaGPUDevice][0].UUID; got != "dev-0" {
			t.Errorf("expected dev-0 (dev-1 is cordoned), got %s", got)
		}
	})

	t.Run("all devices cordoned fails with CardCordoned reason", func(t *testing.T) {
		fit, _, reason := dev.Fit(newDevices(), request, pod, nodeWithCordon("dev-0,dev-1"), &device.PodDevices{})
		if fit {
			t.Fatal("expected no fit, all devices are cordoned")
		}
		if reason != "2/2 CardCordoned" {
			t.Errorf("expected reason %q, got %q", "2/2 CardCordoned", reason)
		}
	})

	t.Run("running pods on a cordoned device are unaffected", func(t *testing.T) {
		devices := newDevices()
		devices[1].Used = 1
		devices[1].Usedcores = 50
		devices[1].Usedmem = 64
		fit, result, _ := dev.Fit(devices, request, pod, nodeWithCordon("dev-1"), &device.PodDevices{})
		if !fit {
			t.Fatal("expected fit onto the non-cordoned device")
		}
		if got := result[NvidiaGPUDevice][0].UUID; got != "dev-0" {
			t.Errorf("expected dev-0, got %s", got)
		}
		if devices[1].Used != 1 {
			t.Errorf("cordon must not touch existing usage on dev-1, got Used=%d", devices[1].Used)
		}
	})

	t.Run("no annotation or no node info means nothing cordoned", func(t *testing.T) {
		for name, ni := range map[string]*device.NodeInfo{
			"node present, annotation absent": {Node: &corev1.Node{}},
			"NodeInfo.Node is nil":            {},
		} {
			fit, _, reason := dev.Fit(newDevices(), request, pod, ni, &device.PodDevices{})
			if !fit {
				t.Errorf("%s: expected fit, got reason: %s", name, reason)
			}
		}
	})
}

func TestDevices_AddResourceUsage(t *testing.T) {
	dev := &NvidiaGPUDevices{}
	usage := &device.DeviceUsage{ID: "dev-0", Usedcores: 15, Usedmem: 2000}
	ctr := &device.ContainerDevice{UUID: "dev-0", Usedcores: 50, Usedmem: 1024}
	if err := dev.AddResourceUsage(&corev1.Pod{}, usage, ctr); err != nil {
		t.Fatal(err)
	}
	if usage.Used != 1 || usage.Usedcores != 65 || usage.Usedmem != 3024 {
		t.Fatalf("unexpected usage: %+v", usage)
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

	makeTestPod := func(numInit, numApp int) *corev1.Pod {
		initContainers := make([]corev1.Container, numInit)
		for i := range initContainers {
			initContainers[i] = corev1.Container{Name: "init"}
		}
		appContainers := make([]corev1.Container, numApp)
		for i := range appContainers {
			appContainers[i] = corev1.Container{Name: "app"}
		}
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: corev1.PodSpec{
				InitContainers: initContainers,
				Containers:     appContainers,
			},
		}
	}

	tests := []struct {
		name           string
		pod            *corev1.Pod
		tmpDevs        map[string]device.ContainerDevices
		allocated      *device.PodDevices
		ns             string
		devUUID        string
		memreq         int64
		coresreq       int64
		expectedResult bool
	}{
		{
			name:           "no tmp and no allocated",
			pod:            makeTestPod(0, 1),
			tmpDevs:        map[string]device.ContainerDevices{},
			allocated:      nil,
			ns:             "default",
			devUUID:        "gpu-0",
			memreq:         100,
			coresreq:       1,
			expectedResult: true,
		},
		{
			name:           "request exceed quota",
			pod:            makeTestPod(0, 1),
			tmpDevs:        map[string]device.ContainerDevices{},
			allocated:      nil,
			ns:             "default",
			devUUID:        "gpu-0",
			memreq:         3000,
			coresreq:       1,
			expectedResult: false,
		},
		{
			name: "tmpdev",
			pod:  makeTestPod(0, 2),
			tmpDevs: map[string]device.ContainerDevices{
				NvidiaGPUDevice: {
					{UUID: "gpu-1", Type: NvidiaGPUDevice, Usedmem: 1024, Usedcores: 5},
				},
			},
			allocated:      nil,
			ns:             "default",
			devUUID:        "gpu-0",
			memreq:         100,
			coresreq:       1,
			expectedResult: true,
		},
		{
			name: "tmpdev exceed quota",
			pod:  makeTestPod(0, 2),
			tmpDevs: map[string]device.ContainerDevices{
				NvidiaGPUDevice: {
					{UUID: "gpu-1", Type: NvidiaGPUDevice, Usedmem: 1024, Usedcores: 5},
				},
			},
			allocated:      nil,
			ns:             "default",
			devUUID:        "gpu-0",
			memreq:         2000,
			coresreq:       1,
			expectedResult: false,
		},
		{
			name:    "allocated devs",
			pod:     makeTestPod(0, 2),
			tmpDevs: map[string]device.ContainerDevices{},
			allocated: &device.PodDevices{
				NvidiaGPUDevice: device.PodSingleDevice{
					device.ContainerDevices{
						{UUID: "gpu-0", Type: NvidiaGPUDevice, Usedmem: 1024, Usedcores: 2},
					},
				},
			},
			ns:             "default",
			devUUID:        "gpu-1",
			memreq:         100,
			coresreq:       1,
			expectedResult: true,
		},
		{
			name:    "allocated devs exceed quota",
			pod:     makeTestPod(0, 2),
			tmpDevs: map[string]device.ContainerDevices{},
			allocated: &device.PodDevices{
				NvidiaGPUDevice: device.PodSingleDevice{
					device.ContainerDevices{
						{UUID: "gpu-0", Type: NvidiaGPUDevice, Usedmem: 1024, Usedcores: 2},
					},
				},
			},
			ns:             "default",
			devUUID:        "gpu-1",
			memreq:         2000,
			coresreq:       1,
			expectedResult: false,
		},
		{
			name: "exceed quota",
			pod:  makeTestPod(0, 3),
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
			devUUID:        "gpu-1",
			memreq:         100,
			coresreq:       1,
			expectedResult: false,
		},
		{
			name: "fit",
			pod:  makeTestPod(0, 3),
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
			devUUID:        "gpu-1",
			memreq:         100,
			coresreq:       1,
			expectedResult: true,
		},
		{
			name:    "fitting second init container maxes against first",
			pod:     makeTestPod(2, 1),
			tmpDevs: map[string]device.ContainerDevices{},
			allocated: &device.PodDevices{
				NvidiaGPUDevice: device.PodSingleDevice{
					device.ContainerDevices{
						{UUID: "gpu-0", Type: NvidiaGPUDevice, Usedmem: 1500, Usedcores: 2},
					},
				},
			},
			ns:             "default",
			devUUID:        "gpu-0",
			memreq:         1500,
			coresreq:       1,
			expectedResult: true,
		},
		{
			name:    "sequential init containers counted as peak not sum",
			pod:     makeTestPod(2, 1),
			tmpDevs: map[string]device.ContainerDevices{},
			allocated: &device.PodDevices{
				NvidiaGPUDevice: device.PodSingleDevice{
					device.ContainerDevices{
						{UUID: "gpu-0", Type: NvidiaGPUDevice, Usedmem: 1024, Usedcores: 2},
					},
					device.ContainerDevices{
						{UUID: "gpu-0", Type: NvidiaGPUDevice, Usedmem: 1024, Usedcores: 2},
					},
				},
			},
			ns:             "default",
			devUUID:        "gpu-0",
			memreq:         500,
			coresreq:       1,
			expectedResult: true,
		},
		{
			name:    "collapsed init peak still enforces quota",
			pod:     makeTestPod(1, 1),
			tmpDevs: map[string]device.ContainerDevices{},
			allocated: &device.PodDevices{
				NvidiaGPUDevice: device.PodSingleDevice{
					device.ContainerDevices{
						{UUID: "gpu-0", Type: NvidiaGPUDevice, Usedmem: 2100, Usedcores: 2},
					},
				},
			},
			ns:             "default",
			devUUID:        "gpu-1",
			memreq:         100,
			coresreq:       1,
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fitQuota(tt.pod, tt.tmpDevs, tt.allocated, tt.ns, tt.devUUID, tt.memreq, tt.coresreq)
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

	oldHandshakeAnnos := util.HandshakeAnnos
	defer func() { util.HandshakeAnnos = oldHandshakeAnnos }()

	util.HandshakeAnnos = make(map[string]string)
	util.HandshakeAnnos[NvidiaGPUDevice] = "hami.io/node-handshake"

	pastTime := time.Now().Add(-2 * time.Hour).Format(time.DateTime)

	tests := []struct {
		name                string
		current             int64
		reported            int64
		handshakeAnnotation string
		cachedRegisterAnno  string
		registerAnno        string
		wantHealthy         bool
		wantNeedReset       bool
	}{
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
		{
			name:                "current>0 reported same: changed register annotation requests refresh",
			current:             4,
			reported:            4,
			handshakeAnnotation: "Requesting_" + pastTime,
			cachedRegisterAnno: device.MarshalNodeDevices([]*device.DeviceInfo{
				{ID: "GPU-0", Count: 10, Devmem: 8192, Devcore: 100, Type: "NVIDIA-A100", Health: true, Mode: "hami-core"},
			}),
			registerAnno: device.MarshalNodeDevices([]*device.DeviceInfo{
				{ID: "GPU-0", Count: 10, Devmem: 8192, Devcore: 100, Type: "NVIDIA-A100", Health: false, Mode: "hami-core"},
			}),
			wantHealthy:   true,
			wantNeedReset: true,
		},
		{
			name:                "current>0 reported same: unchanged register annotation keeps cache",
			current:             4,
			reported:            4,
			handshakeAnnotation: "Requesting_" + pastTime,
			cachedRegisterAnno: device.MarshalNodeDevices([]*device.DeviceInfo{
				{ID: "GPU-0", Count: 10, Devmem: 8192, Devcore: 100, Type: "NVIDIA-A100", Health: true, Mode: "hami-core"},
			}),
			registerAnno: device.MarshalNodeDevices([]*device.DeviceInfo{
				{ID: "GPU-0", Count: 10, Devmem: 8192, Devcore: 100, Type: "NVIDIA-A100", Health: true, Mode: "hami-core"},
			}),
			wantHealthy:   true,
			wantNeedReset: false,
		},
		{
			name:                "current>0 reported same: deleted register annotation requests refresh",
			current:             4,
			reported:            4,
			handshakeAnnotation: "Requesting_" + pastTime,
			cachedRegisterAnno: device.MarshalNodeDevices([]*device.DeviceInfo{
				{ID: "GPU-0", Count: 10, Devmem: 8192, Devcore: 100, Type: "NVIDIA-A100", Health: true, Mode: "hami-core"},
			}),
			registerAnno:  "",
			wantHealthy:   true,
			wantNeedReset: true,
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
			if tt.registerAnno != "" {
				node.Annotations[RegisterAnnos] = tt.registerAnno
			}

			if tt.reported > 0 {
				dev.ReportedGPUNum["test-node"] = tt.reported
			}
			if tt.cachedRegisterAnno != "" {
				dev.ReportedRegisterAnnos["test-node"] = tt.cachedRegisterAnno
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
			name: "gpu count + memory percentage above 100 — clamped to 100",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu":               *resource.NewQuantity(1, resource.BinarySI),
						"nvidia.com/gpumem-percentage": *resource.NewQuantity(150, resource.DecimalSI),
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
			name: "gpu count + memory percentage beyond int32 range — clamped to 100 without wrapping",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu":               *resource.NewQuantity(1, resource.BinarySI),
						"nvidia.com/gpumem-percentage": *resource.NewQuantity(int64(1)<<32+50, resource.DecimalSI),
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
			name: "gpu count + memory percentage equal to sentinel 101 — clamped to 100",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu":               *resource.NewQuantity(1, resource.BinarySI),
						"nvidia.com/gpumem-percentage": *resource.NewQuantity(101, resource.DecimalSI),
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
			name: "gpu count + explicit cores 0",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu":      *resource.NewQuantity(1, resource.BinarySI),
						"nvidia.com/gpucores": *resource.NewQuantity(0, resource.DecimalSI),
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
			name: "gpu count + explicit cores 50",
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
			name: "gpu count + explicit cores 100",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu":      *resource.NewQuantity(1, resource.BinarySI),
						"nvidia.com/gpucores": *resource.NewQuantity(100, resource.DecimalSI),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             1,
				Type:             NvidiaGPUDevice,
				Memreq:           0,
				MemPercentagereq: 100,
				Coresreq:         100,
			},
		},
		{
			name: "gpu count + explicit cores 101 — rejected",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu":      *resource.NewQuantity(1, resource.BinarySI),
						"nvidia.com/gpucores": *resource.NewQuantity(101, resource.DecimalSI),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "gpu count + explicit cores 150 — rejected",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu":      *resource.NewQuantity(1, resource.BinarySI),
						"nvidia.com/gpucores": *resource.NewQuantity(150, resource.DecimalSI),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "gpu count + explicit cores 200 — rejected",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu":      *resource.NewQuantity(1, resource.BinarySI),
						"nvidia.com/gpucores": *resource.NewQuantity(200, resource.DecimalSI),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "gpu count + explicit cores negative -1 — rejected",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu":      *resource.NewQuantity(1, resource.BinarySI),
						"nvidia.com/gpucores": *resource.NewQuantity(-1, resource.DecimalSI),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "gpu count + explicit cores negative -50 — rejected",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu":      *resource.NewQuantity(1, resource.BinarySI),
						"nvidia.com/gpucores": *resource.NewQuantity(-50, resource.DecimalSI),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
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
		{
			name: "gpu count + memory percentage of 0 — treated as unset",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu":               *resource.NewQuantity(1, resource.BinarySI),
						"nvidia.com/gpumem-percentage": *resource.NewQuantity(0, resource.DecimalSI),
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
			name: "memory percentage of 0 in Requests — treated as unset",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						"nvidia.com/gpu":               *resource.NewQuantity(1, resource.BinarySI),
						"nvidia.com/gpumem-percentage": *resource.NewQuantity(0, resource.DecimalSI),
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
			name: "negative memory percentage — treated as unset",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu":               *resource.NewQuantity(1, resource.BinarySI),
						"nvidia.com/gpumem-percentage": *resource.NewQuantity(-1, resource.DecimalSI),
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
			name: "memory percentage of 0 + explicit memory — explicit memory wins",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu":               *resource.NewQuantity(1, resource.BinarySI),
						"nvidia.com/gpumem":            *resource.NewQuantity(2000, resource.DecimalSI),
						"nvidia.com/gpumem-percentage": *resource.NewQuantity(0, resource.DecimalSI),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             1,
				Type:             NvidiaGPUDevice,
				Memreq:           2000,
				MemPercentagereq: 101,
				Coresreq:         0,
			},
		},
		{
			name: "byte-scale memory quantity that overflows int32 is rejected",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu":    *resource.NewQuantity(1, resource.BinarySI),
						"nvidia.com/gpumem": resource.MustParse("16Gi"),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "decimal-form memory request is rejected, not treated as zero",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu":    *resource.NewQuantity(1, resource.BinarySI),
						"nvidia.com/gpumem": resource.MustParse("16.0Gi"),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "gpu count zero is rejected",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu": *resource.NewQuantity(0, resource.BinarySI),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "negative gpu count is rejected",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu": *resource.NewQuantity(-1, resource.BinarySI),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "gpu count above int32 max is rejected",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu": *resource.NewQuantity(
							2147483648,
							resource.BinarySI,
						),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
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

	ctr.Resources.Limits["nvidia.com/gpumem"] = resource.MustParse("1Gi")
	result = dev.GenerateResourceRequests(ctr)
	assert.DeepEqual(t, result, device.ContainerDeviceRequest{})
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

	// a percentage of 0 is unset, so it lands on defaultMemory just like nvidia.com/gpumem: 0
	ctr.Resources.Limits["nvidia.com/gpumem-percentage"] = *resource.NewQuantity(0, resource.DecimalSI)
	result = dev.GenerateResourceRequests(ctr)
	assert.Equal(t, result.Memreq, int32(512))
	assert.Equal(t, result.MemPercentagereq, int32(101))

	delete(ctr.Resources.Limits, "nvidia.com/gpumem-percentage")
	ctr.Resources.Limits["nvidia.com/gpumem"] = *resource.NewQuantity(0, resource.DecimalSI)
	control := dev.GenerateResourceRequests(ctr)
	assert.DeepEqual(t, result, control)
}

// A gpumem-percentage of 0 used to reach Fit as memreq=0: the pod fitted any card and was booked
// with 0 memory, so the device plugin injected CUDA_DEVICE_MEMORY_LIMIT=0m ("no limit").
func TestZeroMemoryPercentageIsAccountedAsWholeCard(t *testing.T) {
	config := NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
	}
	dev := InitNvidiaDevice(config)
	ctr := &corev1.Container{
		Name: "test",
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				"nvidia.com/gpu":               *resource.NewQuantity(1, resource.BinarySI),
				"nvidia.com/gpumem-percentage": *resource.NewQuantity(0, resource.DecimalSI),
			},
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "zero-percentage", Namespace: "zero-percentage-ns"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{*ctr}},
	}
	newCard := func(usedmem int32) []*device.DeviceUsage {
		return []*device.DeviceUsage{{
			ID: "dev-0", Index: 0, Used: 0, Count: 10,
			Usedmem: usedmem, Totalmem: 11264, Totalcore: 100, Usedcores: 0,
			Type: NvidiaGPUDevice, Health: true,
		}}
	}
	req := dev.GenerateResourceRequests(ctr)

	fit, result, reason := dev.Fit(newCard(0), req, pod, &device.NodeInfo{}, &device.PodDevices{})
	assert.Assert(t, fit, "empty card should fit, reason: %s", reason)
	// must book the whole card, not 0
	assert.Equal(t, int32(11264), result[NvidiaGPUDevice][0].Usedmem)

	fit, _, reason = dev.Fit(newCard(11260), req, pod, &device.NodeInfo{}, &device.PodDevices{})
	assert.Assert(t, !fit, "an almost full card must not fit")
	assert.Assert(t, strings.Contains(reason, "CardInsufficientMemory"), "reason: %s", reason)
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
	_, exists := updated.Annotations[HandshakeAnnos]
	assert.Assert(t, !exists, "expected annotation to be removed, but it still exists")
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
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{{Name: "init-no-gpu"}},
					Containers:     []corev1.Container{{Name: "app"}},
				},
			},
			hasLock: false,
		},
		{
			name: "regular-container-only GPU request — acquires lock",
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
		{
			name: "init-container-only GPU request — acquires lock",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-init-gpu", Namespace: "default"},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{{
						Name: "gpu-init",
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								"nvidia.com/gpu": *resource.NewQuantity(1, resource.BinarySI),
							},
						},
					}},
					Containers: []corev1.Container{{
						Name: "cpu-app",
					}},
				},
			},
			hasLock: true,
		},
		{
			name: "init and regular container GPU request — acquires lock",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-init-and-reg-gpu", Namespace: "default"},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{{
						Name: "gpu-init",
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								"nvidia.com/gpu": *resource.NewQuantity(1, resource.BinarySI),
							},
						},
					}},
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

func TestMutateAdmissionIsIdempotent(t *testing.T) {
	tests := []struct {
		name string
		dev  *NvidiaGPUDevices
		ctr  *corev1.Container
	}{
		{
			name: "priority and core policy",
			dev: &NvidiaGPUDevices{config: NvidiaConfig{
				ResourceCountName:            "nvidia.com/gpu",
				ResourceMemoryName:           "nvidia.com/gpumem",
				ResourceCoreName:             "nvidia.com/gpucores",
				ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
				ResourcePriority:             "nvidia.com/priority",
				GPUCorePolicy:                ForceCorePolicy,
			}},
			ctr: &corev1.Container{
				Env: []corev1.EnvVar{{Name: "EXISTING", Value: "value"}},
				Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
					"nvidia.com/gpu":      resource.MustParse("1"),
					"nvidia.com/priority": resource.MustParse("5"),
				}},
			},
		},
		{
			name: "overwrite visible devices preserves conflicting user value",
			dev: &NvidiaGPUDevices{config: NvidiaConfig{
				ResourceCountName:            "nvidia.com/gpu",
				ResourceMemoryName:           "nvidia.com/gpumem",
				ResourceCoreName:             "nvidia.com/gpucores",
				ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
				OverwriteEnv:                 true,
			}},
			ctr: &corev1.Container{
				Env:       []corev1.EnvVar{{Name: "NVIDIA_VISIBLE_DEVICES", Value: "all"}},
				Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pod := &corev1.Pod{}
			_, err := test.dev.MutateAdmission(test.ctr, pod)
			assert.NilError(t, err)
			afterFirstMutation := test.ctr.DeepCopy()

			_, err = test.dev.MutateAdmission(test.ctr, pod)
			assert.NilError(t, err)
			assert.DeepEqual(t, test.ctr, afterFirstMutation)
		})
	}
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

func TestCheckGPUtype_EmptyAnnotation(t *testing.T) {
	// An empty use/nouse type annotation means "no constraint": strings.Contains
	// treats "" as a substring of every card type, so without a guard an empty
	// nouse-gputype would wrongly exclude every device.
	assert.Equal(t, checkGPUtype(map[string]string{GPUInUse: ""}, "NVIDIA-A100"), true)
	assert.Equal(t, checkGPUtype(map[string]string{GPUInUse: "   "}, "NVIDIA-A100"), true)
	assert.Equal(t, checkGPUtype(map[string]string{GPUNoUse: ""}, "NVIDIA-A100"), true)
	assert.Equal(t, checkGPUtype(map[string]string{GPUNoUse: "   "}, "NVIDIA-A100"), true)
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

func TestGetNodeDevices_MigProfilesFromNode(t *testing.T) {
	dev := &NvidiaGPUDevices{}
	node := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-mig",
			Annotations: map[string]string{
				RegisterAnnos: `[{"id":"GPU-0","count":7,"devmem":40960,"devcore":100,"type":"NVIDIA-A100-SXM4-40GB","numa":0,"health":true,"mode":"mig","migProfiles":[{"name":"1g.5gb","memoryMB":5120,"core":14,"sliceCount":1,"instanceCount":7,"multiprocessorCount":14,"placements":[{"start":6,"size":1}]}]}]`,
			},
		},
	}
	result, err := dev.GetNodeDevices(node)
	assert.NilError(t, err)
	assert.Equal(t, len(result), 1)
	assert.Equal(t, len(result[0].MIGProfiles), 1)
	assert.Equal(t, result[0].MIGProfiles[0].Name, "1g.5gb")
	assert.DeepEqual(t, result[0].MIGProfiles[0].Placements, []device.MigPlacement{{Start: 6, Size: 1}})
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
		lockVal string
		hasLock bool
	}{
		{
			name: "no GPU containers — skip release, lock remains",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-no-gpu", Namespace: "default"},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{{Name: "init-no-gpu"}},
					Containers:     []corev1.Container{{Name: "app"}},
				},
			},
			lockVal: "lock-values,default,pod-no-gpu",
			hasLock: true,
		},
		{
			name: "regular-container-only GPU request — releases lock",
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
			lockVal: "lock-values,default,pod-gpu",
			hasLock: false,
		},
		{
			name: "init-container-only GPU request — releases lock",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-init-gpu", Namespace: "default"},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{{
						Name: "gpu-init",
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								"nvidia.com/gpu": *resource.NewQuantity(1, resource.BinarySI),
							},
						},
					}},
					Containers: []corev1.Container{{Name: "cpu-app"}},
				},
			},
			lockVal: "lock-values,default,pod-init-gpu",
			hasLock: false,
		},
		{
			name: "init and regular container GPU request — releases lock",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-init-reg-gpu", Namespace: "default"},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{{
						Name: "gpu-init",
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								"nvidia.com/gpu": *resource.NewQuantity(1, resource.BinarySI),
							},
						},
					}},
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
			lockVal: "lock-values,default,pod-init-reg-gpu",
			hasLock: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client.KubeClient = fake.NewClientset()
			lockVal := tt.lockVal
			if lockVal == "" {
				lockVal = "lock-values,default," + tt.pod.Name
			}
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Annotations: map[string]string{
						nodelock.NodeLockKey: lockVal,
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

func TestFit_MutexPolicy(t *testing.T) {
	nv := InitNvidiaDevice(NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
	})
	devices := []*device.DeviceUsage{
		{ID: "used", Index: 0, Used: 1, Count: 10, Totalmem: 8192, Totalcore: 100, Type: NvidiaGPUDevice, Health: true},
		{ID: "idle", Index: 1, Used: 0, Count: 10, Totalmem: 8192, Totalcore: 100, Type: NvidiaGPUDevice, Health: true},
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Annotations: map[string]string{util.GPUSchedulerPolicyAnnotationKey: util.GPUSchedulerPolicyMutex.String()},
	}}

	// mutex skips the used device and allocates only the idle one.
	one := device.ContainerDeviceRequest{Nums: 1, Memreq: 100, Coresreq: 10, Type: NvidiaGPUDevice}
	fit, result, _ := nv.Fit(devices, one, pod, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, true)
	assert.Equal(t, len(result[NvidiaGPUDevice]), 1)
	assert.Equal(t, result[NvidiaGPUDevice][0].UUID, "idle")

	// mutex cannot satisfy 2 cards when only one device is idle.
	two := device.ContainerDeviceRequest{Nums: 2, Memreq: 100, Coresreq: 10, Type: NvidiaGPUDevice}
	fit, _, _ = nv.Fit(devices, two, pod, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, false)
}

func TestFit_MigPercentageRequestRejectsUndersizedTemplate(t *testing.T) {
	config := NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
	}
	nv := InitNvidiaDevice(config)

	// The only MIG template offers 1024MiB slots, but the pod requests 4096MiB (50% of 8192MiB) via MemPercentagereq.
	devices := []*device.DeviceUsage{
		{
			ID: "dev-0", Index: 0, Used: 0, Count: 1,
			Totalmem: 8192, Totalcore: 100, Type: NvidiaGPUDevice, Health: true,
			Mode: MigMode,
			MigTemplate: []device.Geometry{
				{
					{Name: "1g.5gb", Memory: 1024, Core: 14, Count: 1},
				},
			},
		},
	}
	req := device.ContainerDeviceRequest{Nums: 1, MemPercentagereq: 50, Coresreq: 10, Type: NvidiaGPUDevice}
	fit, _, _ := nv.Fit(devices, req, &corev1.Pod{}, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, false)
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

func TestFit_TopologyBestCombinationZeroScores(t *testing.T) {
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
				{ID: "dev-0", DevicePairScore: device.DevicePairScore{Scores: map[string]int{"dev-1": 0, "dev-2": 0}}},
				{ID: "dev-1", DevicePairScore: device.DevicePairScore{Scores: map[string]int{"dev-0": 0, "dev-2": 0}}},
				{ID: "dev-2", DevicePairScore: device.DevicePairScore{Scores: map[string]int{"dev-0": 0, "dev-1": 0}}},
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
}

func TestFit_TopologyNegativeScores(t *testing.T) {
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
				{ID: "dev-0", DevicePairScore: device.DevicePairScore{Scores: map[string]int{"dev-1": -5, "dev-2": -3}}},
				{ID: "dev-1", DevicePairScore: device.DevicePairScore{Scores: map[string]int{"dev-0": -5, "dev-2": -1}}},
				{ID: "dev-2", DevicePairScore: device.DevicePairScore{Scores: map[string]int{"dev-0": -3, "dev-1": -1}}},
			},
		},
	}

	// Test single-GPU request (exercises computeWorstSingleCard)
	t.Run("SingleGPU", func(t *testing.T) {
		req := device.ContainerDeviceRequest{Nums: 1, Memreq: 100, Coresreq: 10, Type: NvidiaGPUDevice}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{util.GPUSchedulerPolicyAnnotationKey: util.GPUSchedulerPolicyTopology.String()},
			},
		}
		fit, result, _ := nv.Fit(devices, req, pod, nodeInfo, &device.PodDevices{})
		assert.Equal(t, fit, true)
		assert.Equal(t, len(result[NvidiaGPUDevice]), 1)
		assert.Equal(t, result[NvidiaGPUDevice][0].UUID, "dev-0")
	})

	// Test multi-GPU request (exercises computeBestCombination)
	t.Run("MultiGPU", func(t *testing.T) {
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
		assert.Assert(t, uuids["dev-1"])
		assert.Assert(t, uuids["dev-2"])
	})
}

// TestNodeDeleted_ClearsBookkeepingForDeletedNode verifies that NodeDeleted
// removes both ReportedGPUNum and ReportedRegisterAnnos entries for the
// named node while leaving other nodes' entries intact.
func TestNodeDeleted_ClearsBookkeepingForDeletedNode(t *testing.T) {
	dev := InitNvidiaDevice(NvidiaConfig{ResourceCountName: "nvidia.com/gpu"})

	// Populate bookkeeping for two nodes.
	dev.mu.Lock()
	dev.ReportedGPUNum["node-a"] = 4
	dev.ReportedGPUNum["node-b"] = 2
	dev.ReportedRegisterAnnos["node-a"] = "anno-a"
	dev.ReportedRegisterAnnos["node-b"] = "anno-b"
	dev.mu.Unlock()

	// Delete node-a.
	dev.NodeDeleted("node-a")

	dev.mu.Lock()
	defer dev.mu.Unlock()

	// node-a entries must be gone.
	_, gpuPresent := dev.ReportedGPUNum["node-a"]
	assert.Assert(t, !gpuPresent, "expected ReportedGPUNum entry for node-a to be removed")
	_, annoPresent := dev.ReportedRegisterAnnos["node-a"]
	assert.Assert(t, !annoPresent, "expected ReportedRegisterAnnos entry for node-a to be removed")

	// node-b must be untouched.
	assert.Equal(t, dev.ReportedGPUNum["node-b"], int64(2), "ReportedGPUNum for node-b must be unchanged")
	assert.Equal(t, dev.ReportedRegisterAnnos["node-b"], "anno-b", "ReportedRegisterAnnos for node-b must be unchanged")
}

// TestNodeDeleted_IdempotentOnUnknownNode verifies that calling NodeDeleted
// for a node that was never registered does not panic or error.
func TestNodeDeleted_IdempotentOnUnknownNode(t *testing.T) {
	dev := InitNvidiaDevice(NvidiaConfig{ResourceCountName: "nvidia.com/gpu"})
	// Should not panic.
	dev.NodeDeleted("node-never-registered")
	dev.mu.Lock()
	defer dev.mu.Unlock()
	assert.Equal(t, len(dev.ReportedGPUNum), 0)
	assert.Equal(t, len(dev.ReportedRegisterAnnos), 0)
}

// TestNodeDeleted_ReuseAfterDeletion verifies that after a node is deleted its
// bookkeeping is cleared so that a subsequent CheckHealth call for a node with
// the same name (i.e. re-created node) is not hampered by stale state.
func TestNodeDeleted_ReuseAfterDeletion(t *testing.T) {
	config := NvidiaConfig{ResourceCountName: "nvidia.com/gpu"}
	dev := InitNvidiaDevice(config)

	allocatable := corev1.ResourceList{
		corev1.ResourceName(config.ResourceCountName): *resource.NewQuantity(4, resource.DecimalSI),
	}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "reused-node",
			Annotations: map[string]string{
				util.HandshakeAnnos[NvidiaGPUDevice]: "Heartbeat_2099-01-01T00:00:00Z",
			},
		},
		Status: corev1.NodeStatus{Allocatable: allocatable},
	}

	// Simulate a prior registration cycle: CheckHealth establishes bookkeeping.
	dev.mu.Lock()
	dev.ReportedGPUNum["reused-node"] = 4
	dev.ReportedRegisterAnnos["reused-node"] = "old-anno"
	dev.mu.Unlock()

	// Node deleted — bookkeeping must be wiped.
	dev.NodeDeleted("reused-node")

	// After re-creation the new CheckHealth must detect a change (needUpdate=true)
	// because reported=0 (after deletion) != current=4.
	healthy, needUpdate := dev.CheckHealth("NVIDIA", node)
	assert.Equal(t, healthy, true, "re-created node should be healthy")
	assert.Equal(t, needUpdate, true, "re-created node must trigger an update (stale bookkeeping was cleared)")
}

func TestFit_CoresValidation(t *testing.T) {
	dev := InitNvidiaDevice(NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
	})
	devices := []*device.DeviceUsage{
		{
			ID:        "GPU-1",
			Index:     0,
			Count:     10,
			Totalmem:  8000,
			Totalcore: 100,
			Used:      0,
			Usedmem:   0,
			Usedcores: 0,
			Health:    true,
			Type:      NvidiaGPUDevice,
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
		{
			name:     "negative cores -50 is rejected",
			coresreq: -50,
			wantOk:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := device.ContainerDeviceRequest{
				Nums:     1,
				Type:     NvidiaGPUDevice,
				Memreq:   1000,
				Coresreq: tt.coresreq,
			}
			ok, _, _ := dev.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
			assert.Equal(t, ok, tt.wantOk)
		})
	}
}

func TestMutateAdmission_CoresValidation_InvalidCores(t *testing.T) {
	dev := InitNvidiaDevice(NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
	})

	tests := []struct {
		name      string
		cores     string
		wantValid bool
	}{
		{name: "0 cores accepted", cores: "0", wantValid: true},
		{name: "50 cores accepted", cores: "50", wantValid: true},
		{name: "100 cores accepted", cores: "100", wantValid: true},
		{name: "-1 cores rejected", cores: "-1", wantValid: false},
		{name: "101 cores rejected", cores: "101", wantValid: false},
		{name: "50m cores rejected", cores: "50m", wantValid: false},
		{name: "99.1 cores rejected", cores: "99.1", wantValid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctr := &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu":      resource.MustParse("1"),
						"nvidia.com/gpucores": resource.MustParse(tt.cores),
					},
				},
			}
			_, err := dev.MutateAdmission(ctr, &corev1.Pod{})
			if tt.wantValid {
				assert.NilError(t, err)
			} else {
				assert.ErrorContains(t, err, "must be an integer between 0 and 100")
			}
		})
	}
}

func Test_GenerateResourceRequests_CoresValidation(t *testing.T) {
	dev := InitNvidiaDevice(NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
	})

	tests := []struct {
		name    string
		cores   string
		wantOk  bool
		wantVal int32
	}{
		{name: "0 cores", cores: "0", wantOk: true, wantVal: 0},
		{name: "50 cores", cores: "50", wantOk: true, wantVal: 50},
		{name: "100 cores", cores: "100", wantOk: true, wantVal: 100},
		{name: "101 cores", cores: "101", wantOk: false},
		{name: "150 cores", cores: "150", wantOk: false},
		{name: "200 cores", cores: "200", wantOk: false},
		{name: "-1 cores", cores: "-1", wantOk: false},
		{name: "fractional 50m", cores: "50m", wantOk: false},
		{name: "fractional 99.1", cores: "99.1", wantOk: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctr := &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu":      resource.MustParse("1"),
						"nvidia.com/gpucores": resource.MustParse(tt.cores),
					},
				},
			}
			req := dev.GenerateResourceRequests(ctr)
			if tt.wantOk {
				assert.Equal(t, req.Nums, int32(1))
				assert.Equal(t, req.Coresreq, tt.wantVal)
			} else {
				assert.Equal(t, req.Nums, int32(0))
			}
		})
	}
}

// migTopologyDevice builds the NVIDIA backend used by the MIG topology tests.
func migTopologyDevice() *NvidiaGPUDevices {
	return InitNvidiaDevice(NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
	})
}

// migTopologyDevices builds MIG cards with seven single-slice placements each,
// so the Fit loop contributes seven interchangeable candidates per card.
func migTopologyDevices(ids ...string) []*device.DeviceUsage {
	placements := make([]device.MigPlacement, 0, 7)
	for start := range uint32(7) {
		placements = append(placements, device.MigPlacement{Start: start, Size: 1})
	}
	devices := make([]*device.DeviceUsage, 0, len(ids))
	for idx, id := range ids {
		devices = append(devices, &device.DeviceUsage{
			ID: id, Index: uint(idx), Used: 0, Count: 7,
			Totalmem: 81920, Totalcore: 100, Type: NvidiaGPUDevice, Health: true,
			Mode: MigMode,
			MigProfiles: []device.MigProfile{
				{Name: "1g.10gb", MemoryMB: 10240, Core: 14, SliceCount: 1, Placements: placements},
			},
		})
	}
	return devices
}

// migTopologyNodeInfo registers cards with no pair score, as on a node where
// hami.io/node-nvidia-score was never published.
func migTopologyNodeInfo(ids ...string) *device.NodeInfo {
	infos := make([]device.DeviceInfo, 0, len(ids))
	for _, id := range ids {
		infos = append(infos, device.DeviceInfo{ID: id})
	}
	return &device.NodeInfo{Devices: map[string][]device.DeviceInfo{NvidiaGPUDevice: infos}}
}

func migTopologyPod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{util.GPUSchedulerPolicyAnnotationKey: util.GPUSchedulerPolicyTopology.String()},
		},
	}
}

func distinctUUIDs(devices device.ContainerDevices) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(devices))
	for _, dev := range devices {
		if _, ok := seen[dev.UUID]; ok {
			continue
		}
		seen[dev.UUID] = struct{}{}
		out = append(out, dev.UUID)
	}
	return out
}

// Without pair scores every combination ties at zero, so the first one generated
// wins; that used to be several slots of one card with the others left idle.
func TestFit_TopologyMigSpreadsAcrossCardsWithoutScores(t *testing.T) {
	nv := migTopologyDevice()
	devices := migTopologyDevices("dev-0", "dev-1", "dev-2")
	nodeInfo := migTopologyNodeInfo("dev-0", "dev-1", "dev-2")

	req := device.ContainerDeviceRequest{Nums: 2, Memreq: 10240, Coresreq: 10, Type: NvidiaGPUDevice}
	fit, result, msg := nv.Fit(devices, req, migTopologyPod(), nodeInfo, &device.PodDevices{})

	assert.Equal(t, fit, true, msg)
	assert.Equal(t, len(result[NvidiaGPUDevice]), 2)
	assert.Equal(t, len(distinctUUIDs(result[NvidiaGPUDevice])), 2,
		"a 2-GPU request must land on two physical cards, got %v", distinctUUIDs(result[NvidiaGPUDevice]))
}

// The reported scenario: eight MIG cards, seven slots each, five GPUs requested.
// The 56-entry pool enumerated C(56,5) and still packed every slot onto one card.
func TestFit_TopologyMigSpreadsLargeRequest(t *testing.T) {
	ids := []string{"dev-0", "dev-1", "dev-2", "dev-3", "dev-4", "dev-5", "dev-6", "dev-7"}
	nv := migTopologyDevice()
	devices := migTopologyDevices(ids...)
	nodeInfo := migTopologyNodeInfo(ids...)

	req := device.ContainerDeviceRequest{Nums: 5, Memreq: 10240, Coresreq: 10, Type: NvidiaGPUDevice}
	fit, result, msg := nv.Fit(devices, req, migTopologyPod(), nodeInfo, &device.PodDevices{})

	assert.Equal(t, fit, true, msg)
	assert.Equal(t, len(result[NvidiaGPUDevice]), 5)
	assert.Equal(t, len(distinctUUIDs(result[NvidiaGPUDevice])), 5,
		"a 5-GPU request must land on five physical cards, got %v", distinctUUIDs(result[NvidiaGPUDevice]))
}

// Collapsing the pool must not change which cards topology scoring prefers: the
// best-connected pair still wins when the node does publish pair scores.
func TestFit_TopologyMigBestCombinationWithScores(t *testing.T) {
	nv := migTopologyDevice()
	devices := migTopologyDevices("dev-0", "dev-1", "dev-2")
	nodeInfo := &device.NodeInfo{
		Devices: map[string][]device.DeviceInfo{
			NvidiaGPUDevice: {
				{ID: "dev-0", DevicePairScore: device.DevicePairScore{Scores: map[string]int{"dev-1": 100, "dev-2": 200}}},
				{ID: "dev-1", DevicePairScore: device.DevicePairScore{Scores: map[string]int{"dev-0": 100, "dev-2": 150}}},
				{ID: "dev-2", DevicePairScore: device.DevicePairScore{Scores: map[string]int{"dev-0": 200, "dev-1": 150}}},
			},
		},
	}

	req := device.ContainerDeviceRequest{Nums: 2, Memreq: 10240, Coresreq: 10, Type: NvidiaGPUDevice}
	fit, result, msg := nv.Fit(devices, req, migTopologyPod(), nodeInfo, &device.PodDevices{})

	assert.Equal(t, fit, true, msg)
	assert.Equal(t, len(result[NvidiaGPUDevice]), 2)
	uuids := distinctUUIDs(result[NvidiaGPUDevice])
	assert.Equal(t, len(uuids), 2, "got %v", uuids)
	// dev-0 <-> dev-2 scores 200, the highest pair on this node.
	assert.Assert(t, uuids[0] == "dev-0" || uuids[1] == "dev-0", "expected dev-0 in %v", uuids)
	assert.Assert(t, uuids[0] == "dev-2" || uuids[1] == "dev-2", "expected dev-2 in %v", uuids)
}

// The single-GPU path still picks the least-connected card: duplicates scaled
// every card's total by its slot count, which left the ranking intact.
func TestFit_TopologyMigWorstSingleCard(t *testing.T) {
	nv := migTopologyDevice()
	devices := migTopologyDevices("dev-0", "dev-1", "dev-2")
	nodeInfo := &device.NodeInfo{
		Devices: map[string][]device.DeviceInfo{
			NvidiaGPUDevice: {
				{ID: "dev-0", DevicePairScore: device.DevicePairScore{Scores: map[string]int{"dev-1": 100, "dev-2": 200}}},
				{ID: "dev-1", DevicePairScore: device.DevicePairScore{Scores: map[string]int{"dev-0": 100, "dev-2": 150}}},
				{ID: "dev-2", DevicePairScore: device.DevicePairScore{Scores: map[string]int{"dev-0": 200, "dev-1": 150}}},
			},
		},
	}

	req := device.ContainerDeviceRequest{Nums: 1, Memreq: 10240, Coresreq: 10, Type: NvidiaGPUDevice}
	fit, result, msg := nv.Fit(devices, req, migTopologyPod(), nodeInfo, &device.PodDevices{})

	assert.Equal(t, fit, true, msg)
	assert.Equal(t, len(result[NvidiaGPUDevice]), 1)
	// dev-1 totals 250, below dev-0 (300) and dev-2 (350).
	assert.Equal(t, result[NvidiaGPUDevice][0].UUID, "dev-1")
}

// With fewer cards than requested GPUs the full pool is kept, so MIG instances
// on one card still satisfy the request instead of it being rejected.
func TestFit_TopologyMigPacksOneCardWhenNoAlternative(t *testing.T) {
	nv := migTopologyDevice()
	devices := migTopologyDevices("dev-0")
	nodeInfo := migTopologyNodeInfo("dev-0")

	req := device.ContainerDeviceRequest{Nums: 2, Memreq: 10240, Coresreq: 10, Type: NvidiaGPUDevice}
	fit, result, msg := nv.Fit(devices, req, migTopologyPod(), nodeInfo, &device.PodDevices{})

	assert.Equal(t, fit, true, msg)
	assert.Equal(t, len(result[NvidiaGPUDevice]), 2)
	assert.Equal(t, len(distinctUUIDs(result[NvidiaGPUDevice])), 1,
		"the only card on the node must supply both instances")
}

func TestDistinctCardCandidates(t *testing.T) {
	tests := []struct {
		name       string
		candidates device.ContainerDevices
		want       []string
	}{
		{
			name: "collapses repeated cards and keeps first-seen order",
			candidates: device.ContainerDevices{
				{UUID: "dev-2", Idx: 2}, {UUID: "dev-2", Idx: 2},
				{UUID: "dev-0", Idx: 0}, {UUID: "dev-2", Idx: 2},
				{UUID: "dev-1", Idx: 1}, {UUID: "dev-0", Idx: 0},
			},
			want: []string{"dev-2", "dev-0", "dev-1"},
		},
		{
			name:       "already distinct is unchanged",
			candidates: device.ContainerDevices{{UUID: "dev-0"}, {UUID: "dev-1"}},
			want:       []string{"dev-0", "dev-1"},
		},
		{
			name:       "empty stays empty",
			candidates: device.ContainerDevices{},
			want:       []string{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := distinctCardCandidates(tc.candidates)
			assert.Equal(t, len(got), len(tc.want))
			for i := range tc.want {
				assert.Equal(t, got[i].UUID, tc.want[i])
			}
		})
	}
}
