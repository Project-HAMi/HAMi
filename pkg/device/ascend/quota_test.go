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

package ascend

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

const quotaCardMiB = 32768

func initAscendQuotaTest(t *testing.T) *Devices {
	t.Helper()
	oldEnable, oldDevices := enableAscend, device.DevicesMap
	t.Cleanup(func() { enableAscend, device.DevicesMap = oldEnable, oldDevices })
	enableAscend = true
	dev := InitDevices(VNPUs{Configs: []VNPUConfig{{
		ChipName: "910B4", CommonWord: "Ascend910B4",
		ResourceName: "huawei.com/Ascend910B4", ResourceMemoryName: "huawei.com/Ascend910B4-memory",
		MemoryAllocatable: quotaCardMiB, MemoryCapacity: quotaCardMiB, AICore: 20,
		Templates: []Template{{Name: "vir08", Memory: 8192, AICore: 8}, {Name: "vir16", Memory: 16384, AICore: 16}},
	}}})[0]
	device.DevicesMap = map[string]device.Devices{dev.CommonWord(): dev}
	return dev
}

// addMemoryQuota adds a ResourceQuota on the device memory resource and
// removes it when the test ends.
func addMemoryQuota(t *testing.T, dev *Devices, namespace string, limit int64) {
	t.Helper()
	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "memory-quota", Namespace: namespace},
		Spec: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{
			corev1.ResourceName("limits." + dev.config.ResourceMemoryName): *resource.NewQuantity(limit, resource.DecimalSI),
		}},
	}
	device.NewQuotaManager().AddQuota(quota)
	t.Cleanup(func() { device.GetLocalCache().DelQuota(quota) })
}

func quotaCard(dev *Devices, id string, index uint, networkID *float64) *device.DeviceUsage {
	card := &device.DeviceUsage{ID: id, Index: index, Type: dev.CommonWord(), Health: true,
		Count: 4, Totalmem: quotaCardMiB, Totalcore: 20}
	if networkID != nil {
		card.CustomInfo = map[string]any{"NetworkID": *networkID}
	}
	return card
}

// TestFit_WholeCardRequestRespectsMemoryQuota covers a request that sets no
// memory: Fit resolves it to the whole card, which must still fit the quota.
func TestFit_WholeCardRequestRespectsMemoryQuota(t *testing.T) {
	dev := initAscendQuotaTest(t)
	for _, limit := range []int64{0, quotaCardMiB} {
		t.Run(fmt.Sprintf("quota_%d", limit), func(t *testing.T) {
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: t.Name()}}
			addMemoryQuota(t, dev, pod.Namespace, limit)
			request, err := dev.GenerateResourceRequests(&corev1.Container{Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{"huawei.com/Ascend910B4": resource.MustParse("1")},
			}})
			assert.NilError(t, err)

			fit, got, reason := dev.Fit([]*device.DeviceUsage{quotaCard(dev, "npu-0", 0, nil)}, request, pod, &device.NodeInfo{}, &device.PodDevices{})
			if limit == 0 {
				assert.Assert(t, !fit)
				assert.Equal(t, reason, "1/1 "+common.ResourceQuotaNotFit)
				return
			}
			assert.Assert(t, fit, "reason: %s", reason)
			assert.Equal(t, got[dev.CommonWord()][0].Usedmem, int64(quotaCardMiB))
		})
	}
}

// TestFit_TopologySelectionRespectsMemoryQuota covers topology mode, where Fit
// collects every candidate card and picks the final ones after the loop. Each
// card fits the quota on its own, so only the check on the selected cards can
// reject them.
func TestFit_TopologySelectionRespectsMemoryQuota(t *testing.T) {
	dev := initAscendQuotaTest(t)
	net0, net1 := float64(0), float64(1)
	for _, tt := range []struct {
		name    string
		cards   int
		limit   int64
		wantFit bool
	}{
		{name: "all_candidates_selected_over", cards: 2, limit: 2*quotaCardMiB - 1},
		{name: "all_candidates_selected_exact", cards: 2, limit: 2 * quotaCardMiB, wantFit: true},
		{name: "best_combination_over", cards: 3, limit: 2*quotaCardMiB - 1},
		{name: "best_combination_exact", cards: 3, limit: 2 * quotaCardMiB, wantFit: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: t.Name()}}
			addMemoryQuota(t, dev, pod.Namespace, tt.limit)
			var cards []*device.DeviceUsage
			nodeInfo := &device.NodeInfo{Devices: map[string][]device.DeviceInfo{}}
			for i := range tt.cards {
				id, net := fmt.Sprintf("npu-%d", i), &net0
				if i == 2 {
					net = &net1
				}
				cards = append(cards, quotaCard(dev, id, uint(i), net))
				nodeInfo.Devices[dev.CommonWord()] = append(nodeInfo.Devices[dev.CommonWord()],
					device.DeviceInfo{ID: id, Index: uint(i), CustomInfo: map[string]any{"NetworkID": *net}})
			}
			request := device.ContainerDeviceRequest{Nums: 2, Type: dev.CommonWord(), MemPercentagereq: 100}

			fit, got, reason := dev.Fit(cards, request, pod, nodeInfo, &device.PodDevices{})
			assert.Equal(t, fit, tt.wantFit, "reason: %s", reason)
			if !tt.wantFit {
				assert.Assert(t, common.ParseReason(reason)[common.ResourceQuotaNotFit] > 0, "reason: %s", reason)
				return
			}
			// The two cards on network 0 make the best combination. Counting
			// every candidate toward the quota would reject one of them first.
			ids := map[string]bool{}
			for _, d := range got[dev.CommonWord()] {
				ids[d.UUID] = true
			}
			assert.DeepEqual(t, ids, map[string]bool{"npu-0": true, "npu-1": true})
		})
	}
}
