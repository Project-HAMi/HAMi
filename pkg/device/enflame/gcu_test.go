/*
Copyright 2025 The HAMi Authors.

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

package enflame

import (
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

func TestInitGCUDevice(t *testing.T) {
	tests := []struct {
		name   string
		config EnflameConfig
		want   string
	}{
		{
			name: "Test with valid config",
			config: EnflameConfig{
				ResourceNameGCU: "enflame.com/gcu",
			},
			want: "enflame.com/gcu",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dev := InitGCUDevice(tt.config)
			if dev == nil {
				t.Errorf("InitGCUDevice() returned nil")
			}
			if EnflameResourceNameGCU != tt.want {
				t.Errorf("EnflameResourceName = %s, want %s", EnflameResourceNameGCU, tt.want)
			}
			if device.InRequestDevices[EnflameGCUDevice] != "hami.io/enflame-gcu-devices-to-allocate" {
				t.Errorf("InRequestDevices not set correctly")
			}
			if device.SupportDevices[EnflameGCUDevice] != "hami.io/enflame-gcu-devices-allocated" {
				t.Errorf("SupportDevices not set correctly")
			}
		})
	}
}

func TestGCUDevices_CommonWord(t *testing.T) {
	dev := &GCUDevices{}
	assert.Equal(t, dev.CommonWord(), EnflameGCUCommonWord)
}

func TestGCUDevices_MutateAdmission(t *testing.T) {
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
			name: "GCU resource set to limits",
			args: struct {
				ctr *corev1.Container
				p   *corev1.Pod
			}{
				ctr: &corev1.Container{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"enflame.com/gcu": *resource.NewQuantity(2, resource.DecimalSI),
						},
					},
				},
				p: &corev1.Pod{},
			},
			want: true,
		},
		{
			name: "GCU resource not set",
			args: struct {
				ctr *corev1.Container
				p   *corev1.Pod
			}{
				ctr: &corev1.Container{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{},
					},
				},
				p: &corev1.Pod{},
			},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := EnflameConfig{
				ResourceNameGCU: "enflame.com/gcu",
			}
			InitGCUDevice(config)
			dev := &GCUDevices{}
			result, err := dev.MutateAdmission(test.args.ctr, test.args.p)
			assert.Equal(t, result, test.want)
			assert.Equal(t, err, test.err)
		})
	}
}

func TestGCUDevices_CheckHealth(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			devType string
			node    *corev1.Node
		}
		wantHealthy     bool
		wantSchedulable bool
	}{
		{
			name: "Node with GCU resources",
			args: struct {
				devType string
				node    *corev1.Node
			}{
				devType: "GCU",
				node: &corev1.Node{
					Status: corev1.NodeStatus{
						Capacity: corev1.ResourceList{
							"enflame.com/gcu": *resource.NewQuantity(2, resource.DecimalSI),
						},
					},
				},
			},
			wantHealthy:     true,
			wantSchedulable: true,
		},
		{
			name: "Node without GCU resources",
			args: struct {
				devType string
				node    *corev1.Node
			}{
				devType: "GCU",
				node: &corev1.Node{
					Status: corev1.NodeStatus{
						Capacity: corev1.ResourceList{},
					},
				},
			},
			wantHealthy:     false,
			wantSchedulable: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := EnflameConfig{
				ResourceNameGCU: "enflame.com/gcu",
			}
			InitGCUDevice(config)
			dev := &GCUDevices{}
			healthy, schedulable := dev.CheckHealth(test.args.devType, test.args.node)
			assert.Equal(t, healthy, test.wantHealthy)
			assert.Equal(t, schedulable, test.wantSchedulable)
		})
	}
}

func TestGCUDevices_NodeCleanUp(t *testing.T) {
	dev := &GCUDevices{}
	err := dev.NodeCleanUp("test-node")
	assert.Equal(t, err, nil)
}

func TestGCUDevices_GetNodeDevices(t *testing.T) {
	tests := []struct {
		name     string
		node     corev1.Node
		expected []*device.DeviceInfo
		err      error
	}{
		{
			name: "Test with valid node",
			node: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testnode",
				},
				Status: corev1.NodeStatus{
					Capacity: corev1.ResourceList{
						corev1.ResourceName("enflame.com/gcu"): *resource.NewQuantity(2, resource.DecimalSI),
					},
				},
			},
			expected: []*device.DeviceInfo{
				{
					Index:        0,
					ID:           "testnode-GCU-0",
					Count:        1,
					Devmem:       100,
					Devcore:      100,
					Type:         EnflameGCUDevice,
					Numa:         0,
					Health:       true,
					DeviceVendor: EnflameGCUCommonWord,
				},
				{
					Index:        1,
					ID:           "testnode-GCU-1",
					Count:        1,
					Devmem:       100,
					Devcore:      100,
					Type:         EnflameGCUDevice,
					Numa:         0,
					Health:       true,
					DeviceVendor: EnflameGCUCommonWord,
				},
			},
			err: nil,
		},
		{
			name: "Test with missing resource",
			node: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testnode",
				},
				Status: corev1.NodeStatus{
					Capacity: corev1.ResourceList{},
				},
			},
			expected: []*device.DeviceInfo{},
			err:      nil, // Will check for error in test
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := EnflameConfig{
				ResourceNameGCU: "enflame.com/gcu",
			}
			InitGCUDevice(config)
			dev := &GCUDevices{}
			got, err := dev.GetNodeDevices(tt.node)

			if tt.name == "Test with missing resource" {
				if err == nil {
					t.Errorf("GetNodeDevices() error = nil, want error")
				}
				if len(got) != 0 {
					t.Errorf("GetNodeDevices() got %d devices, want 0", len(got))
				}
				return
			}

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
					t.Errorf("Expected devmem %d, got %d", tt.expected[i].Devmem, device.Devmem)
				}
				if device.Type != tt.expected[i].Type {
					t.Errorf("Expected type %s, got %s", tt.expected[i].Type, device.Type)
				}
			}
		})
	}
}

func TestGCUDevices_PatchAnnotations(t *testing.T) {
	config := EnflameConfig{
		ResourceNameGCU: "enflame.com/gcu",
	}
	InitGCUDevice(config)

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
			name:      "With GCU devices",
			annoInput: map[string]string{},
			podDevices: device.PodDevices{
				EnflameGCUDevice: device.PodSingleDevice{
					[]device.ContainerDevice{
						{
							Idx:  0,
							UUID: "testnode-GCU-0",
							Type: "GCU",
						},
					},
				},
			},
			expected: map[string]string{
				device.InRequestDevices[EnflameGCUDevice]: "testnode-GCU-0,GCU,0,0:;",
				device.SupportDevices[EnflameGCUDevice]:   "testnode-GCU-0,GCU,0,0:;",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			annoInputCopy := make(map[string]string)
			for k, v := range tt.annoInput {
				annoInputCopy[k] = v
			}

			dev := &GCUDevices{}
			got := dev.PatchAnnotations(&corev1.Pod{}, &annoInputCopy, tt.podDevices)

			if len(got) != len(tt.expected) {
				t.Errorf("PatchAnnotations() got %d annotations, expected %d", len(got), len(tt.expected))
				return
			}

			for k, v := range tt.expected {
				if got[k] != v {
					t.Errorf("Expected %s %s, got %s", k, v, got[k])
				}
			}
		})
	}
}

func TestGCUDevices_GenerateResourceRequests(t *testing.T) {
	tests := []struct {
		name string
		args *corev1.Container
		want device.ContainerDeviceRequest
	}{
		{
			name: "GCU resource set to limits",
			args: &corev1.Container{
				Name: "testctr",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"enflame.com/gcu": resource.MustParse("3"),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             int32(3),
				Type:             EnflameGCUDevice,
				Memreq:           100,
				MemPercentagereq: 100,
				Coresreq:         100,
			},
		},
		{
			name: "GCU resource set to requests",
			args: &corev1.Container{
				Name: "testctr",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						"enflame.com/gcu": resource.MustParse("2"),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             int32(2),
				Type:             EnflameGCUDevice,
				Memreq:           100,
				MemPercentagereq: 100,
				Coresreq:         100,
			},
		},
		{
			name: "GCU resource not set",
			args: &corev1.Container{
				Name: "testctr",
				Resources: corev1.ResourceRequirements{
					Limits:   corev1.ResourceList{},
					Requests: corev1.ResourceList{},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := EnflameConfig{
				ResourceNameGCU: "enflame.com/gcu",
			}
			InitGCUDevice(config)
			dev := &GCUDevices{}
			result := dev.GenerateResourceRequests(test.args)
			assert.DeepEqual(t, result, test.want)
		})
	}
}

func TestGCUDevices_Fit(t *testing.T) {
	config := EnflameConfig{
		ResourceNameGCU: "enflame.com/gcu",
	}
	dev := InitGCUDevice(config)

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
					Count:     1,
					Usedmem:   0,
					Totalmem:  100,
					Totalcore: 100,
					Usedcores: 0,
					Numa:      0,
					Type:      EnflameGCUDevice,
					Health:    true,
				},
				{
					ID:        "dev-1",
					Index:     1,
					Used:      0,
					Count:     1,
					Usedmem:   0,
					Totalmem:  100,
					Totalcore: 100,
					Usedcores: 0,
					Numa:      0,
					Type:      EnflameGCUDevice,
					Health:    true,
				},
			},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           100,
				MemPercentagereq: 100,
				Coresreq:         100,
				Type:             EnflameGCUDevice,
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
				ID:        "dev-0",
				Index:     0,
				Used:      0,
				Count:     1,
				Usedmem:   0,
				Totalmem:  100,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Health:    true,
				Type:      EnflameGCUDevice,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Type:             "OtherType",
				Memreq:           100,
				MemPercentagereq: 100,
				Coresreq:         100,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardTypeMismatch",
		},
		{
			name: "fit fail: device already used",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      1,
				Count:     1,
				Usedmem:   0,
				Totalmem:  100,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Type:      EnflameGCUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           100,
				MemPercentagereq: 100,
				Coresreq:         100,
				Type:             EnflameGCUDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 ExclusiveDeviceAllocateConflict",
		},
		{
			name: "fit fail: insufficient devices",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      0,
				Count:     1,
				Usedmem:   0,
				Totalmem:  100,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Type:      EnflameGCUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             2,
				Memreq:           100,
				MemPercentagereq: 100,
				Coresreq:         100,
				Type:             EnflameGCUDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    1,
			wantDevIDs: []string{"dev-0"},
			wantReason: "1/1 AllocatedCardsInsufficientRequest",
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
				if len(result[EnflameGCUDevice]) != test.wantLen {
					t.Errorf("expected len: %d, got len %d", test.wantLen, len(result[EnflameGCUDevice]))
				}
				for idx, id := range test.wantDevIDs {
					if id != result[EnflameGCUDevice][idx].UUID {
						t.Errorf("expected device id: %s, got device id %s", id, result[EnflameGCUDevice][idx].UUID)
					}
				}
			}

			if reason != test.wantReason {
				t.Errorf("expected reason: %s, got reason: %s", test.wantReason, reason)
			}
		})
	}
}

func TestGCUDevices_AddResourceUsage(t *testing.T) {
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
				Usedcores: 0,
				Usedmem:   0,
			},
			ctr: &device.ContainerDevice{
				UUID:      "dev-0",
				Usedcores: 50,
				Usedmem:   100,
			},
			wantUsage: &device.DeviceUsage{
				ID:        "dev-0",
				Used:      1,
				Usedcores: 50,
				Usedmem:   100,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dev := &GCUDevices{}
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
