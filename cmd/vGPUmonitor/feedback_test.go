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

// TestCheckBlocking_MultiDeviceContention verifies that CheckBlocking inspects
// every device the container uses, not just the first one that appears in the
// switch map. Here the first device (gpu-0) has no contention while a second
// device (gpu-1) does; CheckBlocking must still report blocking.
func TestCheckBlocking_MultiDeviceContention(t *testing.T) {
	c := &nvidia.ContainerUsage{Info: &multiStubInfo{priority: 1, uuids: []string{"gpu-0", "gpu-1"}}}
	sw := map[string]UtilizationPerDevice{
		"gpu-0": {0, 0},
		"gpu-1": {1, 0},
	}
	if !CheckBlocking(sw, 1, c) {
		t.Error("CheckBlocking: expected true (gpu-1 has contention), got false")
	}
	// Sanity: the sibling CheckPriority already scans all devices and agrees.
	if !CheckPriority(sw, 1, c) {
		t.Error("CheckPriority: expected true (gpu-1 has contention), got false")
	}
}
