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

package hygon

import (
	"fmt"
	"maps"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

func initHygonQuotaTest(t *testing.T, factor int64) (*HCUDevices, *device.QuotaManager) {
	t.Helper()
	oldDevices := device.DevicesMap
	oldCount, oldMemory, oldCore, oldFactor := HygonResourceCount, HygonResourceMemory, HygonResourceCores, MemoryFactor
	oldRequests, oldSupport, oldHandshake := maps.Clone(device.InRequestDevices), maps.Clone(device.SupportDevices), maps.Clone(util.HandshakeAnnos)
	t.Cleanup(func() {
		device.DevicesMap = oldDevices
		HygonResourceCount, HygonResourceMemory, HygonResourceCores, MemoryFactor = oldCount, oldMemory, oldCore, oldFactor
		device.InRequestDevices, device.SupportDevices, util.HandshakeAnnos = oldRequests, oldSupport, oldHandshake
	})
	dev := InitHCUDevice(HygonConfig{
		ResourceCountName: "hygon.com/hcunum", ResourceMemoryName: "hygon.com/hcumem",
		ResourceCoreName: "hygon.com/hcucores", MemoryFactor: factor,
	})
	device.DevicesMap = map[string]device.Devices{HygonHCUDevice: dev}
	return dev, device.NewQuotaManager()
}

func addHygonTestQuota(t *testing.T, qm *device.QuotaManager, namespace string, memory, cores int64) {
	t.Helper()
	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "hcu-quota", Namespace: namespace},
		Spec: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{
			corev1.ResourceName("limits." + HygonResourceMemory): *resource.NewQuantity(memory, resource.DecimalSI),
			corev1.ResourceName("limits." + HygonResourceCores):  *resource.NewQuantity(cores, resource.DecimalSI),
		}},
	}
	qm.AddQuota(quota)
	t.Cleanup(func() { qm.DelQuota(quota) })
}

func hygonQuotaCard(id string, memory int64, index uint) *device.DeviceUsage {
	return &device.DeviceUsage{ID: id, Index: index, Type: HygonHCUDevice, Health: true,
		Count: 10, Totalmem: memory, Totalcore: 100}
}

func TestDevices_Fit_ResourceQuotaWholeCardRequest(t *testing.T) {
	dev, qm := initHygonQuotaTest(t, 1)
	for _, remaining := range []int64{0, 8191, 8192} {
		t.Run(fmt.Sprintf("%d_MiB", remaining), func(t *testing.T) {
			ns := t.Name()
			addHygonTestQuota(t, qm, ns, 8192, 100)
			otherPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "existing"}}
			usage := device.PodDevices{HygonHCUDevice: {{{UUID: "other-card", Usedmem: 8192 - remaining}}}}
			qm.AddUsage(otherPod, usage)
			t.Cleanup(func() { qm.RmUsage(otherPod, usage) })
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: ns}, Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "app", Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{"hygon.com/hcunum": resource.MustParse("1")},
				}}},
			}}
			request, err := dev.GenerateResourceRequests(&pod.Spec.Containers[0])
			assert.NilError(t, err)
			assert.Equal(t, request.Memreq, int64(0))
			assert.Equal(t, request.MemPercentagereq, int64(100))
			card := hygonQuotaCard("hcu-0", 8192, 3)
			originalCard := *card
			before := qm.GetResourceQuota()
			allocated := device.PodDevices{}
			fit, got, reason := dev.Fit([]*device.DeviceUsage{card}, request, pod, &device.NodeInfo{}, &allocated)
			assert.Equal(t, fit, remaining == 8192)
			if remaining < 8192 {
				assert.Equal(t, reason, "1/1 "+common.ResourceQuotaNotFit)
				assert.Equal(t, len(got[HygonHCUDevice]), 0)
			} else {
				assert.Equal(t, reason, "")
				assert.DeepEqual(t, got[HygonHCUDevice], device.ContainerDevices{{
					Idx: 3, UUID: "hcu-0", Type: HygonHCUDevice, Usedmem: 8192,
				}})
			}
			assert.DeepEqual(t, allocated, device.PodDevices{})
			assert.DeepEqual(t, *card, originalCard)
			assert.DeepEqual(t, qm.GetResourceQuota(), before)
		})
	}
}

func TestDevices_Fit_ResourceQuotaMemoryRequests(t *testing.T) {
	for _, tt := range []struct {
		name                                       string
		factor, limit, memory, percent, wantMemory int64
		explicitZero, wantFit                      bool
	}{
		{name: "explicit_memory_below", factor: 1, limit: 4095, memory: 4096, wantMemory: 4096},
		{name: "explicit_memory_exact", factor: 1, limit: 4096, memory: 4096, wantMemory: 4096, wantFit: true},
		{name: "percentage_below", factor: 1, limit: 4095, percent: 50, wantMemory: 4096},
		{name: "percentage_exact", factor: 1, limit: 4096, percent: 50, wantMemory: 4096, wantFit: true},
		{name: "explicit_zero_memory_uses_whole_card", factor: 1, limit: 8191, explicitZero: true, wantMemory: 8192},
		{name: "explicit_zero_memory_exact", factor: 1, limit: 8192, explicitZero: true, wantMemory: 8192, wantFit: true},
		{name: "explicit_zero_quota", factor: 1, limit: 0, wantMemory: 8192},
		{name: "factor_below", factor: 1024, limit: 7, wantMemory: 8192},
		{name: "factor_exact", factor: 1024, limit: 8, wantMemory: 8192, wantFit: true},
		{name: "factor_explicit_memory", factor: 1024, limit: 4, memory: 4, wantMemory: 4096, wantFit: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dev, qm := initHygonQuotaTest(t, tt.factor)
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: t.Name()}}
			addHygonTestQuota(t, qm, pod.Namespace, tt.limit, 100)
			ctr := &corev1.Container{Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
				"hygon.com/hcunum": resource.MustParse("1"),
			}}}
			if tt.memory > 0 || tt.explicitZero {
				ctr.Resources.Limits[corev1.ResourceName(HygonResourceMemory)] = *resource.NewQuantity(tt.memory, resource.DecimalSI)
			}
			request, err := dev.GenerateResourceRequests(ctr)
			assert.NilError(t, err)
			// Hygon has no percentage resource; exercise Fit's supported percentage request directly.
			if tt.percent > 0 {
				request.MemPercentagereq = tt.percent
			}
			before := qm.GetResourceQuota()
			fit, got, reason := dev.Fit([]*device.DeviceUsage{hygonQuotaCard("hcu-0", 8192, 0)}, request, pod, nil, nil)
			assert.Equal(t, fit, tt.wantFit)
			if tt.wantFit {
				assert.Equal(t, reason, "")
				assert.Equal(t, len(got[HygonHCUDevice]), 1)
				assert.Equal(t, got[HygonHCUDevice][0].Usedmem, tt.wantMemory)
			} else {
				assert.Equal(t, reason, "1/1 "+common.ResourceQuotaNotFit)
				assert.Equal(t, len(got[HygonHCUDevice]), 0)
			}
			assert.DeepEqual(t, qm.GetResourceQuota(), before)
		})
	}
}

func TestDevices_Fit_ResourceQuotaCandidateFallback(t *testing.T) {
	dev, qm := initHygonQuotaTest(t, 1)
	for _, tt := range []struct {
		name         string
		count, limit int64
		wantFit      bool
		wantIDs      []string
	}{
		{"smaller_card_fits", 1, 4096, true, []string{"small"}},
		{"multiple_cards_exact", 2, 12288, true, []string{"large", "small"}},
		{"tentative_card_counts_toward_quota", 2, 12287, false, []string{"large"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: t.Name()}}
			addHygonTestQuota(t, qm, pod.Namespace, tt.limit, 100)
			cards := []*device.DeviceUsage{hygonQuotaCard("small", 4096, 0), hygonQuotaCard("large", 8192, 1)}
			before := qm.GetResourceQuota()
			fit, got, reason := dev.Fit(cards, device.ContainerDeviceRequest{
				Nums: tt.count, Type: HygonHCUDevice, MemPercentagereq: 100,
			}, pod, nil, nil)
			assert.Equal(t, fit, tt.wantFit)
			var ids []string
			var memory int64
			for _, allocation := range got[HygonHCUDevice] {
				ids = append(ids, allocation.UUID)
				memory += allocation.Usedmem
			}
			assert.DeepEqual(t, ids, tt.wantIDs)
			if tt.wantFit {
				assert.Equal(t, reason, "")
				assert.Equal(t, memory, tt.limit)
			} else {
				assert.Assert(t, common.ParseReason(reason)[common.ResourceQuotaNotFit] > 0, "reason: %s", reason)
				// Fit may return valid partial candidates on failure; fitInDevices never commits them.
				assert.Equal(t, memory, int64(8192))
			}
			assert.DeepEqual(t, qm.GetResourceQuota(), before)
			for _, card := range cards {
				assert.Equal(t, card.Usedmem, int64(0))
				assert.Equal(t, card.Used, int64(0))
			}
		})
	}
}

func TestFitQuota_ResourceQuotaAggregateUsage(t *testing.T) {
	_, qm := initHygonQuotaTest(t, 1)
	sidecar := corev1.ContainerRestartPolicyAlways
	for _, tt := range []struct {
		name                                              string
		init                                              []corev1.Container
		prior                                             device.PodSingleDevice
		tentative                                         device.ContainerDevices
		memLimit, coreLimit, candidateMem, candidateCores int64
		wantFit                                           bool
	}{
		{name: "earlier_app_container", prior: device.PodSingleDevice{{{UUID: "hcu", Usedmem: 4096}}}, memLimit: 8191, coreLimit: 100, candidateMem: 4096},
		{name: "earlier_app_exact", prior: device.PodSingleDevice{{{UUID: "hcu", Usedmem: 4096}}}, memLimit: 8192, coreLimit: 100, candidateMem: 4096, wantFit: true},
		{name: "sequential_inits_reuse_peak", init: []corev1.Container{{Name: "init-1"}, {Name: "init-2"}}, prior: device.PodSingleDevice{{{UUID: "hcu", Usedmem: 4096}}}, memLimit: 4096, coreLimit: 100, candidateMem: 4096, wantFit: true},
		{name: "app_reuses_init_peak", init: []corev1.Container{{Name: "init"}}, prior: device.PodSingleDevice{{{UUID: "hcu", Usedmem: 4096}}}, memLimit: 4096, coreLimit: 100, candidateMem: 4096, wantFit: true},
		{name: "sidecar_concurrent_with_app", init: []corev1.Container{{Name: "sidecar", RestartPolicy: &sidecar}}, prior: device.PodSingleDevice{{{UUID: "hcu", Usedmem: 4096}}}, memLimit: 8191, coreLimit: 100, candidateMem: 4096},
		{name: "sidecar_and_app_exact", init: []corev1.Container{{Name: "sidecar", RestartPolicy: &sidecar}}, prior: device.PodSingleDevice{{{UUID: "hcu", Usedmem: 4096}}}, memLimit: 8192, coreLimit: 100, candidateMem: 4096, wantFit: true},
		{name: "tentative_memory", tentative: device.ContainerDevices{{UUID: "other", Usedmem: 4096}}, memLimit: 8191, coreLimit: 100, candidateMem: 4096},
		{name: "cores_reject_with_memory_available", prior: device.PodSingleDevice{{{UUID: "hcu", Usedcores: 30}}}, tentative: device.ContainerDevices{{UUID: "other", Usedcores: 20}}, memLimit: 8192, coreLimit: 59, candidateMem: 4096, candidateCores: 10},
		{name: "memory_and_cores_exact", prior: device.PodSingleDevice{{{UUID: "hcu", Usedcores: 30}}}, tentative: device.ContainerDevices{{UUID: "other", Usedcores: 20}}, memLimit: 4096, coreLimit: 60, candidateMem: 4096, candidateCores: 10, wantFit: true},
		{name: "explicit_zero_core_quota", memLimit: 8192, coreLimit: 0, candidateMem: 4096, candidateCores: 10},
		{name: "nil_allocations", memLimit: 4096, coreLimit: 100, candidateMem: 4096, wantFit: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: t.Name()}, Spec: corev1.PodSpec{InitContainers: tt.init}}
			addHygonTestQuota(t, qm, pod.Namespace, tt.memLimit, tt.coreLimit)
			// Spare capacity with sentinels detects append writes beyond the original slice length.
			prior := append(device.PodSingleDevice{}, tt.prior...)
			prior = append(prior, device.ContainerDevices{{UUID: "outer-sentinel"}})
			allocated := device.PodDevices{HygonHCUDevice: prior[:len(tt.prior)]}
			tentative := append(device.ContainerDevices{}, tt.tentative...)
			tentative = append(tentative, device.ContainerDevice{UUID: "inner-sentinel"})
			tmp := map[string]device.ContainerDevices{HygonHCUDevice: tentative[:len(tt.tentative)]}
			beforeQuota := qm.GetResourceQuota()
			beforePrior := make(device.PodSingleDevice, len(prior))
			for i := range prior {
				beforePrior[i] = append(device.ContainerDevices{}, prior[i]...)
			}
			beforeTentative := append(device.ContainerDevices{}, tentative...)
			assert.Equal(t, fitQuota(pod, tmp, &allocated, pod.Namespace, "hcu", tt.candidateMem, tt.candidateCores), tt.wantFit)
			assert.DeepEqual(t, prior, beforePrior)
			assert.DeepEqual(t, tentative, beforeTentative)
			assert.Equal(t, len(allocated[HygonHCUDevice]), len(tt.prior))
			assert.Equal(t, len(tmp[HygonHCUDevice]), len(tt.tentative))
			assert.DeepEqual(t, qm.GetResourceQuota(), beforeQuota)
		})
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: t.Name() + "-unlimited"}}
	var allocated device.PodDevices
	assert.Assert(t, fitQuota(pod, nil, nil, pod.Namespace, "hcu", 8192, 0))
	assert.Assert(t, fitQuota(pod, nil, &allocated, pod.Namespace, "hcu", 8192, 0))
}
