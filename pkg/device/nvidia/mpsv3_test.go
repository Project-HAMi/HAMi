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
	p, ok := mpsV3Fits(pod, dev, 1, nil, 0)
	require.True(t, ok)
	require.Equal(t, int64(3), p)
	_, ok = mpsV3Fits(pod, dev, 1, nil, 2)
	require.False(t, ok)
	dev.Mode = HamiCoreMode
	_, ok = mpsV3Fits(pod, dev, 1, nil, 0)
	require.False(t, ok)
	dev.Mode = MpsV3Mode
	info[MPSV3ModeKey] = "percentage"
	_, ok = mpsV3Fits(pod, dev, 1, nil, 0)
	require.False(t, ok)
	delete(info, MPSV3ModeKey)
	dev.PodInfos = []*device.PodInfo{{Pod: &corev1.Pod{}}}
	_, ok = mpsV3Fits(pod, dev, 1, nil, 0)
	require.False(t, ok)
	dev.PodInfos = nil
	info[MPSV3GPUSetKey] = []string{"GPU-a", "GPU-b"}
	_, ok = mpsV3Fits(pod, dev, 1, nil, 0)
	require.False(t, ok)
	_, ok = mpsV3Fits(pod, dev, 2, device.ContainerDevices{{UUID: "GPU-c"}}, 0)
	require.False(t, ok)
	_, ok = mpsV3Fits(pod, dev, 2, device.ContainerDevices{{UUID: "GPU-b"}}, 0)
	require.True(t, ok)
	pod.Annotations = nil
	delete(info, MPSV3GPUSetKey)
	p, ok = mpsV3Fits(pod, dev, 1, nil, 0)
	require.True(t, ok)
	require.Equal(t, int64(100), p)
}

func TestMPSV3DoesNotExtendExistingGPUSet(t *testing.T) {
	pod := &corev1.Pod{}
	candidate := &device.DeviceUsage{ID: "GPU-c", Mode: MpsV3Mode}
	selected := device.ContainerDevices{{UUID: "GPU-a", CustomInfo: map[string]any{MPSV3GPUSetKey: []string{"GPU-a", "GPU-b"}}}}
	_, ok := mpsV3Fits(pod, candidate, 2, selected, 25)
	require.False(t, ok)
}
