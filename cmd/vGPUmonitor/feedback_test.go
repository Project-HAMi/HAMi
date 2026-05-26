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
