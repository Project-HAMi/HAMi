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
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
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

func TestProcessMigConfigs(t *testing.T) {
	plugin := &NvidiaDevicePlugin{}

	tests := []struct {
		name           string
		migConfigs     map[string]nvidia.MigConfigSpecSlice
		deviceCount    int
		expectErr      bool
		expectedLen    int
		validateResult func(t *testing.T, result nvidia.MigConfigSpecSlice)
	}{
		{
			name:        "nil migConfigs returns error",
			migConfigs:  nil,
			deviceCount: 2,
			expectErr:   true,
		},
		{
			name:        "zero deviceCount returns error",
			migConfigs:  map[string]nvidia.MigConfigSpecSlice{"current": {}},
			deviceCount: 0,
			expectErr:   true,
		},
		{
			name:        "negative deviceCount returns error",
			migConfigs:  map[string]nvidia.MigConfigSpecSlice{"current": {}},
			deviceCount: -1,
			expectErr:   true,
		},
		{
			name: "single config with empty devices expands to all devices",
			migConfigs: map[string]nvidia.MigConfigSpecSlice{
				"current": {
					nvidia.MigConfigSpec{
						Devices:    []int32{},
						MigEnabled: true,
						MigDevices: map[string]int32{"1g.5gb": 7},
					},
				},
			},
			deviceCount: 3,
			expectErr:   false,
			expectedLen: 3,
			validateResult: func(t *testing.T, result nvidia.MigConfigSpecSlice) {
				for i, cfg := range result {
					if len(cfg.Devices) != 1 || cfg.Devices[0] != int32(i) {
						t.Errorf("config[%d].Devices = %v, want [%d]", i, cfg.Devices, i)
					}
					if !cfg.MigEnabled {
						t.Errorf("config[%d].MigEnabled = false, want true", i)
					}
					if cfg.MigDevices["1g.5gb"] != 7 {
						t.Errorf("config[%d].MigDevices[1g.5gb] = %d, want 7", i, cfg.MigDevices["1g.5gb"])
					}
				}
			},
		},
		{
			name: "multiple configs with explicit device mapping",
			migConfigs: map[string]nvidia.MigConfigSpecSlice{
				"current": {
					nvidia.MigConfigSpec{
						Devices:    []int32{0, 1},
						MigEnabled: true,
						MigDevices: map[string]int32{"1g.5gb": 7},
					},
					nvidia.MigConfigSpec{
						Devices:    []int32{2},
						MigEnabled: true,
						MigDevices: map[string]int32{"2g.10gb": 3},
					},
				},
			},
			deviceCount: 3,
			expectErr:   false,
			expectedLen: 3,
			validateResult: func(t *testing.T, result nvidia.MigConfigSpecSlice) {
				// Device 0 should get 1g.5gb config
				if result[0].MigDevices["1g.5gb"] != 7 {
					t.Errorf("device 0: MigDevices[1g.5gb] = %d, want 7", result[0].MigDevices["1g.5gb"])
				}
				if len(result[0].Devices) != 1 || result[0].Devices[0] != 0 {
					t.Errorf("device 0: Devices = %v, want [0]", result[0].Devices)
				}
				// Device 1 should get 1g.5gb config
				if result[1].MigDevices["1g.5gb"] != 7 {
					t.Errorf("device 1: MigDevices[1g.5gb] = %d, want 7", result[1].MigDevices["1g.5gb"])
				}
				// Device 2 should get 2g.10gb config
				if result[2].MigDevices["2g.10gb"] != 3 {
					t.Errorf("device 2: MigDevices[2g.10gb] = %d, want 3", result[2].MigDevices["2g.10gb"])
				}
			},
		},
		{
			name: "device not found in config returns error",
			migConfigs: map[string]nvidia.MigConfigSpecSlice{
				"current": {
					nvidia.MigConfigSpec{
						Devices:    []int32{0},
						MigEnabled: true,
						MigDevices: map[string]int32{"1g.5gb": 7},
					},
				},
			},
			deviceCount: 3,
			expectErr:   true,
		},
		{
			name: "single device single config",
			migConfigs: map[string]nvidia.MigConfigSpecSlice{
				"current": {
					nvidia.MigConfigSpec{
						Devices:    []int32{0},
						MigEnabled: false,
						MigDevices: map[string]int32{},
					},
				},
			},
			deviceCount: 1,
			expectErr:   false,
			expectedLen: 1,
			validateResult: func(t *testing.T, result nvidia.MigConfigSpecSlice) {
				if result[0].MigEnabled {
					t.Error("config[0].MigEnabled = true, want false")
				}
				if len(result[0].Devices) != 1 || result[0].Devices[0] != 0 {
					t.Errorf("config[0].Devices = %v, want [0]", result[0].Devices)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := plugin.processMigConfigs(tt.migConfigs, tt.deviceCount)
			if tt.expectErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(result) != tt.expectedLen {
				t.Fatalf("result length = %d, want %d", len(result), tt.expectedLen)
			}
			if tt.validateResult != nil {
				tt.validateResult(t, result)
			}
		})
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

// timeAfter returns a channel that closes after the given duration.
func timeAfter(d time.Duration) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		time.Sleep(d)
		close(ch)
	}()
	return ch
}

func TestRegisterInAnnotation_DeviceCacheOrder(t *testing.T) {
	previousGetAPIDevices := getAPIDevices
	defer func() { getAPIDevices = previousGetAPIDevices }()

	previousGetNode := getNode
	defer func() { getNode = previousGetNode }()

	previousPatchNodeAnnotations := patchNodeAnnotations
	defer func() { patchNodeAnnotations = previousPatchNodeAnnotations }()

	dummyDevices := []*device.DeviceInfo{
		{
			ID:     "GPU-12345",
			Index:  0,
			Count:  10,
			Devmem: 8192,
			Type:   "NVIDIA-Tesla T4",
		},
	}

	getAPIDevices = func(p *NvidiaDevicePlugin) *[]*device.DeviceInfo {
		return &dummyDevices
	}

	getNode = func(name string) (*corev1.Node, error) {
		return &corev1.Node{}, nil
	}

	plugin := &NvidiaDevicePlugin{
		deviceCache: "",
	}

	// 1. When patchNodeAnnotations fails, deviceCache should NOT be updated.
	patchErr := errors.New("transient API server error")
	patchNodeAnnotations = func(node *corev1.Node, annotations map[string]string) error {
		return patchErr
	}

	changed, err := plugin.RegisterInAnnotation()
	if !changed {
		t.Error("expected changed to be true on patch attempt")
	}
	if err != patchErr {
		t.Errorf("expected error %v, got %v", patchErr, err)
	}
	if plugin.deviceCache != "" {
		t.Errorf("expected deviceCache to remain empty on failure, got %q", plugin.deviceCache)
	}

	// 2. When patchNodeAnnotations succeeds, deviceCache SHOULD be updated.
	patchNodeAnnotations = func(node *corev1.Node, annotations map[string]string) error {
		return nil
	}

	changed, err = plugin.RegisterInAnnotation()
	if !changed {
		t.Error("expected changed to be true on successful patch")
	}
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	expectedCache := device.MarshalNodeDevices(dummyDevices)
	if plugin.deviceCache != expectedCache {
		t.Errorf("expected deviceCache to be %q, got %q", expectedCache, plugin.deviceCache)
	}

	// 3. Next call with unchanged devices returns (false, nil) due to matching cache.
	changed, err = plugin.RegisterInAnnotation()
	if changed {
		t.Error("expected changed to be false on second attempt with unchanged devices")
	}
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

