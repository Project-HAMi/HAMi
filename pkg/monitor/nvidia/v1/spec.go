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

package v1

import (
	"unsafe"

	"github.com/Project-HAMi/HAMi/pkg/monitor/nvidia"
)

const maxDevices = 16

type deviceMemory struct {
	contextSize uint64
	moduleSize  uint64
	bufferSize  uint64
	offset      uint64
	total       uint64
	unused      [3]uint64
}

type deviceUtilization struct {
	decUtil uint64
	encUtil uint64
	smUtil  uint64
	unused  [3]uint64
}

type shrregProcSlotT struct {
	pid         int32
	hostpid     int32
	used        [16]deviceMemory
	monitorused [16]uint64
	deviceUtil  [16]deviceUtilization
	status      int32
	unused      [3]uint64
}

type uuid struct {
	uuid [96]byte
}

type semT struct {
	sem [32]byte
}

type sharedRegionT struct {
	initializedFlag int32
	majorVersion    int32
	minorVersion    int32
	smInitFlag      int32
	ownerPid        uint64
	sem             semT
	num             uint64
	uuids           [16]uuid

	limit   [16]uint64
	smLimit [16]uint64
	procs   [1024]shrregProcSlotT

	procnum           int32
	utilizationSwitch int32
	recentKernel      int32
	priority          int32
	lastKernelTime    int64
	unused            [4]uint64
}

type Spec struct {
	sr *sharedRegionT
}

func (s Spec) DeviceMax() int {
	return maxDevices
}

func (s Spec) DeviceNum() int {
	return int(s.sr.num)
}

func (s Spec) DeviceMemoryContextSize(idx int) uint64 {
	v := uint64(0)
	for _, p := range s.sr.procs[:int(s.sr.procnum)] {
		v += p.used[idx].contextSize
	}
	return v
}

func (s Spec) DeviceMemoryModuleSize(idx int) uint64 {
	v := uint64(0)
	for _, p := range s.sr.procs[:int(s.sr.procnum)] {
		v += p.used[idx].moduleSize
	}
	return v
}

func (s Spec) DeviceMemoryBufferSize(idx int) uint64 {
	v := uint64(0)
	for _, p := range s.sr.procs[:int(s.sr.procnum)] {
		v += p.used[idx].bufferSize
	}
	return v
}

func (s Spec) DeviceMemoryOffset(idx int) uint64 {
	v := uint64(0)
	for _, p := range s.sr.procs[:int(s.sr.procnum)] {
		v += p.used[idx].offset
	}
	return v
}

func (s Spec) DeviceMemoryTotal(idx int) uint64 {
	v := uint64(0)
	for _, p := range s.sr.procs[:int(s.sr.procnum)] {
		v += p.used[idx].total
	}
	return v
}

func (s Spec) DeviceSmUtil(idx int) uint64 {
	v := uint64(0)
	for _, p := range s.sr.procs[:int(s.sr.procnum)] {
		v += p.deviceUtil[idx].smUtil
	}
	return v
}

func (s Spec) SetDeviceSmLimit(l uint64) {
	for idx := range min(int(s.sr.num), maxDevices) {
		s.sr.smLimit[idx] = l
	}
}

func (s Spec) IsValidUUID(idx int) bool {
	return s.sr.uuids[idx].uuid[0] != 0
}

func (s Spec) DeviceUUID(idx int) string {
	return string(s.sr.uuids[idx].uuid[:])
}

func (s Spec) DeviceMemoryLimit(idx int) uint64 {
	return s.sr.limit[idx]
}

func (s Spec) SetDeviceMemoryLimit(l uint64) {
	for idx := range min(int(s.sr.num), maxDevices) {
		s.sr.limit[idx] = l
	}
}

func (s Spec) LastKernelTime() int64 {
	return s.sr.lastKernelTime
}

func CastSpec(data []byte) Spec {
	return Spec{
		sr: (*sharedRegionT)(unsafe.Pointer(&data[0])),
	}
}

//	func (s *SharedRegionT) UsedMemory(idx int) (uint64, error) {
//		return 0, nil
//	}

func (s Spec) GetPriority() int {
	return int(s.sr.priority)
}

func (s Spec) GetRecentKernel() int32 {
	return s.sr.recentKernel
}

func (s Spec) SetRecentKernel(v int32) {
	s.sr.recentKernel = v
}

func (s Spec) GetUtilizationSwitch() int32 {
	return s.sr.utilizationSwitch
}

func (s Spec) SetUtilizationSwitch(v int32) {
	s.sr.utilizationSwitch = v
}

// --- sem_postinit variant (C major=1, minor>=2, e.g. hami-core) ---

type shrregProcSlotTWithSeqlock struct {
	pid         int32
	hostpid     int32
	used        [16]deviceMemory
	monitorused [16]uint64
	deviceUtil  [16]deviceUtilization
	status      int32
	seqlock     uint64
	unused      [2]uint64
}

type sharedRegionTWithSemPostinit struct {
	initializedFlag   int32
	majorVersion      int32
	minorVersion      int32
	smInitFlag        int32
	ownerPid          uint64
	sem               semT
	num               uint64
	uuids             [16]uuid
	limit             [16]uint64
	smLimit           [16]uint64
	procs             [1024]shrregProcSlotTWithSeqlock
	procnum           int32
	utilizationSwitch int32
	recentKernel      int32
	priority          int32
	lastKernelTime    int64
	semPostinit       semT
}

type SpecWithSemPostinit struct {
	sr *sharedRegionTWithSemPostinit
}

func (s SpecWithSemPostinit) DeviceMax() int {
	return maxDevices
}

func (s SpecWithSemPostinit) DeviceNum() int {
	return int(s.sr.num)
}

func (s SpecWithSemPostinit) DeviceMemoryContextSize(idx int) uint64 {
	v := uint64(0)
	for _, p := range s.sr.procs[:int(s.sr.procnum)] {
		v += p.used[idx].contextSize
	}
	return v
}

func (s SpecWithSemPostinit) DeviceMemoryModuleSize(idx int) uint64 {
	v := uint64(0)
	for _, p := range s.sr.procs[:int(s.sr.procnum)] {
		v += p.used[idx].moduleSize
	}
	return v
}

func (s SpecWithSemPostinit) DeviceMemoryBufferSize(idx int) uint64 {
	v := uint64(0)
	for _, p := range s.sr.procs[:int(s.sr.procnum)] {
		v += p.used[idx].bufferSize
	}
	return v
}

func (s SpecWithSemPostinit) DeviceMemoryOffset(idx int) uint64 {
	v := uint64(0)
	for _, p := range s.sr.procs[:int(s.sr.procnum)] {
		v += p.used[idx].offset
	}
	return v
}

func (s SpecWithSemPostinit) DeviceMemoryTotal(idx int) uint64 {
	v := uint64(0)
	for _, p := range s.sr.procs[:int(s.sr.procnum)] {
		v += p.used[idx].total
	}
	return v
}

func (s SpecWithSemPostinit) DeviceSmUtil(idx int) uint64 {
	v := uint64(0)
	for _, p := range s.sr.procs[:int(s.sr.procnum)] {
		v += p.deviceUtil[idx].smUtil
	}
	return v
}

func (s SpecWithSemPostinit) SetDeviceSmLimit(l uint64) {
	for idx := range min(int(s.sr.num), maxDevices) {
		s.sr.smLimit[idx] = l
	}
}

func (s SpecWithSemPostinit) IsValidUUID(idx int) bool {
	return s.sr.uuids[idx].uuid[0] != 0
}

func (s SpecWithSemPostinit) DeviceUUID(idx int) string {
	return string(s.sr.uuids[idx].uuid[:])
}

func (s SpecWithSemPostinit) DeviceMemoryLimit(idx int) uint64 {
	return s.sr.limit[idx]
}

func (s SpecWithSemPostinit) SetDeviceMemoryLimit(l uint64) {
	for idx := range min(int(s.sr.num), maxDevices) {
		s.sr.limit[idx] = l
	}
}

func (s SpecWithSemPostinit) LastKernelTime() int64 {
	return s.sr.lastKernelTime
}

func (s SpecWithSemPostinit) GetPriority() int {
	return int(s.sr.priority)
}

func (s SpecWithSemPostinit) GetRecentKernel() int32 {
	return s.sr.recentKernel
}

func (s SpecWithSemPostinit) SetRecentKernel(v int32) {
	s.sr.recentKernel = v
}

func (s SpecWithSemPostinit) GetUtilizationSwitch() int32 {
	return s.sr.utilizationSwitch
}

func (s SpecWithSemPostinit) SetUtilizationSwitch(v int32) {
	s.sr.utilizationSwitch = v
}

func CastSpecWithSemPostinit(data []byte) SpecWithSemPostinit {
	return SpecWithSemPostinit{
		sr: (*sharedRegionTWithSemPostinit)(unsafe.Pointer(&data[0])),
	}
}

// --- Factory registrations ---

func init() {
	nvidia.RegisterFactory(&v1SemFactory{})  // major=1, minor=2
	nvidia.RegisterFactory(&v1BaseFactory{}) // major=1, minor=1
}

type v1SemFactory struct{}

func (v1SemFactory) Match(h *nvidia.HeaderT, size int64) bool {
	return h.MajorVersion == 1 && h.MinorVersion >= 2
}
func (v1SemFactory) Cast(data []byte) nvidia.UsageInfo { return CastSpecWithSemPostinit(data) }
func (v1SemFactory) Name() string                      { return "v1-sem" }

type v1BaseFactory struct{}

func (v1BaseFactory) Match(h *nvidia.HeaderT, size int64) bool {
	return h.MajorVersion == 1 && h.MinorVersion <= 1
}
func (v1BaseFactory) Cast(data []byte) nvidia.UsageInfo { return CastSpec(data) }
func (v1BaseFactory) Name() string                      { return "v1" }
