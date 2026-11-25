// Copyright 2026 The HAMi Authors.
// SPDX-License-Identifier: Apache-2.0
package nvidia

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

func TestMPSV3AdmissionRejectsReservedEnvironment(t *testing.T) {
	dev := &NvidiaGPUDevices{config: NvidiaConfig{ResourceCountName: "nvidia.com/gpu"}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{AllocateMode: MpsV3Mode}}}
	ctr := &corev1.Container{Name: "worker"}
	for _, name := range []string{"CUDA_MPS_PIPE_DIRECTORY", "NVIDIA_GPU_MEMORY_LIMIT"} {
		ctr.Env = []corev1.EnvVar{{Name: name}}
		ctr.Resources.Limits = nil
		_, err := dev.MutateAdmission(ctr, pod)
		require.NoError(t, err)
		ctr.Resources.Limits = corev1.ResourceList{"nvidia.com/gpu": resource.MustParse("1")}
		_, err = dev.MutateAdmission(ctr, pod)
		require.ErrorContains(t, err, name)
	}
	pod.Annotations[AllocateMode] = HamiCoreMode
	_, err := dev.MutateAdmission(ctr, pod)
	require.NoError(t, err)
}

func TestMPSV3FitReportsIncompatibleStrategy(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{MPSV3ComputeModeAnnotation: MPSV3ServerTimeslice}}}
	req := device.ContainerDeviceRequest{Nums: 1, Type: NvidiaGPUDevice, Memreq: 1024}
	ok, _, reason := (&NvidiaGPUDevices{}).Fit(mpsV3TestGPUs(MPSV3NamespaceCap, "GPU-a"), req, pod, &device.NodeInfo{}, nil)
	require.False(t, ok)
	require.Equal(t, "1/1 "+common.CardMPSV3Incompatible, reason)
}

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
	require.True(t, MatchesOperatingMode("mps-v3", MpsV3Mode))
	require.False(t, MatchesOperatingMode("mps-v3", MpsMode))
	require.False(t, MatchesOperatingMode("mps", MpsV3Mode))
	require.True(t, MatchesOperatingMode("hami-core, mps-v3", MpsV3Mode))
}
func TestMPSV3Scheduling(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{MPSV3SMCountAnnotation: "2"}}}
	info := map[string]any{MPSV3ComputeModeKey: MPSV3NamespaceCap, MPSV3GeometryKey: MPSV3Geometry{72, 2}}
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
	// Node ownership can outlast the Pod. Let Allocate reclaim a stale owner
	// of the same GPU set, while still rejecting live incompatible Pods.
	info[MPSV3GPUSetKey] = []string{"GPU-a"}
	_, ok = mpsV3Fits(pod, dev, 1, nil, 0, nil)
	require.True(t, ok)
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
	candidate := &device.DeviceUsage{ID: "GPU-c", Mode: MpsV3Mode, CustomInfo: map[string]any{MPSV3ComputeModeKey: MPSV3NamespaceCap}}
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
		devices = append(devices, &device.DeviceUsage{ID: id, Type: "NVIDIA", Mode: MpsV3Mode, Health: true, Count: 10, Totalcore: 100, Totalmem: memory, CustomInfo: map[string]any{MPSV3ComputeModeKey: MPSV3NamespaceCap}})
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

func TestMPSV3SMRemainderCanOnlyBeClaimedOnce(t *testing.T) {
	request := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{MPSV3SMCountAnnotation: "12"}}}
	previous := request.DeepCopy()
	dev := &device.DeviceUsage{ID: "GPU-a", Mode: MpsV3Mode, CustomInfo: map[string]any{
		MPSV3ComputeModeKey: MPSV3NamespaceCap, MPSV3GeometryKey: MPSV3Geometry{SMs: 188, Chunk: 8},
	}}
	set := device.PodSingleDevice{{{UUID: dev.ID}}}
	prior := device.PodDevices{NvidiaGPUDevice: set}

	_, ok := mpsV3Fits(request, dev, 1, nil, 0, &prior)
	require.False(t, ok, "an earlier container in the Pod already claims the remainder")
	dev.PodInfos = []*device.PodInfo{{Pod: previous, Devices: prior, RawDevices: prior}}
	_, ok = mpsV3Fits(request, dev, 1, nil, 0, nil)
	require.False(t, ok, "an existing Pod already claims the remainder")

	previous.Annotations[MPSV3SMCountAnnotation] = "16"
	charge, ok := mpsV3Fits(request, dev, 1, nil, 0, nil)
	require.True(t, ok, "the existing partition uses whole chunks")
	require.EqualValues(t, 7, charge)
}

func TestMPSV3RegistrationPreservesSchedulingMetadata(t *testing.T) {
	gpu := "GPU-a"
	nv := &NvidiaGPUDevices{}
	for _, mode := range []string{MPSV3NamespaceCap, MPSV3ServerTimeslice} {
		t.Run(mode, func(t *testing.T) {
			registered := &device.DeviceInfo{
				ID: gpu, Mode: MpsV3Mode, Health: true,
				CustomInfo: map[string]any{
					MPSV3ComputeModeKey: mode, MPSV3GeometryKey: MPSV3Geometry{SMs: 72, Chunk: 2},
					MPSV3GPUSetKey: []string{gpu},
				},
			}
			data, err := json.Marshal([]*device.DeviceInfo{registered})
			require.NoError(t, err)
			node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				RegisterAnnos: string(data),
			}}}
			decoded, err := nv.GetNodeDevices(node)
			require.NoError(t, err)
			require.Len(t, decoded, 1)
			require.True(t, decoded[0].Health)
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{MPSV3ComputeModeAnnotation: mode}}}
			if mode == MPSV3NamespaceCap {
				pod.Annotations[MPSV3SMCountAnnotation] = "16"
			}
			_, ok := mpsV3Fits(pod, &device.DeviceUsage{ID: gpu, Mode: MpsV3Mode, CustomInfo: decoded[0].CustomInfo}, 1, nil, 0, nil)
			require.True(t, ok)
		})
	}
}

func TestMPSV3FitRetriesCompatibleExistingGPUSet(t *testing.T) {
	var devices []*device.DeviceUsage
	for _, id := range []string{"GPU-b", "GPU-d", "GPU-c", "GPU-a"} {
		set := []string{"GPU-a", "GPU-b"}
		if id == "GPU-c" || id == "GPU-d" {
			set = []string{"GPU-c", "GPU-d"}
		}
		d := &device.DeviceUsage{
			ID: id, Type: "NVIDIA", Mode: MpsV3Mode, Health: true, Count: 10, Totalcore: 100, Totalmem: 24576,
			CustomInfo: map[string]any{MPSV3ComputeModeKey: MPSV3NamespaceCap, MPSV3GPUSetKey: set},
		}
		if id == "GPU-b" {
			d.Usedcores = 90
		}
		devices = append(devices, d)
	}
	nv := &NvidiaGPUDevices{}
	request := device.ContainerDeviceRequest{Nums: 2, Type: NvidiaGPUDevice, Memreq: 1024, Coresreq: 25}
	ok, chosen, reason := nv.Fit(devices, request, &corev1.Pod{}, &device.NodeInfo{}, nil)
	require.True(t, ok, reason)
	require.Len(t, chosen[NvidiaGPUDevice], 2)
	require.Equal(t, "GPU-c", chosen[NvidiaGPUDevice][0].UUID)
	require.Equal(t, "GPU-d", chosen[NvidiaGPUDevice][1].UUID)
}

func TestMPSV3FitsUsesPerContainerSets(t *testing.T) {
	a, b, c := device.ContainerDevice{UUID: "GPU-a"}, device.ContainerDevice{UUID: "GPU-b"}, device.ContainerDevice{UUID: "GPU-c"}
	previous := &corev1.Pod{}
	raw := device.PodDevices{NvidiaGPUDevice: {{a, b}, {c}}}
	dev := &device.DeviceUsage{
		ID: b.UUID, Mode: MpsV3Mode, CustomInfo: map[string]any{MPSV3ComputeModeKey: MPSV3NamespaceCap},
		PodInfos: []*device.PodInfo{{
			Pod: previous, RawDevices: raw, Devices: device.CollapseInitContainerUsage(previous, raw),
		}},
	}
	_, ok := mpsV3Fits(&corev1.Pod{}, dev, 2, device.ContainerDevices{a}, 25, nil)
	require.True(t, ok)
	_, ok = mpsV3Fits(&corev1.Pod{}, dev, 2, device.ContainerDevices{c}, 25, nil)
	require.False(t, ok)
}

func mpsV3TestGPUs(mode string, ids ...string) []*device.DeviceUsage {
	devices := make([]*device.DeviceUsage, 0, len(ids))
	for i, id := range ids {
		devices = append(devices, &device.DeviceUsage{
			ID: id, Index: uint(i), Type: NvidiaGPUDevice, Mode: MpsV3Mode,
			Health: true, Count: 10, Totalcore: 100, Totalmem: 24576,
			CustomInfo: map[string]any{
				MPSV3ComputeModeKey: mode,
				MPSV3GeometryKey:    MPSV3Geometry{SMs: 72, Chunk: 2},
			},
		})
	}
	return devices
}

func TestMPSV3MultiGPUQuota(t *testing.T) {
	for _, mode := range []string{MPSV3NamespaceCap, MPSV3ServerTimeslice} {
		for _, topology := range []bool{false, true} {
			for _, tc := range []struct {
				name         string
				cores        int64
				used         int64
				secondMemory int64
				want         bool
			}{
				{name: "equal limits", cores: 25, used: 75, secondMemory: 24576, want: true},
				{name: "one card lacks compute", cores: 25, used: 76, secondMemory: 24576},
				{name: "unequal resolved memory", cores: 25, secondMemory: 49152},
				{name: "default full compute", secondMemory: 24576, want: true},
			} {
				t.Run(fmt.Sprintf("%s/topology=%t/%s", mode, topology, tc.name), func(t *testing.T) {
					devices := mpsV3TestGPUs(mode, "GPU-a", "GPU-b")
					devices[1].Totalmem = tc.secondMemory
					devices[1].Usedcores = tc.used
					if tc.used > 0 {
						devices[1].Used = 1
					}
					pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{MPSV3ComputeModeAnnotation: mode}}}
					if topology {
						pod.Annotations[util.GPUSchedulerPolicyAnnotationKey] = util.GPUSchedulerPolicyTopology.String()
					}
					req := device.ContainerDeviceRequest{Nums: 2, Type: NvidiaGPUDevice, MemPercentagereq: 50, Coresreq: tc.cores}
					ok, result, reason := (&NvidiaGPUDevices{}).Fit(devices, req, pod, &device.NodeInfo{}, nil)
					require.Equal(t, tc.want, ok, reason)
					if ok {
						require.Len(t, result[NvidiaGPUDevice], 2)
						charge := tc.cores
						if charge == 0 {
							charge = 100
						}
						for _, selected := range result[NvidiaGPUDevice] {
							require.EqualValues(t, 12288, selected.Usedmem)
							require.Equal(t, charge, selected.Usedcores)
						}
					}
				})
			}
		}
	}
}

func TestMPSV3TopologyRejectsHigherScoringPartialOverlap(t *testing.T) {
	for _, mode := range []string{MPSV3NamespaceCap, MPSV3ServerTimeslice} {
		for _, numa := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/numa=%t", mode, numa), func(t *testing.T) {
				devices := mpsV3TestGPUs(mode, "GPU-a", "GPU-b", "GPU-c", "GPU-d")
				for i, d := range devices {
					set := []string{"GPU-a", "GPU-b"}
					if i >= 2 {
						set = []string{"GPU-c", "GPU-d"}
					}
					d.CustomInfo[MPSV3GPUSetKey] = set
				}
				node := &device.NodeInfo{Devices: map[string][]device.DeviceInfo{NvidiaGPUDevice: {
					{ID: "GPU-a", DevicePairScore: device.DevicePairScore{Scores: map[string]int{"GPU-b": 10, "GPU-c": 100}}},
					{ID: "GPU-b", DevicePairScore: device.DevicePairScore{Scores: map[string]int{"GPU-a": 10}}},
					{ID: "GPU-c", DevicePairScore: device.DevicePairScore{Scores: map[string]int{"GPU-a": 100, "GPU-d": 50}}},
					{ID: "GPU-d", DevicePairScore: device.DevicePairScore{Scores: map[string]int{"GPU-c": 50}}},
				}}}
				pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
					MPSV3ComputeModeAnnotation:           mode,
					util.GPUSchedulerPolicyAnnotationKey: util.GPUSchedulerPolicyTopology.String(),
				}}}
				if numa {
					pod.Annotations[NumaBind] = "true"
				}
				req := device.ContainerDeviceRequest{Nums: 2, Type: NvidiaGPUDevice, Memreq: 2048, Coresreq: 25}
				ok, result, reason := (&NvidiaGPUDevices{}).Fit(devices, req, pod, node, nil)
				require.True(t, ok, reason)
				var ids []string
				for _, selected := range result[NvidiaGPUDevice] {
					ids = append(ids, selected.UUID)
				}
				require.ElementsMatch(t, []string{"GPU-c", "GPU-d"}, ids)
			})
		}
	}
}

func TestMPSV3MultiGPUStaticSMs(t *testing.T) {
	for _, topology := range []bool{false, true} {
		for _, tc := range []struct {
			name string
			sms  int64
			want bool
		}{
			{name: "equal geometry", sms: 72, want: true},
			{name: "different compute charges", sms: 80},
			{name: "one card too small", sms: 8},
		} {
			t.Run(fmt.Sprintf("topology=%t/%s", topology, tc.name), func(t *testing.T) {
				devices := mpsV3TestGPUs(MPSV3NamespaceCap, "GPU-a", "GPU-b")
				devices[1].CustomInfo[MPSV3GeometryKey] = MPSV3Geometry{SMs: tc.sms, Chunk: 2}
				pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{MPSV3SMCountAnnotation: "16"}}}
				if topology {
					pod.Annotations[util.GPUSchedulerPolicyAnnotationKey] = util.GPUSchedulerPolicyTopology.String()
				}
				req := device.ContainerDeviceRequest{Nums: 2, Type: NvidiaGPUDevice, Memreq: 2048}
				ok, result, reason := (&NvidiaGPUDevices{}).Fit(devices, req, pod, &device.NodeInfo{}, nil)
				require.Equal(t, tc.want, ok, reason)
				if ok {
					require.Len(t, result[NvidiaGPUDevice], 2)
					for _, selected := range result[NvidiaGPUDevice] {
						require.EqualValues(t, 23, selected.Usedcores)
					}
				}
			})
		}
	}
}

func TestMPSV3MultiGPUContainerReservations(t *testing.T) {
	for _, source := range []string{"tentative", "cache", "annotation"} {
		for _, tc := range []struct {
			name string
			ids  []string
			nums int64
			want bool
		}{
			{name: "reuse", ids: []string{"GPU-a", "GPU-b"}, nums: 2, want: true},
			{name: "disjoint", ids: []string{"GPU-d", "GPU-e"}, nums: 2, want: true},
			{name: "partial overlap", ids: []string{"GPU-b", "GPU-d"}, nums: 2},
			{name: "subset", ids: []string{"GPU-a"}, nums: 1},
			{name: "superset", ids: []string{"GPU-a", "GPU-b", "GPU-d"}, nums: 3},
		} {
			t.Run(source+"/"+tc.name, func(t *testing.T) {
				devices := mpsV3TestGPUs(MPSV3NamespaceCap, tc.ids...)
				// Two prior containers must remain separate sets, including
				// when only the persisted annotation is available.
				raw := device.PodDevices{NvidiaGPUDevice: {
					{{UUID: "GPU-a", Type: NvidiaGPUDevice}, {UUID: "GPU-b", Type: NvidiaGPUDevice}},
					{{UUID: "GPU-c", Type: NvidiaGPUDevice}},
				}}
				var allocated *device.PodDevices
				if source == "tentative" {
					allocated = &raw
				} else {
					previous := &device.PodInfo{Pod: &corev1.Pod{}}
					if source == "cache" {
						previous.RawDevices = raw
					} else {
						previous.Annotations = device.EncodePodDevices(map[string]string{NvidiaGPUDevice: AllocatedDevicesAnnotation}, raw)
					}
					for _, d := range devices {
						d.PodInfos = []*device.PodInfo{previous}
					}
				}
				req := device.ContainerDeviceRequest{Nums: tc.nums, Type: NvidiaGPUDevice, Memreq: 2048, Coresreq: 25}
				ok, result, reason := (&NvidiaGPUDevices{}).Fit(devices, req, &corev1.Pod{}, &device.NodeInfo{}, allocated)
				require.Equal(t, tc.want, ok, reason)
				if ok {
					require.Len(t, result[NvidiaGPUDevice], int(tc.nums))
				}
			})
		}
	}
}

func TestMPSV3SchedulingRequiresNodeStrategy(t *testing.T) {
	for _, tc := range []struct {
		name string
		info map[string]any
	}{
		{name: "no metadata"},
		{name: "missing strategy", info: map[string]any{}},
		{name: "null strategy", info: map[string]any{MPSV3ComputeModeKey: nil}},
		{name: "empty strategy", info: map[string]any{MPSV3ComputeModeKey: ""}},
		{name: "invalid type", info: map[string]any{MPSV3ComputeModeKey: 1}},
		{name: "unknown strategy", info: map[string]any{MPSV3ComputeModeKey: "unknown"}},
		{name: "namespace cap", info: map[string]any{MPSV3ComputeModeKey: MPSV3NamespaceCap}},
		{name: "server timeslice", info: map[string]any{MPSV3ComputeModeKey: MPSV3ServerTimeslice}},
	} {
		for _, requested := range []string{"", MPSV3NamespaceCap, MPSV3ServerTimeslice} {
			t.Run(tc.name+"/pod="+requested, func(t *testing.T) {
				pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}
				if requested != "" {
					pod.Annotations[MPSV3ComputeModeAnnotation] = requested
				}
				strategy := requested
				if strategy == "" {
					strategy = MPSV3NamespaceCap
				}
				nodeStrategy, valid := tc.info[MPSV3ComputeModeKey].(string)
				want := valid && nodeStrategy == strategy
				devices := mpsV3TestGPUs(MPSV3NamespaceCap, "GPU-a", "GPU-b")
				for _, d := range devices {
					d.CustomInfo = tc.info
				}
				_, fits := mpsV3Fits(pod, devices[0], 2, nil, 25, nil)
				require.Equal(t, want, fits)
				req := device.ContainerDeviceRequest{Nums: 2, Type: NvidiaGPUDevice, Memreq: 2048, Coresreq: 25}
				fits, _, reason := (&NvidiaGPUDevices{}).Fit(devices, req, pod, &device.NodeInfo{}, nil)
				require.Equal(t, want, fits, reason)
			})
		}
	}
}
