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

package scheduler

import (
	"sort"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
)

// sortedDevices mirrors what fitInDevices does: it wraps the devices in a
// DeviceUsageList (carrying Policy and NumaBind), sorts them, and returns the
// sorted device slice in the same order Fit() will iterate.
func sortedDevices(devs []*device.DeviceUsage, scores []float32, policyName string, numaBind bool) []*device.DeviceUsage {
	dl := policy.DeviceUsageList{Policy: policyName, NumaBind: numaBind}
	for i, d := range devs {
		dl.DeviceLists = append(dl.DeviceLists, &policy.DeviceListsScore{Device: d, Score: scores[i]})
	}
	sort.Sort(dl)
	out := make([]*device.DeviceUsage, 0, len(dl.DeviceLists))
	for _, ds := range dl.DeviceLists {
		out = append(out, ds.Device)
	}
	return out
}

func numaTestNvidia() *nvidia.NvidiaGPUDevices {
	return nvidia.InitNvidiaDevice(nvidia.NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
	})
}

// A 2-card numa-bind request fits only on NUMA 0 (two devices there, one on
// NUMA 1 with a middle score). Fit needs a contiguous same-NUMA run, so the
// sort decides the outcome: score-primary interleaves NUMA 1 and fails;
// NumaBind grouping keeps NUMA 0 together and fits.
func TestNumaBindSortPreservesAffinity(t *testing.T) {
	nv := numaTestNvidia()
	mk := func(id string, numa int) *device.DeviceUsage {
		return &device.DeviceUsage{
			ID: id, Count: 10, Used: 0, Totalmem: 8192, Totalcore: 100,
			Type: nvidia.NvidiaGPUDevice, Health: true, Numa: numa,
		}
	}
	// A(n0,score5), B(n0,score1), C(n1,score3): score-primary spread interleaves
	// C between A and B.
	devs := []*device.DeviceUsage{mk("A_n0", 0), mk("B_n0", 0), mk("C_n1", 1)}
	scores := []float32{5, 1, 3}

	req := device.ContainerDeviceRequest{Nums: 2, Memreq: 100, Coresreq: 10, Type: nvidia.NvidiaGPUDevice}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Annotations: map[string]string{nvidia.NumaBind: "true"},
	}}

	// Without NUMA grouping the same-NUMA run is broken and Fit fails.
	unfit, _, _ := nv.Fit(sortedDevices(devs, scores, "spread", false), req, pod, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, unfit, false)

	// With NUMA grouping (our fix) the two NUMA-0 devices stay contiguous and fit.
	fit, result, _ := nv.Fit(sortedDevices(devs, scores, "spread", true), req, pod, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, true)
	assert.Equal(t, len(result[nvidia.NvidiaGPUDevice]), 2)
	for _, r := range result[nvidia.NvidiaGPUDevice] {
		assert.Assert(t, r.UUID == "A_n0" || r.UUID == "B_n0")
	}
}

// #1806: without numa-bind, Score wins across NUMA. The idlest device is on
// the lower NUMA id, which the old NUMA-primary sort would skip. Score-primary
// picks the globally idlest device regardless of NUMA.
func TestScorePrimarySelectsAcrossNuma(t *testing.T) {
	nv := numaTestNvidia()
	mk := func(id string, numa int) *device.DeviceUsage {
		return &device.DeviceUsage{
			ID: id, Count: 10, Used: 0, Totalmem: 8192, Totalcore: 100,
			Type: nvidia.NvidiaGPUDevice, Health: true, Numa: numa,
		}
	}
	// P(n0,score1)=idlest, Q(n1,score8), R(n1,score5). spread must pick P.
	devs := []*device.DeviceUsage{mk("P_n0", 0), mk("Q_n1", 1), mk("R_n1", 1)}
	scores := []float32{1, 8, 5}

	req := device.ContainerDeviceRequest{Nums: 1, Memreq: 100, Coresreq: 10, Type: nvidia.NvidiaGPUDevice}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{}}

	fit, result, _ := nv.Fit(sortedDevices(devs, scores, "spread", false), req, pod, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, true)
	assert.Equal(t, len(result[nvidia.NvidiaGPUDevice]), 1)
	assert.Equal(t, result[nvidia.NvidiaGPUDevice][0].UUID, "P_n0")
}
