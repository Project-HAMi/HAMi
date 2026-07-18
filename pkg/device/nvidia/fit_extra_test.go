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
	"github.com/Project-HAMi/HAMi/pkg/util"
)

func TestFit_NumaNotFitReason(t *testing.T) {
	config := NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
	}
	nv := InitNvidiaDevice(config)

	// Reverse iteration order: dev-2 (NUMA 0) -> dev-1 (NUMA 0) -> dev-0 (NUMA 1)
	// Request 3 cards: dev-2 + dev-1 selected (Nums=1 left), then NUMA switch at dev-0
	// resets and records NumaNotFit += 2. dev-0 alone cannot satisfy 3-card request.
	devices := []*device.DeviceUsage{
		{ID: "dev-0", Index: 0, Used: 0, Count: 10, Totalmem: 8192, Totalcore: 100, Type: NvidiaGPUDevice, Health: true, Numa: 1},
		{ID: "dev-1", Index: 1, Used: 0, Count: 10, Totalmem: 8192, Totalcore: 100, Type: NvidiaGPUDevice, Health: true, Numa: 0},
		{ID: "dev-2", Index: 2, Used: 0, Count: 10, Totalmem: 8192, Totalcore: 100, Type: NvidiaGPUDevice, Health: true, Numa: 0},
	}
	req := device.ContainerDeviceRequest{Nums: 3, Memreq: 100, Coresreq: 10, Type: NvidiaGPUDevice}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{NumaBind: "true"},
		},
	}
	fit, _, reason := nv.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, false)
	parsed := common.ParseReason(reason)
	// Note: original semantics use len(tmpDevs) which is the map size (device-type count),
	// not the per-card slice length. For NVIDIA-only allocations this is always 1.
	assert.Equal(t, parsed[common.NumaNotFit], 1)
}

func TestFit_ResourceQuotaNotFitReason(t *testing.T) {
	config := NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
		MemoryFactor:                 1,
	}
	nv := InitNvidiaDevice(config)
	prevDevicesMap := device.DevicesMap
	device.DevicesMap = make(map[string]device.Devices)
	device.DevicesMap[NvidiaGPUDevice] = nv
	t.Cleanup(func() { device.DevicesMap = prevDevicesMap })

	const ns = "fit-quota-ns"
	rq := &corev1.ResourceQuota{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ResourceQuota"},
		ObjectMeta: metav1.ObjectMeta{Name: "fit-extra-quota", Namespace: ns},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceName("limits.nvidia.com/gpumem"): resource.MustParse("100"),
			},
		},
	}
	qm := device.NewQuotaManager()
	qm.AddQuota(rq)
	t.Cleanup(func() { qm.DelQuota(rq) })

	devices := []*device.DeviceUsage{
		{ID: "dev-0", Index: 0, Used: 0, Count: 10, Totalmem: 8192, Totalcore: 100, Type: NvidiaGPUDevice, Health: true},
	}
	req := device.ContainerDeviceRequest{Nums: 1, Memreq: 500, Coresreq: 10, Type: NvidiaGPUDevice}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: ns}}

	fit, _, reason := nv.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, false)
	parsed := common.ParseReason(reason)
	assert.Equal(t, parsed[common.ResourceQuotaNotFit], 1)
}

func TestFit_MigMultiSlotSelection(t *testing.T) {
	config := NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
	}
	nv := InitNvidiaDevice(config)

	// One physical MIG card exposing 3 slots of 1g.5gb (5GB each). Request asks for 2 slots
	// with 4GB each — both must come from the same physical card via the i++ stay-loop.
	devices := []*device.DeviceUsage{
		{
			ID:        "GPU-AAA",
			Index:     0,
			Used:      0,
			Count:     10,
			Totalmem:  24576,
			Usedmem:   0,
			Totalcore: 100,
			Usedcores: 0,
			Type:      NvidiaGPUDevice,
			Health:    true,
			Mode:      MigMode,
			MigUsage:  device.MigInUse{UsageList: device.MIGS{}},
			MigTemplate: []device.Geometry{
				{{Name: "1g.5gb", Memory: 5120, Core: 14, Count: 3}},
			},
		},
	}
	req := device.ContainerDeviceRequest{Nums: 2, Memreq: 4096, Coresreq: 0, Type: NvidiaGPUDevice}
	pod := &corev1.Pod{}

	fit, result, _ := nv.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, true)
	assert.Equal(t, len(result[NvidiaGPUDevice]), 2)
	assert.Equal(t, result[NvidiaGPUDevice][0].UUID, "GPU-AAA")
	assert.Equal(t, result[NvidiaGPUDevice][1].UUID, "GPU-AAA")
	assert.Equal(t, result[NvidiaGPUDevice][0].Usedmem, int32(4096))
	assert.Equal(t, result[NvidiaGPUDevice][1].Usedmem, int32(4096))
}

func TestFit_CardNotFoundCustomFilterRuleReason(t *testing.T) {
	config := NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
	}
	nv := InitNvidiaDevice(config)

	// MIG mode card with a single small template that cannot satisfy the request.
	devices := []*device.DeviceUsage{
		{
			ID:        "dev-0",
			Index:     0,
			Used:      0,
			Count:     10,
			Totalmem:  8192,
			Usedmem:   0,
			Totalcore: 100,
			Usedcores: 0,
			Type:      NvidiaGPUDevice,
			Health:    true,
			Mode:      MigMode,
			MigUsage:  device.MigInUse{UsageList: device.MIGS{}},
			MigTemplate: []device.Geometry{
				{{Name: "1g.5gb", Memory: 100, Core: 14, Count: 1}},
			},
		},
	}
	req := device.ContainerDeviceRequest{Nums: 1, Memreq: 4096, Coresreq: 10, Type: NvidiaGPUDevice}
	pod := &corev1.Pod{}

	fit, _, reason := nv.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, false)
	parsed := common.ParseReason(reason)
	assert.Equal(t, parsed[common.CardNotFoundCustomFilterRule], 1)
}

func TestFit_CoresreqOver100IsClamped(t *testing.T) {
	config := NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
	}
	nv := InitNvidiaDevice(config)

	devices := []*device.DeviceUsage{
		{ID: "dev-0", Index: 0, Used: 0, Count: 10, Totalmem: 8192, Totalcore: 100, Type: NvidiaGPUDevice, Health: true},
	}
	// Coresreq=200 must be clamped to 100 inside Fit so the recorded Usedcores is 100, not 200.
	req := device.ContainerDeviceRequest{Nums: 1, Memreq: 100, Coresreq: 200, Type: NvidiaGPUDevice}
	pod := &corev1.Pod{}

	fit, result, _ := nv.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, true)
	assert.Equal(t, len(result[NvidiaGPUDevice]), 1)
	assert.Equal(t, result[NvidiaGPUDevice][0].Usedcores, int32(100))
	assert.Equal(t, result[NvidiaGPUDevice][0].Usedmem, int32(100))
}

func TestFit_MultiCardFailureReasonCount(t *testing.T) {
	config := NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
	}
	nv := InitNvidiaDevice(config)

	// 3 cards all short on memory: each contributes one CardInsufficientMemory hit.
	devices := []*device.DeviceUsage{
		{ID: "dev-0", Index: 0, Used: 0, Count: 10, Totalmem: 100, Usedmem: 80, Totalcore: 100, Type: NvidiaGPUDevice, Health: true},
		{ID: "dev-1", Index: 1, Used: 0, Count: 10, Totalmem: 100, Usedmem: 80, Totalcore: 100, Type: NvidiaGPUDevice, Health: true},
		{ID: "dev-2", Index: 2, Used: 0, Count: 10, Totalmem: 100, Usedmem: 80, Totalcore: 100, Type: NvidiaGPUDevice, Health: true},
	}
	req := device.ContainerDeviceRequest{Nums: 1, Memreq: 50, Coresreq: 10, Type: NvidiaGPUDevice}
	pod := &corev1.Pod{}

	fit, _, reason := nv.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, false)
	parsed := common.ParseReason(reason)
	assert.Equal(t, parsed[common.CardInsufficientMemory], 3)
}

func TestFit_TopologyModeWithPartialSelection(t *testing.T) {
	config := NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
	}
	nv := InitNvidiaDevice(config)

	// Request 4 cards but only 3 healthy -> AllocatedCardsInsufficientRequest.
	devices := []*device.DeviceUsage{
		{ID: "dev-0", Index: 0, Used: 0, Count: 10, Totalmem: 8192, Totalcore: 100, Type: NvidiaGPUDevice, Health: true},
		{ID: "dev-1", Index: 1, Used: 0, Count: 10, Totalmem: 8192, Totalcore: 100, Type: NvidiaGPUDevice, Health: true},
		{ID: "dev-2", Index: 2, Used: 0, Count: 10, Totalmem: 8192, Totalcore: 100, Type: NvidiaGPUDevice, Health: true},
		{ID: "dev-3", Index: 3, Used: 0, Count: 10, Totalmem: 8192, Totalcore: 100, Type: NvidiaGPUDevice, Health: false},
	}
	req := device.ContainerDeviceRequest{Nums: 4, Memreq: 100, Coresreq: 10, Type: NvidiaGPUDevice}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{util.GPUSchedulerPolicyAnnotationKey: util.GPUSchedulerPolicyTopology.String()},
		},
	}
	fit, result, reason := nv.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
	// Topology mode: not enough cards selected (3 < 4) -> neither exact nor over branch hits,
	// falls through to AllocatedCardsInsufficientRequest.
	assert.Equal(t, fit, false)
	assert.Equal(t, len(result[NvidiaGPUDevice]), 3)
	parsed := common.ParseReason(reason)
	// Note: AllocatedCardsInsufficientRequest uses len(tmpDevs) (map size, = 1 for NVIDIA-only)
	// per the original device.go semantics; not the per-card slice length.
	assert.Equal(t, parsed[common.AllocatedCardsInsufficientRequest], 1)
}

func TestFit_EmptyDeviceList(t *testing.T) {
	config := NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
	}
	nv := InitNvidiaDevice(config)

	req := device.ContainerDeviceRequest{Nums: 1, Memreq: 100, Coresreq: 10, Type: NvidiaGPUDevice}
	fit, result, reason := nv.Fit([]*device.DeviceUsage{}, req, &corev1.Pod{}, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, false)
	assert.Equal(t, len(result), 0)
	// No reason categories recorded -> empty string after GenReason.
	assert.Equal(t, reason, "")
}

func TestFit_ZeroNumRequest(t *testing.T) {
	config := NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
	}
	nv := InitNvidiaDevice(config)

	devices := []*device.DeviceUsage{
		{ID: "dev-0", Index: 0, Used: 0, Count: 10, Totalmem: 8192, Totalcore: 100, Type: NvidiaGPUDevice, Health: true},
	}
	req := device.ContainerDeviceRequest{Nums: 0, Memreq: 100, Coresreq: 10, Type: NvidiaGPUDevice}
	pod := &corev1.Pod{}

	fit, _, _ := nv.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
	// Nums=0 means "no device requested" -> immediately satisfied.
	assert.Equal(t, fit, true)
}

// TestFit_HealthPrecedenceOverTypeMismatch is a regression guard: the original Fit
// code ran the health check before checkType. The refactor must preserve that order:
// when a card is simultaneously unhealthy and type-mismatched, the reported reason
// must be CardNotHealth.
func TestFit_HealthPrecedenceOverTypeMismatch(t *testing.T) {
	config := NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
	}
	nv := InitNvidiaDevice(config)

	// Card is both unhealthy AND type-mismatched (annotation forces type filter that
	// the card model does not satisfy). Health check must win and record CardNotHealth
	// only; CardTypeMismatch must not be recorded since checkType runs after health.
	devices := []*device.DeviceUsage{
		{
			ID:        "dev-0",
			Index:     0,
			Used:      0,
			Count:     10,
			Totalmem:  8192,
			Totalcore: 100,
			Type:      "WrongType",
			Health:    false,
		},
	}
	req := device.ContainerDeviceRequest{Nums: 1, Memreq: 100, Coresreq: 10, Type: NvidiaGPUDevice}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				GPUInUse: "NonMatchingModel",
			},
		},
	}

	fit, _, reason := nv.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, false)
	parsed := common.ParseReason(reason)
	_, hasHealth := parsed[common.CardNotHealth]
	_, hasTypeMismatch := parsed[common.CardTypeMismatch]
	assert.Equal(t, hasHealth, true)
	assert.Equal(t, hasTypeMismatch, false)
}
