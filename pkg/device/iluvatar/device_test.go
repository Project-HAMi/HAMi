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
	"strconv"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/util"
)

func TestGetNodeDevices(t *testing.T) {
	IluvatarResourceCores = "iluvatar.ai/MR-V100.vCore"
	IluvatarResourceMemory = "iluvatar.ai/MR-V100.vMem"

	tests := []struct {
		name     string
		node     corev1.Node
		expected []*util.DeviceInfo
		err      error
	}{
		{
			name: "Test with valid node",
			node: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Status: corev1.NodeStatus{
					Capacity: corev1.ResourceList{
						corev1.ResourceName(IluvatarResourceCores):  *resource.NewQuantity(100, resource.DecimalSI),
						corev1.ResourceName(IluvatarResourceMemory): *resource.NewQuantity(128, resource.DecimalSI),
					},
				},
			},
			expected: []*util.DeviceInfo{
				{
					Index:   0,
					ID:      "test-iluvatar-0",
					Count:   100,
					Devmem:  32768,
					Devcore: 100,
					Type:    IluvatarGPUDevice,
					Numa:    0,
					Health:  true,
				},
			},
			err: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dev := &IluvatarDevices{}
			got, err := dev.GetNodeDevices(tt.node)
			if (err != nil) != (tt.err != nil) {
				t.Errorf("GetNodeDevices() error = %v, expected %v", err, tt.err)
				return
			}

			if len(got) != len(tt.expected) {
				t.Errorf("GetNodeDevices() got %d devices, expected %d", len(got), len(tt.expected))
				return
			}

			for i, device := range got {
				if device.Index != tt.expected[i].Index {
					t.Errorf("Expected index %d, got %d", tt.expected[i].Index, device.Index)
				}
				if device.ID != tt.expected[i].ID {
					t.Errorf("Expected id %s, got %s", tt.expected[i].ID, device.ID)
				}
				if device.Devcore != tt.expected[i].Devcore {
					t.Errorf("Expected devcore %d, got %d", tt.expected[i].Devcore, device.Devcore)
				}
				if device.Devmem != tt.expected[i].Devmem {
					t.Errorf("Expected cevmem %d, got %d", tt.expected[i].Devmem, device.Devmem)
				}
			}
		})
	}
}

func TestPatchAnnotations(t *testing.T) {
	InitIluvatarDevice(IluvatarConfig{})

	tests := []struct {
		name       string
		annoInput  map[string]string
		podDevices util.PodDevices
		expected   map[string]string
	}{
		{
			name:       "No devices",
			annoInput:  map[string]string{},
			podDevices: util.PodDevices{},
			expected:   map[string]string{},
		},
		{
			name:      "With devices",
			annoInput: map[string]string{},
			podDevices: util.PodDevices{
				IluvatarGPUDevice: util.PodSingleDevice{
					[]util.ContainerDevice{
						{
							Idx:  0,
							UUID: "k8s-gpu-iluvatar-0",
							Type: "Iluvatar",
						},
					},
				},
			},
			expected: map[string]string{
				util.InRequestDevices[IluvatarGPUDevice]: "k8s-gpu-iluvatar-0,Iluvatar,0,0,0:;",
				util.SupportDevices[IluvatarGPUDevice]:   "k8s-gpu-iluvatar-0,Iluvatar,0,0,0:;",
				"iluvatar.ai/gpu-assigned":               "false",
				"iluvatar.ai/predicate-time":             strconv.FormatInt(time.Now().UnixNano(), 10),
				IluvatarDeviceSelection + "0":            "0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			annoInputCopy := make(map[string]string)
			for k, v := range tt.annoInput {
				annoInputCopy[k] = v
			}

			dev := &IluvatarDevices{}
			got := dev.PatchAnnotations(&annoInputCopy, tt.podDevices)

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
