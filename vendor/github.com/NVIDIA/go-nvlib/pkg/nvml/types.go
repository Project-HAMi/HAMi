/*
 * Copyright (c) 2022, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package nvml

import (
	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

// Interface defines the functions implemented by an NVML library
//
//go:generate moq -out nvml_mock.go . Interface
type Interface interface {
	DeviceGetCount() (int, Return)
	DeviceGetHandleByIndex(Index int) (Device, Return)
	DeviceGetHandleByUUID(UUID string) (Device, Return)
	ErrorString(r Return) string
	EventSetCreate() (EventSet, Return)
	Init() Return
	Lookup(string) error
	Shutdown() Return
	SystemGetCudaDriverVersion() (int, Return)
	SystemGetDriverVersion() (string, Return)
}

// Device defines the functions implemented by an NVML device
//
//go:generate moq -out device_mock.go . Device
type Device interface {
	CreateGpuInstanceWithPlacement(*GpuInstanceProfileInfo, *GpuInstancePlacement) (GpuInstance, Return)
	GetArchitecture() (DeviceArchitecture, Return)
	GetAttributes() (DeviceAttributes, Return)
	GetBrand() (BrandType, Return)
	GetComputeInstanceId() (int, Return)
	GetCudaComputeCapability() (int, int, Return)
	GetDeviceHandleFromMigDeviceHandle() (Device, Return)
	GetGpuInstanceById(ID int) (GpuInstance, Return)
	GetGpuInstanceId() (int, Return)
	GetGpuInstancePossiblePlacements(*GpuInstanceProfileInfo) ([]GpuInstancePlacement, Return)
	GetGpuInstanceProfileInfo(Profile int) (GpuInstanceProfileInfo, Return)
	GetGpuInstances(Info *GpuInstanceProfileInfo) ([]GpuInstance, Return)
	GetIndex() (int, Return)
	GetMaxMigDeviceCount() (int, Return)
	GetMemoryInfo() (Memory, Return)
	GetMigDeviceHandleByIndex(Index int) (Device, Return)
	GetMigMode() (int, int, Return)
	GetMinorNumber() (int, Return)
	GetName() (string, Return)
	GetNvLinkRemotePciInfo(int) (PciInfo, Return)
	GetNvLinkState(int) (EnableState, Return)
	GetPciInfo() (PciInfo, Return)
	GetSupportedEventTypes() (uint64, Return)
	GetTopologyCommonAncestor(Device) (GpuTopologyLevel, Return)
	GetUUID() (string, Return)
	IsMigDeviceHandle() (bool, Return)
	RegisterEvents(uint64, EventSet) Return
	SetMigMode(Mode int) (Return, Return)
}

// GpuInstance defines the functions implemented by a GpuInstance
//
//go:generate moq -out gi_mock.go . GpuInstance
type GpuInstance interface {
	CreateComputeInstance(Info *ComputeInstanceProfileInfo) (ComputeInstance, Return)
	Destroy() Return
	GetComputeInstanceById(ID int) (ComputeInstance, Return)
	GetComputeInstanceProfileInfo(Profile int, EngProfile int) (ComputeInstanceProfileInfo, Return)
	GetComputeInstances(Info *ComputeInstanceProfileInfo) ([]ComputeInstance, Return)
	GetInfo() (GpuInstanceInfo, Return)
}

// ComputeInstance defines the functions implemented by a ComputeInstance
//
//go:generate moq -out ci_mock.go . ComputeInstance
type ComputeInstance interface {
	Destroy() Return
	GetInfo() (ComputeInstanceInfo, Return)
}

// GpuInstanceInfo holds info about a GPU Instance
type GpuInstanceInfo struct {
	Device    Device
	Id        uint32
	ProfileId uint32
	Placement GpuInstancePlacement
}

// ComputeInstanceInfo holds info about a Compute Instance
type ComputeInstanceInfo struct {
	Device      Device
	GpuInstance GpuInstance
	Id          uint32
	ProfileId   uint32
	Placement   ComputeInstancePlacement
}

// EventData defines NVML event Data
type EventData struct {
	Device            Device
	EventType         uint64
	EventData         uint64
	GpuInstanceId     uint32
	ComputeInstanceId uint32
}

// EventSet defines NVML event Data
type EventSet nvml.EventSet

// Return defines an NVML return type
type Return nvml.Return

// Memory holds info about GPU device memory
type Memory nvml.Memory

// PciInfo holds info about the PCI connections of a GPU dvice
type PciInfo nvml.PciInfo

// GpuInstanceProfileInfo holds info about a GPU Instance Profile
type GpuInstanceProfileInfo nvml.GpuInstanceProfileInfo

// GpuInstancePlacement holds placement info about a GPU Instance
type GpuInstancePlacement nvml.GpuInstancePlacement

// ComputeInstanceProfileInfo holds info about a Compute Instance Profile
type ComputeInstanceProfileInfo nvml.ComputeInstanceProfileInfo

// ComputeInstancePlacement holds placement info about a Compute Instance
type ComputeInstancePlacement nvml.ComputeInstancePlacement

// DeviceAttributes stores information about MIG devices
type DeviceAttributes nvml.DeviceAttributes

// DeviceArchitecture represents the hardware architecture of a GPU device
type DeviceArchitecture nvml.DeviceArchitecture

// BrandType represents the brand of a GPU device
type BrandType nvml.BrandType

// GpuTopologyLevel represents level relationships within a system between two GPUs
type GpuTopologyLevel nvml.GpuTopologyLevel

// EnableState represents a generic enable/disable enum
type EnableState nvml.EnableState
