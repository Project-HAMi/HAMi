package ascend

import (
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/device"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		{ID: "dev-0", Index: 0, Count: 1, Used: 0, Totalmem: 32000, CustomInfo: map[string]any{"NetworkID": float64(0)}},
		{ID: "dev-1", Index: 1, Count: 1, Used: 0, Totalmem: 32000, CustomInfo: map[string]any{"NetworkID": float64(0)}},
		{ID: "dev-2", Index: 2, Count: 1, Used: 0, Totalmem: 32000, CustomInfo: map[string]any{"NetworkID": float64(0)}},
		{ID: "dev-4", Index: 4, Count: 1, Used: 0, Totalmem: 32000, CustomInfo: map[string]any{"NetworkID": float64(0)}},
		{ID: "dev-6", Index: 6, Count: 1, Used: 0, Totalmem: 32000, CustomInfo: map[string]any{"NetworkID": float64(0)}},
	}
	req := device.ContainerDeviceRequest{Nums: 4, Type: Ascend910CType}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test-pod"}}

	fit, tmpDevs, reason := dev.Fit(devices, req, pod, nodeInfo, nil)

	if fit {
		t.Errorf("expected fit=false when only 2 of 4 requested NPUs form full module pairs, got fit=true, allocated=%d, reason=%q",
			len(tmpDevs[Ascend910CType]), reason)
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
		{ID: "dev-0", Index: 0, Count: 1, Used: 0, Totalmem: 32000, CustomInfo: map[string]any{"NetworkID": float64(0)}},
		{ID: "dev-2", Index: 2, Count: 1, Used: 0, Totalmem: 32000, CustomInfo: map[string]any{"NetworkID": float64(0)}},
	}
	req := device.ContainerDeviceRequest{Nums: 2, Type: Ascend910CType}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test-pod-2"}}

	fit, tmpDevs, reason := dev.Fit(devices, req, pod, nodeInfo, nil)

	if fit {
		t.Errorf("expected fit=false when the 2 candidate NPUs come from two different partial modules (not one full pair), got fit=true, allocated=%d, reason=%q",
			len(tmpDevs[Ascend910CType]), reason)
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
		{ID: "dev-0", Index: 0, Count: 1, Used: 0, Totalmem: 32000, CustomInfo: map[string]any{"NetworkID": float64(0)}},
		{ID: "dev-1", Index: 1, Count: 1, Used: 0, Totalmem: 32000, CustomInfo: map[string]any{"NetworkID": float64(0)}},
	}
	req := device.ContainerDeviceRequest{Nums: 2, Type: Ascend910CType}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test-pod-3"}}

	fit, tmpDevs, reason := dev.Fit(devices, req, pod, nodeInfo, nil)

	if !fit || len(tmpDevs[Ascend910CType]) != 2 {
		t.Errorf("expected fit=true with 2 allocated devices for one full module pair, got fit=%v, allocated=%d, reason=%q",
			fit, len(tmpDevs[Ascend910CType]), reason)
	}
}
