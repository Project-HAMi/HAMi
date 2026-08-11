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
	"time"

	"github.com/Project-HAMi/HAMi/pkg/monitor/nvidia"
)

// stubDeviceMax mirrors the production Spec.DeviceMax, which reports the constant
// maxDevices (16) rather than the live device count.
const stubDeviceMax = 16

// stubInfo mocks nvidia.UsageInfo. The container's real device UUIDs occupy the
// leading slots of a fixed-size table; DeviceMax still reports the constant slot
// count and the trailing slots read back invalid, so the check functions walk the
// same 16 slots they do in production.
type stubInfo struct {
	priority   int
	uuids      []string
	total      []uint64
	limit      []uint64
	ctxSize    []uint64
	modSize    []uint64
	bufSize    []uint64
	smUtil     []uint64
	lastKernel int64
}

func slot(v []uint64, i int) uint64 {
	if i >= 0 && i < len(v) {
		return v[i]
	}
	return 0
}

func (s *stubInfo) DeviceMax() int { return stubDeviceMax }
func (s *stubInfo) DeviceNum() int { return len(s.uuids) }
func (s *stubInfo) DeviceUUID(i int) string {
	if i < len(s.uuids) {
		return s.uuids[i]
	}
	return ""
}
func (s *stubInfo) DeviceMemoryContextSize(i int) uint64 { return slot(s.ctxSize, i) }
func (s *stubInfo) DeviceMemoryModuleSize(i int) uint64  { return slot(s.modSize, i) }
func (s *stubInfo) DeviceMemoryBufferSize(i int) uint64  { return slot(s.bufSize, i) }
func (s *stubInfo) DeviceMemoryOffset(int) uint64        { return 0 }
func (s *stubInfo) DeviceMemoryTotal(i int) uint64       { return slot(s.total, i) }
func (s *stubInfo) DeviceSmUtil(i int) uint64            { return slot(s.smUtil, i) }
func (s *stubInfo) SetDeviceSmLimit(uint64)              {}
func (s *stubInfo) IsValidUUID(i int) bool               { return i < len(s.uuids) }
func (s *stubInfo) DeviceMemoryLimit(i int) uint64       { return slot(s.limit, i) }
func (s *stubInfo) SetDeviceMemoryLimit(uint64)          {}
func (s *stubInfo) LastKernelTime() int64                { return s.lastKernel }
func (s *stubInfo) GetPriority() int                     { return s.priority }
func (s *stubInfo) GetRecentKernel() int32               { return 1 }
func (s *stubInfo) SetRecentKernel(int32)                {}
func (s *stubInfo) GetUtilizationSwitch() int32          { return 0 }
func (s *stubInfo) SetUtilizationSwitch(int32)           {}

func TestCheckFunctionsHighPriority(t *testing.T) {
	sw := map[string]UtilizationPerDevice{"gpu-0": {0, 1}}
	c := &nvidia.ContainerUsage{Info: &stubInfo{priority: 3, uuids: []string{"gpu-0"}}}
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
			c := &nvidia.ContainerUsage{Info: &stubInfo{priority: test.priority, uuids: test.uuids}}
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

func TestObserve(t *testing.T) {
	// Call Observe with an empty lister to cover the missing lines for codecov
	// and to ensure no panics occur with locking/unlocking.
	lister := &nvidia.ContainerLister{}

	// Test that Observe actually acquires the lock deterministically.
	lister.Lock()
	
	reachedLock := make(chan struct{})
	observeTestHook = func() {
		close(reachedLock)
	}
	defer func() { observeTestHook = nil }()
	
	done := make(chan struct{})
	go func() {
		Observe(lister)
		close(done)
	}()
	
	// Wait until Observe reaches the lock boundary
	<-reachedLock
	
	// Ensure Observe is now blocked on the lock
	select {
	case <-done:
		t.Fatal("Observe completed while lock was held by another goroutine, indicating it did not acquire the lock!")
	default:
		// Expected: Observe is blocked
	}
	
	// Release the lock
	lister.UnLock()
	
	// Now Observe should complete
	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("Observe did not complete after lock was released")
	}
}
