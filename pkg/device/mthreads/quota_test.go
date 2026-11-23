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

package mthreads

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

// TestFit_WholeCardRequestRespectsMemoryQuota covers a request that sets no
// memory: Fit resolves it to the whole card, which must still fit the quota.
func TestFit_WholeCardRequestRespectsMemoryQuota(t *testing.T) {
	oldDevices := device.DevicesMap
	oldCount, oldMemory, oldCores := MthreadsResourceCount, MthreadsResourceMemory, MthreadsResourceCores
	oldRequests, oldSupport := maps.Clone(device.InRequestDevices), maps.Clone(device.SupportDevices)
	t.Cleanup(func() {
		device.DevicesMap = oldDevices
		MthreadsResourceCount, MthreadsResourceMemory, MthreadsResourceCores = oldCount, oldMemory, oldCores
		device.InRequestDevices, device.SupportDevices = oldRequests, oldSupport
	})
	dev := InitMthreadsDevice(MthreadsConfig{
		ResourceCountName:  "mthreads.com/vgpu",
		ResourceMemoryName: "mthreads.com/sgpu-memory",
		ResourceCoreName:   "mthreads.com/sgpu-core",
	})
	device.DevicesMap = map[string]device.Devices{MthreadsGPUDevice: dev}

	const cardMiB = defaultMemoryPerMthreadsGPU * MemoryFactor
	for _, limit := range []int64{0, defaultMemoryPerMthreadsGPU} {
		t.Run(fmt.Sprintf("quota_%d", limit), func(t *testing.T) {
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: t.Name()}}
			addMemoryQuota(t, pod.Namespace, MthreadsResourceMemory, limit)
			request, err := dev.GenerateResourceRequests(&corev1.Container{Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{"mthreads.com/vgpu": resource.MustParse("1")},
			}})
			assert.NilError(t, err)
			card := &device.DeviceUsage{ID: "mt-0", Type: MthreadsGPUDevice, Health: true,
				Count: 100, Totalmem: cardMiB, Totalcore: coresPerMthreadsGPU}

			fit, got, reason := dev.Fit([]*device.DeviceUsage{card}, request, pod, &device.NodeInfo{}, &device.PodDevices{})
			if limit == 0 {
				assert.Assert(t, !fit)
				assert.Equal(t, reason, "1/1 "+common.ResourceQuotaNotFit)
				return
			}
			assert.Assert(t, fit, "reason: %s", reason)
			assert.Equal(t, got[MthreadsGPUDevice][0].Usedmem, int64(cardMiB))
		})
	}
}
