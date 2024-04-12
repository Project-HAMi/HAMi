/**
# Copyright (c) 2022, NVIDIA CORPORATION.  All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
**/

package nvml

import "github.com/NVIDIA/go-nvml/pkg/nvml"

type nvmlDevice nvml.Device

var _ Device = (*nvmlDevice)(nil)

// GetIndex returns the index of a Device
func (d nvmlDevice) GetIndex() (int, Return) {
	i, r := nvml.Device(d).GetIndex()
	return i, Return(r)
}

// GetPciInfo returns the PCI info of a Device
func (d nvmlDevice) GetPciInfo() (PciInfo, Return) {
	p, r := nvml.Device(d).GetPciInfo()
	return PciInfo(p), Return(r)
}

// GetMemoryInfo returns the memory info of a Device
func (d nvmlDevice) GetMemoryInfo() (Memory, Return) {
	p, r := nvml.Device(d).GetMemoryInfo()
	return Memory(p), Return(r)
}

// GetUUID returns the UUID of a Device
func (d nvmlDevice) GetUUID() (string, Return) {
	u, r := nvml.Device(d).GetUUID()
	return u, Return(r)
}

// GetMinorNumber returns the minor number of a Device
func (d nvmlDevice) GetMinorNumber() (int, Return) {
	m, r := nvml.Device(d).GetMinorNumber()
	return m, Return(r)
}

// IsMigDeviceHandle returns whether a Device is a MIG device or not
func (d nvmlDevice) IsMigDeviceHandle() (bool, Return) {
	b, r := nvml.Device(d).IsMigDeviceHandle()
	return b, Return(r)
}

// GetDeviceHandleFromMigDeviceHandle returns the parent Device of a MIG device
func (d nvmlDevice) GetDeviceHandleFromMigDeviceHandle() (Device, Return) {
	p, r := nvml.Device(d).GetDeviceHandleFromMigDeviceHandle()
	return nvmlDevice(p), Return(r)
}

// SetMigMode sets the MIG mode of a Device
func (d nvmlDevice) SetMigMode(mode int) (Return, Return) {
	r1, r2 := nvml.Device(d).SetMigMode(mode)
	return Return(r1), Return(r2)
}

// GetMigMode returns the MIG mode of a Device
func (d nvmlDevice) GetMigMode() (int, int, Return) {
	s1, s2, r := nvml.Device(d).GetMigMode()
	return s1, s2, Return(r)
}

// GetGpuInstanceById returns the GPU Instance associated with a particular ID
func (d nvmlDevice) GetGpuInstanceById(id int) (GpuInstance, Return) {
	gi, r := nvml.Device(d).GetGpuInstanceById(id)
	return nvmlGpuInstance(gi), Return(r)
}

// GetGpuInstanceProfileInfo returns the profile info of a GPU Instance
func (d nvmlDevice) GetGpuInstanceProfileInfo(profile int) (GpuInstanceProfileInfo, Return) {
	p, r := nvml.Device(d).GetGpuInstanceProfileInfo(profile)
	return GpuInstanceProfileInfo(p), Return(r)
}

// GetGpuInstancePossiblePlacements returns the possible placements of a GPU Instance
func (d nvmlDevice) GetGpuInstancePossiblePlacements(info *GpuInstanceProfileInfo) ([]GpuInstancePlacement, Return) {
	nvmlPlacements, r := nvml.Device(d).GetGpuInstancePossiblePlacements((*nvml.GpuInstanceProfileInfo)(info))
	var placements []GpuInstancePlacement
	for _, p := range nvmlPlacements {
		placements = append(placements, GpuInstancePlacement(p))
	}
	return placements, Return(r)
}

// GetGpuInstances returns the set of GPU Instances associated with a Device
func (d nvmlDevice) GetGpuInstances(info *GpuInstanceProfileInfo) ([]GpuInstance, Return) {
	nvmlGis, r := nvml.Device(d).GetGpuInstances((*nvml.GpuInstanceProfileInfo)(info))
	var gis []GpuInstance
	for _, gi := range nvmlGis {
		gis = append(gis, nvmlGpuInstance(gi))
	}
	return gis, Return(r)
}

// CreateGpuInstanceWithPlacement creates a GPU Instance with a specific placement
func (d nvmlDevice) CreateGpuInstanceWithPlacement(info *GpuInstanceProfileInfo, placement *GpuInstancePlacement) (GpuInstance, Return) {
	gi, r := nvml.Device(d).CreateGpuInstanceWithPlacement((*nvml.GpuInstanceProfileInfo)(info), (*nvml.GpuInstancePlacement)(placement))
	return nvmlGpuInstance(gi), Return(r)
}

// GetMaxMigDeviceCount returns the maximum number of MIG devices that can be created on a Device
func (d nvmlDevice) GetMaxMigDeviceCount() (int, Return) {
	m, r := nvml.Device(d).GetMaxMigDeviceCount()
	return m, Return(r)
}

// GetMigDeviceHandleByIndex returns the handle to a MIG device given its index
func (d nvmlDevice) GetMigDeviceHandleByIndex(Index int) (Device, Return) {
	h, r := nvml.Device(d).GetMigDeviceHandleByIndex(Index)
	return nvmlDevice(h), Return(r)
}

// GetGpuInstanceId returns the GPU Instance ID of a MIG device
func (d nvmlDevice) GetGpuInstanceId() (int, Return) {
	gi, r := nvml.Device(d).GetGpuInstanceId()
	return gi, Return(r)
}

// GetComputeInstanceId returns the Compute Instance ID of a MIG device
func (d nvmlDevice) GetComputeInstanceId() (int, Return) {
	ci, r := nvml.Device(d).GetComputeInstanceId()
	return ci, Return(r)
}

// GetCudaComputeCapability returns the compute capability major and minor versions for a device
func (d nvmlDevice) GetCudaComputeCapability() (int, int, Return) {
	major, minor, r := nvml.Device(d).GetCudaComputeCapability()
	return major, minor, Return(r)
}

// GetAttributes returns the device attributes for a MIG device
func (d nvmlDevice) GetAttributes() (DeviceAttributes, Return) {
	a, r := nvml.Device(d).GetAttributes()
	return DeviceAttributes(a), Return(r)
}

// GetName returns the product name of a Device
func (d nvmlDevice) GetName() (string, Return) {
	n, r := nvml.Device(d).GetName()
	return n, Return(r)
}

// GetBrand returns the brand of a Device
func (d nvmlDevice) GetBrand() (BrandType, Return) {
	b, r := nvml.Device(d).GetBrand()
	return BrandType(b), Return(r)
}

// GetArchitecture returns the architecture of a Device
func (d nvmlDevice) GetArchitecture() (DeviceArchitecture, Return) {
	a, r := nvml.Device(d).GetArchitecture()
	return DeviceArchitecture(a), Return(r)
}

// RegisterEvents registers the specified event set and type with the device
func (d nvmlDevice) RegisterEvents(EventTypes uint64, Set EventSet) Return {
	return Return(nvml.Device(d).RegisterEvents(EventTypes, nvml.EventSet(Set)))
}

// GetSupportedEventTypes returns the events supported by the device
func (d nvmlDevice) GetSupportedEventTypes() (uint64, Return) {
	e, r := nvml.Device(d).GetSupportedEventTypes()
	return e, Return(r)
}

// GetTopologyCommonAncestor retrieves the common ancestor for two devices.
func (d nvmlDevice) GetTopologyCommonAncestor(o Device) (GpuTopologyLevel, Return) {
	other, ok := o.(nvmlDevice)
	if !ok {
		return 0, ERROR_INVALID_ARGUMENT
	}

	l, r := nvml.Device(d).GetTopologyCommonAncestor(nvml.Device(other))
	return GpuTopologyLevel(l), Return(r)
}

// GetNvLinkState retrieves the state of the device's NvLink for the link specified.
func (d nvmlDevice) GetNvLinkState(link int) (EnableState, Return) {
	s, r := nvml.Device(d).GetNvLinkState(link)
	return EnableState(s), Return(r)
}

// GetNvLinkRemotePciInfo retrieves the PCI information for the remote node on a NvLink link.
// Note: pciSubSystemId is not filled in this function and is indeterminate.
func (d nvmlDevice) GetNvLinkRemotePciInfo(link int) (PciInfo, Return) {
	p, r := nvml.Device(d).GetNvLinkRemotePciInfo(link)
	return PciInfo(p), Return(r)
}
