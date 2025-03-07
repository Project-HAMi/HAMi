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
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

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
		want []*util.DeviceInfo
		err  error
	}{
		{
			name: "no annotation",
			args: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			want: []*util.DeviceInfo{},
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
			want: []*util.DeviceInfo{
				{
					ID:      "test-0",
					Count:   int32(1),
					Devmem:  int32(1024),
					Devcore: int32(100),
					Type:    dev.CommonWord(),
					Numa:    0,
					Health:  true,
					Index:   uint(0),
					Mode:    "test",
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
			want: []*util.DeviceInfo{},
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
			want: []*util.DeviceInfo{},
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
			want: []*util.DeviceInfo{},
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

func Test_CheckType(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			annos map[string]string
			d     util.DeviceUsage
			n     util.ContainerDeviceRequest
		}
		want1 bool
		want2 bool
		want3 bool
	}{
		{
			name: "the same type",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
				n     util.ContainerDeviceRequest
			}{
				annos: map[string]string{
					"hygon.com/use-dcutype": "DCU",
				},
				d: util.DeviceUsage{
					Type: "DCU",
				},
				n: util.ContainerDeviceRequest{
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
				d     util.DeviceUsage
				n     util.ContainerDeviceRequest
			}{
				annos: map[string]string{
					"hygon.com/use-dcutype": "DCU",
				},
				d: util.DeviceUsage{
					Type: "DCU",
				},
				n: util.ContainerDeviceRequest{
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
			result1, result2, result3 := dev.CheckType(test.args.annos, test.args.d, test.args.n)
			assert.Equal(t, result1, test.want1)
			assert.Equal(t, result2, test.want2)
			assert.Equal(t, result3, test.want3)
		})
	}
}

func Test_CheckUUID(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			annos map[string]string
			d     util.DeviceUsage
		}
		want bool
	}{
		{
			name: "device id the same as the dcu in use uuid",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{
					"hygon.com/use-gpuuuid": "123",
				},
				d: util.DeviceUsage{
					ID: "123",
				},
			},
			want: true,
		},
		{
			name: "device id the different from the dcu in use uuid",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{
					"hygon.com/use-gpuuuid": "123",
				},
				d: util.DeviceUsage{
					ID: "456",
				},
			},
			want: false,
		},
		{
			name: "no dcu in use uuid annos",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{},
				d: util.DeviceUsage{
					ID: "456",
				},
			},
			want: true,
		},
		{
			name: "device id the same as the dcu no use uuid",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{
					"hygon.com/nouse-gpuuuid": "123",
				},
				d: util.DeviceUsage{
					ID: "123",
				},
			},
			want: false,
		},
		{
			name: "device id the different from the dcu no use uuid",
			args: struct {
				annos map[string]string
				d     util.DeviceUsage
			}{
				annos: map[string]string{
					"hygon.com/nouse-gpuuuid": "123",
				},
				d: util.DeviceUsage{
					ID: "456",
				},
			},
			want: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := DCUDevices{}
			result := dev.CheckUUID(test.args.annos, test.args.d)
			assert.Equal(t, result, test.want)
		})
	}
}

func Test_PatchAnnotations(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			annoinput *map[string]string
			pd        util.PodDevices
		}
		want map[string]string
	}{
		{
			name: "exist device",
			args: struct {
				annoinput *map[string]string
				pd        util.PodDevices
			}{
				annoinput: &map[string]string{},
				pd: util.PodDevices{
					HygonDCUDevice: util.PodSingleDevice{
						[]util.ContainerDevice{
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
				util.InRequestDevices[HygonDCUDevice]: "test1,DCU,2048,1:;",
				util.SupportDevices[HygonDCUDevice]:   "test1,DCU,2048,1:;",
			},
		},
		{
			name: "no device",
			args: struct {
				annoinput *map[string]string
				pd        util.PodDevices
			}{
				annoinput: &map[string]string{},
				pd:        util.PodDevices{},
			},
			want: map[string]string{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := DCUDevices{}
			result := dev.PatchAnnotations(test.args.annoinput, test.args.pd)
			assert.DeepEqual(t, result, test.want)
		})
	}
}

func Test_GenerateResourceRequests(t *testing.T) {
	tests := []struct {
		name string
		args *corev1.Container
		want util.ContainerDeviceRequest
	}{
		{
			name: "don't set to limits and request",
			args: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits:   corev1.ResourceList{},
					Requests: corev1.ResourceList{},
				},
			},
			want: util.ContainerDeviceRequest{},
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
			want: util.ContainerDeviceRequest{
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
			want: util.ContainerDeviceRequest{
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
			want: util.ContainerDeviceRequest{
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

func Test_NodeCleanUp(t *testing.T) {
	client.InitGlobalClient()
	tests := []struct {
		name string
		args string
	}{
		{
			name: "node name don't exist",
			args: "test-node1",
		},
		{
			name: "node name exist",
			args: "dcu-node1",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := DCUDevices{}
			ctx := context.Background()
			node := corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "dcu-node1",
				},
			}
			client.GetClient().CoreV1().Nodes().Create(ctx, &node, metav1.CreateOptions{})
			result := dev.NodeCleanUp(test.args)
			if result != nil {
				klog.Errorln("get node failed", result.Error())
			}
			client.GetClient().CoreV1().Nodes().Delete(ctx, node.Name, metav1.DeleteOptions{})
		})
	}
}
