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

package ascend

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
)

// TestAscend910C_FitPartialAllocationBug reproduces the case where a pod
// requests 4 NPUs but the node only has 1 full module (2 NPUs) plus 3
// partial modules (1 NPU each). Before the fix, Fit() would incorrectly
// report success while allocating only 2 devices.
func TestAscend910C_FitPartialAllocationBug(t *testing.T) {
	dev := &Devices{config: VNPUConfig{CommonWord: Ascend910CType}}
	nodeInfo := &device.NodeInfo{
		Node: &corev1.Node{},
		Devices: map[string][]device.DeviceInfo{
			Ascend910CType: {
				{ID: "dev-0", Index: 0, CustomInfo: map[string]any{"NetworkID": float64(0)}},
				{ID: "dev-1", Index: 1, CustomInfo: map[string]any{"NetworkID": float64(0)}},
				{ID: "dev-2", Index: 2, CustomInfo: map[string]any{"NetworkID": float64(0)}},
				{ID: "dev-4", Index: 4, CustomInfo: map[string]any{"NetworkID": float64(0)}},
				{ID: "dev-6", Index: 6, CustomInfo: map[string]any{"NetworkID": float64(0)}},
			},
		},
	}
	devices := []*device.DeviceUsage{
		{ID: "dev-0", Index: 0, Count: 1, Used: 0, Totalmem: 32000, Health: true, CustomInfo: map[string]any{"NetworkID": float64(0)}},
		{ID: "dev-1", Index: 1, Count: 1, Used: 0, Totalmem: 32000, Health: true, CustomInfo: map[string]any{"NetworkID": float64(0)}},
		{ID: "dev-2", Index: 2, Count: 1, Used: 0, Totalmem: 32000, Health: true, CustomInfo: map[string]any{"NetworkID": float64(0)}},
		{ID: "dev-4", Index: 4, Count: 1, Used: 0, Totalmem: 32000, Health: true, CustomInfo: map[string]any{"NetworkID": float64(0)}},
		{ID: "dev-6", Index: 6, Count: 1, Used: 0, Totalmem: 32000, Health: true, CustomInfo: map[string]any{"NetworkID": float64(0)}},
	}
	req := device.ContainerDeviceRequest{Nums: 4, Type: Ascend910CType}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test-pod"}}

	fit, tmpDevs, reason := dev.Fit(devices, req, pod, nodeInfo, nil)

	if fit {
		t.Errorf("expected fit=false when only 2 of 4 requested NPUs form full module pairs, got fit=true, allocated=%d, reason=%q",
			len(tmpDevs[Ascend910CType]), reason)
	}
	if !strings.Contains(reason, common.AllocatedCardsInsufficientRequest) {
		t.Errorf("expected reason to contain %q, got %q", common.AllocatedCardsInsufficientRequest, reason)
	}
}

// TestAscend910C_FitExactCountBypassBug reproduces the L568-571 bypass: a
// pod requests 2 NPUs, and exactly 2 candidate NPUs exist on the node, but
// they sit on two different partial modules rather than one full pair.
// Before the fix, Fit() returned true without ever validating module
// pairing, because the candidate count already matched originReq.
func TestAscend910C_FitExactCountBypassBug(t *testing.T) {
	dev := &Devices{config: VNPUConfig{CommonWord: Ascend910CType}}
	nodeInfo := &device.NodeInfo{
		Node: &corev1.Node{},
		Devices: map[string][]device.DeviceInfo{
			Ascend910CType: {
				// dev-0 (module 0) and dev-2 (module 1) are each the sole
				// occupied NPU of their respective module - no full pair.
				{ID: "dev-0", Index: 0, CustomInfo: map[string]any{"NetworkID": float64(0)}},
				{ID: "dev-2", Index: 2, CustomInfo: map[string]any{"NetworkID": float64(0)}},
			},
		},
	}
	devices := []*device.DeviceUsage{
		{ID: "dev-0", Index: 0, Count: 1, Used: 0, Totalmem: 32000, Health: true, CustomInfo: map[string]any{"NetworkID": float64(0)}},
		{ID: "dev-2", Index: 2, Count: 1, Used: 0, Totalmem: 32000, Health: true, CustomInfo: map[string]any{"NetworkID": float64(0)}},
	}
	req := device.ContainerDeviceRequest{Nums: 2, Type: Ascend910CType}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test-pod-2"}}

	fit, tmpDevs, reason := dev.Fit(devices, req, pod, nodeInfo, nil)

	if fit {
		t.Errorf("expected fit=false when the 2 candidate NPUs come from two different partial modules (not one full pair), got fit=true, allocated=%d, reason=%q",
			len(tmpDevs[Ascend910CType]), reason)
	}
	if !strings.Contains(reason, common.AllocatedCardsInsufficientRequest) {
		t.Errorf("expected reason to contain %q, got %q", common.AllocatedCardsInsufficientRequest, reason)
	}
}

// TestAscend910C_FitFullPairSucceeds is the positive-path sanity check:
// a request for 2 NPUs against one genuinely full module (2 NPUs, same
// card) should still succeed after the fix.
func TestAscend910C_FitFullPairSucceeds(t *testing.T) {
	dev := &Devices{config: VNPUConfig{CommonWord: Ascend910CType}}
	nodeInfo := &device.NodeInfo{
		Node: &corev1.Node{},
		Devices: map[string][]device.DeviceInfo{
			Ascend910CType: {
				// Index 0 and 1 belong to the same module (idx/2 == 0 for both).
				{ID: "dev-0", Index: 0, CustomInfo: map[string]any{"NetworkID": float64(0)}},
				{ID: "dev-1", Index: 1, CustomInfo: map[string]any{"NetworkID": float64(0)}},
			},
		},
	}
	devices := []*device.DeviceUsage{
		{ID: "dev-0", Index: 0, Count: 1, Used: 0, Totalmem: 32000, Health: true, CustomInfo: map[string]any{"NetworkID": float64(0)}},
		{ID: "dev-1", Index: 1, Count: 1, Used: 0, Totalmem: 32000, Health: true, CustomInfo: map[string]any{"NetworkID": float64(0)}},
	}
	req := device.ContainerDeviceRequest{Nums: 2, Type: Ascend910CType}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test-pod-3"}}

	fit, tmpDevs, reason := dev.Fit(devices, req, pod, nodeInfo, nil)

	if !fit || len(tmpDevs[Ascend910CType]) != 2 {
		t.Errorf("expected fit=true with 2 allocated devices for one full module pair, got fit=%v, allocated=%d, reason=%q",
			fit, len(tmpDevs[Ascend910CType]), reason)
	}
}

// TestAscend910C_FitWithoutNetworkID_ValidatesPairing ensures that Ascend 910C
// full-pair validation runs for multi-device requests even when CustomInfo["NetworkID"]
// is absent on devices (needTopology=false).
func TestAscend910C_FitWithoutNetworkID_ValidatesPairing(t *testing.T) {
	dev := &Devices{config: VNPUConfig{CommonWord: Ascend910CType}}
	nodeInfo := &device.NodeInfo{
		Node: &corev1.Node{},
		Devices: map[string][]device.DeviceInfo{
			Ascend910CType: {
				// dev-0 (module 0) and dev-2 (module 1) without NetworkID in CustomInfo.
				{ID: "dev-0", Index: 0, CustomInfo: map[string]any{}},
				{ID: "dev-2", Index: 2, CustomInfo: map[string]any{}},
			},
		},
	}
	devices := []*device.DeviceUsage{
		{ID: "dev-0", Index: 0, Count: 1, Used: 0, Totalmem: 32000, Health: true, CustomInfo: map[string]any{}},
		{ID: "dev-2", Index: 2, Count: 1, Used: 0, Totalmem: 32000, Health: true, CustomInfo: map[string]any{}},
	}
	req := device.ContainerDeviceRequest{Nums: 2, Type: Ascend910CType}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test-pod-no-netid"}}

	fit, tmpDevs, reason := dev.Fit(devices, req, pod, nodeInfo, nil)

	if fit {
		t.Errorf("expected fit=false when NetworkID is absent and the 2 candidate NPUs belong to different modules (indices 0 and 2), got fit=true, allocated=%d, reason=%q",
			len(tmpDevs[Ascend910CType]), reason)
	}

	// Positive check without NetworkID when candidate devices form a full module pair (indices 0 and 1).
	nodeInfo.Devices[Ascend910CType] = []device.DeviceInfo{
		{ID: "dev-0", Index: 0, CustomInfo: map[string]any{}},
		{ID: "dev-1", Index: 1, CustomInfo: map[string]any{}},
	}
	devicesPair := []*device.DeviceUsage{
		{ID: "dev-0", Index: 0, Count: 1, Used: 0, Totalmem: 32000, Health: true, CustomInfo: map[string]any{}},
		{ID: "dev-1", Index: 1, Count: 1, Used: 0, Totalmem: 32000, Health: true, CustomInfo: map[string]any{}},
	}
	fitPair, tmpDevsPair, reasonPair := dev.Fit(devicesPair, req, pod, nodeInfo, nil)
	if !fitPair || len(tmpDevsPair[Ascend910CType]) != 2 {
		t.Errorf("expected fit=true when NetworkID is absent but candidate devices form a full module pair (indices 0 and 1), got fit=%v, allocated=%d, reason=%q",
			fitPair, len(tmpDevsPair[Ascend910CType]), reasonPair)
	}
}

// TestComputeBestCombination910C_NoFullPairsReturnsEmpty directly answers
// whether computeBestCombination910C returns an empty combination (rather
// than panicking or fabricating a partial/incorrect pairing) when none of
// the candidate NPUs share a full physical module and no NetworkID is set
// anywhere in CustomInfo. It also verifies that Fit() turns that empty
// combination into a clean fit=false rejection, so such pods are correctly
// left unschedulable rather than crashing the scheduler or being silently
// under-allocated.
func TestComputeBestCombination910C_NoFullPairsReturnsEmpty(t *testing.T) {
	dev := &Devices{config: VNPUConfig{CommonWord: Ascend910CType}}
	nodeInfo := &device.NodeInfo{
		Node: &corev1.Node{},
		Devices: map[string][]device.DeviceInfo{
			Ascend910CType: {
				// Four singleton NPUs, each alone on its own module
				// (indices 0, 2, 4, 6 -> module IDs 0, 1, 2, 3), no
				// NetworkID present anywhere in CustomInfo.
				{ID: "dev-0", Index: 0, CustomInfo: map[string]any{}},
				{ID: "dev-2", Index: 2, CustomInfo: map[string]any{}},
				{ID: "dev-4", Index: 4, CustomInfo: map[string]any{}},
				{ID: "dev-6", Index: 6, CustomInfo: map[string]any{}},
			},
		},
	}

	// Unit-level check: computeBestCombination910C itself must return an
	// empty slice here, not panic and not fabricate a mismatched pairing.
	candidates := device.ContainerDevices{
		{Idx: 0, UUID: "dev-0"},
		{Idx: 2, UUID: "dev-2"},
		{Idx: 4, UUID: "dev-4"},
		{Idx: 6, UUID: "dev-6"},
	}
	combination := dev.computeBestCombination910C(nodeInfo, 4, candidates)
	if len(combination) != 0 {
		t.Errorf("expected computeBestCombination910C to return an empty combination when no candidates share a full module, got %d devices", len(combination))
	}

	// End-to-end check: Fit() must turn that empty combination into a clean
	// rejection, not a panic and not a false "success".
	devices := []*device.DeviceUsage{
		{ID: "dev-0", Index: 0, Count: 1, Used: 0, Totalmem: 32000, Health: true, CustomInfo: map[string]any{}},
		{ID: "dev-2", Index: 2, Count: 1, Used: 0, Totalmem: 32000, Health: true, CustomInfo: map[string]any{}},
		{ID: "dev-4", Index: 4, Count: 1, Used: 0, Totalmem: 32000, Health: true, CustomInfo: map[string]any{}},
		{ID: "dev-6", Index: 6, Count: 1, Used: 0, Totalmem: 32000, Health: true, CustomInfo: map[string]any{}},
	}
	req := device.ContainerDeviceRequest{Nums: 4, Type: Ascend910CType}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test-pod-no-netid-no-pairs"}}

	fit, tmpDevs, reason := dev.Fit(devices, req, pod, nodeInfo, nil)
	if fit {
		t.Errorf("expected fit=false when no candidates without NetworkID form full module pairs, got fit=true, allocated=%d, reason=%q",
			len(tmpDevs[Ascend910CType]), reason)
	}
}
