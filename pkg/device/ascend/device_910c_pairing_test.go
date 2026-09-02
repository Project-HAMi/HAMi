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

// TestAscend910C_FitPartialAllocationBug tests rejection when partial cards cannot fulfill the request.
func TestAscend910C_FitPartialAllocationBug(t *testing.T) {
	dev := &Devices{config: VNPUConfig{CommonWord: Ascend910CType, SuperPod: true}}
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

// TestAscend910C_FitExactCountBypassBug tests pairing validation when candidate count matches originReq.
func TestAscend910C_FitExactCountBypassBug(t *testing.T) {
	dev := &Devices{config: VNPUConfig{CommonWord: Ascend910CType, SuperPod: true}}
	nodeInfo := &device.NodeInfo{
		Node: &corev1.Node{},
		Devices: map[string][]device.DeviceInfo{
			Ascend910CType: {
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

// TestAscend910C_FitFullPairSucceeds tests successful allocation for full module pairs.
func TestAscend910C_FitFullPairSucceeds(t *testing.T) {
	dev := &Devices{config: VNPUConfig{CommonWord: Ascend910CType, SuperPod: true}}
	nodeInfo := &device.NodeInfo{
		Node: &corev1.Node{},
		Devices: map[string][]device.DeviceInfo{
			Ascend910CType: {
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

// TestAscend910C_FitWithoutNetworkID_ValidatesPairing tests pairing validation when NetworkID is missing.
func TestAscend910C_FitWithoutNetworkID_ValidatesPairing(t *testing.T) {
	dev := &Devices{config: VNPUConfig{CommonWord: Ascend910CType, SuperPod: true}}
	nodeInfo := &device.NodeInfo{
		Node: &corev1.Node{},
		Devices: map[string][]device.DeviceInfo{
			Ascend910CType: {
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

// TestComputeBestCombination910C_NoFullPairsReturnsEmpty tests empty combination when no candidate NPUs form a full module.
func TestComputeBestCombination910C_NoFullPairsReturnsEmpty(t *testing.T) {
	dev := &Devices{config: VNPUConfig{CommonWord: Ascend910CType, SuperPod: true}}
	nodeInfo := &device.NodeInfo{
		Node: &corev1.Node{},
		Devices: map[string][]device.DeviceInfo{
			Ascend910CType: {
				{ID: "dev-0", Index: 0, CustomInfo: map[string]any{}},
				{ID: "dev-2", Index: 2, CustomInfo: map[string]any{}},
				{ID: "dev-4", Index: 4, CustomInfo: map[string]any{}},
				{ID: "dev-6", Index: 6, CustomInfo: map[string]any{}},
			},
		},
	}

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
