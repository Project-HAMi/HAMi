// Copyright 2026 The HAMi Authors.
// SPDX-License-Identifier: Apache-2.0
package nvidia

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

func TestMPSV3Geometry(t *testing.T) {
	for _, tc := range []struct {
		g            MPSV3Geometry
		sms, percent int64
		ok           bool
	}{
		{MPSV3Geometry{72, 2}, 2, 3, true}, {MPSV3Geometry{72, 2}, 36, 50, true}, {MPSV3Geometry{72, 2}, 3, 0, false},
		{MPSV3Geometry{188, 8}, 188, 100, true}, {MPSV3Geometry{188, 8}, 12, 7, true}, {MPSV3Geometry{188, 8}, 10, 0, false},
		{MPSV3Geometry{72, 2}, 74, 0, false}, {MPSV3Geometry{72, 2}, 0, 0, false}, {MPSV3Geometry{}, 2, 0, false},
	} {
		p, err := tc.g.Percent(tc.sms)
		if tc.ok {
			require.NoError(t, err)
			require.Equal(t, tc.percent, p)
		} else {
			require.Error(t, err)
		}
	}
}
func TestMPSV3RequestAndMode(t *testing.T) {
	for _, raw := range []string{"", "0", "-1", "2.5", "9223372036854775808"} {
		_, err := MPSV3RequestedSMs(map[string]string{MPSV3SMCountAnnotation: raw})
		require.Error(t, err)
	}
	n, err := MPSV3RequestedSMs(nil)
	require.NoError(t, err)
	require.Zero(t, n)
	require.True(t, matchesOperatingMode("mps-v3", MpsV3Mode))
	require.False(t, matchesOperatingMode("mps-v3", MpsMode))
	require.False(t, matchesOperatingMode("mps", MpsV3Mode))
	require.True(t, matchesOperatingMode("hami-core, mps-v3", MpsV3Mode))
}
func TestMPSV3Scheduling(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{MPSV3SMCountAnnotation: "2"}}}
	info := map[string]any{MPSV3GeometryKey: MPSV3Geometry{72, 2}}
	// Registration goes through JSON, so the scheduler receives maps/numbers.
	data, err := json.Marshal(info)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &info))
	dev := &device.DeviceUsage{ID: "GPU-a", Mode: MpsV3Mode, CustomInfo: info}
	p, ok := mpsV3Fits(pod, dev, 1, nil, 0, nil)
	require.True(t, ok)
	require.Equal(t, int64(3), p)
	_, ok = mpsV3Fits(pod, dev, 1, nil, 2, nil)
	require.False(t, ok)
	dev.Mode = HamiCoreMode
	_, ok = mpsV3Fits(pod, dev, 1, nil, 0, nil)
	require.False(t, ok)
	dev.Mode = MpsV3Mode
	info[MPSV3ModeKey] = "percentage"
	_, ok = mpsV3Fits(pod, dev, 1, nil, 0, nil)
	require.False(t, ok)
	delete(info, MPSV3ModeKey)
	dev.PodInfos = []*device.PodInfo{{Pod: &corev1.Pod{}}}
	_, ok = mpsV3Fits(pod, dev, 1, nil, 0, nil)
	require.False(t, ok)
	dev.PodInfos = nil
	info[MPSV3GPUSetKey] = []string{"GPU-a", "GPU-b"}
	_, ok = mpsV3Fits(pod, dev, 1, nil, 0, nil)
	require.False(t, ok)
	_, ok = mpsV3Fits(pod, dev, 2, device.ContainerDevices{{UUID: "GPU-c"}}, 0, nil)
	require.False(t, ok)
	_, ok = mpsV3Fits(pod, dev, 2, device.ContainerDevices{{UUID: "GPU-b"}}, 0, nil)
	require.True(t, ok)
	pod.Annotations = nil
	delete(info, MPSV3GPUSetKey)
	p, ok = mpsV3Fits(pod, dev, 1, nil, 0, nil)
	require.True(t, ok)
	require.Equal(t, int64(100), p)
}

func TestMPSV3DoesNotExtendExistingGPUSet(t *testing.T) {
	pod := &corev1.Pod{}
	candidate := &device.DeviceUsage{ID: "GPU-c", Mode: MpsV3Mode}
	selected := device.ContainerDevices{{UUID: "GPU-a", CustomInfo: map[string]any{MPSV3GPUSetKey: []string{"GPU-a", "GPU-b"}}}}
	_, ok := mpsV3Fits(pod, candidate, 2, selected, 25, nil)
	require.False(t, ok)
}

func TestMPSV3TimesliceScheduling(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{MPSV3ComputeModeAnnotation: MPSV3ServerTimeslice}}}
	usage := &device.DeviceUsage{
		ID: "GPU-a", Type: "NVIDIA A10", Mode: MpsV3Mode, Health: true, Count: 10, Used: 1,
		Totalcore: 100, Usedcores: 25, Totalmem: 8192,
		CustomInfo: map[string]any{MPSV3ComputeModeKey: MPSV3ServerTimeslice},
	}
	nv := &NvidiaGPUDevices{}
	req := device.ContainerDeviceRequest{Nums: 1, Type: NvidiaGPUDevice, Memreq: 1024, Coresreq: 75}
	alreadyAllocated := device.PodDevices{NvidiaGPUDevice: {{device.ContainerDevice{UUID: "GPU-a"}}}}
	fit, result, _ := nv.Fit([]*device.DeviceUsage{usage}, req, pod, &device.NodeInfo{}, &alreadyAllocated)
	require.True(t, fit)
	require.Equal(t, int64(75), result[NvidiaGPUDevice][0].Usedcores)
	req.Coresreq = 76
	fit, _, _ = nv.Fit([]*device.DeviceUsage{usage}, req, pod, &device.NodeInfo{}, &alreadyAllocated)
	require.False(t, fit)
	req.Coresreq = 0
	fit, _, _ = nv.Fit([]*device.DeviceUsage{usage}, req, pod, &device.NodeInfo{}, &alreadyAllocated)
	require.False(t, fit)
	usage.Used, usage.Usedcores = 0, 0
	fit, result, _ = nv.Fit([]*device.DeviceUsage{usage}, req, pod, &device.NodeInfo{}, nil)
	require.True(t, fit)
	require.Equal(t, int64(100), result[NvidiaGPUDevice][0].Usedcores)
	_, ok := mpsV3Fits(pod, usage, 2, nil, 25, &alreadyAllocated)
	require.False(t, ok)
	pod.Annotations = nil
	_, ok = mpsV3Fits(pod, usage, 1, nil, 25, nil)
	require.False(t, ok)
	usage.CustomInfo[MPSV3ComputeModeKey] = MPSV3NamespaceCap
	_, ok = mpsV3Fits(pod, usage, 1, nil, 25, nil)
	require.True(t, ok)
}

func TestMPSV3BorrowingRequest(t *testing.T) {
	g := MPSV3Geometry{SMs: 188, Chunk: 8}
	for _, tc := range []struct {
		mode, sms, shareable, max string
		want                      bool
	}{
		{"", "12", "", "0", true},
		{"", "16", "true", "2", true},
		{"", "16", "false", "unlimited", true},
		{"", "16", "", "23", true},
		{"", "16", "", "1", false},
		{"", "16", "", "24", false},
		{"", "16", "", "-1", false},
		{"", "12", "true", "", false},
		{"", "12", "", "unlimited", false},
		{"", "16", "yes", "", false},
		{"", "", "false", "", false},
		{"", "", "", "0", false},
		{MPSV3ServerTimeslice, "16", "", "", false},
	} {
		annotations := make(map[string]string)
		for key, value := range map[string]string{
			MPSV3ComputeModeAnnotation: tc.mode, MPSV3SMCountAnnotation: tc.sms,
			MPSV3SMShareableAnnotation: tc.shareable, MPSV3SMMaxChunksAnnotation: tc.max,
		} {
			if value != "" {
				annotations[key] = value
			}
		}
		_, sms, policy, err := MPSV3Request(annotations)
		if err == nil {
			err = policy.Validate(g, sms)
		}
		require.Equal(t, tc.want, err == nil, "%+v: %v", tc, err)
	}
}

func TestMPSV3NodePolicy(t *testing.T) {
	defaults, err := (MPSV3NodeConfig{}).Resolve()
	require.NoError(t, err)
	require.Equal(t, MPSV3NamespaceCap, defaults.ComputeMode)
	weighted, err := (MPSV3NodeConfig{ComputeMode: MPSV3ServerTimeslice}).Resolve()
	require.NoError(t, err)
	require.Equal(t, "default", weighted.Timeslice)
	zero := 0
	for _, invalid := range []MPSV3NodeConfig{
		{ComputeMode: "unknown"},
		{Timeslice: "short"},
		{ComputeMode: MPSV3ServerTimeslice, Timeslice: "unknown"},
		{ComputeMode: MPSV3ServerTimeslice, TimesliceYieldLevel: &zero},
	} {
		_, err := invalid.Resolve()
		require.Error(t, err)
	}
}

func TestMPSV3FitRetriesCompatibleMemoryGroup(t *testing.T) {
	var devices []*device.DeviceUsage
	for i, id := range []string{"GPU-a", "GPU-b", "GPU-c"} {
		memory := int64(24 * 1024)
		if i == 2 {
			memory *= 2
		}
		devices = append(devices, &device.DeviceUsage{ID: id, Type: "NVIDIA", Mode: MpsV3Mode, Health: true, Count: 10, Totalcore: 100, Totalmem: memory})
	}
	nv := &NvidiaGPUDevices{}
	request := device.ContainerDeviceRequest{Nums: 2, Type: NvidiaGPUDevice, MemPercentagereq: 50, Coresreq: 25}
	ok, result, reason := nv.Fit(devices, request, &corev1.Pod{}, &device.NodeInfo{}, nil)
	require.True(t, ok, reason)
	require.Len(t, result[NvidiaGPUDevice], 2)
	for _, selected := range result[NvidiaGPUDevice] {
		require.Equal(t, int64(12*1024), selected.Usedmem)
	}
}

func TestMPSV3RejectsPartialOverlapInEitherOrder(t *testing.T) {
	a, b, c := device.ContainerDevice{UUID: "GPU-a"}, device.ContainerDevice{UUID: "GPU-b"}, device.ContainerDevice{UUID: "GPU-c"}
	previous := device.PodSingleDevice{{a, b}}
	require.False(t, mpsV3FitsPreviousSets(previous, c.UUID, 2, device.ContainerDevices{a}))
	require.False(t, mpsV3FitsPreviousSets(previous, a.UUID, 2, device.ContainerDevices{c}))
	require.True(t, mpsV3FitsPreviousSets(previous, b.UUID, 2, device.ContainerDevices{a}))
}
