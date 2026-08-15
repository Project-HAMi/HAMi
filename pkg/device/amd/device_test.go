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

package amd

import (
	"math"
	"strings"
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_MutateAdmission(t *testing.T) {
	tests := []struct {
		name    string
		ctr     *corev1.Container
		want    bool
		wantErr string
	}{
		{
			name: "gpu count in limits",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"amd.com/gpu": *resource.NewQuantity(2, resource.DecimalSI),
					},
				},
			},
			want: true,
		},
		{
			name: "gpu memory only (no count) -> false",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"amd.com/gpu-mem": *resource.NewQuantity(1024, resource.DecimalSI),
					},
				},
			},
			want: false,
		},
		{
			name: "core percentage only (no count) -> false",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"amd.com/gpu-core-pct": *resource.NewQuantity(50, resource.DecimalSI),
					},
				},
			},
			want: false,
		},
		{
			name: "requests only (limits empty) -> false",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						"amd.com/gpu": *resource.NewQuantity(1, resource.DecimalSI),
					},
				},
			},
			want: false,
		},
		{
			name: "rejects zero core percentage",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"amd.com/gpu":          *resource.NewQuantity(1, resource.DecimalSI),
						"amd.com/gpu-core-pct": *resource.NewQuantity(0, resource.DecimalSI),
					},
				},
			},
			wantErr: "must be an integer percentage between 1 and 100",
		},
		{
			name: "rejects core percentage above 100",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"amd.com/gpu":          *resource.NewQuantity(1, resource.DecimalSI),
						"amd.com/gpu-core-pct": *resource.NewQuantity(101, resource.DecimalSI),
					},
				},
			},
			wantErr: "must be an integer percentage between 1 and 100",
		},
		{
			name: "accepts partial memory without a core percentage",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"amd.com/gpu":     *resource.NewQuantity(1, resource.DecimalSI),
						"amd.com/gpu-mem": *resource.NewQuantity(1024, resource.DecimalSI),
					},
				},
			},
			want: true,
		},
		{
			name: "no resources",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dev := InitAMDGPUDevice(AMDConfig{
				ResourceCountName:  "amd.com/gpu",
				ResourceMemoryName: "amd.com/gpu-mem",
				ResourceCoreName:   "amd.com/gpu-core-pct",
			})
			got, err := dev.MutateAdmission(tt.ctr, &corev1.Pod{})
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			assert.NilError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_GetNodeDevices(t *testing.T) {
	const registerJSON = `[{"id":"GPU-0","index":0,"count":1,"devmem":8192,"devcore":100,"type":"amd","numa":0,"health":true}]`

	dev := InitAMDGPUDevice(AMDConfig{ResourceCountName: "amd.com/gpu"})
	node := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-1",
			Annotations: map[string]string{
				RegisterAnnos: registerJSON,
			},
		},
	}

	got, err := dev.GetNodeDevices(node)
	assert.NilError(t, err)
	assert.Equal(t, 1, len(got))
	assert.Equal(t, "GPU-0", got[0].ID)
	assert.Equal(t, "amd", got[0].Type)
	assert.Equal(t, AMDCommonWord, got[0].DeviceVendor)

	_, err = dev.GetNodeDevices(corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-2"}})
	assert.ErrorContains(t, err, "annos not found")
}

func Test_PatchAnnotations(t *testing.T) {
	dev := InitAMDGPUDevice(AMDConfig{ResourceCountName: "amd.com/gpu"})
	pd := device.PodDevices{
		AMDDevice: device.PodSingleDevice{
			{
				{Idx: 0, UUID: "test1", Type: AMDDevice, Usedmem: 100, Usedcores: 3},
				{Idx: 1, UUID: "test2", Type: AMDDevice, Usedmem: 200, Usedcores: 4},
			},
		},
	}
	want := device.EncodePodSingleDevice(pd[AMDDevice])

	annos := map[string]string{}
	out := dev.PatchAnnotations(&corev1.Pod{}, &annos, pd)

	assert.Equal(t, want, out[device.InRequestDevices[AMDDevice]])
	assert.Equal(t, want, out[device.SupportDevices[AMDDevice]])
}

func Test_checkType(t *testing.T) {
	dev := AMDDevices{}
	ok, found, _ := dev.checkType(
		map[string]string{},
		device.DeviceUsage{Type: "AMD-MI300X"},
		device.ContainerDeviceRequest{Type: AMDDevice},
	)
	assert.Equal(t, true, ok)
	assert.Equal(t, true, found)

	_, found, _ = dev.checkType(
		map[string]string{AMDInUse: "MI250"},
		device.DeviceUsage{Type: "AMD-MI300X"},
		device.ContainerDeviceRequest{Type: AMDDevice},
	)
	assert.Equal(t, false, found)

	ok, _, _ = dev.checkType(
		map[string]string{},
		device.DeviceUsage{Type: "AMD-MI300X"},
		device.ContainerDeviceRequest{Type: "other"},
	)
	assert.Equal(t, false, ok)
}

func Test_GenerateResourceRequests(t *testing.T) {
	dev := InitAMDGPUDevice(AMDConfig{
		ResourceCountName:  "amd.com/gpu",
		ResourceMemoryName: "amd.com/gpu-mem",
		ResourceCoreName:   "amd.com/gpu-core-pct",
	})

	t.Run("parses limits only", func(t *testing.T) {
		ctr := &corev1.Container{
			Name: "c1",
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"amd.com/gpu":          *resource.NewQuantity(2, resource.DecimalSI),
					"amd.com/gpu-mem":      *resource.NewQuantity(4096, resource.DecimalSI),
					"amd.com/gpu-core-pct": *resource.NewQuantity(33, resource.DecimalSI),
				},
				Requests: corev1.ResourceList{
					// must be ignored
					"amd.com/gpu":          *resource.NewQuantity(99, resource.DecimalSI),
					"amd.com/gpu-mem":      *resource.NewQuantity(99, resource.DecimalSI),
					"amd.com/gpu-core-pct": *resource.NewQuantity(99, resource.DecimalSI),
				},
			},
		}
		got := dev.GenerateResourceRequests(ctr)
		assert.DeepEqual(t, device.ContainerDeviceRequest{
			Nums:             2,
			Type:             AMDDevice,
			Memreq:           4096,
			MemPercentagereq: 0,
			Coresreq:         33,
		}, got)
	})

	t.Run("no gpu limits -> empty", func(t *testing.T) {
		ctr := &corev1.Container{
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"amd.com/gpu-mem":      *resource.NewQuantity(4096, resource.DecimalSI),
					"amd.com/gpu-core-pct": *resource.NewQuantity(33, resource.DecimalSI),
				},
			},
		}
		got := dev.GenerateResourceRequests(ctr)
		assert.DeepEqual(t, device.ContainerDeviceRequest{}, got)
	})

	t.Run("defaults an omitted core limit to 100 percent", func(t *testing.T) {
		ctr := &corev1.Container{
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"amd.com/gpu":     *resource.NewQuantity(1, resource.DecimalSI),
					"amd.com/gpu-mem": *resource.NewQuantity(4096, resource.DecimalSI),
				},
			},
		}
		got := dev.GenerateResourceRequests(ctr)
		assert.Equal(t, int32(100), got.Coresreq)
	})

	for _, tc := range []struct {
		name     string
		resource corev1.ResourceName
		value    int64
	}{
		{name: "rejects zero count", resource: "amd.com/gpu", value: 0},
		{name: "rejects overflowing count", resource: "amd.com/gpu", value: math.MaxInt32 + 1},
		{name: "rejects negative memory", resource: "amd.com/gpu-mem", value: -1},
		{name: "rejects overflowing memory", resource: "amd.com/gpu-mem", value: math.MaxInt32 + 1},
		{name: "rejects zero core percentage", resource: "amd.com/gpu-core-pct", value: 0},
		{name: "rejects excessive core percentage", resource: "amd.com/gpu-core-pct", value: 101},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := corev1.ResourceList{
				"amd.com/gpu": *resource.NewQuantity(1, resource.DecimalSI),
			}
			limits[tc.resource] = *resource.NewQuantity(tc.value, resource.DecimalSI)
			ctr := &corev1.Container{Resources: corev1.ResourceRequirements{Limits: limits}}
			assert.DeepEqual(t, device.ContainerDeviceRequest{}, dev.GenerateResourceRequests(ctr))
		})
	}
}

func TestDevices_Fit(t *testing.T) {
	dev := InitAMDGPUDevice(AMDConfig{ResourceCountName: "amd.com/gpu"})

	t.Run("allocate two whole cards (memreq=0 uses totalmem; coresreq=0 uses totalcore when whole mem)", func(t *testing.T) {
		devices := []*device.DeviceUsage{
			{
				ID: "dev-0", Index: 0, Used: 0, Count: 2,
				Usedmem: 0, Totalmem: 1000, Totalcore: 100, Usedcores: 0,
				Type: AMDDevice, Health: true, CustomInfo: map[string]any{},
			},
			{
				ID: "dev-1", Index: 1, Used: 0, Count: 12,
				Usedmem: 0, Totalmem: 1000, Totalcore: 100, Usedcores: 0,
				Type: AMDDevice, Health: true, CustomInfo: map[string]any{},
			},
		}
		req := device.ContainerDeviceRequest{Nums: 2, Type: AMDDevice}
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}

		ok, got, reason := dev.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
		assert.Equal(t, true, ok)
		assert.Equal(t, 2, len(got[AMDDevice]))
		// reverse iteration prefers higher-count card first
		assert.Equal(t, "dev-1", got[AMDDevice][0].UUID)
		assert.Equal(t, "dev-0", got[AMDDevice][1].UUID)
		assert.Equal(t, int32(1000), got[AMDDevice][0].Usedmem)
		assert.Equal(t, int32(100), got[AMDDevice][0].Usedcores)
		assert.Equal(t, "", reason)
	})

	t.Run("core percentage maps per device", func(t *testing.T) {
		devices := []*device.DeviceUsage{
			{
				ID: "dev-0", Index: 0, Used: 0, Count: 2,
				Usedmem: 0, Totalmem: 1000, Totalcore: 10, Usedcores: 0,
				Type: AMDDevice, Health: true, CustomInfo: map[string]any{},
			},
			{
				ID: "dev-1", Index: 1, Used: 0, Count: 12,
				Usedmem: 0, Totalmem: 1000, Totalcore: 100, Usedcores: 0,
				Type: AMDDevice, Health: true, CustomInfo: map[string]any{},
			},
		}
		req := device.ContainerDeviceRequest{Nums: 1, Type: AMDDevice, Memreq: 500, MemPercentagereq: 0, Coresreq: 50}
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}

		ok, got, reason := dev.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
		assert.Equal(t, true, ok)
		assert.Equal(t, "dev-1", got[AMDDevice][0].UUID)
		assert.Equal(t, int32(500), got[AMDDevice][0].Usedmem)
		assert.Equal(t, int32(50), got[AMDDevice][0].Usedcores) // 50% of 100
		assert.Equal(t, "", reason)
	})

	t.Run("retains the registered product type in the allocation", func(t *testing.T) {
		const productType = "AMD_Instinct_MI300X_VF"
		devices := []*device.DeviceUsage{
			{
				ID: "dev-0", Index: 0, Used: 0, Count: 2,
				Usedmem: 0, Totalmem: 1000, Totalcore: 100, Usedcores: 0,
				Type: productType, Health: true, CustomInfo: map[string]any{},
			},
		}
		req := device.ContainerDeviceRequest{Nums: 1, Type: AMDDevice, Memreq: 100, Coresreq: 10}
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}

		ok, got, reason := dev.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
		assert.Equal(t, true, ok)
		assert.Equal(t, productType, got[AMDDevice][0].Type)
		assert.Equal(t, "", reason)
	})

	t.Run("clamps a positive percentage to at least one CU", func(t *testing.T) {
		devices := []*device.DeviceUsage{{
			ID: "dev-0", Index: 0, Count: 2, Totalmem: 1000, Totalcore: 64,
			Type: AMDDevice, Health: true, CustomInfo: map[string]any{},
		}}
		req := device.ContainerDeviceRequest{Nums: 1, Type: AMDDevice, Memreq: 100, Coresreq: 1}
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}

		ok, got, _ := dev.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
		assert.Equal(t, true, ok)
		assert.Equal(t, int32(1), got[AMDDevice][0].Usedcores)
	})

	t.Run("clamps a core request to the device total", func(t *testing.T) {
		devices := []*device.DeviceUsage{{
			ID: "dev-0", Index: 0, Count: 2, Totalmem: 1000, Totalcore: 64,
			Type: AMDDevice, Health: true, CustomInfo: map[string]any{},
		}}
		req := device.ContainerDeviceRequest{Nums: 1, Type: AMDDevice, Memreq: 100, Coresreq: 101}
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}

		ok, got, _ := dev.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
		assert.Equal(t, true, ok)
		assert.Equal(t, int32(64), got[AMDDevice][0].Usedcores)
	})

	t.Run("insufficient memory", func(t *testing.T) {
		devices := []*device.DeviceUsage{
			{
				ID: "dev-0", Index: 0, Used: 0, Count: 2,
				Usedmem: 900, Totalmem: 1000, Totalcore: 100, Usedcores: 0,
				Type: AMDDevice, Health: true, CustomInfo: map[string]any{},
			},
		}
		req := device.ContainerDeviceRequest{Nums: 1, Type: AMDDevice, Memreq: 500, MemPercentagereq: 0, Coresreq: 10}
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}

		ok, _, reason := dev.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
		assert.Equal(t, false, ok)
		assert.Assert(t, strings.Contains(reason, common.CardInsufficientMemory))
	})

	t.Run("insufficient core", func(t *testing.T) {
		devices := []*device.DeviceUsage{
			{
				ID: "dev-0", Index: 0, Used: 0, Count: 2,
				Usedmem: 0, Totalmem: 1000, Totalcore: 10, Usedcores: 9,
				Type: AMDDevice, Health: true, CustomInfo: map[string]any{},
			},
		}
		req := device.ContainerDeviceRequest{Nums: 1, Type: AMDDevice, Memreq: 100, MemPercentagereq: 0, Coresreq: 100}
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}

		ok, _, reason := dev.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
		assert.Equal(t, false, ok)
		assert.Assert(t, strings.Contains(reason, common.CardInsufficientCore))
	})

	t.Run("uuid mismatch (use)", func(t *testing.T) {
		devices := []*device.DeviceUsage{
			{
				ID: "dev-1", Index: 0, Used: 0, Count: 2,
				Usedmem: 0, Totalmem: 1000, Totalcore: 100, Usedcores: 0,
				Type: AMDDevice, Health: true, CustomInfo: map[string]any{},
			},
		}
		req := device.ContainerDeviceRequest{Nums: 1, Type: AMDDevice, Memreq: 100, MemPercentagereq: 0, Coresreq: 10}
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{AMDUseUUID: "dev-0"}}}

		ok, _, reason := dev.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
		assert.Equal(t, false, ok)
		assert.Assert(t, strings.Contains(reason, common.CardUUIDMismatch))
	})

	t.Run("time slicing exhausted", func(t *testing.T) {
		devices := []*device.DeviceUsage{
			{
				ID: "dev-0", Index: 0, Used: 2, Count: 2,
				Usedmem: 0, Totalmem: 1000, Totalcore: 100, Usedcores: 0,
				Type: AMDDevice, Health: true, CustomInfo: map[string]any{},
			},
		}
		req := device.ContainerDeviceRequest{Nums: 1, Type: AMDDevice, Memreq: 100, MemPercentagereq: 0, Coresreq: 10}
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}

		ok, _, reason := dev.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
		assert.Equal(t, false, ok)
		assert.Assert(t, strings.Contains(reason, common.CardTimeSlicingExhausted))
	})

	t.Run("unhealthy device is rejected", func(t *testing.T) {
		devices := []*device.DeviceUsage{
			{
				ID: "dev-0", Index: 0, Used: 0, Count: 2,
				Usedmem: 0, Totalmem: 1000, Totalcore: 100, Usedcores: 0,
				Type: AMDDevice, Health: false, CustomInfo: map[string]any{},
			},
		}
		req := device.ContainerDeviceRequest{Nums: 1, Type: AMDDevice, Memreq: 100, MemPercentagereq: 0, Coresreq: 10}
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}

		ok, _, reason := dev.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
		assert.Equal(t, false, ok)
		assert.Assert(t, strings.Contains(reason, common.CardNotHealth))
	})
}

func TestCheckAMDType(t *testing.T) {
	tests := []struct {
		name     string
		annos    map[string]string
		cardType string
		want     bool
	}{
		{"no annotations", map[string]string{}, "MI300X", true},
		{"matching use type", map[string]string{AMDInUse: "MI300"}, "MI300X", true},
		{"non-matching use type", map[string]string{AMDInUse: "MI250"}, "MI300X", false},
		{"matching nouse type excludes card", map[string]string{AMDNoUse: "MI300"}, "MI300X", false},
		{"non-matching nouse type keeps card", map[string]string{AMDNoUse: "MI250"}, "MI300X", true},
		// Regression: an empty nouse-gputype annotation must not exclude every card.
		{"empty nouse annotation keeps card", map[string]string{AMDNoUse: ""}, "MI300X", true},
		{"empty use annotation keeps card", map[string]string{AMDInUse: ""}, "MI300X", true},
		{"whitespace-only nouse annotation keeps card", map[string]string{AMDNoUse: "   "}, "MI300X", true},
		{"whitespace-only use annotation keeps card", map[string]string{AMDInUse: "   "}, "MI300X", true},
		// Regression: a trailing/leading empty member in a comma-separated list must not
		// match every card via strings.Contains(cardType, "").
		{"nouse with trailing comma only excludes named type", map[string]string{AMDNoUse: "MI250,"}, "MI300X", true},
		{"nouse with leading comma only excludes named type", map[string]string{AMDNoUse: ",MI250"}, "MI300X", true},
		{"nouse with blank member still excludes named type", map[string]string{AMDNoUse: " MI250 , "}, "MI250X", false},
		{"nouse with only commas keeps card", map[string]string{AMDNoUse: ", "}, "MI300X", true},
		{"use with trailing comma matches named type only", map[string]string{AMDInUse: "MI300,"}, "MI250X", false},
		{"use with only commas rejects card", map[string]string{AMDInUse: ", "}, "MI300X", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkAMDType(tt.annos, tt.cardType)
			assert.Equal(t, tt.want, got)
		})
	}
}
