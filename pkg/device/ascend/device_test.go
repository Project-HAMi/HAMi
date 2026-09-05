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
		{
			// Multi-chip config: allAscendResourceNames must be collected from every
			// chip and shared across all returned Devices instances, so a container
			// requesting one chip's resource is recognized as an Ascend container by
			// every other chip's MutateAdmission.
			name:         "multi-chip shares allAscendResourceNames",
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
					Templates:          []Template{{Name: "vir08", Memory: int64(8738), AICore: int32(8)}},
				},
				{
					ChipName:           "910B4",
					CommonWord:         "Ascend910B4",
					ResourceName:       "huawei.com/Ascend910B4",
					ResourceMemoryName: "huawei.com/Ascend910B4-memory",
					MemoryAllocatable:  int64(32768),
					MemoryCapacity:     int64(32768),
					AICore:             int32(20),
					Templates:          []Template{{Name: "vir08", Memory: int64(8738), AICore: int32(8)}},
				},
			},
			want: []*Devices{
				{config: VNPUConfig{ChipName: "910A", CommonWord: "Ascend910A", ResourceName: "huawei.com/Ascend910A", ResourceMemoryName: "huawei.com/Ascend910A-memory", MemoryAllocatable: int64(32768), MemoryCapacity: int64(32768), AICore: int32(30), Templates: []Template{{Name: "vir08", Memory: int64(8738), AICore: int32(8)}}}},
				{config: VNPUConfig{ChipName: "910B4", CommonWord: "Ascend910B4", ResourceName: "huawei.com/Ascend910B4", ResourceMemoryName: "huawei.com/Ascend910B4-memory", MemoryAllocatable: int64(32768), MemoryCapacity: int64(32768), AICore: int32(20), Templates: []Template{{Name: "vir08", Memory: int64(8738), AICore: int32(8)}}}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			enableAscend = test.enableAscend
			devices := InitDevices(VNPUs{Configs: test.args})
			assert.Equal(t, len(devices), len(test.want), "Expected length of result to match want")
			if enableAscend {
				for k, v := range devices {
					assert.Equal(t, v, devices[k], "load ascend vnpu config %s: %v", devices[k].config.CommonWord, devices[k].config)
				}
				// Multi-chip: every Devices instance must share the full resource-name
				// list collected from all chips, so containerRequestsAnyAscendResource
				// works across sibling chips in the webhook loop.
				if len(test.args) > 1 {
					wantNames := []corev1.ResourceName{"huawei.com/Ascend910A", "huawei.com/Ascend910B4"}
					for _, d := range devices {
						assert.DeepEqual(t, d.allAscendResourceNames, wantNames)
					}
				}
				assert.Equal(t, "hami.io/Ascend910A-devices-to-allocate", device.InRequestDevices[test.args[0].CommonWord])
				assert.Equal(t, "hami.io/Ascend910A-devices-allocated", device.SupportDevices[test.args[0].CommonWord])
				if len(test.args) == 1 && len(test.want) > 0 {
					assert.Equal(t, test.want[0].handshakeAnno, util.HandshakeAnnos[test.args[0].CommonWord])
				}
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
				"huawei.com/Ascend910A": "[{\"UUID\":\"device-0\",\"temp\":\"vir08\",\"memory\":8738,\"core\":1}]",
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

func Test_PatchAnnotations_VNPUCoreMode(t *testing.T) {
	dev := Devices{
		config: VNPUConfig{
			CommonWord:   "Ascend910B3",
			ResourceName: "huawei.com/Ascend910B3",
		},
	}

	tests := []struct {
		name string
		pod  *corev1.Pod
		pd   device.PodDevices
		want map[string]string
	}{
		{
			name: "vNPU-mode patch: check json contains cores and memory instead of template",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"huawei.com/vnpu-mode": "hami-core",
					},
				},
			},
			pd: device.PodDevices{
				"Ascend910B3": device.PodSingleDevice{
					[]device.ContainerDevice{
						{
							Idx:       0,
							UUID:      "ascend-uuid-1",
							Type:      "Ascend",
							Usedcores: 5,
							Usedmem:   8192,
						},
					},
				},
			},
			want: map[string]string{
				"huawei.com/Ascend910B3": "[{\"UUID\":\"ascend-uuid-1\",\"core\":5,\"memory\":8192}]",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			annoInput := make(map[string]string)
			result := dev.PatchAnnotations(test.pod, &annoInput, test.pd)

			val, ok := result["huawei.com/Ascend910B3"]
			assert.Assert(t, ok)
			assert.Assert(t, strings.Contains(val, "\"core\":5"))
			assert.Assert(t, strings.Contains(val, "\"memory\":8192"))
			assert.Assert(t, !strings.Contains(val, "\"temp\""))
		})
	}
}

func Test_PatchAnnotations_ZeroCore(t *testing.T) {
	dev := Devices{
		config: VNPUConfig{
			CommonWord:     "Ascend910A",
			MemoryCapacity: int64(32768),
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

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{},
		},
	}
	pd := device.PodDevices{
		"Ascend910A": device.PodSingleDevice{
			[]device.ContainerDevice{
				{
					Idx:       0,
					UUID:      "device-zero-core",
					Type:      "Ascend",
					Usedcores: 0,
					Usedmem:   8738,
				},
			},
		},
	}

	annoInput := make(map[string]string)
	result := dev.PatchAnnotations(pod, &annoInput, pd)

	val, ok := result["huawei.com/Ascend910A"]
	assert.Assert(t, ok)
	assert.Assert(t, strings.Contains(val, "\"memory\":8738"))
	assert.Assert(t, strings.Contains(val, "\"temp\":\"vir08\""))
	assert.Assert(t, !strings.Contains(val, "\"core\""))
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

func Test_MutateAdmission_NilRequests(t *testing.T) {
	// Regression test: a pod that declares only limits (no requests block)
	// must not panic when MutateAdmission writes the trimmed memory request.
	tests := []struct {
		name       string
		ctr        corev1.Container
		wantMemory string
	}{
		{
			name: "memory limit set, requests block absent",
			ctr: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"huawei.com/Ascend910A":        resource.MustParse("1"),
						"huawei.com/Ascend910A-memory": resource.MustParse("8738"),
					},
				},
			},
			wantMemory: "8738",
		},
		{
			name: "count only, no memory limit, requests block absent",
			ctr: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"huawei.com/Ascend910A": resource.MustParse("1"),
					},
				},
			},
			wantMemory: "32768", // defaults to whole-card MemoryAllocatable
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
			pod := corev1.Pod{}
			result, err := dev.MutateAdmission(&test.ctr, &pod)
			if err != nil {
				t.Fatalf("exec MutateAdmission method expect no error, but got %v", err)
			}
			if !result {
				t.Fatalf("exec MutateAdmission method expect return is true, but got is false")
			}
			got, ok := test.ctr.Resources.Requests[corev1.ResourceName("huawei.com/Ascend910A-memory")]
			if !ok {
				t.Fatalf("expect memory request to be set, but it is absent")
			}
			if got.String() != test.wantMemory {
				t.Fatalf("expect memory request %s, but got %s", test.wantMemory, got.String())
			}
		})
	}
}

func Test_MutateAdmission_EmptyResourceMemoryName(t *testing.T) {
	dev := Devices{
		config: VNPUConfig{
			ChipName:           "Ascend910A",
			CommonWord:         "Ascend910A",
			ResourceName:       "huawei.com/Ascend910A",
			ResourceMemoryName: "",
			MemoryCapacity:     0,
			MemoryAllocatable:  0,
		},
	}
	ctr := corev1.Container{
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				"huawei.com/Ascend910A": resource.MustParse("1"),
			},
		},
	}
	pod := corev1.Pod{}
	result, err := dev.MutateAdmission(&ctr, &pod)
	if err != nil {
		t.Fatalf("MutateAdmission returned unexpected error: %v", err)
	}
	if !result {
		t.Fatalf("MutateAdmission expected true, got false")
	}
	if _, ok := ctr.Resources.Limits[corev1.ResourceName("")]; ok {
		t.Fatalf("MutateAdmission should not inject empty resource name into Limits")
	}
	if _, ok := ctr.Resources.Requests[corev1.ResourceName("")]; ok {
		t.Fatalf("MutateAdmission should not inject empty resource name into Requests")
	}
}

func Test_MutateAdmission_OverwriteEnvDoesNotOverrideAscendContainer(t *testing.T) {
	// Regression: a container that requests an Ascend resource (here Ascend910B4) must
	// NOT get an empty ASCEND_VISIBLE_DEVICES injected by a sibling chip's
	// MutateAdmission. Previously each chip injected an empty value per-chip when
	// !ok, and the empty pod-spec env overrode the real value injected later by the
	// device plugin, so ascend-docker-runtime saw an empty value and skipped device
	// mounting.
	allResNames := []corev1.ResourceName{
		"huawei.com/Ascend910A",
		"huawei.com/Ascend910B4",
	}
	dev := Devices{
		config: VNPUConfig{
			ResourceName:       "huawei.com/Ascend910A",
			ResourceMemoryName: "huawei.com/Ascend910A-memory",
			MemoryAllocatable:  int64(32768),
			MemoryCapacity:     int64(32768),
			OverwriteEnv:       true,
			Templates: []Template{
				{Name: "vir08", Memory: int64(8738), AICore: int32(8)},
			},
		},
		allAscendResourceNames: allResNames,
	}
	ctr := corev1.Container{
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				"huawei.com/Ascend910B4":        resource.MustParse("1"),
				"huawei.com/Ascend910B4-memory": resource.MustParse("8192"),
			},
		},
	}
	got, err := dev.MutateAdmission(&ctr, &corev1.Pod{})
	assert.NilError(t, err)
	assert.Equal(t, got, false) // 910A device: container did not request 910A
	for _, e := range ctr.Env {
		if e.Name == "ASCEND_VISIBLE_DEVICES" {
			t.Fatalf("expected no ASCEND_VISIBLE_DEVICES env injected for ascend container, got %q", e.Value)
		}
	}
}

func Test_MutateAdmission_OverwriteEnvInjectsEmptyForNonAscendContainer(t *testing.T) {
	// A container that requests NO Ascend resource should get exactly one empty
	// ASCEND_VISIBLE_DEVICES injected (to clear image-baked values), even when
	// multiple chips' MutateAdmission run in the webhook device loop.
	allResNames := []corev1.ResourceName{
		"huawei.com/Ascend910A",
		"huawei.com/Ascend910B4",
	}
	mkDev := func(resourceName string) *Devices {
		return &Devices{
			config: VNPUConfig{
				ResourceName:       resourceName,
				ResourceMemoryName: resourceName + "-memory",
				MemoryAllocatable:  int64(32768),
				MemoryCapacity:     int64(32768),
				OverwriteEnv:       true,
				Templates:          []Template{{Name: "vir08", Memory: int64(8738), AICore: int32(8)}},
			},
			allAscendResourceNames: allResNames,
		}
	}
	ctr := corev1.Container{
		Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{}},
		Env: []corev1.EnvVar{
			{Name: "ASCEND_VISIBLE_DEVICES", Value: "2"}, // image-baked leak
		},
	}
	// Simulate the webhook device loop: every chip's MutateAdmission runs.
	for _, name := range allResNames {
		_, err := mkDev(string(name)).MutateAdmission(&ctr, &corev1.Pod{})
		assert.NilError(t, err)
	}
	emptyCount := 0
	for _, e := range ctr.Env {
		if e.Name == "ASCEND_VISIBLE_DEVICES" && e.Value == "" {
			emptyCount++
		}
	}
	assert.Equal(t, emptyCount, 1, "expected exactly one empty ASCEND_VISIBLE_DEVICES")
}

func Test_MutateAdmission_OverwriteEnvLastWinsInjectsAfterRealValue(t *testing.T) {
	// Regression: kubelet dedupes same-name env vars last-wins, so an empty
	// ASCEND_VISIBLE_DEVICES earlier in the list does NOT hide a real value that
	// comes after it (["", "2"] resolves to "2"). The guard must check the LAST
	// same-name entry and still inject, so the container ends up seeing "".
	allResNames := []corev1.ResourceName{
		"huawei.com/Ascend910A",
		"huawei.com/Ascend910B4",
	}
	dev := &Devices{
		config: VNPUConfig{
			ResourceName:       "huawei.com/Ascend910A",
			ResourceMemoryName: "huawei.com/Ascend910A-memory",
			MemoryAllocatable:  int64(32768),
			MemoryCapacity:     int64(32768),
			OverwriteEnv:       true,
			Templates:          []Template{{Name: "vir08", Memory: int64(8738), AICore: int32(8)}},
		},
		allAscendResourceNames: allResNames,
	}
	ctr := corev1.Container{
		Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{}},
		Env: []corev1.EnvVar{
			{Name: "ASCEND_VISIBLE_DEVICES", Value: ""},  // e.g. injected by another webhook
			{Name: "ASCEND_VISIBLE_DEVICES", Value: "2"}, // real value after the empty one
		},
	}
	got, err := dev.MutateAdmission(&ctr, &corev1.Pod{})
	assert.NilError(t, err)
	assert.Equal(t, got, false)
	last := ctr.Env[len(ctr.Env)-1]
	assert.Equal(t, last.Name, "ASCEND_VISIBLE_DEVICES")
	assert.Equal(t, last.Value, "", "expected an empty value injected after the real one")
	assert.Assert(t, last.ValueFrom == nil, "injected entry must be a literal")
}

func Test_MutateAdmission_OverwriteEnvIgnoresValueFromEntry(t *testing.T) {
	// An ASCEND_VISIBLE_DEVICES populated via ValueFrom must NOT be treated as an
	// existing empty literal value: hasEnvWithValue skips ValueFrom entries so the
	// safety injection still runs for a non-Ascend container.
	allResNames := []corev1.ResourceName{
		"huawei.com/Ascend910A",
		"huawei.com/Ascend910B4",
	}
	dev := &Devices{
		config: VNPUConfig{
			ResourceName:       "huawei.com/Ascend910A",
			ResourceMemoryName: "huawei.com/Ascend910A-memory",
			MemoryAllocatable:  int64(32768),
			MemoryCapacity:     int64(32768),
			OverwriteEnv:       true,
			Templates:          []Template{{Name: "vir08", Memory: int64(8738), AICore: int32(8)}},
		},
		allAscendResourceNames: allResNames,
	}
	ctr := corev1.Container{
		Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{}},
		Env: []corev1.EnvVar{
			{Name: "ASCEND_VISIBLE_DEVICES", ValueFrom: &corev1.EnvVarSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "cm"},
					Key:                  "devices",
				},
			}},
		},
	}
	got, err := dev.MutateAdmission(&ctr, &corev1.Pod{})
	assert.NilError(t, err)
	assert.Equal(t, got, false)
	// The ValueFrom entry must not block injection: an empty literal entry should be appended.
	foundEmpty := false
	for _, e := range ctr.Env {
		if e.Name == "ASCEND_VISIBLE_DEVICES" && e.Value == "" && e.ValueFrom == nil {
			foundEmpty = true
			break
		}
	}
	assert.Assert(t, foundEmpty, "expected an empty literal ASCEND_VISIBLE_DEVICES to be injected despite the ValueFrom entry")
}

// hasInjectedEmptyAVD reports whether ctr.Env contains a literal empty
// ASCEND_VISIBLE_DEVICES entry (the clearing injection).
func hasInjectedEmptyAVD(env []corev1.EnvVar) bool {
	for _, e := range env {
		if e.Name == "ASCEND_VISIBLE_DEVICES" && e.ValueFrom == nil && e.Value == "" {
			return true
		}
	}
	return false
}

// Test_MutateAdmission_OverwriteEnvOptOut covers the universal opt-out annotation
// (hami.io/overwrite-env pod-level + hami.io/overwrite-env-containers JSON container-level)
// resolved via util.OverwriteEnvDecision, with the backend's dev.config.OverwriteEnv
// as the Unset fallback. The three-state decision: On forces injection, Off skips,
// Unset falls back to config. Container-level overrides pod-level (both directions).
func Test_MutateAdmission_OverwriteEnvOptOut(t *testing.T) {
	allResNames := []corev1.ResourceName{
		"huawei.com/Ascend910A",
		"huawei.com/Ascend910B4",
	}
	mkDev := func(overwriteEnv bool) *Devices {
		return &Devices{
			config: VNPUConfig{
				ResourceName:       "huawei.com/Ascend910A",
				ResourceMemoryName: "huawei.com/Ascend910A-memory",
				MemoryAllocatable:  int64(32768),
				MemoryCapacity:     int64(32768),
				OverwriteEnv:       overwriteEnv,
				Templates:          []Template{{Name: "vir08", Memory: int64(8738), AICore: int32(8)}},
			},
			allAscendResourceNames: allResNames,
		}
	}
	mkPod := func(ann map[string]string) *corev1.Pod {
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: ann}}
	}
	// nonAscendCtr requests no Ascend resource → eligible for the clearing injection.
	nonAscendCtr := func() corev1.Container {
		return corev1.Container{
			Name:      "main",
			Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{}},
		}
	}

	type tc struct {
		name        string
		configOn    bool // dev.config.OverwriteEnv
		annotations map[string]string
		wantInject  bool
	}
	cases := []tc{
		// Unset (no annotations) → fall back to config.
		{name: "unset config true injects", configOn: true, wantInject: true},
		{name: "unset config false skips", configOn: false, wantInject: false},
		// Pod-level annotation forces the decision regardless of config.
		{name: "pod false skips despite config true", configOn: true, annotations: map[string]string{"hami.io/overwrite-env": "false"}, wantInject: false},
		{name: "pod true injects despite config false", configOn: false, annotations: map[string]string{"hami.io/overwrite-env": "true"}, wantInject: true},
		// Container-level overrides pod-level (both directions).
		{name: "container false overrides pod true", configOn: true, annotations: map[string]string{"hami.io/overwrite-env": "true", "hami.io/overwrite-env-containers": `{"main":"false"}`}, wantInject: false},
		{name: "container true reverse-overrides pod false", configOn: true, annotations: map[string]string{"hami.io/overwrite-env": "false", "hami.io/overwrite-env-containers": `{"main":"true"}`}, wantInject: true},
		// Invalid annotation value is treated as absent → falls back to lower layer.
		{name: "invalid pod value falls back to config true", configOn: true, annotations: map[string]string{"hami.io/overwrite-env": "yes"}, wantInject: true},
		{name: "invalid container value falls back to pod false", configOn: true, annotations: map[string]string{"hami.io/overwrite-env": "false", "hami.io/overwrite-env-containers": `{"main":"maybe"}`}, wantInject: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dev := mkDev(c.configOn)
			ctr := nonAscendCtr()
			_, err := dev.MutateAdmission(&ctr, mkPod(c.annotations))
			assert.NilError(t, err)
			assert.Equal(t, hasInjectedEmptyAVD(ctr.Env), c.wantInject)
		})
	}

	// Ascend container opt-out is a no-op: a container requesting an Ascend resource
	// never enters the injection branch, so opt-out annotations have no effect.
	t.Run("ascend container opt-out is no-op regardless of annotation", func(t *testing.T) {
		dev := mkDev(true)
		ctr := corev1.Container{
			Name: "main",
			Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
				"huawei.com/Ascend910B4": resource.MustParse("1"),
			}},
		}
		// 910A device sees !ok for a 910B4 container; opt-out true would force inject
		// for a non-ascend container, but an ascend container must be left untouched.
		_, err := dev.MutateAdmission(&ctr, mkPod(map[string]string{"hami.io/overwrite-env": "true"}))
		assert.NilError(t, err)
		assert.Equal(t, hasInjectedEmptyAVD(ctr.Env), false, "ascend container must not be injected even with opt-out true")
	})

	// Requests-only (no Limits) Ascend container is still recognized as an Ascend
	// container and must not be injected (containerRequestsAnyAscendResource checks
	// both Limits and Requests).
	t.Run("requests-only ascend container is not injected", func(t *testing.T) {
		dev := mkDev(true)
		ctr := corev1.Container{
			Name: "main",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					"huawei.com/Ascend910B4": resource.MustParse("1"),
				},
			},
		}
		_, err := dev.MutateAdmission(&ctr, mkPod(nil))
		assert.NilError(t, err)
		assert.Equal(t, hasInjectedEmptyAVD(ctr.Env), false, "requests-only ascend container must not be injected")
	})
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
				SuperPod:          true,
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
				SuperPod:          true,
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
				SuperPod:          true,
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

func Test_MutateAdmission910C_VNPUSplit(t *testing.T) {
	devCfg := VNPUConfig{
		CommonWord:         "Ascend910C",
		ResourceName:       "huawei.com/Ascend910C",
		ResourceMemoryName: "huawei.com/Ascend910C-memory",
		MemoryAllocatable:  65536,
		MemoryCapacity:     65536,
		Templates: []Template{
			{Name: "vir05_1c_16g", Memory: 16384, AICore: 5, AICPU: 1},
			{Name: "vir10_3c_32g", Memory: 32768, AICore: 10, AICPU: 3},
		},
	}

	t.Run("request 1 + vNPU memory slice is not rounded up", func(t *testing.T) {
		ctr := corev1.Container{
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"huawei.com/Ascend910C":        resource.MustParse("1"),
					"huawei.com/Ascend910C-memory": resource.MustParse("16384"),
				},
				Requests: corev1.ResourceList{
					"huawei.com/Ascend910C": resource.MustParse("1"),
				},
			},
		}
		dev := Devices{config: devCfg}
		result, err := dev.MutateAdmission(&ctr, &corev1.Pod{})
		assert.NilError(t, err)
		assert.Assert(t, result)

		npuQty := ctr.Resources.Limits["huawei.com/Ascend910C"]
		gotNPU, _ := npuQty.AsInt64()
		assert.Equal(t, gotNPU, int64(1), "vNPU request must stay at 1, not be rounded up to 2")

		memQty := ctr.Resources.Limits["huawei.com/Ascend910C-memory"]
		gotMem, _ := memQty.AsInt64()
		assert.Equal(t, gotMem, int64(16384), "memory should map to the vir05_1c_16g template")
	})

	t.Run("odd NPU count is allowed when the card is split", func(t *testing.T) {
		ctr := corev1.Container{
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"huawei.com/Ascend910C": resource.MustParse("3"),
				},
				Requests: corev1.ResourceList{
					"huawei.com/Ascend910C": resource.MustParse("3"),
				},
			},
		}
		dev := Devices{config: devCfg}
		result, err := dev.MutateAdmission(&ctr, &corev1.Pod{})
		assert.NilError(t, err)
		assert.Assert(t, result)

		npuQty := ctr.Resources.Limits["huawei.com/Ascend910C"]
		gotNPU, _ := npuQty.AsInt64()
		assert.Equal(t, gotNPU, int64(3), "odd count must be preserved in split mode")
	})
}

func Test_MutateAdmission_VNPUCoreMode(t *testing.T) {
	const VNPUModeAnnotation = "huawei.com/vnpu-mode"
	const VNPUModeHamiCore = "hami-core"

	tests := []struct {
		name string
		args struct {
			ctr corev1.Container
			pod corev1.Pod
		}
		wantPostStart bool
		wantMem       int64
		wantCore      int64
	}{
		{
			name: "vNPU-mode hami-core: keep raw memory without postStart",
			args: struct {
				ctr corev1.Container
				pod corev1.Pod
			}{
				ctr: corev1.Container{
					Name: "test-container",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"huawei.com/Ascend910B3":        resource.MustParse("1"),
							"huawei.com/Ascend910B3-memory": resource.MustParse("15360"),
							"huawei.com/Ascend910B3-core":   resource.MustParse("20"),
						},
						Requests: corev1.ResourceList{
							"huawei.com/Ascend910B3":        resource.MustParse("1"),
							"huawei.com/Ascend910B3-memory": resource.MustParse("15360"),
							"huawei.com/Ascend910B3-core":   resource.MustParse("20"),
						},
					},
				},
				pod: corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							VNPUModeAnnotation: VNPUModeHamiCore,
						},
					},
				},
			},
			wantPostStart: false,
			wantMem:       15360,
			wantCore:      20,
		},
		{
			name: "no vnpu-mode annotation: no postStart, memory trimmed",
			args: struct {
				ctr corev1.Container
				pod corev1.Pod
			}{
				ctr: corev1.Container{
					Name: "test-container",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"huawei.com/Ascend910B3":        resource.MustParse("1"),
							"huawei.com/Ascend910B3-memory": resource.MustParse("15360"),
						},
						Requests: corev1.ResourceList{
							"huawei.com/Ascend910B3":        resource.MustParse("1"),
							"huawei.com/Ascend910B3-memory": resource.MustParse("15360"),
						},
					},
				},
				pod: corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
			},
			wantPostStart: false,
			wantMem:       32768, // no template configured -> MemoryAllocatable
			wantCore:      0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := Devices{
				config: VNPUConfig{
					CommonWord:         "Ascend910B3",
					ResourceName:       "huawei.com/Ascend910B3",
					ResourceMemoryName: "huawei.com/Ascend910B3-memory",
					ResourceCoreName:   "huawei.com/Ascend910B3-core",
					MemoryAllocatable:  32768,
					MemoryCapacity:     32768,
				},
			}

			ok, err := dev.MutateAdmission(&test.args.ctr, &test.args.pod)
			assert.NilError(t, err)
			assert.Equal(t, ok, true)

			if !test.wantPostStart {
				if test.args.ctr.Lifecycle != nil {
					assert.Assert(t, test.args.ctr.Lifecycle.PostStart == nil, "PostStart should not be set")
				}
			}

			memLimit := test.args.ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceMemoryName)]
			assert.Equal(t, memLimit.Value(), test.wantMem)

			coreLimit := test.args.ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceCoreName)]
			assert.Equal(t, coreLimit.Value(), test.wantCore)
		})
	}
}

// Test_MutateAdmission_HardSplitCoreRejected verifies that a -core request is
// rejected on hard split (non hami-core) but accepted in hami-core soft-split
// mode, and that a hard-split request without -core is unaffected.
func Test_MutateAdmission_HardSplitCoreRejected(t *testing.T) {
	// 8738 maps to the vir08 template so hard-split memory trimming succeeds.
	newCtr := func(core string) corev1.Container {
		limits := corev1.ResourceList{
			"huawei.com/Ascend910B3":        resource.MustParse("1"),
			"huawei.com/Ascend910B3-memory": resource.MustParse("8738"),
		}
		requests := corev1.ResourceList{
			"huawei.com/Ascend910B3":        resource.MustParse("1"),
			"huawei.com/Ascend910B3-memory": resource.MustParse("8738"),
		}
		if core != "" {
			limits["huawei.com/Ascend910B3-core"] = resource.MustParse(core)
			requests["huawei.com/Ascend910B3-core"] = resource.MustParse(core)
		}
		return corev1.Container{
			Name:      "test-container",
			Resources: corev1.ResourceRequirements{Limits: limits, Requests: requests},
		}
	}

	tests := []struct {
		name     string
		ctr      corev1.Container
		hamiCore bool
		wantErr  bool
	}{
		{name: "hard split with core is rejected", ctr: newCtr("10"), hamiCore: false, wantErr: true},
		{name: "hard split with core over physical is rejected", ctr: newCtr("25"), hamiCore: false, wantErr: true},
		{name: "hard split with core=0 is allowed", ctr: newCtr("0"), hamiCore: false, wantErr: false},
		{name: "hard split without core is allowed", ctr: newCtr(""), hamiCore: false, wantErr: false},
		{name: "hami-core soft split with core is allowed", ctr: newCtr("10"), hamiCore: true, wantErr: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := Devices{
				config: VNPUConfig{
					CommonWord:         "Ascend910B3",
					ResourceName:       "huawei.com/Ascend910B3",
					ResourceMemoryName: "huawei.com/Ascend910B3-memory",
					ResourceCoreName:   "huawei.com/Ascend910B3-core",
					MemoryAllocatable:  32768,
					MemoryCapacity:     32768,
					Templates: []Template{
						{Name: "vir08", Memory: 8738, AICore: 8},
						{Name: "vir16", Memory: 17476, AICore: 16},
					},
				},
			}
			pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{}}
			if test.hamiCore {
				pod.Annotations = map[string]string{VNPUModeAnnotation: VNPUModeHamiCore}
			}
			ctr := test.ctr
			_, err := dev.MutateAdmission(&ctr, &pod)
			if test.wantErr {
				assert.Assert(t, err != nil, "expected admission to be rejected")
				assert.Assert(t, strings.Contains(err.Error(), "hami-core"),
					"error should mention hami-core, got %v", err)
			} else {
				assert.NilError(t, err)
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

func Test_GenerateResourceRequests_VNPUCoreMode(t *testing.T) {
	tests := []struct {
		name string
		args corev1.Container
		want device.ContainerDeviceRequest
	}{
		{
			name: "parse custom vNPU core and memory resources",
			args: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"huawei.com/Ascend910B3":        resource.MustParse("1"),
						"huawei.com/Ascend910B3-core":   resource.MustParse("10"),
						"huawei.com/Ascend910B3-memory": resource.MustParse("15360"),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             "Ascend910B3",
				Memreq:           int32(15360),
				Coresreq:         int32(10),
				MemPercentagereq: int32(0),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := Devices{
				config: VNPUConfig{
					CommonWord:         "Ascend910B3",
					ResourceName:       "huawei.com/Ascend910B3",
					ResourceCoreName:   "huawei.com/Ascend910B3-core",
					ResourceMemoryName: "huawei.com/Ascend910B3-memory",
					MemoryAllocatable:  32768,
					MemoryCapacity:     32768,
					Templates: []Template{
						{Name: "vir08", Memory: 8738, AICore: 8},
						{Name: "vir16", Memory: 17476, AICore: 16},
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
				Memreq:           int32(1280),
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
				Memreq:           int32(12800),
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
				Memreq:           int32(128),
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

// Test_GenerateResourceRequests_OutOfRangeValues checks that out-of-range values are rejected, not silently wrapped.
func Test_GenerateResourceRequests_OutOfRangeValues(t *testing.T) {
	coreModeConfig := VNPUConfig{
		CommonWord:         "Ascend910B3",
		ResourceName:       "huawei.com/Ascend910B3",
		ResourceCoreName:   "huawei.com/Ascend910B3-core",
		ResourceMemoryName: "huawei.com/Ascend910B3-memory",
		MemoryAllocatable:  int64(65536),
		MemoryCapacity:     int64(65536),
	}

	tests := []struct {
		name string
		dev  Devices
		args corev1.Container
		want device.ContainerDeviceRequest
	}{
		{
			// 16Gi in bytes wraps to 0 when narrowed to int32; Ascend memory is counted in MB.
			name: "memory requested in bytes exceeds int32 range on soft-partitioning path",
			dev:  Devices{config: coreModeConfig},
			args: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"huawei.com/Ascend910B3":        resource.MustParse("1"),
						"huawei.com/Ascend910B3-core":   resource.MustParse("10"),
						"huawei.com/Ascend910B3-memory": resource.MustParse("16Gi"),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "memory requested in bytes exceeds int32 range on trim path",
			dev:  Devices{config: coreModeConfig},
			args: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"huawei.com/Ascend910B3":        resource.MustParse("1"),
						"huawei.com/Ascend910B3-memory": resource.MustParse("16Gi"),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			// -1m makes AsInt64 return ok=false, so it must be rejected by sign, not defaulted to 100%.
			name: "negative fractional memory request",
			dev:  Devices{config: coreModeConfig},
			args: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"huawei.com/Ascend910B3":        resource.MustParse("1"),
						"huawei.com/Ascend910B3-memory": resource.MustParse("-1m"),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			// A plain whole -100 passes AsInt64 (ok=true), so it must be rejected by sign before the int32 narrowing.
			name: "negative whole memory request",
			dev:  Devices{config: coreModeConfig},
			args: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"huawei.com/Ascend910B3":        resource.MustParse("1"),
						"huawei.com/Ascend910B3-memory": resource.MustParse("-100"),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "oversized device count exceeds int32 range",
			dev:  Devices{config: coreModeConfig},
			args: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"huawei.com/Ascend910B3": resource.MustParse("2200000000"),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "negative core request",
			dev:  Devices{config: coreModeConfig},
			args: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"huawei.com/Ascend910B3":      resource.MustParse("1"),
						"huawei.com/Ascend910B3-core": resource.MustParse("-1"),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "oversized core request exceeds int32 range",
			dev:  Devices{config: coreModeConfig},
			args: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"huawei.com/Ascend910B3":      resource.MustParse("1"),
						"huawei.com/Ascend910B3-core": resource.MustParse("2200000000"),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := test.dev.GenerateResourceRequests(&test.args)
			assert.Equal(t, result, test.want)
		})
	}
}

// Test_GenerateResourceRequests_MemoryFactorOverflow covers a value that fits int32 but overflows after MemoryFactor.
func Test_GenerateResourceRequests_MemoryFactorOverflow(t *testing.T) {
	tests := []struct {
		name string
		dev  Devices
		args corev1.Container
	}{
		{
			name: "scaled memory overflows int32 on soft-partitioning path",
			dev: Devices{
				config: VNPUConfig{
					CommonWord:         "Ascend910B3",
					ResourceName:       "huawei.com/Ascend910B3",
					ResourceCoreName:   "huawei.com/Ascend910B3-core",
					ResourceMemoryName: "huawei.com/Ascend910B3-memory",
					MemoryAllocatable:  int64(65536),
					MemoryCapacity:     int64(65536),
					MemoryFactor:       int32(10),
				},
			},
			args: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"huawei.com/Ascend910B3":        resource.MustParse("1"),
						"huawei.com/Ascend910B3-core":   resource.MustParse("10"),
						"huawei.com/Ascend910B3-memory": resource.MustParse("300000000"),
					},
				},
			},
		},
		{
			name: "scaled memory overflows int32 on trim path",
			dev: Devices{
				config: VNPUConfig{
					CommonWord:         "Ascend910A",
					ResourceName:       "huawei.com/Ascend910A",
					ResourceMemoryName: "huawei.com/Ascend910A-memory",
					MemoryAllocatable:  int64(32768),
					MemoryCapacity:     int64(32768),
					MemoryFactor:       int32(10),
				},
			},
			args: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"huawei.com/Ascend910A":        resource.MustParse("1"),
						"huawei.com/Ascend910A-memory": resource.MustParse("300000000"),
					},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := test.dev.GenerateResourceRequests(&test.args)
			assert.Equal(t, result, device.ContainerDeviceRequest{})
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
	devs := InitDevices(VNPUs{Configs: config})

	tests := []struct {
		name           string
		devices        []*device.DeviceUsage
		request        device.ContainerDeviceRequest
		annos          map[string]string
		nodeAnnotation map[string]string
		wantFit        bool
		wantLen        int
		wantDevIDs     []string
		wantReason     string
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
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         120,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "core limit out of range",
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
		{
			name: "fit fail: hami-core pod on legacy node (ModeNotFit)",
			devices: []*device.DeviceUsage{{
				ID: "dev-0", Index: 0, Used: 0, Count: 100,
				Usedmem: 0, Totalmem: 32768, Totalcore: 100, Usedcores: 0,
				Numa: 0, Health: true,
			}},
			request: device.ContainerDeviceRequest{
				Nums: 1, Memreq: 15360, MemPercentagereq: 0, Coresreq: 20,
			},
			annos: map[string]string{
				VNPUModeAnnotation: VNPUModeHamiCore,
			},
			wantFit:        false,
			wantLen:        0,
			wantDevIDs:     []string{},
			wantReason:     "1/1 ModeNotFit",
			nodeAnnotation: map[string]string{},
		},
		{
			name: "fit ok: annotation-less pod on hami-core node follows node",
			devices: []*device.DeviceUsage{{
				ID: "dev-0", Index: 0, Used: 0, Count: 100,
				Usedmem: 0, Totalmem: 32768, Totalcore: 100, Usedcores: 0,
				Numa: 0, Health: true,
			}},
			request: device.ContainerDeviceRequest{
				Nums: 1, Memreq: 8738, MemPercentagereq: 0, Coresreq: 0,
			},
			annos:          map[string]string{},
			wantFit:        true,
			wantLen:        1,
			wantDevIDs:     []string{"dev-0"},
			wantReason:     "",
			nodeAnnotation: map[string]string{VNPUNodeSelectorAnnotation: "true"},
		},
		{
			name: "fit fail: whole-card hami-core pod on legacy node (ModeNotFit)",
			devices: []*device.DeviceUsage{{
				ID: "dev-0", Index: 0, Used: 0, Count: 100,
				Usedmem: 0, Totalmem: 32768, Totalcore: 100, Usedcores: 0,
				Numa: 0, Health: true,
			}},
			request: device.ContainerDeviceRequest{
				Nums: 1, Memreq: 32768, MemPercentagereq: 0, Coresreq: 0,
			},
			annos: map[string]string{
				VNPUModeAnnotation: VNPUModeHamiCore,
			},
			wantFit:        false,
			wantLen:        0,
			wantDevIDs:     []string{},
			wantReason:     "1/1 ModeNotFit",
			nodeAnnotation: map[string]string{},
		},
		{
			name: "fit fail: memory-less hami-core pod on legacy node (ModeNotFit)",
			devices: []*device.DeviceUsage{{
				ID: "dev-0", Index: 0, Used: 0, Count: 100,
				Usedmem: 0, Totalmem: 32768, Totalcore: 100, Usedcores: 0,
				Numa: 0, Health: true,
			}},
			request: device.ContainerDeviceRequest{
				Nums: 1, Memreq: 0, MemPercentagereq: 101, Coresreq: 0,
			},
			annos: map[string]string{
				VNPUModeAnnotation: VNPUModeHamiCore,
			},
			wantFit:        false,
			wantLen:        0,
			wantDevIDs:     []string{},
			wantReason:     "1/1 ModeNotFit",
			nodeAnnotation: map[string]string{},
		},
		{
			name: "fit success: whole-card hami-core pod on hami-core node",
			devices: []*device.DeviceUsage{{
				ID: "dev-0", Index: 0, Used: 0, Count: 100,
				Usedmem: 0, Totalmem: 32768, Totalcore: 100, Usedcores: 0,
				Numa: 0, Health: true,
			}},
			request: device.ContainerDeviceRequest{
				Nums: 1, Memreq: 32768, MemPercentagereq: 0, Coresreq: 0,
			},
			annos: map[string]string{
				VNPUModeAnnotation: VNPUModeHamiCore,
			},
			wantFit:        true,
			wantLen:        1,
			wantDevIDs:     []string{"dev-0"},
			wantReason:     "",
			nodeAnnotation: map[string]string{VNPUNodeSelectorAnnotation: "true"},
		},
		{
			name: "fit success: whole-card legacy pod on hami-core node",
			devices: []*device.DeviceUsage{{
				ID: "dev-0", Index: 0, Used: 0, Count: 100,
				Usedmem: 0, Totalmem: 32768, Totalcore: 100, Usedcores: 0,
				Numa: 0, Health: true,
			}},
			request: device.ContainerDeviceRequest{
				Nums: 1, Memreq: 32768, MemPercentagereq: 0, Coresreq: 0,
			},
			annos:          map[string]string{},
			wantFit:        true,
			wantLen:        1,
			wantDevIDs:     []string{"dev-0"},
			wantReason:     "",
			nodeAnnotation: map[string]string{VNPUNodeSelectorAnnotation: "true"},
		},
		{
			name: "mutex policy rejects used device",
			devices: []*device.DeviceUsage{
				{
					ID:        "dev-0",
					Index:     0,
					Used:      1,
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
			annos:      map[string]string{"hami.io/gpu-scheduler-policy": "mutex"},
			wantFit:    false,
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
				Totalmem:  1280,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Health:    false,
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
			wantReason: "1/1 CardNotHealth",
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
					Health:    true,
				},
			},
			request: device.ContainerDeviceRequest{
				Nums:             3,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         20,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "2/2 AllocatedCardsInsufficientRequest",
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
					Node: &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: test.nodeAnnotation,
						},
					},
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
  superPod: true
`

	var config []VNPUConfig
	if err := yaml.Unmarshal([]byte(configStr), &config); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}
	enableAscend = true
	devs := InitDevices(VNPUs{Configs: config})

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

func Test_GenerateResourceRequests_CoresValidation(t *testing.T) {
	dev := &Devices{
		config: VNPUConfig{
			CommonWord:       "Ascend910A",
			ResourceName:     "huawei.com/Ascend910",
			ResourceCoreName: "huawei.com/Ascend910-core",
		},
	}

	tests := []struct {
		name    string
		cores   int64
		rawCore string
		wantReq bool
	}{
		{
			name:    "cores 0 accepted",
			cores:   0,
			wantReq: true,
		},
		{
			name:    "cores 50 accepted",
			cores:   50,
			wantReq: true,
		},
		{
			name:    "cores 100 accepted",
			cores:   100,
			wantReq: true,
		},
		{
			name:    "cores 101 rejected",
			cores:   101,
			wantReq: false,
		},
		{
			name:    "cores 150 rejected",
			cores:   150,
			wantReq: false,
		},
		{
			name:    "cores 200 rejected",
			cores:   200,
			wantReq: false,
		},
		{
			name:    "negative cores -1 rejected",
			cores:   -1,
			wantReq: false,
		},
		{
			name:    "fractional cores 50m rejected",
			rawCore: "50m",
			wantReq: false,
		},
		{
			name:    "fractional cores 99.1 rejected",
			rawCore: "99.1",
			wantReq: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			coreQty := *resource.NewQuantity(tt.cores, resource.DecimalSI)
			if tt.rawCore != "" {
				coreQty = resource.MustParse(tt.rawCore)
			}
			ctr := &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"huawei.com/Ascend910":      resource.MustParse("1"),
						"huawei.com/Ascend910-core": coreQty,
					},
				},
			}
			req := dev.GenerateResourceRequests(ctr)
			if tt.wantReq {
				assert.Equal(t, int32(1), req.Nums)
				assert.Equal(t, int32(tt.cores), req.Coresreq)
			} else {
				assert.DeepEqual(t, req, device.ContainerDeviceRequest{})
			}
		})
	}
}

func TestFit_CoresValidation(t *testing.T) {
	dev := &Devices{
		config: VNPUConfig{
			CommonWord: "Ascend910A",
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
			Type:      "Ascend910A",
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
				Type:     "Ascend910A",
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
			Type:     "Ascend910A",
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
			Type:     "Ascend910A",
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
