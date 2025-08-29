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
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/util"
	"gopkg.in/yaml.v2"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

func Test_InitDevices(t *testing.T) {
	tests := []struct {
		name string
		args []VGPUConfig
		want []*Devices
	}{
		{
			name: "test with vaild configuration",
			args: []VGPUConfig{
				{
					ChipName:           "iluvatar mr-v100",
					CommonWord:         "mrv100",
					ResourceName:       "iluvatar.ai/mrv100",
					ResourceMemoryName: "iluvatar.ai/mrv100-memory",
				},
			},
			want: []*Devices{
				{
					config: VGPUConfig{
						ChipName:           "iluvatar mr-v100",
						CommonWord:         "mrv100",
						ResourceName:       "iluvatar.ai/mrv100",
						ResourceMemoryName: "iluvatar.ai/mrv100-memory",
					},
					nodeRegisterAnno: "hami.io/node-register-mrv100",
					useUUIDAnno:      "hami.io/use-mrv100-uuid",
					noUseUUIDAnno:    "hami.io/no-use-mrv100-uuid",
					handshakeAnno:    "hami.io/node-handshake-mrv100",
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			devices := InitDevices(test.args)
			assert.Equal(t, len(devices), len(test.want), "Expected length of result to match want")
			for k, v := range devices {
				assert.Equal(t, v, devices[k], "load iluvatar vgpu config %s: %v", devices[k].config.CommonWord, devices[k].config)
			}
			assert.Equal(t, "hami.io/mrv100-devices-to-allocate", util.InRequestDevices[test.args[0].CommonWord])
			assert.Equal(t, "hami.io/mrv100-devices-allocated", util.SupportDevices[test.args[0].CommonWord])
			assert.Equal(t, test.want[0].handshakeAnno, util.HandshakeAnnos[test.args[0].CommonWord])
		})
	}
}

func Test_GetNodeDevices(t *testing.T) {
	dev := Devices{
		nodeRegisterAnno: `hami.io/node-register-mrv100`,
	}
	//	dev := Devices{}
	tests := []struct {
		name string
		args corev1.Node
		want []*util.DeviceInfo
		err  error
	}{
		{
			name: "exist device",
			args: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node-01",
					Annotations: map[string]string{
						dev.nodeRegisterAnno: `GPU-0,2,128,100,mrv100,0,true,0,:`,
						//						"hami.io/node-register-mrv100": `GPU-bad51c5a-ed4c-591d-91bf-c04a12e19eae,2,128,100,mrv100,0,true,0,`,
					},
				},
			},
			want: []*util.DeviceInfo{
				{
					ID:      "GPU-0",
					Count:   int32(2),
					Devcore: int32(100),
					Devmem:  int32(128),
					Type:    "mrv100",
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
			want: []*util.DeviceInfo{},
			err:  errors.New("no device found on node"),
		},
		{
			name: "no annotation",
			args: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node-03",
				},
			},
			want: []*util.DeviceInfo{},
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
			want: []*util.DeviceInfo{},
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
	commonWord := "mrv100"
	dev := Devices{
		nodeRegisterAnno: fmt.Sprintf("hami.io/node-register-%s", commonWord),
		useUUIDAnno:      fmt.Sprintf("hami.io/use-%s-uuid", commonWord),
		noUseUUIDAnno:    fmt.Sprintf("hami.io/no-use-%s-uuid", commonWord),
		handshakeAnno:    fmt.Sprintf("hami.io/node-handshake-%s", commonWord),
		config: VGPUConfig{
			CommonWord: commonWord,
		},
	}
	util.InRequestDevices[commonWord] = fmt.Sprintf("hami.io/%s-devices-to-allocate", commonWord)
	util.SupportDevices[commonWord] = fmt.Sprintf("hami.io/%s-devices-allocated", commonWord)
	util.HandshakeAnnos[commonWord] = dev.handshakeAnno

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
					dev.config.CommonWord: util.PodSingleDevice{
						[]util.ContainerDevice{
							{
								Idx:       0,
								UUID:      "device-0",
								Type:      "mrv100",
								Usedcores: 1,
								Usedmem:   8738,
							},
						},
					},
				},
			},
			want: map[string]string{
				util.InRequestDevices[dev.config.CommonWord]: "device-0,mrv100,8738,1:;",
				util.SupportDevices[dev.config.CommonWord]:   "device-0,mrv100,8738,1:;",
				"iluvatar.ai/predicate-time":                 strconv.FormatInt(time.Now().UnixNano(), 10),
				"iluvatar.ai/predicate-gpu-idx-0":            "0",
				"iluvatar.ai/gpu-assigned":                   "false",
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
		config: VGPUConfig{
			CommonWord: "mrv100",
		},
	}
	tests := []struct {
		name string
		args struct {
			annos map[string]string
			d     util.DeviceUsage
			n     util.ContainerDeviceRequest
		}
		want bool
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
					Type: "mrv100",
				},
			},
			want: true,
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
					Type: "mrv50",
				},
			},
			want: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, result, _ := dev.CheckType(test.args.annos, test.args.d, test.args.n)
			assert.Equal(t, result, test.want)
		})
	}
}

func Test_checkUUID(t *testing.T) {
	dev := Devices{
		useUUIDAnno:   "hami.io/use-mrv100-uuid",
		noUseUUIDAnno: "hami.io/no-use-mrv100-uuid",
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
			name: "don't set GPUUseUUID,GPUNoUseUUID and annotation",
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
			name: "set GPUUseUUID,don't set GPUNoUseUUID,annotation and device match",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{
					dev.useUUIDAnno: "test123,111",
				},
				d: util.DeviceUsage{
					ID: "test123",
				},
			},
			want: true,
		},
		{
			name: "don't set GPUUseUUID, set GPUNoUseUUID,annotation and device match",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{
					dev.noUseUUIDAnno: "test123,222",
				},
				d: util.DeviceUsage{
					ID: "test123",
				},
			},
			want: false,
		},
		{
			name: "set GPUUseUUID, don't set GPUNoUseUUID,annotation and device not match",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{
					dev.useUUIDAnno: "test123,222",
				},
				d: util.DeviceUsage{
					ID: "test456",
				},
			},
			want: false,
		},
		{
			name: "don't set GPUUseUUID, set GPUNoUseUUID,annotation and device not match",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{
					dev.noUseUUIDAnno: "test123,222",
				},
				d: util.DeviceUsage{
					ID: "test456",
				},
			},
			want: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := dev.CheckUUID(test.args.annos, test.args.d)
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
				devType: "iluvatar.ai/mrv100",
				n: corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							util.HandshakeAnnos["iluvatar.ai/mrv100"]: "Requesting_2128-12-02 00:00:00",
						},
					},
				},
			},
			want1: true,
			want2: true,
		},
		{
			name: "Deleted state",
			args: struct {
				devType string
				n       corev1.Node
			}{
				devType: "iluvatar.ai/mrv100",
				n: corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							util.HandshakeAnnos["iluvatar.ai/mrv100"]: "Deleted",
						},
					},
				},
			},
			want1: true,
			want2: true,
		},
		{
			name: "Unknown state",
			args: struct {
				devType string
				n       corev1.Node
			}{
				devType: "iluvatar.ai/mrv100",
				n: corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							util.HandshakeAnnos["iluvatar.ai/mrv100"]: "Unknown",
						},
					},
				},
			},
			want1: true,
			want2: true,
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
							"iluvatar.ai/mrv100":        resource.MustParse("2"),
							"iluvatar.ai/mrv100-memory": resource.MustParse("8738"),
						},
					},
				},
				pod: corev1.Pod{},
			},
			want: true,
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
							"iluvatar.ai/mrv100":        resource.MustParse("1"),
							"iluvatar.ai/mrv100-memory": resource.MustParse("20000"),
						},
						Requests: corev1.ResourceList{
							"iluvatar.ai/mrv100-memory": resource.MustParse("20000"),
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
				config: VGPUConfig{
					ResourceName:       "iluvatar.ai/mrv100",
					ResourceMemoryName: "iluvatar.ai/mrv100-memory",
				},
			}
			result, _ := dev.MutateAdmission(&test.args.ctr, &test.args.pod)

			if result != test.want {
				t.Fatalf("exec MutateAdmission method expect return is %+v, but got is %+v", test.want, result)
			}
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
			want: util.ContainerDeviceRequest{},
		},
		{
			name: "resourcename and resourcememoryname set to limits and request",
			args: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"iluvatar.ai/mrv100":        resource.MustParse("2"),
						"iluvatar.ai/mrv100-memory": resource.MustParse("8738"),
					},
					Requests: corev1.ResourceList{
						"iluvatar.ai/mrv100":        resource.MustParse("2"),
						"iluvatar.ai/mrv100-memory": resource.MustParse("8738"),
					},
				},
			},
			want: util.ContainerDeviceRequest{
				Nums:             int32(2),
				Type:             "mrv100",
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
						"iluvatar.ai/mrv100": resource.MustParse("2"),
					},
					Requests: corev1.ResourceList{
						"iluvatar.ai/mrv100": resource.MustParse("2"),
					},
				},
			},
			want: util.ContainerDeviceRequest{
				Nums:             int32(2),
				Type:             "mrv100",
				Memreq:           int32(0),
				MemPercentagereq: int32(100),
				Coresreq:         int32(0),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := Devices{
				config: VGPUConfig{
					CommonWord:         "mrv100",
					ResourceName:       "iluvatar.ai/mrv100",
					ResourceMemoryName: "iluvatar.ai/mrv100-memory",
				},
			}
			result := dev.GenerateResourceRequests(&test.args)

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
				config: VGPUConfig{
					CommonWord:         "mrv100",
					ResourceName:       "iluvatar.ai/mr100",
					ResourceMemoryName: "iluvatar.ai/mr100-memory",
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
				config: VGPUConfig{
					CommonWord:         "mrv100",
					ResourceName:       "iluvatar.ai/mr100",
					ResourceMemoryName: "iluvatar.ai/mr100-memory",
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
	configStr := `
- chipName: Iluvatar MR-V100
  commonWord: mrv100
  resourceName: iluvatar.ai/mrv100-vgpu
  resourceMemoryName: iluvatar.ai/mrv100-memory
  resourceCoreName: iluvatar.ai/mrv100-core
- chipName: Iluvatar MR-V50
  commonWord: mrv50
  resourceName: iluvatar.ai/mrv50-vgpu
  resourceMemoryName: iluvatar.ai/mrv50-memory
  resourceCoreName: iluvatar.ai/mrv50-core
- chipName: Iluvatar BI-V150
  commonWord: biv150
  resourceName: iluvatar.ai/biv150-vgpu
  resourceMemoryName: iluvatar.ai/biv150-memory
  resourceCoreName: iluvatar.ai/biv150-core
- chipName: Iluvatar BI-V100
  commonWord: biv100
  resourceName: iluvatar.ai/biv100-vgpu
  resourceMemoryName: iluvatar.ai/biv100-memory
  resourceCoreName: iluvatar.ai/biv100-core
`

	var config []VGPUConfig
	if err := yaml.Unmarshal([]byte(configStr), &config); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}
	devs := InitDevices(config)

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
			request: util.ContainerDeviceRequest{
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
			}},
			request: util.ContainerDeviceRequest{
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
				Health:    true,
			}},
			request: util.ContainerDeviceRequest{
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
				Health:    true,
			}},
			request: util.ContainerDeviceRequest{
				Nums:             2,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
			},
			annos:      map[string]string{"hami.io/use-mrv100-uuid": "dev-0"},
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
				Health:    true,
			}},
			request: util.ContainerDeviceRequest{
				Nums:             2,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
			},
			annos:      map[string]string{"hami.io/no-use-mrv100-uuid": "dev-0"},
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
				Health:    true,
			}},
			request: util.ContainerDeviceRequest{
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
				Health:    true,
			}},
			request: util.ContainerDeviceRequest{
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
				Health:    true,
			}},
			request: util.ContainerDeviceRequest{
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
				Health:    true,
			}},
			request: util.ContainerDeviceRequest{
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
				Health:    true,
			}},
			request: util.ContainerDeviceRequest{
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
				Health:    true,
			}},
			request: util.ContainerDeviceRequest{
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
				allocated := &util.PodDevices{}
				fit, result, reason := dev.Fit(test.devices, test.request, test.annos, &corev1.Pod{}, &util.NodeInfo{}, allocated)
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
