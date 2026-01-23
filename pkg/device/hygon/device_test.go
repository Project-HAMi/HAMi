/*cardtype string
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

package hygon

import (
	"context"
	"errors"
	"flag"
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
)

func Test_MutateAdmission(t *testing.T) {
	config := HygonConfig{
		ResourceMemoryName: "hygon.com/dcumem",
		ResourceCountName:  "hygon.com/dcunum",
		ResourceCoreName:   "hygon.com/dcucores",
	}
	InitDCUDevice(config)
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
							"hygon.com/dcunum": resource.MustParse("1"),
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
			dev := DCUDevices{}
			result, err := dev.MutateAdmission(test.args.ctr, test.args.p)
			if err != test.err {
				klog.InfoS("set to resource limits failed")
			}
			assert.Equal(t, result, test.want)
		})
	}
}

func Test_checkDCUtype(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			annos    map[string]string
			cardtype string
		}
		want bool
	}{
		{
			name: "exist one dcu in use type",
			args: struct {
				annos    map[string]string
				cardtype string
			}{
				annos: map[string]string{
					"hygon.com/use-dcutype": "dcu",
				},
				cardtype: "dcu",
			},
			want: true,
		},
		{
			name: "the different of dcu in use type",
			args: struct {
				annos    map[string]string
				cardtype string
			}{
				annos: map[string]string{
					"hygon.com/use-dcutype": "dcu",
				},
				cardtype: "test",
			},
			want: false,
		},
		{
			name: "no dcu in use annotation",
			args: struct {
				annos    map[string]string
				cardtype string
			}{
				annos:    map[string]string{},
				cardtype: "dcu",
			},
			want: true,
		},
		{
			name: "exist multi dcu in use type",
			args: struct {
				annos    map[string]string
				cardtype string
			}{
				annos: map[string]string{
					"hygon.com/use-dcutype": "dcu,test",
				},
				cardtype: "dcu",
			},
			want: true,
		},
		{
			name: "exist one dcu no use type",
			args: struct {
				annos    map[string]string
				cardtype string
			}{
				annos: map[string]string{
					"hygon.com/nouse-dcutype": "dcu",
				},
				cardtype: "dcu",
			},
			want: false,
		},
		{
			name: "no dcu in use annotation",
			args: struct {
				annos    map[string]string
				cardtype string
			}{
				annos:    map[string]string{},
				cardtype: "dcu",
			},
			want: true,
		},
		{
			name: "exist multi dcu no use type",
			args: struct {
				annos    map[string]string
				cardtype string
			}{
				annos: map[string]string{
					"hygon.com/nouse-dcutype": "test,dcu",
				},
				cardtype: "test",
			},
			want: false,
		},
		{
			name: "the different of dcu no use type",
			args: struct {
				annos    map[string]string
				cardtype string
			}{
				annos: map[string]string{
					"hygon.com/nouse-dcutype": "dcu",
				},
				cardtype: "test",
			},
			want: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := checkDCUtype(test.args.annos, test.args.cardtype)
			assert.Equal(t, result, test.want)
		})
	}
}

func Test_GetNodeDevices(t *testing.T) {
	dev := DCUDevices{}
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
			name: "exist dcu device",
			args: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"hami.io/node-dcu-register": "test-0,1,1024,100,DCU,0,true,0,test:",
					},
				},
			},
			want: []*device.DeviceInfo{
				{
					ID:           "test-0",
					Count:        int32(1),
					Devmem:       int32(1024),
					Devcore:      int32(100),
					Type:         dev.CommonWord(),
					Numa:         0,
					Health:       true,
					Index:        uint(0),
					Mode:         "test",
					DeviceVendor: HygonDCUCommonWord,
				},
			},
			err: nil,
		},
		{
			name: "no dcu device",
			args: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"hami.io/node-dcu-register": ":",
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
						"hami.io/node-dcu-register": "",
					},
				},
			},
			want: []*device.DeviceInfo{},
			err:  errors.New("node annotations not decode successfully"),
		},
		{
			name: "dcu device length less than 7",
			args: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"hami.io/node-dcu-register": "test-0,1,1024,100,DCU:",
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
				devType: "hygon.com/dcu",
				n: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							util.HandshakeAnnos["hygon.com/dcu"]: "Requesting_2025-01-07 00:00:00",
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
				devType: "hygon.com/dcu",
				n: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							util.HandshakeAnnos["hygon.com/dcu"]: "Deleted",
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
				devType: "hygon.com/dcu",
				n: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							util.HandshakeAnnos["hygon.com/dcu"]: "Unknown",
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
			dev := DCUDevices{}
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
				annos: map[string]string{
					"hygon.com/use-dcutype": "DCU",
				},
				d: device.DeviceUsage{
					Type: "DCU",
				},
				n: device.ContainerDeviceRequest{
					Type: "DCU",
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
				annos: map[string]string{
					"hygon.com/use-dcutype": "DCU",
				},
				d: device.DeviceUsage{
					Type: "DCU",
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
			dev := DCUDevices{}
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
					HygonDCUDevice: device.PodSingleDevice{
						[]device.ContainerDevice{
							{
								Idx:       1,
								UUID:      "test1",
								Type:      HygonDCUDevice,
								Usedmem:   int32(2048),
								Usedcores: int32(1),
							},
						},
					},
				},
			},
			want: map[string]string{
				device.InRequestDevices[HygonDCUDevice]: "test1,DCU,2048,1:;",
				device.SupportDevices[HygonDCUDevice]:   "test1,DCU,2048,1:;",
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
			dev := DCUDevices{}
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
			name: "dcuResourceCount,dcuesourceMem and dcuResourceCores set to limits and request",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"hygon.com/dcunum":   resource.MustParse("1"),
						"hygon.com/dcucores": resource.MustParse("1"),
						"hygon.com/dcumem":   resource.MustParse("1024"),
					},
					Requests: corev1.ResourceList{
						"hygon.com/dcunum":   resource.MustParse("1"),
						"hygon.com/dcucores": resource.MustParse("1"),
						"hygon.com/dcumem":   resource.MustParse("1024"),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             HygonDCUDevice,
				Memreq:           int32(1024),
				MemPercentagereq: int32(0),
				Coresreq:         int32(1),
			},
		},
		{
			name: "dcuResourceMem,dcuResourceCores don't set limit and dcuResourceCount set to limits and request",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"hygon.com/dcunum": resource.MustParse("1"),
					},
					Requests: corev1.ResourceList{
						"hygon.com/dcunum":   resource.MustParse("1"),
						"hygon.com/dcucores": resource.MustParse("1"),
						"hygon.com/dcumem":   resource.MustParse("1024"),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             HygonDCUDevice,
				Memreq:           int32(1024),
				MemPercentagereq: int32(0),
				Coresreq:         int32(1),
			},
		},
		{
			name: "dcuResourceMem don't set limit and request,dcuResourceCores don't set limit and dcuResourceCount set to limits and request",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"hygon.com/dcunum": resource.MustParse("1"),
					},
					Requests: corev1.ResourceList{
						"hygon.com/dcunum":   resource.MustParse("1"),
						"hygon.com/dcucores": resource.MustParse("1"),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             HygonDCUDevice,
				Memreq:           int32(0),
				MemPercentagereq: int32(100),
				Coresreq:         int32(1),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := DCUDevices{}
			fs := flag.FlagSet{}
			ParseConfig(&fs)
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
				"hygon.com/dcunum": resource.MustParse("1"),
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

			dev := InitDCUDevice(HygonConfig{
				ResourceCountName:  "hygon.com/dcunum",
				ResourceMemoryName: "hygon.com/dcumem",
				ResourceCoreName:   "hygon.com/dcucores",
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
			_, ok := node.Annotations[NodeLockDCU]
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
				"hygon.com/dcunum": resource.MustParse("1"),
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
					Annotations: map[string]string{"test-annotation-key": "test-annotation-value", device.InRequestDevices["DCU"]: "some-value", NodeLockDCU: "lock-values,default,nozerorr"},
				},
			}

			// Add the node to the fake clientset
			_, err := client.KubeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("Failed to create test node: %v", err)
			}

			dev := InitDCUDevice(HygonConfig{
				ResourceCountName:  "hygon.com/dcunum",
				ResourceMemoryName: "hygon.com/dcumem",
				ResourceCoreName:   "hygon.com/dcucores",
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
			_, ok := node.Annotations[NodeLockDCU]
			assert.Equal(t, ok, tt.hasLock)
		})
	}
}

func TestDevices_Fit(t *testing.T) {
	config := HygonConfig{
		ResourceCountName:  "hygon.com/dcunum",
		ResourceMemoryName: "hygon.com/dcumem",
		ResourceCoreName:   "hygon.com/dcucores",
	}
	dev := InitDCUDevice(config)

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
					Type:      HygonDCUDevice,
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
					Type:      HygonDCUDevice,
					Health:    true,
				},
			},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           64,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             HygonDCUDevice,
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
				Type:      HygonDCUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             HygonDCUDevice,
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
				Type:      HygonDCUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             HygonDCUDevice,
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
				Type:      HygonDCUDevice,
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
				Type:      HygonDCUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             2,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             HygonDCUDevice,
			},
			annos:      map[string]string{DCUUseUUID: "dev-0"},
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
				Type:      HygonDCUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             2,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             HygonDCUDevice,
			},
			annos:      map[string]string{DCUNoUseUUID: "dev-0"},
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
				Type:      HygonDCUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             HygonDCUDevice,
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
				Type:      HygonDCUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         120,
				Type:             HygonDCUDevice,
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
				Type:      HygonDCUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         100,
				Type:             HygonDCUDevice,
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
				Type:      HygonDCUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         0,
				Type:             HygonDCUDevice,
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
				Type:      HygonDCUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             2,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         20,
				Type:             HygonDCUDevice,
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
				Type:      HygonDCUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           0,
				MemPercentagereq: 10,
				Coresreq:         20,
				Type:             HygonDCUDevice,
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
				if len(result[HygonDCUDevice]) != test.wantLen {
					t.Errorf("expected len: %d, got len %d", test.wantLen, len(result[HygonDCUDevice]))
				}
				for idx, id := range test.wantDevIDs {
					if id != result[HygonDCUDevice][idx].UUID {
						t.Errorf("expected device id: %s, got device id %s", id, result[HygonDCUDevice][idx].UUID)
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
			dev := &DCUDevices{}
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
