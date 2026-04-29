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

package api

import "sync"

type Header struct {
	InitializedFlag int32
	MajorVersion    int32
	MinorVersion    int32
}

type UsageInfo interface {
	DeviceMax() int
	DeviceNum() int
	DeviceMemoryContextSize(idx int) uint64
	DeviceMemoryModuleSize(idx int) uint64
	DeviceMemoryBufferSize(idx int) uint64
	DeviceMemoryOffset(idx int) uint64
	DeviceMemoryTotal(idx int) uint64
	DeviceSmUtil(idx int) uint64
	SetDeviceSmLimit(l uint64)
	IsValidUUID(idx int) bool
	DeviceUUID(idx int) string
	DeviceMemoryLimit(idx int) uint64
	SetDeviceMemoryLimit(l uint64)
	LastKernelTime() int64
	GetPriority() int
	GetRecentKernel() int32
	SetRecentKernel(v int32)
	GetUtilizationSwitch() int32
	SetUtilizationSwitch(v int32)
}

type CacheFactory interface {
	Match(header *Header, fileSize int64) bool
	Cast(data []byte) UsageInfo
	Name() string
}

var (
	factories   []CacheFactory
	factoriesMu sync.RWMutex
)

func RegisterFactory(f CacheFactory) {
	factoriesMu.Lock()
	defer factoriesMu.Unlock()
	factories = append(factories, f)
}

func FindFactory(header *Header, fileSize int64) CacheFactory {
	factoriesMu.RLock()
	defer factoriesMu.RUnlock()
	for _, f := range factories {
		if f.Match(header, fileSize) {
			return f
		}
	}
	return nil
}
