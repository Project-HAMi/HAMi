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
	"testing"
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	nvmlmock "github.com/NVIDIA/go-nvml/pkg/nvml/mock"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/rm"
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

func TestGetAPIDevicesErrorsOnNVMLInitFailure(t *testing.T) {
	originalInit := nvmlInit
	nvmlInit = func() nvml.Return { return nvml.ERROR_LIBRARY_NOT_FOUND }
	defer func() { nvmlInit = originalInit }()

	plugin := &NvidiaDevicePlugin{rm: &rm.ResourceManagerMock{DevicesFunc: func() rm.Devices { return rm.Devices{} }}}
	devices, err := plugin.getAPIDevices()
	if err == nil {
		t.Fatal("getAPIDevices did not return an error when NVML initialization failed")
	}
	if devices != nil {
		t.Fatalf("getAPIDevices() = %v on NVML init failure, want nil", devices)
	}
}

func TestRegisterInAnnotationPropagatesNVMLInitError(t *testing.T) {
	originalInit := nvmlInit
	nvmlInit = func() nvml.Return { return nvml.ERROR_LIBRARY_NOT_FOUND }
	defer func() { nvmlInit = originalInit }()

	plugin := &NvidiaDevicePlugin{rm: &rm.ResourceManagerMock{DevicesFunc: func() rm.Devices { return rm.Devices{} }}}
	changed, err := plugin.RegisterInAnnotation()
	if err == nil {
		t.Fatal("RegisterInAnnotation did not propagate the NVML init error")
	}
	if changed {
		t.Fatal("RegisterInAnnotation() changed = true on NVML init failure, want false")
	}
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
	devices, err := plugin.getAPIDevices()
	if err != nil {
		t.Fatalf("getAPIDevices() returned unexpected error: %v", err)
	}
	if devices == nil || len(*devices) != 0 {
		t.Fatalf("getAPIDevices() = %v, want non-nil empty slice", devices)
	}
}

func TestGetAPIDevicesErrorsOnPerDeviceNVMLFailures(t *testing.T) {
	originalInit := nvmlInit
	originalShutdown := nvml.Shutdown
	originalGetHandleByUUID := nvml.DeviceGetHandleByUUID
	nvmlInit = func() nvml.Return { return nvml.SUCCESS }
	nvml.Shutdown = func() nvml.Return { return nvml.SUCCESS }
	defer func() {
		nvmlInit = originalInit
		nvml.Shutdown = originalShutdown
		nvml.DeviceGetHandleByUUID = originalGetHandleByUUID
	}()

	const testUUID = "GPU-test-uuid"
	tests := []struct {
		name            string
		getHandleReturn nvml.Return
		device          *nvmlmock.Device
	}{
		{
			name:            "DeviceGetHandleByUUID fails",
			getHandleReturn: nvml.ERROR_UNINITIALIZED,
			device:          &nvmlmock.Device{},
		},
		{
			name:            "GetIndex fails",
			getHandleReturn: nvml.SUCCESS,
			device: &nvmlmock.Device{
				GetIndexFunc: func() (int, nvml.Return) {
					return 0, nvml.ERROR_UNKNOWN
				},
			},
		},
		{
			name:            "GetMemoryInfo fails",
			getHandleReturn: nvml.SUCCESS,
			device: &nvmlmock.Device{
				GetIndexFunc: func() (int, nvml.Return) {
					return 0, nvml.SUCCESS
				},
				GetMemoryInfoFunc: func() (nvml.Memory, nvml.Return) {
					return nvml.Memory{}, nvml.ERROR_UNKNOWN
				},
			},
		},
		{
			name:            "GetName fails",
			getHandleReturn: nvml.SUCCESS,
			device: &nvmlmock.Device{
				GetIndexFunc: func() (int, nvml.Return) {
					return 0, nvml.SUCCESS
				},
				GetMemoryInfoFunc: func() (nvml.Memory, nvml.Return) {
					return nvml.Memory{Total: 8 * 1024 * 1024 * 1024}, nvml.SUCCESS
				},
				GetNameFunc: func() (string, nvml.Return) {
					return "", nvml.ERROR_UNKNOWN
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nvml.DeviceGetHandleByUUID = func(uuid string) (nvml.Device, nvml.Return) {
				return tt.device, tt.getHandleReturn
			}
			plugin := &NvidiaDevicePlugin{rm: &rm.ResourceManagerMock{DevicesFunc: func() rm.Devices {
				return rm.Devices{testUUID: &rm.Device{}}
			}}}
			devices, err := plugin.getAPIDevices()
			if err == nil {
				t.Fatal("getAPIDevices did not return an error when a per-device NVML query failed")
			}
			if devices != nil {
				t.Fatalf("getAPIDevices() = %v on per-device NVML failure, want nil", devices)
			}
		})
	}
}

func TestGetAPIDevicesRegistersHealthyDevice(t *testing.T) {
	originalInit := nvmlInit
	originalShutdown := nvml.Shutdown
	originalGetHandleByUUID := nvml.DeviceGetHandleByUUID
	nvmlInit = func() nvml.Return { return nvml.SUCCESS }
	nvml.Shutdown = func() nvml.Return { return nvml.SUCCESS }
	nvml.DeviceGetHandleByUUID = func(uuid string) (nvml.Device, nvml.Return) {
		return &nvmlmock.Device{
			GetIndexFunc: func() (int, nvml.Return) {
				return 3, nvml.SUCCESS
			},
			GetMemoryInfoFunc: func() (nvml.Memory, nvml.Return) {
				return nvml.Memory{Total: 8 * 1024 * 1024 * 1024}, nvml.SUCCESS
			},
			GetNameFunc: func() (string, nvml.Return) {
				return "Tesla T4", nvml.SUCCESS
			},
			GetPciInfoFunc: func() (nvml.PciInfo, nvml.Return) {
				return nvml.PciInfo{}, nvml.ERROR_UNKNOWN
			},
		}, nvml.SUCCESS
	}
	defer func() {
		nvmlInit = originalInit
		nvml.Shutdown = originalShutdown
		nvml.DeviceGetHandleByUUID = originalGetHandleByUUID
	}()

	const testUUID = "GPU-test-uuid"
	plugin := &NvidiaDevicePlugin{
		rm: &rm.ResourceManagerMock{DevicesFunc: func() rm.Devices {
			return rm.Devices{testUUID: &rm.Device{Device: kubeletdevicepluginv1beta1.Device{ID: testUUID, Health: "healthy"}}}
		}},
		schedulerConfig: nvidia.NvidiaConfig{
			NodeDefaultConfig: nvidia.NodeDefaultConfig{
				DeviceSplitCount:    ptr[uint](2),
				DeviceMemoryScaling: ptr[float64](1),
				DeviceCoreScaling:   ptr[float64](1),
			},
		},
	}
	devices, err := plugin.getAPIDevices()
	if err != nil {
		t.Fatalf("getAPIDevices() returned unexpected error: %v", err)
	}
	if devices == nil || len(*devices) != 1 {
		t.Fatalf("getAPIDevices() = %v, want exactly one device", devices)
	}
	got := (*devices)[0]
	wantDevmem := int32(8 * 1024)
	if got.ID != testUUID || got.Index != 3 || got.Count != 2 ||
		got.Devmem != wantDevmem || got.Devcore != 100 ||
		got.Type != "NVIDIA-Tesla T4" || !got.Health {
		t.Fatalf("getAPIDevices()[0] = %+v, mismatched device info", got)
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
