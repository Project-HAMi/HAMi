/*
Copyright 2026 The HAMi Authors.

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

package vastai

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"
)

func Test_MutateAdmission(t *testing.T) {
	config := VastaiConfig{
		ResourceCountName: "vastaitech.com/va",
	}
	InitVastaiDevice(config)
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
			name: "set to resource limits",
			args: struct {
				ctr *corev1.Container
				p   *corev1.Pod
			}{
				ctr: &corev1.Container{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"vastaitech.com/va": resource.MustParse("1"),
						},
					},
				},
				p: &corev1.Pod{},
			},
			want: true,
			err:  nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := VastaiDevices{}
			result, err := dev.MutateAdmission(test.args.ctr, test.args.p)
			if err != test.err {
				klog.InfoS("set to resource limits failed")
			}
			assert.Equal(t, result, test.want)
		})
	}
}

func Test_GetNodeDevices(t *testing.T) {
	dev := VastaiDevices{}
	tests := []struct {
		name string
		args corev1.Node
		want []*device.DeviceInfo
		err  error
	}{
		{
			name: "no annotation",
			args: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			want: []*device.DeviceInfo{},
			err:  errors.New("annos not found " + RegisterAnnos),
		},
		{
			name: "exist vastai device",
			args: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"hami.io/node-va-register": "[{\"id\":\"7-0-batch-0\",\"count\":1,\"type\":\"Vastai\",\"health\":true,\"devicepairscore\":{}}]",
					},
				},
			},
			want: []*device.DeviceInfo{
				{
					ID:           "7-0-batch-0",
					Count:        int32(1),
					Devmem:       int32(0),
					Devcore:      int32(0),
					Type:         dev.CommonWord(),
					Numa:         0,
					Health:       true,
					Index:        uint(0),
					Mode:         "",
					DeviceVendor: VastaiCommonWord,
				},
			},
			err: nil,
		},
		{
			name: "no vasta device",
			args: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"hami.io/node-va-register": ":",
					},
				},
			},
			want: []*device.DeviceInfo{},
			err:  errors.New("no gpu found on node"),
		},
		{
			name: "node annotations not decode successfully",
			args: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"hami.io/node-va-register": "",
					},
				},
			},
			want: []*device.DeviceInfo{},
			err:  errors.New("node annotations not decode successfully"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := dev.GetNodeDevices(test.args)
			if err != nil {
				klog.Errorf("got %v, want %v", err, test.err)
			}
			assert.DeepEqual(t, result, test.want)
		})
	}
}

func Test_CheckHealth(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			devType string
			n       *corev1.Node
		}
		want1 bool
		want2 bool
	}{
		{
			name: "Requesting state expired",
			args: struct {
				devType string
				n       *corev1.Node
			}{
				devType: "vastaitech.com/va",
				n: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							util.HandshakeAnnos["hami.io/node-handshake-va"]: "Requesting_2025-01-07 00:00:00",
						},
					},
				},
			},
			want1: false,
			want2: false,
		},
		{
			name: "Deleted state",
			args: struct {
				devType string
				n       *corev1.Node
			}{
				devType: "vastaitech.com/va",
				n: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							util.HandshakeAnnos["hami.io/node-handshake-va"]: "Deleted",
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
				n       *corev1.Node
			}{
				devType: "vastaitech.com/va",
				n: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							util.HandshakeAnnos["hami.io/node-handshake-va"]: "Unknown",
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
			dev := VastaiDevices{}
			result1, result2 := dev.CheckHealth(test.args.devType, test.args.n)
			assert.Equal(t, result1, test.want1)
			assert.Equal(t, result2, test.want2)
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
				d: device.DeviceUsage{
					Type: "Vastai",
				},
				n: device.ContainerDeviceRequest{
					Type: "Vastai",
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
				d: device.DeviceUsage{
					Type: "Vastai",
				},
				n: device.ContainerDeviceRequest{
					Type: "test",
				},
			},
			want1: false,
			want2: false,
			want3: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := VastaiDevices{}
			result1, result2, result3 := dev.checkType(test.args.annos, test.args.d, test.args.n)
			assert.Equal(t, result1, test.want1)
			assert.Equal(t, result2, test.want2)
			assert.Equal(t, result3, test.want3)
		})
	}
}

func Test_PatchAnnotations(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			annoinput *map[string]string
			pd        device.PodDevices
		}
		want map[string]string
	}{
		{
			name: "exist device",
			args: struct {
				annoinput *map[string]string
				pd        device.PodDevices
			}{
				annoinput: &map[string]string{},
				pd: device.PodDevices{
					VastaiDevice: device.PodSingleDevice{
						[]device.ContainerDevice{
							{
								Idx:       1,
								UUID:      "test1",
								Type:      VastaiDevice,
								Usedmem:   int32(2048),
								Usedcores: int32(1),
							},
						},
					},
				},
			},
			want: map[string]string{
				device.InRequestDevices[VastaiDevice]: "test1,Vastai,2048,1:;",
				device.SupportDevices[VastaiDevice]:   "test1,Vastai,2048,1:;",
			},
		},
		{
			name: "no device",
			args: struct {
				annoinput *map[string]string
				pd        device.PodDevices
			}{
				annoinput: &map[string]string{},
				pd:        device.PodDevices{},
			},
			want: map[string]string{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := VastaiDevices{}
			result := dev.PatchAnnotations(&corev1.Pod{}, test.args.annoinput, test.args.pd)
			assert.DeepEqual(t, result, test.want)
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
			name: "don't set to limits and request",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits:   corev1.ResourceList{},
					Requests: corev1.ResourceList{},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "set to limits and request",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"vastaitech.com/va": resource.MustParse("1"),
					},
					Requests: corev1.ResourceList{
						"vastaitech.com/va": resource.MustParse("1"),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             VastaiDevice,
				Memreq:           int32(0),
				MemPercentagereq: int32(100),
				Coresreq:         int32(0),
			},
		},
		{
			name: "only set to limits",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"vastaitech.com/va": resource.MustParse("1"),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             VastaiDevice,
				Memreq:           int32(0),
				MemPercentagereq: int32(100),
				Coresreq:         int32(0),
			},
		},
		{
			name: "only set to request",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						"vastaitech.com/va": resource.MustParse("1"),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             VastaiDevice,
				Memreq:           int32(0),
				MemPercentagereq: int32(100),
				Coresreq:         int32(0),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := VastaiDevices{}
			result := dev.GenerateResourceRequests(test.args)
			assert.DeepEqual(t, result, test.want)
		})
	}
}

func TestDevices_LockNode(t *testing.T) {
	tests := []struct {
		name        string
		node        *corev1.Node
		pod         *corev1.Pod
		hasLock     bool
		expectError bool
	}{
		{
			name:        "Test with no containers",
			node:        &corev1.Node{},
			pod:         &corev1.Pod{Spec: corev1.PodSpec{}},
			hasLock:     false,
			expectError: false,
		},
		{
			name: "Test with non-zero resource requests",
			node: &corev1.Node{},
			pod: &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
				"vastaitech.com/va": resource.MustParse("1"),
			}}}}}},
			hasLock:     true,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize fake clientset and pre-load test data
			client.KubeClient = fake.NewSimpleClientset()
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "testNode",
					Annotations: map[string]string{"test-annotation-key": "test-annotation-value", device.InRequestDevices["DCU"]: "some-value"},
				},
			}

			// Add the node to the fake clientset
			_, err := client.KubeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("Failed to create test node: %v", err)
			}

			dev := InitVastaiDevice(VastaiConfig{
				ResourceCountName: "vastaitech.com/va",
			})
			err = dev.LockNode(node, tt.pod)
			if tt.expectError {
				assert.Equal(t, err != nil, true)
			} else {
				assert.NilError(t, err)
			}
			node, err = client.KubeClient.CoreV1().Nodes().Get(context.Background(), node.Name, metav1.GetOptions{})
			assert.NilError(t, err)
			fmt.Println(node.Annotations)
			_, ok := node.Annotations[nodelock.NodeLockKey]
			assert.Equal(t, ok, tt.hasLock)
		})
	}
}

func TestDevices_ReleaseNodeLock(t *testing.T) {
	tests := []struct {
		name        string
		node        *corev1.Node
		pod         *corev1.Pod
		hasLock     bool
		expectError bool
	}{
		{
			name:        "Test with no containers",
			node:        &corev1.Node{},
			pod:         &corev1.Pod{Spec: corev1.PodSpec{}},
			hasLock:     true,
			expectError: false,
		},
		{
			name: "Test with non-zero resource requests",
			node: &corev1.Node{},
			pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
				Name:      "nozerorr",
				Namespace: "default",
			}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
				"vastaitech.com/va": resource.MustParse("1"),
			}}}}}},
			hasLock:     false,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize fake clientset and pre-load test data
			client.KubeClient = fake.NewSimpleClientset()
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "testNode",
					Annotations: map[string]string{"test-annotation-key": "test-annotation-value", device.InRequestDevices[VastaiDevice]: "some-value", nodelock.NodeLockKey: "lock-values,default,nozerorr"},
				},
			}

			// Add the node to the fake clientset
			_, err := client.KubeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("Failed to create test node: %v", err)
			}

			dev := InitVastaiDevice(VastaiConfig{
				ResourceCountName: "vastaitech.com/va",
			})
			err = dev.ReleaseNodeLock(node, tt.pod)
			if tt.expectError {
				assert.Equal(t, err != nil, true)
			} else {
				assert.NilError(t, err)
			}
			node, err = client.KubeClient.CoreV1().Nodes().Get(context.Background(), node.Name, metav1.GetOptions{})
			assert.NilError(t, err)
			fmt.Println(node.Annotations)
			_, ok := node.Annotations[nodelock.NodeLockKey]
			assert.Equal(t, ok, tt.hasLock)
		})
	}
}

func TestDevices_Fit(t *testing.T) {
	config := VastaiConfig{
		ResourceCountName: "vastaitech.com/va",
	}
	dev := InitVastaiDevice(config)

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
					Totalmem:  0,
					Totalcore: 0,
					Usedcores: 0,
					Numa:      0,
					Type:      VastaiDevice,
					Health:    true,
				},
				{
					ID:        "dev-1",
					Index:     1,
					Used:      0,
					Count:     1,
					Usedmem:   0,
					Totalmem:  0,
					Totalcore: 0,
					Usedcores: 0,
					Numa:      0,
					Type:      VastaiDevice,
					Health:    true,
				},
			},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           0,
				MemPercentagereq: 0,
				Coresreq:         0,
				Type:             VastaiDevice,
			},
			annos:      map[string]string{},
			wantFit:    true,
			wantLen:    1,
			wantDevIDs: []string{"dev-0"},
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
				Totalmem:  0,
				Totalcore: 0,
				Usedcores: 0,
				Numa:      0,
				Health:    true,
				Type:      VastaiDevice,
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
				Count:     1,
				Usedmem:   0,
				Totalmem:  0,
				Totalcore: 0,
				Usedcores: 0,
				Numa:      0,
				Type:      VastaiDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           0,
				MemPercentagereq: 0,
				Coresreq:         0,
				Type:             VastaiDevice,
			},
			annos:      map[string]string{VastaiUseUUID: "dev-0"},
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
				Count:     1,
				Usedmem:   0,
				Totalmem:  0,
				Totalcore: 0,
				Usedcores: 0,
				Numa:      0,
				Type:      VastaiDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           0,
				MemPercentagereq: 0,
				Coresreq:         0,
				Type:             VastaiDevice,
			},
			annos:      map[string]string{VastaiNoUseUUID: "dev-0"},
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
				Used:      1,
				Count:     1,
				Usedmem:   0,
				Totalmem:  0,
				Totalcore: 0,
				Usedcores: 0,
				Numa:      0,
				Type:      VastaiDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           0,
				MemPercentagereq: 0,
				Coresreq:         0,
				Type:             VastaiDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardTimeSlicingExhausted",
		},
		{
			name: "fit fail:  AllocatedCardsInsufficientRequest",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      0,
				Count:     1,
				Usedmem:   0,
				Totalmem:  0,
				Totalcore: 0,
				Usedcores: 0,
				Numa:      0,
				Type:      VastaiDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             2,
				Memreq:           0,
				MemPercentagereq: 0,
				Coresreq:         0,
				Type:             VastaiDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
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
				if len(result[VastaiDevice]) != test.wantLen {
					t.Errorf("expected len: %d, got len %d", test.wantLen, len(result[VastaiDevice]))
				}
				for idx, id := range test.wantDevIDs {
					if id != result[VastaiDevice][idx].UUID {
						t.Errorf("expected device id: %s, got device id %s", id, result[VastaiDevice][idx].UUID)
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
	}{
		{
			name: "test add resource usage",
			deviceUsage: &device.DeviceUsage{
				ID:        "dev-0",
				Used:      1,
				Usedcores: 0,
				Usedmem:   0,
			},
			ctr: &device.ContainerDevice{
				UUID:      "dev-0",
				Usedcores: 0,
				Usedmem:   0,
			},
			wantUsage: &device.DeviceUsage{
				ID:        "dev-0",
				Used:      2,
				Usedcores: 0,
				Usedmem:   0,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dev := &VastaiDevices{}
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
