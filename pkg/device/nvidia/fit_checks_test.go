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

package nvidia

import (
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
)

func newCheckCtx(req device.ContainerDeviceRequest, pod *corev1.Pod) *cardCheckCtx {
	nv := InitNvidiaDevice(NvidiaConfig{})
	return &cardCheckCtx{
		request:    &req,
		pod:        pod,
		commonWord: nv.CommonWord(),
		nv:         nv,
	}
}

func TestCheckCardHealth(t *testing.T) {
	tests := []struct {
		name     string
		health   bool
		wantPass bool
	}{
		{"healthy card passes", true, true},
		{"unhealthy card fails", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dev := &device.DeviceUsage{ID: "dev-0", Health: tc.health}
			reason := checkCardHealth(dev, &corev1.Pod{})
			if tc.wantPass {
				assert.Equal(t, reason, "")
			} else {
				assert.Equal(t, reason, common.CardNotHealth)
			}
		})
	}
}

func TestCheckCardUUID(t *testing.T) {
	tests := []struct {
		name     string
		devID    string
		annos    map[string]string
		wantPass bool
	}{
		{
			name:     "no annotation constraint passes",
			devID:    "GPU-aaa",
			annos:    map[string]string{},
			wantPass: true,
		},
		{
			name:  "use-gpuuuid matches passes",
			devID: "GPU-aaa",
			annos: map[string]string{
				GPUUseUUID: "GPU-aaa,GPU-bbb",
			},
			wantPass: true,
		},
		{
			name:  "use-gpuuuid mismatch fails",
			devID: "GPU-ccc",
			annos: map[string]string{
				GPUUseUUID: "GPU-aaa,GPU-bbb",
			},
			wantPass: false,
		},
		{
			name:  "nouse-gpuuuid match fails",
			devID: "GPU-aaa",
			annos: map[string]string{
				GPUNoUseUUID: "GPU-aaa,GPU-bbb",
			},
			wantPass: false,
		},
		{
			name:  "nouse-gpuuuid mismatch passes",
			devID: "GPU-ccc",
			annos: map[string]string{
				GPUNoUseUUID: "GPU-aaa,GPU-bbb",
			},
			wantPass: true,
		},
		{
			name:  "empty use-gpuuuid is no constraint",
			devID: "GPU-aaa",
			annos: map[string]string{
				GPUUseUUID: "",
			},
			wantPass: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dev := &device.DeviceUsage{ID: tc.devID, Health: true}
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: tc.annos}}
			ctx := newCheckCtx(device.ContainerDeviceRequest{Type: NvidiaGPUDevice}, pod)
			reason := checkCardUUID(dev, ctx)
			if tc.wantPass {
				assert.Equal(t, reason, "")
			} else {
				assert.Equal(t, reason, common.CardUUIDMismatch)
			}
		})
	}
}

func TestCheckCardTimeSlicing(t *testing.T) {
	tests := []struct {
		name     string
		count    int32
		used     int32
		wantPass bool
	}{
		{"plenty of slots", 100, 0, true},
		{"one slot remaining", 10, 9, true},
		{"exactly exhausted", 10, 10, false},
		{"over-committed", 10, 15, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dev := &device.DeviceUsage{ID: "dev-0", Count: tc.count, Used: tc.used, Health: true}
			ctx := newCheckCtx(device.ContainerDeviceRequest{Type: NvidiaGPUDevice}, &corev1.Pod{})
			reason := checkCardTimeSlicing(dev, ctx)
			if tc.wantPass {
				assert.Equal(t, reason, "")
			} else {
				assert.Equal(t, reason, common.CardTimeSlicingExhausted)
			}
		})
	}
}

func TestNormalizeCoresreq(t *testing.T) {
	tests := []struct {
		name       string
		input      int32
		wantOutput int32
	}{
		{"zero stays zero", 0, 0},
		{"under 100 unchanged", 50, 50},
		{"exactly 100 unchanged", 100, 100},
		{"101 clamped to 100", 101, 100},
		{"200 clamped to 100", 200, 100},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := device.ContainerDeviceRequest{Coresreq: tc.input}
			ctx := &cardCheckCtx{request: &req, pod: &corev1.Pod{}}
			dev := &device.DeviceUsage{ID: "dev-0"}
			normalizeCoresreq(ctx.request, ctx.pod, dev)
			assert.Equal(t, ctx.request.Coresreq, tc.wantOutput)
		})
	}
}

func TestComputeMemreq(t *testing.T) {
	tests := []struct {
		name          string
		memreq        int32
		memPercentage int32
		totalmem      int32
		wantMemreq    int32
	}{
		{
			name:          "explicit memreq wins",
			memreq:        2048,
			memPercentage: 50,
			totalmem:      8192,
			wantMemreq:    2048,
		},
		{
			name:          "percentage applied when memreq zero",
			memreq:        0,
			memPercentage: 50,
			totalmem:      8192,
			wantMemreq:    4096,
		},
		{
			name:          "percentage 101 (unset) with memreq zero gives zero",
			memreq:        0,
			memPercentage: 101,
			totalmem:      8192,
			wantMemreq:    0,
		},
		{
			name:          "percentage 100 of 16GB",
			memreq:        0,
			memPercentage: 100,
			totalmem:      16384,
			wantMemreq:    16384,
		},
		{
			name:          "percentage 25 of 8GB",
			memreq:        0,
			memPercentage: 25,
			totalmem:      8192,
			wantMemreq:    2048,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := device.ContainerDeviceRequest{Memreq: tc.memreq, MemPercentagereq: tc.memPercentage}
			dev := &device.DeviceUsage{Totalmem: tc.totalmem}
			got := computeMemreq(req, dev)
			assert.Equal(t, got, tc.wantMemreq)
		})
	}
}

func TestCheckCardQuota_NoConstraint(t *testing.T) {
	nv := InitNvidiaDevice(NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
	})
	device.DevicesMap = map[string]device.Devices{NvidiaGPUDevice: nv}
	t.Cleanup(func() { device.DevicesMap = nil })

	dev := &device.DeviceUsage{ID: "dev-0", Health: true, Count: 10, Used: 0, Totalmem: 8192, Totalcore: 100}
	req := device.ContainerDeviceRequest{Type: NvidiaGPUDevice, Memreq: 100, Coresreq: 10}
	ctx := newCheckCtx(req, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default"}})
	ctx.memreq = 100

	reason := checkCardQuota(dev, ctx)
	assert.Equal(t, reason, "")
}

func TestCheckCardQuota_Exceeded(t *testing.T) {
	nv := InitNvidiaDevice(NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
	})
	device.DevicesMap = map[string]device.Devices{NvidiaGPUDevice: nv}
	t.Cleanup(func() { device.DevicesMap = nil })

	rq := &corev1.ResourceQuota{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ResourceQuota"},
		ObjectMeta: metav1.ObjectMeta{Name: "q", Namespace: "limited"},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceName("limits.nvidia.com/gpumem"): resource.MustParse("100"),
			},
		},
	}
	qm := device.NewQuotaManager()
	qm.AddQuota(rq)
	t.Cleanup(func() { qm.DelQuota(rq) })

	dev := &device.DeviceUsage{ID: "dev-0", Health: true, Count: 10, Used: 0, Totalmem: 8192, Totalcore: 100}
	req := device.ContainerDeviceRequest{Type: NvidiaGPUDevice, Memreq: 500, Coresreq: 10}
	ctx := newCheckCtx(req, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "limited"}})
	ctx.memreq = 500

	reason := checkCardQuota(dev, ctx)
	assert.Equal(t, reason, common.ResourceQuotaNotFit)
}

func TestCheckCardMemory(t *testing.T) {
	tests := []struct {
		name     string
		totalmem int32
		usedmem  int32
		memreq   int32
		wantPass bool
	}{
		{"plenty of memory", 8192, 0, 100, true},
		{"exact fit", 1024, 512, 512, true},
		{"one byte short", 1024, 512, 513, false},
		{"all used", 1024, 1024, 1, false},
		{"zero request fits", 1024, 1024, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dev := &device.DeviceUsage{ID: "dev-0", Totalmem: tc.totalmem, Usedmem: tc.usedmem}
			ctx := &cardCheckCtx{
				request: &device.ContainerDeviceRequest{},
				pod:     &corev1.Pod{},
				memreq:  tc.memreq,
			}
			reason := checkCardMemory(dev, ctx)
			if tc.wantPass {
				assert.Equal(t, reason, "")
			} else {
				assert.Equal(t, reason, common.CardInsufficientMemory)
			}
		})
	}
}

func TestCheckCardCore(t *testing.T) {
	tests := []struct {
		name      string
		totalcore int32
		usedcores int32
		coresreq  int32
		wantPass  bool
	}{
		{"plenty of cores", 100, 0, 10, true},
		{"exact fit", 100, 50, 50, true},
		{"one short", 100, 50, 51, false},
		{"all used with request", 100, 100, 1, false},
		{"zero request with cores used", 100, 100, 0, true},
		{"zero totalcore with non-zero request fails", 0, 0, 50, false},
		{"zero totalcore with zero request passes", 0, 0, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dev := &device.DeviceUsage{ID: "dev-0", Totalcore: tc.totalcore, Usedcores: tc.usedcores}
			req := device.ContainerDeviceRequest{Coresreq: tc.coresreq}
			ctx := &cardCheckCtx{request: &req, pod: &corev1.Pod{}}
			reason := checkCardCore(dev, ctx)
			if tc.wantPass {
				assert.Equal(t, reason, "")
			} else {
				assert.Equal(t, reason, common.CardInsufficientCore)
			}
		})
	}
}

func TestCheckCardExclusive(t *testing.T) {
	tests := []struct {
		name      string
		totalcore int32
		coresreq  int32
		used      int32
		wantPass  bool
	}{
		{"non-exclusive passes even when shared", 100, 50, 5, true},
		{"exclusive request on free card passes", 100, 100, 0, true},
		{"exclusive request on shared card fails", 100, 100, 1, false},
		{"exclusive request on heavily shared card fails", 100, 100, 10, false},
		{"exclusive request on non-100 totalcore passes", 200, 100, 5, true},
		{"coresreq below 100 never exclusive", 100, 99, 10, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dev := &device.DeviceUsage{ID: "dev-0", Totalcore: tc.totalcore, Used: tc.used}
			req := device.ContainerDeviceRequest{Coresreq: tc.coresreq}
			ctx := &cardCheckCtx{request: &req, pod: &corev1.Pod{}}
			reason := checkCardExclusive(dev, ctx)
			if tc.wantPass {
				assert.Equal(t, reason, "")
			} else {
				assert.Equal(t, reason, common.ExclusiveDeviceAllocateConflict)
			}
		})
	}
}

func TestCheckCardComputeExhausted(t *testing.T) {
	tests := []struct {
		name      string
		totalcore int32
		usedcores int32
		coresreq  int32
		wantPass  bool
	}{
		{"cores available, request zero passes", 100, 50, 0, true},
		{"cores full, request zero fails", 100, 100, 0, false},
		{"cores full but request non-zero passes (handled by core check)", 100, 100, 10, true},
		{"zero totalcore passes", 0, 0, 0, true},
		{"cores partial, request zero passes", 100, 30, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dev := &device.DeviceUsage{ID: "dev-0", Totalcore: tc.totalcore, Usedcores: tc.usedcores}
			req := device.ContainerDeviceRequest{Coresreq: tc.coresreq}
			ctx := &cardCheckCtx{request: &req, pod: &corev1.Pod{}}
			reason := checkCardComputeExhausted(dev, ctx)
			if tc.wantPass {
				assert.Equal(t, reason, "")
			} else {
				assert.Equal(t, reason, common.CardComputeUnitsExhausted)
			}
		})
	}
}

func TestCheckCardCustomRule_NonMigPasses(t *testing.T) {
	dev := &device.DeviceUsage{ID: "dev-0", Mode: HamiCoreMode}
	req := device.ContainerDeviceRequest{Type: NvidiaGPUDevice, Memreq: 100}
	ctx := newCheckCtx(req, &corev1.Pod{})
	reason := checkCardCustomRule(dev, ctx)
	assert.Equal(t, reason, "")
}

func TestCheckCardCustomRule_MigNoTemplateFails(t *testing.T) {
	dev := &device.DeviceUsage{
		ID:       "dev-0",
		Mode:     MigMode,
		MigUsage: device.MigInUse{UsageList: device.MIGS{}},
		MigTemplate: []device.Geometry{
			{{Name: "1g.5gb", Memory: 100, Core: 14, Count: 1}},
		},
	}
	req := device.ContainerDeviceRequest{Type: NvidiaGPUDevice, Memreq: 4096}
	ctx := newCheckCtx(req, &corev1.Pod{})
	reason := checkCardCustomRule(dev, ctx)
	assert.Equal(t, reason, common.CardNotFoundCustomFilterRule)
}

func TestRunCardChecks_ShortCircuit(t *testing.T) {
	// Note: cardCheckPipeline does NOT include checkCardHealth (that runs in the Fit loop
	// before runCardChecks). The pipeline starts with UUID and short-circuits on the
	// first failure encountered in pipeline order.

	t.Run("uuid fails before timeslicing", func(t *testing.T) {
		dev := &device.DeviceUsage{
			ID:     "GPU-other",
			Health: true,
			Count:  0,
			Used:   5,
		}
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{GPUUseUUID: "GPU-aaa"}}}
		ctx := newCheckCtx(device.ContainerDeviceRequest{Type: NvidiaGPUDevice}, pod)
		reason := runCardChecks(dev, ctx)
		assert.Equal(t, reason, common.CardUUIDMismatch)
	})

	t.Run("timeslicing fails before memory", func(t *testing.T) {
		dev := &device.DeviceUsage{
			ID:       "dev-0",
			Health:   true,
			Count:    5,
			Used:     5,
			Totalmem: 100,
			Usedmem:  100,
		}
		ctx := newCheckCtx(device.ContainerDeviceRequest{Type: NvidiaGPUDevice, Memreq: 50}, &corev1.Pod{})
		reason := runCardChecks(dev, ctx)
		assert.Equal(t, reason, common.CardTimeSlicingExhausted)
	})

	t.Run("memory fails before core", func(t *testing.T) {
		// Both memory and core fail; memory must win because it runs earlier in the pipeline.
		dev := &device.DeviceUsage{
			ID:        "dev-0",
			Health:    true,
			Count:     10,
			Used:      0,
			Totalmem:  100,
			Usedmem:   80,
			Totalcore: 100,
			Usedcores: 95,
		}
		ctx := newCheckCtx(device.ContainerDeviceRequest{Type: NvidiaGPUDevice, Memreq: 50, Coresreq: 50}, &corev1.Pod{})
		ctx.memreq = 50
		reason := runCardChecks(dev, ctx)
		assert.Equal(t, reason, common.CardInsufficientMemory)
	})

	t.Run("core fails before exclusive", func(t *testing.T) {
		// Both core and exclusive fail (Totalcore==100, Coresreq==100, Used>0 triggers exclusive;
		// Totalcore-Usedcores < Coresreq triggers core). Core runs earlier so it must win.
		dev := &device.DeviceUsage{
			ID:        "dev-0",
			Health:    true,
			Count:     10,
			Used:      5,
			Totalmem:  8192,
			Usedmem:   0,
			Totalcore: 100,
			Usedcores: 95,
		}
		ctx := newCheckCtx(device.ContainerDeviceRequest{Type: NvidiaGPUDevice, Memreq: 100, Coresreq: 100}, &corev1.Pod{})
		ctx.memreq = 100
		reason := runCardChecks(dev, ctx)
		assert.Equal(t, reason, common.CardInsufficientCore)
	})

	t.Run("all pass returns empty reason", func(t *testing.T) {
		dev := &device.DeviceUsage{
			ID:        "dev-0",
			Health:    true,
			Count:     10,
			Used:      0,
			Totalmem:  8192,
			Usedmem:   0,
			Totalcore: 100,
			Usedcores: 0,
		}
		ctx := newCheckCtx(device.ContainerDeviceRequest{Type: NvidiaGPUDevice, Memreq: 100, Coresreq: 10}, &corev1.Pod{})
		ctx.memreq = 100
		reason := runCardChecks(dev, ctx)
		assert.Equal(t, reason, "")
	})
}
