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

package kunlun

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

// TestKunlunVDevices_Fit_RespectsMemoryQuota checks the quota in Fit. The
// webhook already rejects these requests at admission, but the namespace's
// usage can change before the pod is scheduled.
func TestKunlunVDevices_Fit_RespectsMemoryQuota(t *testing.T) {
	oldDevices := device.DevicesMap
	oldCount, oldMemory := KunlunResourceVCount, KunlunResourceVMemory
	oldRequests, oldSupport := maps.Clone(device.InRequestDevices), maps.Clone(device.SupportDevices)
	t.Cleanup(func() {
		device.DevicesMap = oldDevices
		KunlunResourceVCount, KunlunResourceVMemory = oldCount, oldMemory
		device.InRequestDevices, device.SupportDevices = oldRequests, oldSupport
	})
	dev := InitKunlunVDevice(KunlunConfig{
		ResourceVCountName:  "kunlunxin.com/vxpu",
		ResourceVMemoryName: "kunlunxin.com/vxpu-memory",
	})
	device.DevicesMap = map[string]device.Devices{dev.CommonWord(): dev}

	for _, limit := range []int64{0, KunlunMaxMemory} {
		t.Run(fmt.Sprintf("quota_%d", limit), func(t *testing.T) {
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: t.Name()}}
			addMemoryQuota(t, pod.Namespace, KunlunResourceVMemory, limit)
			request, err := dev.GenerateResourceRequests(&corev1.Container{Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{"kunlunxin.com/vxpu": resource.MustParse("1")},
			}})
			assert.NilError(t, err)
			var cards []*device.DeviceUsage
			for i := range 8 {
				cards = append(cards, &device.DeviceUsage{ID: fmt.Sprintf("xpu-%d", i), Index: uint(i), Type: dev.CommonWord(),
					Health: true, Count: 4, Totalmem: KunlunMaxMemory, Totalcore: 100})
			}

			fit, got, reason := dev.Fit(cards, request, pod, &device.NodeInfo{}, &device.PodDevices{})
			if limit == 0 {
				assert.Assert(t, !fit)
				assert.Assert(t, common.ParseReason(reason)[common.ResourceQuotaNotFit] > 0, "reason: %s", reason)
				assert.Equal(t, len(got[dev.CommonWord()]), 0)
				return
			}
			assert.Assert(t, fit, "reason: %s", reason)
			assert.Equal(t, got[dev.CommonWord()][0].Usedmem, int64(KunlunMaxMemory))
		})
	}
}
