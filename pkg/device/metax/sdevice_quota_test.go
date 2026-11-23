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

package metax

import (
	"fmt"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
)

// addMemoryQuota adds a ResourceQuota with the memory limit given in the
// resource's own unit, and removes it when the test ends.
func addMemoryQuota(t *testing.T, namespace, resourceName string, limit int64) {
	t.Helper()
	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "memory-quota", Namespace: namespace},
		Spec: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{
			corev1.ResourceName("limits." + resourceName): *resource.NewQuantity(limit, resource.DecimalSI),
		}},
	}
	device.NewQuotaManager().AddQuota(quota)
	t.Cleanup(func() { device.GetLocalCache().DelQuota(quota) })
}

func initSGPUQuotaTest(t *testing.T) *MetaxSDevices {
	t.Helper()
	oldDevices := device.DevicesMap
	oldCount, oldCore, oldMemory := MetaxResourceNameVCount, MetaxResourceNameVCore, MetaxResourceNameVMemory
	oldTopology := MetaxTopologyAware
	t.Cleanup(func() {
		device.DevicesMap = oldDevices
		MetaxResourceNameVCount, MetaxResourceNameVCore, MetaxResourceNameVMemory = oldCount, oldCore, oldMemory
		MetaxTopologyAware = oldTopology
	})
	dev := InitMetaxSDevice(MetaxConfig{
		ResourceVCountName:  "metax-tech.com/sgpu",
		ResourceVCoreName:   "metax-tech.com/vcore",
		ResourceVMemoryName: "metax-tech.com/vmemory",
	})
	device.DevicesMap = map[string]device.Devices{MetaxSGPUDevice: dev}
	return dev
}

func sgpuQuotaCard(id string, index uint) *device.DeviceUsage {
	return &device.DeviceUsage{ID: id, Index: index, Type: MetaxSGPUDevice, Health: true,
		Count: 100, Totalmem: sgpuQuotaCardUnits * MemoryFactor, Totalcore: 100}
}

const sgpuQuotaCardUnits = 64 // a 64 GiB card in 1 GiB units

// TestMetaxSDevices_Fit_WholeCardRequestRespectsMemoryQuota covers a request
// that sets no memory: Fit resolves it to the whole card, which must still fit
// the quota.
func TestMetaxSDevices_Fit_WholeCardRequestRespectsMemoryQuota(t *testing.T) {
	dev := initSGPUQuotaTest(t)
	for _, limit := range []int64{0, sgpuQuotaCardUnits} {
		t.Run(fmt.Sprintf("quota_%d", limit), func(t *testing.T) {
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: t.Name()}}
			addMemoryQuota(t, pod.Namespace, MetaxResourceNameVMemory, limit)
			request, err := dev.GenerateResourceRequests(&corev1.Container{Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{"metax-tech.com/sgpu": resource.MustParse("1")},
			}})
			assert.NilError(t, err)

			fit, got, reason := dev.Fit([]*device.DeviceUsage{sgpuQuotaCard("sgpu-0", 0)}, request, pod, &device.NodeInfo{}, &device.PodDevices{})
			if limit == 0 {
				assert.Assert(t, !fit)
				assert.Equal(t, reason, "1/1 "+common.ResourceQuotaNotFit)
				return
			}
			assert.Assert(t, fit, "reason: %s", reason)
			assert.Equal(t, got[MetaxSGPUDevice][0].Usedmem, int64(sgpuQuotaCardUnits*MemoryFactor))
		})
	}
}

// TestMetaxSDevices_Fit_SelectedDevicesRespectMemoryQuota covers two cards
// that each fit the quota on their own but not together.
func TestMetaxSDevices_Fit_SelectedDevicesRespectMemoryQuota(t *testing.T) {
	dev := initSGPUQuotaTest(t)
	for _, tt := range []struct {
		limit   int64
		wantFit bool
	}{
		{2*sgpuQuotaCardUnits - 1, false},
		{2 * sgpuQuotaCardUnits, true},
	} {
		t.Run(fmt.Sprintf("quota_%d", tt.limit), func(t *testing.T) {
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: t.Name()}}
			addMemoryQuota(t, pod.Namespace, MetaxResourceNameVMemory, tt.limit)
			cards := []*device.DeviceUsage{sgpuQuotaCard("sgpu-0", 0), sgpuQuotaCard("sgpu-1", 1)}
			request := device.ContainerDeviceRequest{Nums: 2, Type: MetaxSGPUDevice, MemPercentagereq: 100}

			fit, got, reason := dev.Fit(cards, request, pod, &device.NodeInfo{}, &device.PodDevices{})
			assert.Equal(t, fit, tt.wantFit, "reason: %s", reason)
			if !tt.wantFit {
				assert.Assert(t, common.ParseReason(reason)[common.ResourceQuotaNotFit] > 0, "reason: %s", reason)
				assert.Equal(t, len(got[MetaxSGPUDevice]), 0)
				return
			}
			assert.Equal(t, len(got[MetaxSGPUDevice]), 2)
		})
	}
}
