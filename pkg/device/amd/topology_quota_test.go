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

package amd

import (
	"fmt"
	"math/bits"
	"slices"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

func TestFitTopologyQuotaFeasibleCombination(t *testing.T) {
	dev := InitAMDGPUDevice(AMDConfig{
		ResourceCountName: "amd.com/gpu", ResourceMemoryName: "amd.com/gpumem", ResourceCoreName: "amd.com/gpucores",
	})
	original := device.DevicesMap
	device.DevicesMap = map[string]device.Devices{AMDDevice: dev}
	t.Cleanup(func() { device.DevicesMap = original })
	qm := device.NewQuotaManager()

	tests := []struct {
		name      string
		limit     int64
		used      int64
		nums      int64
		size      int
		configure func([]*device.DeviceUsage, *corev1.Pod, *device.PodDevices)
		want      []string
	}{
		{name: "lower-scoring pair fits", limit: 8000, want: []string{"small-b", "small-a"}},
		{name: "first feasible score tie", limit: 12000, want: []string{"large-b", "small-b"}},
		{name: "preferred linked pair still wins", limit: 16000, want: []string{"large-b", "large-a"}},
		{name: "no feasible pair", limit: 7000},
		{name: "zero quota rejects every candidate", limit: 0},
		{name: "namespace usage reduces remaining quota", limit: 14000, used: 6000, want: []string{"small-b", "small-a"}},
		{
			name: "best feasible topology outranks the first feasible pair", limit: 12000,
			configure: func(gpus []*device.DeviceUsage, _ *corev1.Pod, _ *device.PodDevices) {
				gpus[0].CustomInfo = map[string]any{"pciBDF": "c", "xgmiPeers": []string{"d"}}
				gpus[1].CustomInfo = map[string]any{"pciBDF": "d", "xgmiPeers": []string{"c"}}
			},
			want: []string{"small-b", "small-a"},
		},
		{
			name: "other app container is included", limit: 14000,
			configure: func(_ []*device.DeviceUsage, pod *corev1.Pod, allocated *device.PodDevices) {
				pod.Spec.Containers = []corev1.Container{{Name: "earlier"}, {Name: "workload"}}
				(*allocated)[AMDDevice] = device.PodSingleDevice{{{UUID: "large-a", Type: AMDDevice, Usedmem: 6000}}}
			},
			want: []string{"small-b", "small-a"},
		},
		{
			name: "regular init memory can be reused", limit: 28000,
			configure: func(_ []*device.DeviceUsage, pod *corev1.Pod, allocated *device.PodDevices) {
				pod.Spec.InitContainers = []corev1.Container{{Name: "init"}}
				(*allocated)[AMDDevice] = device.PodSingleDevice{{
					{UUID: "large-a", Type: AMDDevice, Usedmem: 12000},
					{UUID: "large-b", Type: AMDDevice, Usedmem: 12000},
				}}
			},
			want: []string{"large-b", "large-a"},
		},
		{
			name: "sidecar memory remains reserved", limit: 16000,
			configure: func(_ []*device.DeviceUsage, pod *corev1.Pod, allocated *device.PodDevices) {
				restart := corev1.ContainerRestartPolicyAlways
				pod.Spec.InitContainers = []corev1.Container{{Name: "sidecar", RestartPolicy: &restart}}
				(*allocated)[AMDDevice] = device.PodSingleDevice{{
					{UUID: "large-a", Type: AMDDevice, Usedmem: 4000},
					{UUID: "large-b", Type: AMDDevice, Usedmem: 4000},
				}}
			},
			want: []string{"small-b", "small-a"},
		},
		{
			name: "feasible alternative NUMA group", limit: 12000,
			configure: func(gpus []*device.DeviceUsage, pod *corev1.Pod, _ *device.PodDevices) {
				pod.Annotations[AMDNumaBind] = "true"
				gpus[0].Numa, gpus[1].Numa = 1, 1
			},
			want: []string{"small-b", "small-a"},
		},
		{
			name: "quota cannot relax NUMA binding", limit: 8000,
			configure: func(gpus []*device.DeviceUsage, pod *corev1.Pod, _ *device.PodDevices) {
				pod.Annotations[AMDNumaBind] = "true"
				gpus[0].Numa, gpus[2].Numa = 0, 0
				gpus[1].Numa, gpus[3].Numa = 1, 1
			},
		},
		{
			name: "large pool uses feasible fallback", limit: 8000, size: 17,
			configure: func(gpus []*device.DeviceUsage, _ *corev1.Pod, _ *device.PodDevices) {
				for i := 4; i < len(gpus); i++ {
					*gpus[i] = device.DeviceUsage{ID: fmt.Sprintf("extra-%d", i), Count: 2, Totalmem: 16000, Totalcore: 100, Numa: 0, Type: AMDDevice, Health: true}
				}
			},
			want: []string{"small-b", "small-a"},
		},
		{
			name: "large pool keeps an already feasible first pair", limit: 16000, size: 17,
			configure: func(gpus []*device.DeviceUsage, _ *corev1.Pod, _ *device.PodDevices) {
				for i := 4; i < len(gpus); i++ {
					*gpus[i] = device.DeviceUsage{ID: fmt.Sprintf("extra-%d", i), Count: 2, Totalmem: 16000, Totalcore: 100, Numa: 0, Type: AMDDevice, Health: true}
				}
			},
			want: []string{"extra-16", "extra-15"},
		},
		{
			name: "large pool fallback uses effective init cost", limit: 32000, nums: 3, size: 18,
			configure: func(gpus []*device.DeviceUsage, pod *corev1.Pod, allocated *device.PodDevices) {
				for i := range gpus {
					*gpus[i] = device.DeviceUsage{ID: fmt.Sprintf("extra-%d", i), Count: 2, Totalmem: 16000, Totalcore: 100, Numa: 0, Type: AMDDevice, Health: true}
				}
				pod.Spec.InitContainers = []corev1.Container{{Name: "init"}}
				(*allocated)[AMDDevice] = device.PodSingleDevice{{
					{UUID: "extra-0", Type: AMDDevice, Usedmem: 12000},
					{UUID: "extra-1", Type: AMDDevice, Usedmem: 12000},
				}}
			},
			want: []string{"extra-1", "extra-0", "extra-17"},
		},
		{
			name: "large pool has no feasible fallback", limit: 8000, nums: 3, size: 18,
			configure: func(gpus []*device.DeviceUsage, _ *corev1.Pod, _ *device.PodDevices) {
				for i := range gpus {
					*gpus[i] = device.DeviceUsage{ID: fmt.Sprintf("extra-%d", i), Count: 2, Totalmem: 8000, Totalcore: 100, Numa: 0, Type: AMDDevice, Health: true}
				}
			},
		},
	}
	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			namespace := fmt.Sprintf("amd-topology-quota-%d", index)
			quota := &corev1.ResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "memory", Namespace: namespace},
				Spec: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{
					"limits.amd.com/gpumem": *resource.NewQuantity(tt.limit, resource.DecimalSI),
				}},
			}
			qm.AddQuota(quota)
			t.Cleanup(func() { qm.DelQuota(quota) })
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "workload", Namespace: namespace, Annotations: map[string]string{
				util.GPUSchedulerPolicyAnnotationKey: string(util.GPUSchedulerPolicyTopology),
			}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "workload"}}}}
			if tt.used > 0 {
				usage := device.PodDevices{AMDDevice: {{{UUID: "other", Type: AMDDevice, Usedmem: tt.used}}}}
				qm.AddUsage(pod, usage)
				t.Cleanup(func() { qm.RmUsage(pod, usage) })
			}
			gpus := []*device.DeviceUsage{
				{ID: "small-a", Count: 2, Totalmem: 8000, Totalcore: 100, Numa: 0, Type: AMDDevice, Health: true},
				{ID: "small-b", Count: 2, Totalmem: 8000, Totalcore: 100, Numa: 0, Type: AMDDevice, Health: true},
				{ID: "large-a", Count: 2, Totalmem: 16000, Totalcore: 100, Numa: 0, Type: AMDDevice, Health: true, CustomInfo: map[string]any{"pciBDF": "a", "xgmiPeers": []string{"b"}}},
				{ID: "large-b", Count: 2, Totalmem: 16000, Totalcore: 100, Numa: 0, Type: AMDDevice, Health: true, CustomInfo: map[string]any{"pciBDF": "b", "xgmiPeers": []string{"a"}}},
			}
			for len(gpus) < tt.size {
				gpus = append(gpus, &device.DeviceUsage{})
			}
			allocated := device.PodDevices{}
			if tt.configure != nil {
				tt.configure(gpus, pod, &allocated)
			}
			before := make(device.PodDevices)
			for typ, containers := range allocated {
				for _, ctr := range containers {
					before[typ] = append(before[typ], slices.Clone(ctr))
				}
			}
			nums := tt.nums
			if nums == 0 {
				nums = 2
			}
			request := device.ContainerDeviceRequest{Nums: nums, Type: AMDDevice, MemPercentagereq: 50, Coresreq: 25}
			ok, got, reason := dev.Fit(gpus, request, pod, &device.NodeInfo{}, &allocated)
			assert.DeepEqual(t, allocated, before)
			if tt.want == nil {
				assert.Equal(t, false, ok)
				assert.Assert(t, strings.Contains(reason, common.ResourceQuotaNotFit), reason)
				return
			}
			assert.Equal(t, true, ok, reason)
			var ids []string
			for _, c := range got[AMDDevice] {
				ids = append(ids, c.UUID)
			}
			assert.DeepEqual(t, ids, tt.want)
			last := len(got[AMDDevice]) - 1
			c := got[AMDDevice][last]
			assert.Assert(t, fitQuota(pod, map[string]device.ContainerDevices{AMDDevice: got[AMDDevice][:last]}, &allocated, pod.Namespace, c.UUID, c.Usedmem))
		})
	}
}

func TestFitTopologyQuotaMatchesExhaustiveSearch(t *testing.T) {
	dev := InitAMDGPUDevice(AMDConfig{ResourceCountName: "amd.com/gpu", ResourceMemoryName: "amd.com/gpumem"})
	original := device.DevicesMap
	device.DevicesMap = map[string]device.Devices{AMDDevice: dev}
	t.Cleanup(func() { device.DevicesMap = original })
	qm := device.NewQuotaManager()
	for scenario := range 30 {
		t.Run(fmt.Sprintf("scenario-%d", scenario), func(t *testing.T) {
			namespace := fmt.Sprintf("amd-topology-oracle-%d", scenario)
			limit := int64(8000 + scenario%7*2000)
			quota := &corev1.ResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "memory", Namespace: namespace},
				Spec: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{
					"limits.amd.com/gpumem": *resource.NewQuantity(limit, resource.DecimalSI),
				}},
			}
			qm.AddQuota(quota)
			t.Cleanup(func() { qm.DelQuota(quota) })
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "workload", Namespace: namespace, Annotations: map[string]string{
				util.GPUSchedulerPolicyAnnotationKey: string(util.GPUSchedulerPolicyTopology),
			}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "workload"}}}}
			numaBind := scenario%3 == 0
			if numaBind {
				pod.Annotations[AMDNumaBind] = "true"
			}
			var gpus []*device.DeviceUsage
			for i, mem := range []int64{6000, 8000, 12000, 16000, 10000, 14000} {
				var peers []string
				for j := range 6 {
					if i != j && (i+j+scenario)%3 == 0 {
						peers = append(peers, fmt.Sprintf("pci-%d", j))
					}
				}
				gpus = append(gpus, &device.DeviceUsage{
					ID: fmt.Sprintf("gpu-%d", i), Count: 4, Totalmem: mem, Totalcore: 100, Numa: i % 2,
					Type: AMDDevice, Health: true, CustomInfo: map[string]any{"pciBDF": fmt.Sprintf("pci-%d", i), "xgmiPeers": peers},
				})
			}
			allocated := device.PodDevices{}
			if scenario%5 != 0 {
				pod.Spec.InitContainers = []corev1.Container{{Name: "init"}}
				if scenario%5 == 1 {
					restart := corev1.ContainerRestartPolicyAlways
					pod.Spec.InitContainers[0].RestartPolicy = &restart
				}
				allocated[AMDDevice] = device.PodSingleDevice{{
					{UUID: "gpu-0", Type: AMDDevice, Usedmem: 4000},
					{UUID: "gpu-3", Type: AMDDevice, Usedmem: 5000},
				}}
			}
			nums := 2 + scenario%2
			scoreOf := func(chosen device.ContainerDevices) int {
				score := 0
				for i, a := range chosen {
					var ai int
					_, _ = fmt.Sscanf(a.UUID, "gpu-%d", &ai)
					for _, b := range chosen[i+1:] {
						var bi int
						_, _ = fmt.Sscanf(b.UUID, "gpu-%d", &bi)
						if (ai+bi+scenario)%3 == 0 {
							score += 4
						}
						if ai%2 == bi%2 {
							score++
						}
					}
				}
				return score
			}
			bestScore := -1
			// Independently enumerate complete allocations and charge their collapsed usage.
			for mask := uint(1); mask < 1<<len(gpus); mask++ {
				if bits.OnesCount(mask) != nums {
					continue
				}
				var chosen device.ContainerDevices
				numa := -1
				valid := true
				for i, gpu := range gpus {
					if mask&(1<<i) == 0 {
						continue
					}
					if numaBind && numa != -1 && numa != gpu.Numa {
						valid = false
					}
					numa = gpu.Numa
					chosen = append(chosen, device.ContainerDevice{UUID: gpu.ID, Type: AMDDevice, Usedmem: gpu.Totalmem / 2})
				}
				if !valid {
					continue
				}
				raw := device.PodDevices{AMDDevice: append(slices.Clone(allocated[AMDDevice]), chosen)}
				var memory int64
				for _, ctr := range device.CollapseInitContainerUsage(pod, raw)[AMDDevice] {
					for _, c := range ctr {
						memory += c.Usedmem
					}
				}
				if memory <= limit {
					bestScore = max(bestScore, scoreOf(chosen))
				}
			}
			ok, got, reason := dev.Fit(gpus, device.ContainerDeviceRequest{Nums: int64(nums), Type: AMDDevice, MemPercentagereq: 50, Coresreq: 25}, pod, &device.NodeInfo{}, &allocated)
			assert.Equal(t, ok, bestScore >= 0, reason)
			if ok {
				assert.Equal(t, len(got[AMDDevice]), nums)
				assert.Equal(t, scoreOf(got[AMDDevice]), bestScore)
			}
		})
	}
}

func BenchmarkBestLinkedCombination(b *testing.B) {
	for _, size := range []int{8, 16, 32} {
		b.Run(fmt.Sprintf("candidates-%d", size), func(b *testing.B) {
			var gpus []*device.DeviceUsage
			var candidates device.ContainerDevices
			for i := range size {
				id := fmt.Sprintf("gpu-%d", i)
				gpus = append(gpus, &device.DeviceUsage{ID: id, Numa: i % 2})
				candidates = append(candidates, device.ContainerDevice{UUID: id, Usedmem: 1000})
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if got, ok := bestLinkedCombination(gpus, candidates, size/2, false, nil); !ok || len(got) != size/2 {
					b.Fatal("no complete combination")
				}
			}
		})
	}
}

func BenchmarkBestLinkedCombinationQuota(b *testing.B) {
	for _, tt := range []struct {
		name  string
		size  int
		limit int64
		fit   bool
	}{
		{name: "exhaustive-fit", size: 16, limit: 56000, fit: true},
		{name: "exhaustive-rejection", size: 16, limit: 24000},
		{name: "fallback-fit", size: 32, limit: 136000, fit: true},
		{name: "fallback-rejection", size: 32, limit: 120000},
	} {
		b.Run(tt.name, func(b *testing.B) {
			var gpus []*device.DeviceUsage
			var candidates device.ContainerDevices
			cost := make(map[string]int64)
			for i := range tt.size {
				id := fmt.Sprintf("gpu-%d", i)
				gpus = append(gpus, &device.DeviceUsage{ID: id, Numa: i % 2})
				candidates = append(candidates, device.ContainerDevice{UUID: id})
				cost[id] = int64(tt.size-i) * 1000
			}
			quota := &combinationQuota{cost: cost, fits: func(mem int64) bool { return mem <= tt.limit }}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				got, ok := bestLinkedCombination(gpus, candidates, tt.size/2, false, quota)
				if ok != tt.fit || (ok && len(got) != tt.size/2) {
					b.Fatal("incorrect quota feasibility")
				}
			}
		})
	}
}
