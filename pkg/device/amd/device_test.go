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
			name: "gpu memory in limits",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"amd.com/gpu-mem": *resource.NewQuantity(1024, resource.DecimalSI),
					},
				},
			},
			want: true,
		},
		{
			name: "core percentage in limits",
			ctr: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"amd.com/gpu-core-pct": *resource.NewQuantity(50, resource.DecimalSI),
					},
				},
			},
			want: true,
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
		got, _ := dev.GenerateResourceRequests(ctr)
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
		got, _ := dev.GenerateResourceRequests(ctr)
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
		got, _ := dev.GenerateResourceRequests(ctr)
		assert.Equal(t, int32(100), got.Coresreq)
	})

	t.Run("uses core percentage from requests when limits omitted", func(t *testing.T) {
		ctr := &corev1.Container{
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"amd.com/gpu": *resource.NewQuantity(1, resource.DecimalSI),
				},
				Requests: corev1.ResourceList{
					"amd.com/gpu-core-pct": *resource.NewQuantity(42, resource.DecimalSI),
				},
			},
		}
		got, _ := dev.GenerateResourceRequests(ctr)
		assert.Equal(t, int32(42), got.Coresreq)
	})

	t.Run("cores 1, 50, 100 accepted and out-of-range rejected", func(t *testing.T) {
		for _, cores := range []int64{1, 50, 100} {
			ctr := &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"amd.com/gpu":          *resource.NewQuantity(1, resource.DecimalSI),
						"amd.com/gpu-core-pct": *resource.NewQuantity(cores, resource.DecimalSI),
					},
				},
			}
			got, _ := dev.GenerateResourceRequests(ctr)
			assert.Equal(t, int32(cores), got.Coresreq)
		}
		for _, cores := range []int64{0, 101, 150, 200, -1} {
			ctr := &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"amd.com/gpu":          *resource.NewQuantity(1, resource.DecimalSI),
						"amd.com/gpu-core-pct": *resource.NewQuantity(cores, resource.DecimalSI),
					},
				},
			}
			got, _ := dev.GenerateResourceRequests(ctr)
			assert.DeepEqual(t, device.ContainerDeviceRequest{}, got)
		}
		for _, rawCore := range []string{"50m", "99.1"} {
			ctr := &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"amd.com/gpu":          *resource.NewQuantity(1, resource.DecimalSI),
						"amd.com/gpu-core-pct": resource.MustParse(rawCore),
					},
				},
			}
			got, _ := dev.GenerateResourceRequests(ctr)
			assert.DeepEqual(t, device.ContainerDeviceRequest{}, got)
		}
	})

	t.Run("zero count is device-less, not invalid", func(t *testing.T) {
		ctr := &corev1.Container{Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
			"amd.com/gpu": *resource.NewQuantity(0, resource.DecimalSI),
		}}}
		got, err := dev.GenerateResourceRequests(ctr)
		assert.NilError(t, err)
		assert.DeepEqual(t, device.ContainerDeviceRequest{}, got)
	})

	for _, tc := range []struct {
		name     string
		resource corev1.ResourceName
		value    int64
	}{
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
			got, err := dev.GenerateResourceRequests(ctr)
			assert.DeepEqual(t, device.ContainerDeviceRequest{}, got)
			assert.ErrorContains(t, err, "out of range")
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

	t.Run("rounds cores up to whole WGPs on RDNA", func(t *testing.T) {
		for _, tc := range []struct {
			name                  string
			total, used, coresReq int32
			info                  map[string]any
			wantCores             int32
			wantOK                bool
		}{
			{"rdna odd cores", 64, 0, 5, map[string]any{"cuPerWGP": float64(2)}, 4, true},
			{"cdna keeps single cus", 64, 0, 5, map[string]any{}, 3, true},
			{"one-wgp apu", 2, 0, 50, map[string]any{"cuPerWGP": float64(2)}, 2, true},
			{"huge wgp size does not overflow", 4, 0, 75, map[string]any{"cuPerWGP": float64(math.MaxInt32)}, 4, true},
			{"fractional wgp size is ignored", 64, 0, 5, map[string]any{"cuPerWGP": 2.5}, 3, true},
			{"apu wgp already taken", 2, 2, 50, map[string]any{"cuPerWGP": float64(2)}, 0, false},
		} {
			devices := []*device.DeviceUsage{{
				ID: "dev-0", Count: 10, Totalmem: 1000, Totalcore: tc.total, Usedcores: tc.used,
				Type: AMDDevice, Health: true, CustomInfo: tc.info,
			}}
			req := device.ContainerDeviceRequest{Nums: 1, Type: AMDDevice, Memreq: 100, Coresreq: tc.coresReq}
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}
			ok, got, _ := dev.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
			assert.Equal(t, tc.wantOK, ok, tc.name)
			if tc.wantOK {
				assert.Equal(t, tc.wantCores, got[AMDDevice][0].Usedcores, tc.name)
			}
		}
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

	t.Run("rejects an out of range core request", func(t *testing.T) {
		devices := []*device.DeviceUsage{{
			ID: "dev-0", Index: 0, Count: 2, Totalmem: 1000, Totalcore: 64,
			Type: AMDDevice, Health: true, CustomInfo: map[string]any{},
		}}
		for _, cores := range []int32{101, 150, 200, -1} {
			req := device.ContainerDeviceRequest{Nums: 1, Type: AMDDevice, Memreq: 100, Coresreq: cores}
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}

			ok, _, reason := dev.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
			assert.Equal(t, false, ok)
			assert.Equal(t, "core limit out of range", reason)
		}
	})

	t.Run("empty devices with invalid coresreq returns out of range", func(t *testing.T) {
		req := device.ContainerDeviceRequest{Nums: 1, Type: AMDDevice, Memreq: 100, Coresreq: 150}
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}
		ok, _, reason := dev.Fit([]*device.DeviceUsage{}, req, pod, &device.NodeInfo{}, &device.PodDevices{})
		assert.Equal(t, false, ok)
		assert.Equal(t, "core limit out of range", reason)
	})

	t.Run("mismatched device type with invalid coresreq returns out of range", func(t *testing.T) {
		req := device.ContainerDeviceRequest{Nums: 1, Type: AMDDevice, Memreq: 100, Coresreq: -1}
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}
		mismatchDevs := []*device.DeviceUsage{
			{ID: "other-0", Type: "OtherType", Health: true},
		}
		ok, _, reason := dev.Fit(mismatchDevs, req, pod, &device.NodeInfo{}, &device.PodDevices{})
		assert.Equal(t, false, ok)
		assert.Equal(t, "core limit out of range", reason)
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

func TestMemoryPercentage(t *testing.T) {
	dev := InitAMDGPUDevice(AMDConfig{
		ResourceCountName:            "amd.com/gpu",
		ResourceMemoryName:           "amd.com/gpumem",
		ResourceMemoryPercentageName: "amd.com/gpumem-percentage",
	})
	ctr := func(pct string) *corev1.Container {
		return &corev1.Container{
			Name: "c1",
			Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
				"amd.com/gpu":               *resource.NewQuantity(1, resource.DecimalSI),
				"amd.com/gpumem-percentage": resource.MustParse(pct),
			}},
		}
	}

	t.Run("mutate admits a percentage-only container", func(t *testing.T) {
		c := &corev1.Container{Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
			"amd.com/gpumem-percentage": *resource.NewQuantity(50, resource.DecimalSI),
		}}}
		ok, err := dev.MutateAdmission(c, &corev1.Pod{})
		assert.NilError(t, err)
		assert.Equal(t, true, ok)
		got, err := dev.GenerateResourceRequests(c)
		assert.NilError(t, err)
		assert.Equal(t, int32(1), got.Nums)
		assert.Equal(t, int32(50), got.MemPercentagereq)
	})

	t.Run("a percentage only in requests still gets one card", func(t *testing.T) {
		c := &corev1.Container{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
			"amd.com/gpumem-percentage": *resource.NewQuantity(50, resource.DecimalSI),
		}}}
		ok, err := dev.MutateAdmission(c, &corev1.Pod{})
		assert.NilError(t, err)
		assert.Equal(t, true, ok)
		got, err := dev.GenerateResourceRequests(c)
		assert.NilError(t, err)
		assert.Equal(t, int32(1), got.Nums)
		assert.Equal(t, int32(50), got.MemPercentagereq)
	})

	for _, bad := range []string{"101", "-1", "1500m"} {
		t.Run("rejects "+bad, func(t *testing.T) {
			_, err := dev.MutateAdmission(ctr(bad), &corev1.Pod{})
			assert.ErrorContains(t, err, "must be an integer between 0 and 100")
			c := ctr(bad)
			delete(c.Resources.Limits, "amd.com/gpu")
			_, err = dev.MutateAdmission(c, &corev1.Pod{})
			assert.ErrorContains(t, err, "must be an integer between 0 and 100")
			_, err = dev.GenerateResourceRequests(ctr(bad))
			assert.ErrorContains(t, err, "must be an integer between 0 and 100")
		})
	}

	t.Run("generate carries the percentage and ignores zero", func(t *testing.T) {
		got, err := dev.GenerateResourceRequests(ctr("40"))
		assert.NilError(t, err)
		assert.Equal(t, int32(40), got.MemPercentagereq)
		got, err = dev.GenerateResourceRequests(ctr("0"))
		assert.NilError(t, err)
		assert.Equal(t, int32(0), got.MemPercentagereq)
	})

	t.Run("fit converts the percentage to memory per card", func(t *testing.T) {
		devices := []*device.DeviceUsage{{
			ID: "dev-0", Count: 4, Totalmem: 16384, Totalcore: 100,
			Type: AMDDevice, Health: true, CustomInfo: map[string]any{},
		}}
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}
		req := device.ContainerDeviceRequest{Nums: 1, Type: AMDDevice, MemPercentagereq: 25, Coresreq: 50}
		ok, got, reason := dev.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
		assert.Equal(t, true, ok, reason)
		assert.Equal(t, int32(4096), got[AMDDevice][0].Usedmem)

		// an absolute request wins over the percentage
		req.Memreq = 1024
		ok, got, reason = dev.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
		assert.Equal(t, true, ok, reason)
		assert.Equal(t, int32(1024), got[AMDDevice][0].Usedmem)

		// 100% books the whole card
		req = device.ContainerDeviceRequest{Nums: 1, Type: AMDDevice, MemPercentagereq: 100, Coresreq: 50}
		_, got, _ = dev.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
		assert.Equal(t, int32(16384), got[AMDDevice][0].Usedmem)

		// not enough memory left on the only card
		devices[0].Usedmem = 14000
		req = device.ContainerDeviceRequest{Nums: 1, Type: AMDDevice, MemPercentagereq: 25, Coresreq: 50}
		ok, _, reason = dev.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
		assert.Equal(t, false, ok)
		assert.Assert(t, strings.Contains(reason, common.CardInsufficientMemory))
	})

	t.Run("tiny percentage on a small card still books memory", func(t *testing.T) {
		devices := []*device.DeviceUsage{{
			ID: "dev-0", Count: 4, Totalmem: 50, Totalcore: 100,
			Type: AMDDevice, Health: true, CustomInfo: map[string]any{},
		}}
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}
		req := device.ContainerDeviceRequest{Nums: 1, Type: AMDDevice, MemPercentagereq: 1, Coresreq: 50}
		_, got, _ := dev.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
		assert.Equal(t, int32(1), got[AMDDevice][0].Usedmem)
	})
}

func TestPriority(t *testing.T) {
	dev := InitAMDGPUDevice(AMDConfig{
		ResourceCountName:    "amd.com/gpu",
		ResourcePriorityName: "amd.com/priority",
	})
	ctr := func(priority string, env ...corev1.EnvVar) *corev1.Container {
		limits := corev1.ResourceList{"amd.com/gpu": *resource.NewQuantity(1, resource.DecimalSI)}
		if priority != "" {
			limits["amd.com/priority"] = resource.MustParse(priority)
		}
		return &corev1.Container{Name: "c1", Env: env, Resources: corev1.ResourceRequirements{Limits: limits}}
	}
	envOf := func(c *corev1.Container) []string {
		var out []string
		for _, e := range c.Env {
			out = append(out, e.Name+"="+e.Value)
		}
		return out
	}

	for priority, want := range map[string]string{"0": "AMD_TASK_PRIORITY=0", "1": "AMD_TASK_PRIORITY=1", "7": "AMD_TASK_PRIORITY=7"} {
		t.Run("priority "+priority+" becomes the env", func(t *testing.T) {
			c := ctr(priority)
			ok, err := dev.MutateAdmission(c, &corev1.Pod{})
			assert.NilError(t, err)
			assert.Equal(t, true, ok)
			assert.DeepEqual(t, []string{want}, envOf(c))
		})
	}

	t.Run("an env the container sets is replaced, not duplicated", func(t *testing.T) {
		c := ctr("1",
			corev1.EnvVar{Name: "KEEP", Value: "x"},
			corev1.EnvVar{Name: "AMD_TASK_PRIORITY", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}}},
		)
		_, err := dev.MutateAdmission(c, &corev1.Pod{})
		assert.NilError(t, err)
		assert.DeepEqual(t, []string{"KEEP=x", "AMD_TASK_PRIORITY=1"}, envOf(c))
		assert.Assert(t, c.Env[1].ValueFrom == nil)
	})

	t.Run("no priority resource leaves the env alone", func(t *testing.T) {
		c := ctr("")
		_, err := dev.MutateAdmission(c, &corev1.Pod{})
		assert.NilError(t, err)
		assert.Equal(t, 0, len(c.Env))
	})

	t.Run("a priority alone does not claim a card", func(t *testing.T) {
		c := &corev1.Container{Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
			"amd.com/priority": *resource.NewQuantity(0, resource.DecimalSI),
		}}}
		ok, err := dev.MutateAdmission(c, &corev1.Pod{})
		assert.NilError(t, err)
		assert.Equal(t, false, ok)
	})

	for _, bad := range []string{"-1", "1500m", "3000000000"} {
		t.Run("rejects "+bad, func(t *testing.T) {
			c := ctr(bad)
			_, err := dev.MutateAdmission(c, &corev1.Pod{})
			assert.ErrorContains(t, err, "must be an integer between 0 and")
			assert.Equal(t, 0, len(c.Env))
		})
	}

	t.Run("an unconfigured backend ignores the resource", func(t *testing.T) {
		plain := InitAMDGPUDevice(AMDConfig{ResourceCountName: "amd.com/gpu"})
		c := ctr("1")
		_, err := plain.MutateAdmission(c, &corev1.Pod{})
		assert.NilError(t, err)
		assert.Equal(t, 0, len(c.Env))
	})
}
