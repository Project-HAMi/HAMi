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

package main

import (
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/monitor/nvidia"
)

type stubInfo struct{ priority int }

func (s *stubInfo) DeviceMax() int                     { return 1 }
func (s *stubInfo) DeviceNum() int                     { return 0 }
func (s *stubInfo) DeviceUUID(int) string              { return "gpu-0" }
func (s *stubInfo) DeviceMemoryContextSize(int) uint64 { return 0 }
func (s *stubInfo) DeviceMemoryModuleSize(int) uint64  { return 0 }
func (s *stubInfo) DeviceMemoryBufferSize(int) uint64  { return 0 }
func (s *stubInfo) DeviceMemoryOffset(int) uint64      { return 0 }
func (s *stubInfo) DeviceMemoryTotal(int) uint64       { return 0 }
func (s *stubInfo) DeviceSmUtil(int) uint64            { return 0 }
func (s *stubInfo) SetDeviceSmLimit(uint64)            {}
func (s *stubInfo) IsValidUUID(int) bool               { return true }
func (s *stubInfo) DeviceMemoryLimit(int) uint64       { return 0 }
func (s *stubInfo) SetDeviceMemoryLimit(uint64)        {}
func (s *stubInfo) LastKernelTime() int64              { return 0 }
func (s *stubInfo) GetPriority() int                   { return s.priority }
func (s *stubInfo) GetRecentKernel() int32             { return 1 }
func (s *stubInfo) SetRecentKernel(int32)              {}
func (s *stubInfo) GetUtilizationSwitch() int32        { return 0 }
func (s *stubInfo) SetUtilizationSwitch(int32)         {}

func TestCheckFunctionsHighPriority(t *testing.T) {
	sw := map[string]UtilizationPerDevice{"gpu-0": {0, 1}}
	c := &nvidia.ContainerUsage{Info: &stubInfo{priority: 3}}
	if !CheckBlocking(sw, 3, c) {
		t.Error("CheckBlocking: expected true")
	}
	if !CheckPriority(sw, 3, c) {
		t.Error("CheckPriority: expected true")
	}
	sw2 := map[string]UtilizationPerDevice{"gpu-0": {0, 0}}
	if CheckBlocking(sw2, 2, c) {
		t.Error("CheckBlocking: expected false")
	}
}

// multiStubInfo mocks a container that uses several devices, so the check
// functions can be exercised over more than one UUID.
type multiStubInfo struct {
	priority int
	uuids    []string
}

func (s *multiStubInfo) DeviceMax() int                     { return len(s.uuids) }
func (s *multiStubInfo) DeviceNum() int                     { return len(s.uuids) }
func (s *multiStubInfo) DeviceUUID(i int) string            { return s.uuids[i] }
func (s *multiStubInfo) DeviceMemoryContextSize(int) uint64 { return 0 }
func (s *multiStubInfo) DeviceMemoryModuleSize(int) uint64  { return 0 }
func (s *multiStubInfo) DeviceMemoryBufferSize(int) uint64  { return 0 }
func (s *multiStubInfo) DeviceMemoryOffset(int) uint64      { return 0 }
func (s *multiStubInfo) DeviceMemoryTotal(int) uint64       { return 0 }
func (s *multiStubInfo) DeviceSmUtil(int) uint64            { return 0 }
func (s *multiStubInfo) SetDeviceSmLimit(uint64)            {}
func (s *multiStubInfo) IsValidUUID(int) bool               { return true }
func (s *multiStubInfo) DeviceMemoryLimit(int) uint64       { return 0 }
func (s *multiStubInfo) SetDeviceMemoryLimit(uint64)        {}
func (s *multiStubInfo) LastKernelTime() int64              { return 0 }
func (s *multiStubInfo) GetPriority() int                   { return s.priority }
func (s *multiStubInfo) GetRecentKernel() int32             { return 1 }
func (s *multiStubInfo) SetRecentKernel(int32)              {}
func (s *multiStubInfo) GetUtilizationSwitch() int32        { return 0 }
func (s *multiStubInfo) SetUtilizationSwitch(int32)         {}

// TestCheckBlocking_MultiDevice verifies that CheckBlocking inspects every device
// the container uses, not just the first one that appears in the switch map. The
// cases cover several device counts, all-clear, contention isolated to the last
// device, and UUIDs missing from the switch map. In every case contention (when
// present) sits at an index below the priority, so CheckPriority agrees and is
// asserted for parity.
func TestCheckBlocking_MultiDevice(t *testing.T) {
	tests := []struct {
		name     string
		priority int
		uuids    []string
		sw       map[string]UtilizationPerDevice
		want     bool
	}{
		{
			name:     "two devices, contention on the second",
			priority: 1,
			uuids:    []string{"gpu-0", "gpu-1"},
			sw:       map[string]UtilizationPerDevice{"gpu-0": {0, 0}, "gpu-1": {1, 0}},
			want:     true,
		},
		{
			name:     "three devices, all clear",
			priority: 1,
			uuids:    []string{"gpu-0", "gpu-1", "gpu-2"},
			sw:       map[string]UtilizationPerDevice{"gpu-0": {0, 0}, "gpu-1": {0, 0}, "gpu-2": {0, 0}},
			want:     false,
		},
		{
			name:     "four devices, contention only on the last",
			priority: 1,
			uuids:    []string{"gpu-0", "gpu-1", "gpu-2", "gpu-3"},
			sw:       map[string]UtilizationPerDevice{"gpu-0": {0, 0}, "gpu-1": {0, 0}, "gpu-2": {0, 0}, "gpu-3": {1, 0}},
			want:     true,
		},
		{
			name:     "some device UUIDs missing from switch map, present one contended",
			priority: 1,
			uuids:    []string{"gpu-0", "gpu-1", "gpu-2"},
			sw:       map[string]UtilizationPerDevice{"gpu-1": {1, 0}},
			want:     true,
		},
		{
			name:     "none of the container UUIDs are in the switch map",
			priority: 1,
			uuids:    []string{"gpu-0", "gpu-1"},
			sw:       map[string]UtilizationPerDevice{"gpu-9": {1, 0}},
			want:     false,
		},
		{
			name:     "empty switch map",
			priority: 1,
			uuids:    []string{"gpu-0", "gpu-1"},
			sw:       map[string]UtilizationPerDevice{},
			want:     false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := &nvidia.ContainerUsage{Info: &multiStubInfo{priority: test.priority, uuids: test.uuids}}
			if got := CheckBlocking(test.sw, test.priority, c); got != test.want {
				t.Errorf("CheckBlocking: want %v, got %v", test.want, got)
			}
			// The sibling CheckPriority scans all devices too and, for these
			// inputs, agrees with CheckBlocking.
			if got := CheckPriority(test.sw, test.priority, c); got != test.want {
				t.Errorf("CheckPriority: want %v, got %v", test.want, got)
			}
		})
	}
}
