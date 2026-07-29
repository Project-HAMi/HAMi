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

package kunlun

import (
	"context"
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
)

func testVConfig() KunlunConfig {
	return KunlunConfig{
		ResourceCountName:   "kunlunxin.com/xpu",
		ResourceVCountName:  "kunlunxin.com/vxpu",
		ResourceVMemoryName: "kunlunxin.com/vxpu-memory",
	}
}

func Test_InitKunlunVDevice(t *testing.T) {
	dev := InitKunlunVDevice(testVConfig())
	assert.Assert(t, dev != nil)
	assert.Equal(t, KunlunResourceVCount, "kunlunxin.com/vxpu")
	assert.Equal(t, KunlunResourceVMemory, "kunlunxin.com/vxpu-memory")
	assert.Equal(t, device.InRequestDevices[XPUDevice], "hami.io/xpu-devices-to-allocate")
	assert.Equal(t, device.SupportDevices[XPUDevice], "hami.io/xpu-devices-allocated")
	assert.Equal(t, util.HandshakeAnnos[XPUDevice], HandshakeAnnos)

	// Calling again must not panic and must not clobber the already-registered entries.
	dev2 := InitKunlunVDevice(testVConfig())
	assert.Assert(t, dev2 != nil)
	assert.Equal(t, device.InRequestDevices[XPUDevice], "hami.io/xpu-devices-to-allocate")
	assert.Equal(t, device.SupportDevices[XPUDevice], "hami.io/xpu-devices-allocated")
	assert.Equal(t, util.HandshakeAnnos[XPUDevice], HandshakeAnnos)
}

func Test_trimMemory(t *testing.T) {
	dev := &KunlunVDevices{}
	tests := []struct {
		name string
		in   int64
		want int64
	}{
		{name: "zero", in: 0, want: 24576},
		{name: "at first threshold", in: 24576, want: 24576},
		{name: "just above first threshold", in: 24577, want: 49152},
		{name: "at second threshold", in: 49152, want: 49152},
		{name: "just above second threshold", in: 49153, want: KunlunMaxMemory},
		{name: "far above", in: 200000, want: KunlunMaxMemory},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, dev.trimMemory(test.in), test.want)
		})
	}
}

func Test_CommonWord(t *testing.T) {
	dev := &KunlunVDevices{}
	assert.Equal(t, dev.CommonWord(), XPUDevice)
}

func Test_MutateAdmission(t *testing.T) {
	InitKunlunVDevice(testVConfig())
	dev := &KunlunVDevices{}

	tests := []struct {
		name       string
		ctr        *corev1.Container
		want       bool
		wantMemory string
	}{
		{
			name: "no vcount limit",
			ctr:  &corev1.Container{},
			want: false,
		},
		{
			name: "vcount without vmemory",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceName(KunlunResourceVCount): resource.MustParse("1"),
					},
				},
			},
			want: true,
		},
		{
			name: "vcount with vmemory gets trimmed",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceName(KunlunResourceVCount):  resource.MustParse("1"),
						corev1.ResourceName(KunlunResourceVMemory): resource.MustParse("30000"),
					},
					Requests: corev1.ResourceList{},
				},
			},
			want:       true,
			wantMemory: "49152",
		},
		{
			name: "vcount with vmemory and nil requests does not panic",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceName(KunlunResourceVCount):  resource.MustParse("1"),
						corev1.ResourceName(KunlunResourceVMemory): resource.MustParse("30000"),
					},
				},
			},
			want:       true,
			wantMemory: "49152",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := dev.MutateAdmission(test.ctr, &corev1.Pod{})
			assert.NilError(t, err)
			assert.Equal(t, got, test.want)
			if test.wantMemory != "" {
				gotQty := test.ctr.Resources.Limits[corev1.ResourceName(KunlunResourceVMemory)]
				assert.Equal(t, gotQty.String(), test.wantMemory)
				reqQty := test.ctr.Resources.Requests[corev1.ResourceName(KunlunResourceVMemory)]
				assert.Equal(t, reqQty.String(), test.wantMemory)
			}
		})
	}
}

func Test_KunlunVDevices_CheckHealth(t *testing.T) {
	InitKunlunVDevice(testVConfig())
	dev := &KunlunVDevices{}

	t.Run("unknown handshake state patches node via fake client", func(t *testing.T) {
		client.KubeClient = fake.NewClientset()
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-unknown",
				Annotations: map[string]string{
					util.HandshakeAnnos[XPUDevice]: "Unknown",
				},
			},
		}
		_, err := client.KubeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
		assert.NilError(t, err)

		online, deleted := dev.CheckHealth(XPUDevice, node)
		assert.Equal(t, online, true)
		assert.Equal(t, deleted, true)
	})

	t.Run("requesting state recently set is still online", func(t *testing.T) {
		client.KubeClient = fake.NewClientset()
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-requesting",
				Annotations: map[string]string{
					util.HandshakeAnnos[XPUDevice]: "Requesting_" + time.Now().Format(time.DateTime),
				},
			},
		}
		online, deleted := dev.CheckHealth(XPUDevice, node)
		assert.Equal(t, online, true)
		assert.Equal(t, deleted, false)
	})
}

func Test_NodeCleanUp(t *testing.T) {
	dev := &KunlunVDevices{}
	client.KubeClient = fake.NewClientset()
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "node-cleanup",
			Annotations: map[string]string{HandshakeAnnos: "Unknown"},
		},
	}
	_, err := client.KubeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
	assert.NilError(t, err)

	err = dev.NodeCleanUp(node.Name)
	assert.NilError(t, err)

	got, err := client.KubeClient.CoreV1().Nodes().Get(context.Background(), node.Name, metav1.GetOptions{})
	assert.NilError(t, err)
	_, ok := got.Annotations[HandshakeAnnos]
	assert.Equal(t, ok, false)
}

func Test_GetNodeDevices(t *testing.T) {
	dev := &KunlunVDevices{}

	tests := []struct {
		name    string
		node    corev1.Node
		wantLen int
		wantErr string
	}{
		{
			name:    "annotation missing",
			node:    corev1.Node{},
			wantLen: 0,
			wantErr: "annos not found " + RegisterAnnos,
		},
		{
			name: "invalid json",
			node: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{RegisterAnnos: "not-json"},
				},
			},
			wantLen: 0,
			wantErr: "invalid",
		},
		{
			name: "empty device list",
			node: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{RegisterAnnos: "[]"},
				},
			},
			wantLen: 0,
			wantErr: "no device found on node",
		},
		{
			name: "valid devices get vendor tagged",
			node: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						RegisterAnnos: `[{"id":"xpu-0","index":0,"count":1,"devmem":98304,"devcore":100,"type":"XPU","health":true}]`,
					},
				},
			},
			wantLen: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := dev.GetNodeDevices(test.node)
			assert.Equal(t, len(got), test.wantLen)
			if test.wantErr != "" {
				assert.ErrorContains(t, err, test.wantErr)
				return
			}
			assert.NilError(t, err)
			for _, d := range got {
				assert.Equal(t, d.DeviceVendor, XPUDevice)
			}
		})
	}
}

func Test_KunlunVDevices_PatchAnnotations(t *testing.T) {
	dev := &KunlunVDevices{}

	t.Run("with devices", func(t *testing.T) {
		annoinput := &map[string]string{}
		pd := device.PodDevices{
			XPUDevice: device.PodSingleDevice{
				device.ContainerDevices{
					{Idx: 0, UUID: "xpu-0", Type: XPUDevice, Usedmem: 24576, Usedcores: 100},
				},
			},
		}
		got := dev.PatchAnnotations(&corev1.Pod{}, annoinput, pd)
		assert.Assert(t, got[device.InRequestDevices[XPUDevice]] != "")
		assert.Assert(t, got[device.SupportDevices[XPUDevice]] != "")
	})

	t.Run("no devices for this type leaves annotations untouched", func(t *testing.T) {
		annoinput := &map[string]string{}
		pd := device.PodDevices{}
		got := dev.PatchAnnotations(&corev1.Pod{}, annoinput, pd)
		assert.Equal(t, len(got), 0)
	})
}

func Test_KunlunVDevices_LockNode(t *testing.T) {
	InitKunlunVDevice(testVConfig())
	tests := []struct {
		name    string
		pod     *corev1.Pod
		hasLock bool
	}{
		{
			name:    "no containers requesting xpu",
			pod:     &corev1.Pod{Spec: corev1.PodSpec{}},
			hasLock: false,
		},
		{
			name: "container requesting xpu locks node",
			pod: &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{
				Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
					corev1.ResourceName(KunlunResourceVCount): resource.MustParse("1"),
				}},
			}}}},
			hasLock: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := &KunlunVDevices{}
			client.KubeClient = fake.NewClientset()
			node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "lock-node"}}
			_, err := client.KubeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
			assert.NilError(t, err)

			err = dev.LockNode(node, test.pod)
			assert.NilError(t, err)

			got, err := client.KubeClient.CoreV1().Nodes().Get(context.Background(), node.Name, metav1.GetOptions{})
			assert.NilError(t, err)
			_, ok := got.Annotations[NodeLock]
			assert.Equal(t, ok, test.hasLock)
		})
	}
}

func Test_KunlunVDevices_ReleaseNodeLock(t *testing.T) {
	InitKunlunVDevice(testVConfig())
	tests := []struct {
		name    string
		pod     *corev1.Pod
		hasLock bool
	}{
		{
			name:    "no containers requesting xpu leaves lock",
			pod:     &corev1.Pod{Spec: corev1.PodSpec{}},
			hasLock: true,
		},
		{
			name: "container requesting xpu releases matching lock",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "owner-pod", Namespace: "default"},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
						corev1.ResourceName(KunlunResourceVCount): resource.MustParse("1"),
					}},
				}}},
			},
			hasLock: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := &KunlunVDevices{}
			client.KubeClient = fake.NewClientset()
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "release-node",
					Annotations: map[string]string{NodeLock: "lock-values,default,owner-pod"},
				},
			}
			_, err := client.KubeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
			assert.NilError(t, err)

			err = dev.ReleaseNodeLock(node, test.pod)
			assert.NilError(t, err)

			got, err := client.KubeClient.CoreV1().Nodes().Get(context.Background(), node.Name, metav1.GetOptions{})
			assert.NilError(t, err)
			_, ok := got.Annotations[NodeLock]
			assert.Equal(t, ok, test.hasLock)
		})
	}
}

func Test_KunlunVDevices_CheckType(t *testing.T) {
	dev := &KunlunVDevices{}

	t.Run("matching type", func(t *testing.T) {
		match, found := dev.CheckType(nil, device.DeviceUsage{}, device.ContainerDeviceRequest{Type: XPUDevice})
		assert.Equal(t, match, true)
		assert.Equal(t, found, false)
	})

	t.Run("different type", func(t *testing.T) {
		match, found := dev.CheckType(nil, device.DeviceUsage{}, device.ContainerDeviceRequest{Type: "other"})
		assert.Equal(t, match, false)
		assert.Equal(t, found, false)
	})
}

func Test_KunlunVDevices_GenerateResourceRequests(t *testing.T) {
	InitKunlunVDevice(testVConfig())
	dev := &KunlunVDevices{}

	tests := []struct {
		name string
		ctr  *corev1.Container
		want device.ContainerDeviceRequest
	}{
		{
			name: "no vcount requested",
			ctr:  &corev1.Container{},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "vcount without memory defaults to full device",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceName(KunlunResourceVCount): resource.MustParse("1"),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             1,
				Type:             XPUDevice,
				Memreq:           KunlunMaxMemory,
				MemPercentagereq: 100,
				Coresreq:         100,
			},
		},
		{
			name: "vcount with memory from requests fallback",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceName(KunlunResourceVCount):  resource.MustParse("2"),
						corev1.ResourceName(KunlunResourceVMemory): resource.MustParse("24576"),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             2,
				Type:             XPUDevice,
				Memreq:           24576,
				MemPercentagereq: 0,
				Coresreq:         25,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := dev.GenerateResourceRequests(test.ctr)
			assert.DeepEqual(t, got, test.want)
		})
	}
}

func Test_KunlunVDevices_ScoreNode(t *testing.T) {
	dev := &KunlunVDevices{}
	got := dev.ScoreNode(&corev1.Node{}, device.PodSingleDevice{}, nil, "binpack")
	assert.Equal(t, got, float32(0))
}

func Test_KunlunVDevices_AddResourceUsage(t *testing.T) {
	dev := &KunlunVDevices{}
	usage := &device.DeviceUsage{Used: 1, Usedcores: 10, Usedmem: 1000}
	ctr := &device.ContainerDevice{Usedcores: 20, Usedmem: 2000}

	err := dev.AddResourceUsage(&corev1.Pod{}, usage, ctr)
	assert.NilError(t, err)
	assert.Equal(t, usage.Used, int32(2))
	assert.Equal(t, usage.Usedcores, int32(30))
	assert.Equal(t, usage.Usedmem, int32(3000))
}

func Test_KunlunVDevices_GetResourceNames(t *testing.T) {
	InitKunlunVDevice(testVConfig())
	dev := &KunlunVDevices{}
	got := dev.GetResourceNames()
	assert.Equal(t, got.ResourceCountName, "kunlunxin.com/vxpu")
	assert.Equal(t, got.ResourceMemoryName, "kunlunxin.com/vxpu-memory")
	assert.Equal(t, got.ResourceCoreName, "")
}

func Test_FitVXPU_direct(t *testing.T) {
	tests := []struct {
		name    string
		usage   *device.DeviceUsage
		request device.ContainerDeviceRequest
		want    bool
	}{
		{
			name:    "would exceed total memory",
			usage:   &device.DeviceUsage{Usedmem: 90000, Totalmem: 98304},
			request: device.ContainerDeviceRequest{Memreq: 20000},
			want:    false,
		},
		{
			name:    "idle device always fits",
			usage:   &device.DeviceUsage{Used: 0, Usedmem: 0, Totalmem: 98304},
			request: device.ContainerDeviceRequest{Memreq: 24576},
			want:    true,
		},
		{
			name:    "shared device with matching average memory",
			usage:   &device.DeviceUsage{Used: 2, Usedmem: 49152, Totalmem: 98304},
			request: device.ContainerDeviceRequest{Memreq: 24576},
			want:    true,
		},
		{
			name:    "shared device with mismatched average memory",
			usage:   &device.DeviceUsage{Used: 1, Usedmem: 49152, Totalmem: 98304},
			request: device.ContainerDeviceRequest{Memreq: 24576},
			want:    false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, FitVXPU(test.usage, test.request), test.want)
		})
	}
}
