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

package plugin

import (
	"fmt"
	"testing"
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	nvmlmock "github.com/NVIDIA/go-nvml/pkg/nvml/mock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/rm"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

func TestInt8SliceString(t *testing.T) {
	tests := []struct {
		name     string
		input    int8Slice
		expected string
	}{
		{
			name:     "null terminated string",
			input:    int8Slice{'h', 'e', 'l', 'l', 'o', 0, 'x', 'y'},
			expected: "hello",
		},
		{
			name:     "no null terminator",
			input:    int8Slice{'a', 'b', 'c'},
			expected: "abc",
		},
		{
			name:     "empty slice",
			input:    int8Slice{},
			expected: "",
		},
		{
			name:     "only null byte",
			input:    int8Slice{0},
			expected: "",
		},
		{
			name:     "null at start",
			input:    int8Slice{0, 'a', 'b'},
			expected: "",
		},
		{
			name:     "PCI bus ID format",
			input:    int8Slice{'0', '0', '0', '0', ':', '3', 'b', ':', '0', '0', '.', '0', 0, 0, 0, 0},
			expected: "0000:3b:00.0",
		},
		{
			name:     "single character",
			input:    int8Slice{'Z', 0},
			expected: "Z",
		},
		{
			name:     "nil slice",
			input:    nil,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.input.String()
			if got != tt.expected {
				t.Errorf("int8Slice.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestGetNumaNode(t *testing.T) {
	t.Run("error getting PCI info", func(t *testing.T) {
		dev := &nvmlmock.Device{GetPciInfoFunc: func() (nvml.PciInfo, nvml.Return) {
			return nvml.PciInfo{}, nvml.ERROR_UNKNOWN
		}}
		hasNode, node, err := GetNumaNode(dev)
		if err == nil || hasNode || node != 0 {
			t.Fatalf("GetNumaNode() = (%v, %v, %v), want (false, 0, error)", hasNode, node, err)
		}
	})

	t.Run("numa node file is absent", func(t *testing.T) {
		dev := &nvmlmock.Device{GetPciInfoFunc: func() (nvml.PciInfo, nvml.Return) {
			return nvml.PciInfo{BusId: [32]int8{'0', '0', '0', '0', 'D', 'E', 'A', 'D', ':', 'B', 'E', ':', 'E', 'F', '.', '0'}}, nvml.SUCCESS
		}}
		hasNode, node, err := GetNumaNode(dev)
		if err == nil || hasNode || node != 0 {
			t.Fatalf("GetNumaNode() = (%v, %v, %v), want (false, 0, error)", hasNode, node, err)
		}
	})
}

func TestGetAPIDevicesPanicsOnNVMLInitFailure(t *testing.T) {
	originalInit := nvmlInit
	nvmlInit = func() nvml.Return { return nvml.ERROR_LIBRARY_NOT_FOUND }
	defer func() { nvmlInit = originalInit }()

	plugin := &NvidiaDevicePlugin{rm: &rm.ResourceManagerMock{DevicesFunc: func() rm.Devices { return rm.Devices{} }}}
	defer func() {
		if recover() == nil {
			t.Fatal("getAPIDevices did not panic when NVML initialization failed")
		}
	}()
	plugin.getAPIDevices()
}

func TestGetAPIDevicesShutsDownAfterNVMLInit(t *testing.T) {
	originalInit := nvmlInit
	originalShutdown := nvml.Shutdown
	nvmlInit = func() nvml.Return { return nvml.SUCCESS }
	nvml.Shutdown = func() nvml.Return { return nvml.SUCCESS }
	defer func() {
		nvmlInit = originalInit
		nvml.Shutdown = originalShutdown
	}()

	plugin := &NvidiaDevicePlugin{rm: &rm.ResourceManagerMock{DevicesFunc: func() rm.Devices { return rm.Devices{} }}}
	devices := plugin.getAPIDevices()
	if devices == nil || len(*devices) != 0 {
		t.Fatalf("getAPIDevices() = %v, want non-nil empty slice", devices)
	}
}

func TestWatchAndRegisterDisableSignal(t *testing.T) {
	disableCh := make(chan bool, 1)
	ackCh := make(chan bool, 1)

	// Send disable signal before starting
	disableCh <- true

	// Create a minimal plugin - WatchAndRegister will read the disable signal
	// and send an ack, then sleep. We verify the ack arrives.
	plugin := &NvidiaDevicePlugin{}

	done := make(chan struct{})
	go func() {
		plugin.WatchAndRegister(disableCh, ackCh)
	}()

	go func() {
		// Wait for the ack that confirms WatchAndRegister entered disabled state
		ack := <-ackCh
		if !ack {
			t.Error("expected ack to be true")
		}
		close(done)
	}()

	// Use a select with timeout to avoid hanging forever
	select {
	case <-done:
		// Success: received the ack
	case <-timeAfter(3 * time.Second):
		t.Fatal("timed out waiting for disable ack from WatchAndRegister")
	}
}

// TestRegisterInAnnotationRetriesAfterPatchFailure verifies that a failed
// node-annotation patch does not poison deviceCache: the next call must
// still see the device info as "changed" and retry the patch, instead of
// silently early-returning forever because deviceCache already matches.
func TestRegisterInAnnotationRetriesAfterPatchFailure(t *testing.T) {
	originalInit := nvmlInit
	originalShutdown := nvml.Shutdown
	nvmlInit = func() nvml.Return { return nvml.SUCCESS }
	nvml.Shutdown = func() nvml.Return { return nvml.SUCCESS }
	defer func() {
		nvmlInit = originalInit
		nvml.Shutdown = originalShutdown
	}()

	previousKubeClient := client.KubeClient
	previousNodeName := util.NodeName
	util.NodeName = "test-node"
	defer func() {
		client.KubeClient = previousKubeClient
		util.NodeName = previousNodeName
	}()

	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "test-node"}}
	fakeClient := fake.NewSimpleClientset(node)
	fakeClient.PrependReactor("patch", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("simulated patch failure")
	})
	client.KubeClient = fakeClient

	plugin := &NvidiaDevicePlugin{rm: &rm.ResourceManagerMock{DevicesFunc: func() rm.Devices { return rm.Devices{} }}}

	changed, err := plugin.RegisterInAnnotation()
	if err == nil {
		t.Fatal("RegisterInAnnotation() error = nil, want error from failed patch")
	}
	if !changed {
		t.Fatal("RegisterInAnnotation() changed = false, want true (a patch was attempted)")
	}
	if plugin.deviceCache != "" {
		t.Fatalf("deviceCache = %q after a failed patch, want unchanged (empty)", plugin.deviceCache)
	}

	// A second call with the same device set must retry the patch (and
	// surface the same error) rather than treating deviceCache as already
	// up to date.
	changed, err = plugin.RegisterInAnnotation()
	if err == nil {
		t.Fatal("second RegisterInAnnotation() error = nil, want the retry to also surface the patch failure")
	}
	if !changed {
		t.Fatal("second RegisterInAnnotation() changed = false, want true (patch should be retried, not skipped)")
	}
}

// TestRegisterInAnnotationDoesNotPoisonCacheOnScoreError verifies that a
// failure computing the topology score (ENABLE_TOPOLOGY_SCORE=true) does not
// poison deviceCache either: the cache is only written after every step,
// including score calculation, completes successfully - matching the
// patch-failure behavior verified above.
func TestRegisterInAnnotationDoesNotPoisonCacheOnScoreError(t *testing.T) {
	originalInit := nvmlInit
	originalShutdown := nvml.Shutdown
	nvmlInit = func() nvml.Return { return nvml.SUCCESS }
	nvml.Shutdown = func() nvml.Return { return nvml.SUCCESS }
	defer func() {
		nvmlInit = originalInit
		nvml.Shutdown = originalShutdown
	}()

	originalCalculateGPUScore := calculateGPUScore
	calculateGPUScore = func([]string) (nvidia.ListDeviceScore, bool, error) {
		return nil, false, fmt.Errorf("simulated topology score failure")
	}
	defer func() { calculateGPUScore = originalCalculateGPUScore }()

	t.Setenv("ENABLE_TOPOLOGY_SCORE", "true")

	previousKubeClient := client.KubeClient
	previousNodeName := util.NodeName
	util.NodeName = "test-node"
	defer func() {
		client.KubeClient = previousKubeClient
		util.NodeName = previousNodeName
	}()

	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "test-node"}}
	client.KubeClient = fake.NewSimpleClientset(node)

	plugin := &NvidiaDevicePlugin{rm: &rm.ResourceManagerMock{DevicesFunc: func() rm.Devices { return rm.Devices{} }}}

	changed, err := plugin.RegisterInAnnotation()
	if err == nil {
		t.Fatal("RegisterInAnnotation() error = nil, want error from failed score calculation")
	}
	if changed {
		t.Fatal("RegisterInAnnotation() changed = true, want false (no patch was attempted)")
	}
	if plugin.deviceCache != "" {
		t.Fatalf("deviceCache = %q after a failed score calculation, want unchanged (empty)", plugin.deviceCache)
	}
}

// timeAfter returns a channel that closes after the given duration.
func timeAfter(d time.Duration) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		time.Sleep(d)
		close(ch)
	}()
	return ch
}
