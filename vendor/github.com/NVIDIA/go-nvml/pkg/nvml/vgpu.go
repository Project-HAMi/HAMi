// Copyright (c) 2020, NVIDIA CORPORATION.  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package nvml

import (
	"unsafe"
)

// nvml.VgpuMetadata
type VgpuMetadata struct {
	nvmlVgpuMetadata
	OpaqueData []byte
}

// nvml.VgpuPgpuMetadata
type VgpuPgpuMetadata struct {
	nvmlVgpuPgpuMetadata
	OpaqueData []byte
}

// nvml.VgpuTypeGetClass()
func VgpuTypeGetClass(VgpuTypeId VgpuTypeId) (string, Return) {
	var Size uint32 = DEVICE_NAME_BUFFER_SIZE
	VgpuTypeClass := make([]byte, DEVICE_NAME_BUFFER_SIZE)
	ret := nvmlVgpuTypeGetClass(VgpuTypeId, &VgpuTypeClass[0], &Size)
	return string(VgpuTypeClass[:clen(VgpuTypeClass)]), ret
}

func (VgpuTypeId VgpuTypeId) GetClass() (string, Return) {
	return VgpuTypeGetClass(VgpuTypeId)
}

// nvml.VgpuTypeGetName()
func VgpuTypeGetName(VgpuTypeId VgpuTypeId) (string, Return) {
	var Size uint32 = DEVICE_NAME_BUFFER_SIZE
	VgpuTypeName := make([]byte, DEVICE_NAME_BUFFER_SIZE)
	ret := nvmlVgpuTypeGetName(VgpuTypeId, &VgpuTypeName[0], &Size)
	return string(VgpuTypeName[:clen(VgpuTypeName)]), ret
}

func (VgpuTypeId VgpuTypeId) GetName() (string, Return) {
	return VgpuTypeGetName(VgpuTypeId)
}

// nvml.VgpuTypeGetGpuInstanceProfileId()
func VgpuTypeGetGpuInstanceProfileId(VgpuTypeId VgpuTypeId) (uint32, Return) {
	var Size uint32
	ret := nvmlVgpuTypeGetGpuInstanceProfileId(VgpuTypeId, &Size)
	return Size, ret
}

func (VgpuTypeId VgpuTypeId) GetGpuInstanceProfileId() (uint32, Return) {
	return VgpuTypeGetGpuInstanceProfileId(VgpuTypeId)
}

// nvml.VgpuTypeGetDeviceID()
func VgpuTypeGetDeviceID(VgpuTypeId VgpuTypeId) (uint64, uint64, Return) {
	var DeviceID, SubsystemID uint64
	ret := nvmlVgpuTypeGetDeviceID(VgpuTypeId, &DeviceID, &SubsystemID)
	return DeviceID, SubsystemID, ret
}

func (VgpuTypeId VgpuTypeId) GetDeviceID() (uint64, uint64, Return) {
	return VgpuTypeGetDeviceID(VgpuTypeId)
}

// nvml.VgpuTypeGetFramebufferSize()
func VgpuTypeGetFramebufferSize(VgpuTypeId VgpuTypeId) (uint64, Return) {
	var FbSize uint64
	ret := nvmlVgpuTypeGetFramebufferSize(VgpuTypeId, &FbSize)
	return FbSize, ret
}

func (VgpuTypeId VgpuTypeId) GetFramebufferSize() (uint64, Return) {
	return VgpuTypeGetFramebufferSize(VgpuTypeId)
}

// nvml.VgpuTypeGetNumDisplayHeads()
func VgpuTypeGetNumDisplayHeads(VgpuTypeId VgpuTypeId) (int, Return) {
	var NumDisplayHeads uint32
	ret := nvmlVgpuTypeGetNumDisplayHeads(VgpuTypeId, &NumDisplayHeads)
	return int(NumDisplayHeads), ret
}

func (VgpuTypeId VgpuTypeId) GetNumDisplayHeads() (int, Return) {
	return VgpuTypeGetNumDisplayHeads(VgpuTypeId)
}

// nvml.VgpuTypeGetResolution()
func VgpuTypeGetResolution(VgpuTypeId VgpuTypeId, DisplayIndex int) (uint32, uint32, Return) {
	var Xdim, Ydim uint32
	ret := nvmlVgpuTypeGetResolution(VgpuTypeId, uint32(DisplayIndex), &Xdim, &Ydim)
	return Xdim, Ydim, ret
}

func (VgpuTypeId VgpuTypeId) GetResolution(DisplayIndex int) (uint32, uint32, Return) {
	return VgpuTypeGetResolution(VgpuTypeId, DisplayIndex)
}

// nvml.VgpuTypeGetLicense()
func VgpuTypeGetLicense(VgpuTypeId VgpuTypeId) (string, Return) {
	VgpuTypeLicenseString := make([]byte, GRID_LICENSE_BUFFER_SIZE)
	ret := nvmlVgpuTypeGetLicense(VgpuTypeId, &VgpuTypeLicenseString[0], GRID_LICENSE_BUFFER_SIZE)
	return string(VgpuTypeLicenseString[:clen(VgpuTypeLicenseString)]), ret
}

func (VgpuTypeId VgpuTypeId) GetLicense() (string, Return) {
	return VgpuTypeGetLicense(VgpuTypeId)
}

// nvml.VgpuTypeGetFrameRateLimit()
func VgpuTypeGetFrameRateLimit(VgpuTypeId VgpuTypeId) (uint32, Return) {
	var FrameRateLimit uint32
	ret := nvmlVgpuTypeGetFrameRateLimit(VgpuTypeId, &FrameRateLimit)
	return FrameRateLimit, ret
}

func (VgpuTypeId VgpuTypeId) GetFrameRateLimit() (uint32, Return) {
	return VgpuTypeGetFrameRateLimit(VgpuTypeId)
}

// nvml.VgpuTypeGetMaxInstances()
func VgpuTypeGetMaxInstances(Device Device, VgpuTypeId VgpuTypeId) (int, Return) {
	var VgpuInstanceCount uint32
	ret := nvmlVgpuTypeGetMaxInstances(Device, VgpuTypeId, &VgpuInstanceCount)
	return int(VgpuInstanceCount), ret
}

func (Device Device) VgpuTypeGetMaxInstances(VgpuTypeId VgpuTypeId) (int, Return) {
	return VgpuTypeGetMaxInstances(Device, VgpuTypeId)
}

func (VgpuTypeId VgpuTypeId) GetMaxInstances(Device Device) (int, Return) {
	return VgpuTypeGetMaxInstances(Device, VgpuTypeId)
}

// nvml.VgpuTypeGetMaxInstancesPerVm()
func VgpuTypeGetMaxInstancesPerVm(VgpuTypeId VgpuTypeId) (int, Return) {
	var VgpuInstanceCountPerVm uint32
	ret := nvmlVgpuTypeGetMaxInstancesPerVm(VgpuTypeId, &VgpuInstanceCountPerVm)
	return int(VgpuInstanceCountPerVm), ret
}

func (VgpuTypeId VgpuTypeId) GetMaxInstancesPerVm() (int, Return) {
	return VgpuTypeGetMaxInstancesPerVm(VgpuTypeId)
}

// nvml.VgpuInstanceGetVmID()
func VgpuInstanceGetVmID(VgpuInstance VgpuInstance) (string, VgpuVmIdType, Return) {
	var VmIdType VgpuVmIdType
	VmId := make([]byte, DEVICE_UUID_BUFFER_SIZE)
	ret := nvmlVgpuInstanceGetVmID(VgpuInstance, &VmId[0], DEVICE_UUID_BUFFER_SIZE, &VmIdType)
	return string(VmId[:clen(VmId)]), VmIdType, ret
}

func (VgpuInstance VgpuInstance) GetVmID() (string, VgpuVmIdType, Return) {
	return VgpuInstanceGetVmID(VgpuInstance)
}

// nvml.VgpuInstanceGetUUID()
func VgpuInstanceGetUUID(VgpuInstance VgpuInstance) (string, Return) {
	Uuid := make([]byte, DEVICE_UUID_BUFFER_SIZE)
	ret := nvmlVgpuInstanceGetUUID(VgpuInstance, &Uuid[0], DEVICE_UUID_BUFFER_SIZE)
	return string(Uuid[:clen(Uuid)]), ret
}

func (VgpuInstance VgpuInstance) GetUUID() (string, Return) {
	return VgpuInstanceGetUUID(VgpuInstance)
}

// nvml.VgpuInstanceGetVmDriverVersion()
func VgpuInstanceGetVmDriverVersion(VgpuInstance VgpuInstance) (string, Return) {
	Version := make([]byte, SYSTEM_DRIVER_VERSION_BUFFER_SIZE)
	ret := nvmlVgpuInstanceGetVmDriverVersion(VgpuInstance, &Version[0], SYSTEM_DRIVER_VERSION_BUFFER_SIZE)
	return string(Version[:clen(Version)]), ret
}

func (VgpuInstance VgpuInstance) GetVmDriverVersion() (string, Return) {
	return VgpuInstanceGetVmDriverVersion(VgpuInstance)
}

// nvml.VgpuInstanceGetFbUsage()
func VgpuInstanceGetFbUsage(VgpuInstance VgpuInstance) (uint64, Return) {
	var FbUsage uint64
	ret := nvmlVgpuInstanceGetFbUsage(VgpuInstance, &FbUsage)
	return FbUsage, ret
}

func (VgpuInstance VgpuInstance) GetFbUsage() (uint64, Return) {
	return VgpuInstanceGetFbUsage(VgpuInstance)
}

// nvml.VgpuInstanceGetLicenseInfo()
func VgpuInstanceGetLicenseInfo(VgpuInstance VgpuInstance) (VgpuLicenseInfo, Return) {
	var LicenseInfo VgpuLicenseInfo
	ret := nvmlVgpuInstanceGetLicenseInfo(VgpuInstance, &LicenseInfo)
	return LicenseInfo, ret
}

func (VgpuInstance VgpuInstance) GetLicenseInfo() (VgpuLicenseInfo, Return) {
	return VgpuInstanceGetLicenseInfo(VgpuInstance)
}

// nvml.VgpuInstanceGetLicenseStatus()
func VgpuInstanceGetLicenseStatus(VgpuInstance VgpuInstance) (int, Return) {
	var Licensed uint32
	ret := nvmlVgpuInstanceGetLicenseStatus(VgpuInstance, &Licensed)
	return int(Licensed), ret
}

func (VgpuInstance VgpuInstance) GetLicenseStatus() (int, Return) {
	return VgpuInstanceGetLicenseStatus(VgpuInstance)
}

// nvml.VgpuInstanceGetType()
func VgpuInstanceGetType(VgpuInstance VgpuInstance) (VgpuTypeId, Return) {
	var VgpuTypeId VgpuTypeId
	ret := nvmlVgpuInstanceGetType(VgpuInstance, &VgpuTypeId)
	return VgpuTypeId, ret
}

func (VgpuInstance VgpuInstance) GetType() (VgpuTypeId, Return) {
	return VgpuInstanceGetType(VgpuInstance)
}

// nvml.VgpuInstanceGetFrameRateLimit()
func VgpuInstanceGetFrameRateLimit(VgpuInstance VgpuInstance) (uint32, Return) {
	var FrameRateLimit uint32
	ret := nvmlVgpuInstanceGetFrameRateLimit(VgpuInstance, &FrameRateLimit)
	return FrameRateLimit, ret
}

func (VgpuInstance VgpuInstance) GetFrameRateLimit() (uint32, Return) {
	return VgpuInstanceGetFrameRateLimit(VgpuInstance)
}

// nvml.VgpuInstanceGetEccMode()
func VgpuInstanceGetEccMode(VgpuInstance VgpuInstance) (EnableState, Return) {
	var EccMode EnableState
	ret := nvmlVgpuInstanceGetEccMode(VgpuInstance, &EccMode)
	return EccMode, ret
}

func (VgpuInstance VgpuInstance) GetEccMode() (EnableState, Return) {
	return VgpuInstanceGetEccMode(VgpuInstance)
}

// nvml.VgpuInstanceGetEncoderCapacity()
func VgpuInstanceGetEncoderCapacity(VgpuInstance VgpuInstance) (int, Return) {
	var EncoderCapacity uint32
	ret := nvmlVgpuInstanceGetEncoderCapacity(VgpuInstance, &EncoderCapacity)
	return int(EncoderCapacity), ret
}

func (VgpuInstance VgpuInstance) GetEncoderCapacity() (int, Return) {
	return VgpuInstanceGetEncoderCapacity(VgpuInstance)
}

// nvml.VgpuInstanceSetEncoderCapacity()
func VgpuInstanceSetEncoderCapacity(VgpuInstance VgpuInstance, EncoderCapacity int) Return {
	return nvmlVgpuInstanceSetEncoderCapacity(VgpuInstance, uint32(EncoderCapacity))
}

func (VgpuInstance VgpuInstance) SetEncoderCapacity(EncoderCapacity int) Return {
	return VgpuInstanceSetEncoderCapacity(VgpuInstance, EncoderCapacity)
}

// nvml.VgpuInstanceGetEncoderStats()
func VgpuInstanceGetEncoderStats(VgpuInstance VgpuInstance) (int, uint32, uint32, Return) {
	var SessionCount, AverageFps, AverageLatency uint32
	ret := nvmlVgpuInstanceGetEncoderStats(VgpuInstance, &SessionCount, &AverageFps, &AverageLatency)
	return int(SessionCount), AverageFps, AverageLatency, ret
}

func (VgpuInstance VgpuInstance) GetEncoderStats() (int, uint32, uint32, Return) {
	return VgpuInstanceGetEncoderStats(VgpuInstance)
}

// nvml.VgpuInstanceGetEncoderSessions()
func VgpuInstanceGetEncoderSessions(VgpuInstance VgpuInstance) (int, EncoderSessionInfo, Return) {
	var SessionCount uint32
	var SessionInfo EncoderSessionInfo
	ret := nvmlVgpuInstanceGetEncoderSessions(VgpuInstance, &SessionCount, &SessionInfo)
	return int(SessionCount), SessionInfo, ret
}

func (VgpuInstance VgpuInstance) GetEncoderSessions() (int, EncoderSessionInfo, Return) {
	return VgpuInstanceGetEncoderSessions(VgpuInstance)
}

// nvml.VgpuInstanceGetFBCStats()
func VgpuInstanceGetFBCStats(VgpuInstance VgpuInstance) (FBCStats, Return) {
	var FbcStats FBCStats
	ret := nvmlVgpuInstanceGetFBCStats(VgpuInstance, &FbcStats)
	return FbcStats, ret
}

func (VgpuInstance VgpuInstance) GetFBCStats() (FBCStats, Return) {
	return VgpuInstanceGetFBCStats(VgpuInstance)
}

// nvml.VgpuInstanceGetFBCSessions()
func VgpuInstanceGetFBCSessions(VgpuInstance VgpuInstance) (int, FBCSessionInfo, Return) {
	var SessionCount uint32
	var SessionInfo FBCSessionInfo
	ret := nvmlVgpuInstanceGetFBCSessions(VgpuInstance, &SessionCount, &SessionInfo)
	return int(SessionCount), SessionInfo, ret
}

func (VgpuInstance VgpuInstance) GetFBCSessions() (int, FBCSessionInfo, Return) {
	return VgpuInstanceGetFBCSessions(VgpuInstance)
}

// nvml.VgpuInstanceGetGpuInstanceId()
func VgpuInstanceGetGpuInstanceId(VgpuInstance VgpuInstance) (int, Return) {
	var gpuInstanceId uint32
	ret := nvmlVgpuInstanceGetGpuInstanceId(VgpuInstance, &gpuInstanceId)
	return int(gpuInstanceId), ret
}

func (VgpuInstance VgpuInstance) GetGpuInstanceId() (int, Return) {
	return VgpuInstanceGetGpuInstanceId(VgpuInstance)
}

// nvml.VgpuInstanceGetGpuPciId()
func VgpuInstanceGetGpuPciId(VgpuInstance VgpuInstance) (string, Return) {
	var Length uint32 = 1 // Will be reduced upon returning
	for {
		VgpuPciId := make([]byte, Length)
		ret := nvmlVgpuInstanceGetGpuPciId(VgpuInstance, &VgpuPciId[0], &Length)
		if ret == SUCCESS {
			return string(VgpuPciId[:clen(VgpuPciId)]), ret
		}
		if ret != ERROR_INSUFFICIENT_SIZE {
			return "", ret
		}
		Length *= 2
	}
}

func (VgpuInstance VgpuInstance) GetGpuPciId() (string, Return) {
	return VgpuInstanceGetGpuPciId(VgpuInstance)
}

// nvml.VgpuInstanceGetMetadata()
func VgpuInstanceGetMetadata(VgpuInstance VgpuInstance) (VgpuMetadata, Return) {
	var VgpuMetadata VgpuMetadata
	OpaqueDataSize := unsafe.Sizeof(VgpuMetadata.nvmlVgpuMetadata.OpaqueData)
	VgpuMetadataSize := unsafe.Sizeof(VgpuMetadata.nvmlVgpuMetadata) - OpaqueDataSize
	for {
		BufferSize := uint32(VgpuMetadataSize + OpaqueDataSize)
		Buffer := make([]byte, BufferSize)
		nvmlVgpuMetadataPtr := (*nvmlVgpuMetadata)(unsafe.Pointer(&Buffer[0]))
		ret := nvmlVgpuInstanceGetMetadata(VgpuInstance, nvmlVgpuMetadataPtr, &BufferSize)
		if ret == SUCCESS {
			VgpuMetadata.nvmlVgpuMetadata = *nvmlVgpuMetadataPtr
			VgpuMetadata.OpaqueData = Buffer[VgpuMetadataSize:BufferSize]
			return VgpuMetadata, ret
		}
		if ret != ERROR_INSUFFICIENT_SIZE {
			return VgpuMetadata, ret
		}
		OpaqueDataSize = 2 * OpaqueDataSize
	}
}

func (VgpuInstance VgpuInstance) GetMetadata() (VgpuMetadata, Return) {
	return VgpuInstanceGetMetadata(VgpuInstance)
}

// nvml.VgpuInstanceGetAccountingMode()
func VgpuInstanceGetAccountingMode(VgpuInstance VgpuInstance) (EnableState, Return) {
	var Mode EnableState
	ret := nvmlVgpuInstanceGetAccountingMode(VgpuInstance, &Mode)
	return Mode, ret
}

func (VgpuInstance VgpuInstance) GetAccountingMode() (EnableState, Return) {
	return VgpuInstanceGetAccountingMode(VgpuInstance)
}

// nvml.VgpuInstanceGetAccountingPids()
func VgpuInstanceGetAccountingPids(VgpuInstance VgpuInstance) ([]int, Return) {
	var Count uint32 = 1 // Will be reduced upon returning
	for {
		Pids := make([]uint32, Count)
		ret := nvmlVgpuInstanceGetAccountingPids(VgpuInstance, &Count, &Pids[0])
		if ret == SUCCESS {
			return uint32SliceToIntSlice(Pids[:Count]), ret
		}
		if ret != ERROR_INSUFFICIENT_SIZE {
			return nil, ret
		}
		Count *= 2
	}
}

func (VgpuInstance VgpuInstance) GetAccountingPids() ([]int, Return) {
	return VgpuInstanceGetAccountingPids(VgpuInstance)
}

// nvml.VgpuInstanceGetAccountingStats()
func VgpuInstanceGetAccountingStats(VgpuInstance VgpuInstance, Pid int) (AccountingStats, Return) {
	var Stats AccountingStats
	ret := nvmlVgpuInstanceGetAccountingStats(VgpuInstance, uint32(Pid), &Stats)
	return Stats, ret
}

func (VgpuInstance VgpuInstance) GetAccountingStats(Pid int) (AccountingStats, Return) {
	return VgpuInstanceGetAccountingStats(VgpuInstance, Pid)
}

// nvml.GetVgpuCompatibility()
func GetVgpuCompatibility(nvmlVgpuMetadata *nvmlVgpuMetadata, PgpuMetadata *nvmlVgpuPgpuMetadata) (VgpuPgpuCompatibility, Return) {
	var CompatibilityInfo VgpuPgpuCompatibility
	ret := nvmlGetVgpuCompatibility(nvmlVgpuMetadata, PgpuMetadata, &CompatibilityInfo)
	return CompatibilityInfo, ret
}

// nvml.GetVgpuVersion()
func GetVgpuVersion() (VgpuVersion, VgpuVersion, Return) {
	var Supported, Current VgpuVersion
	ret := nvmlGetVgpuVersion(&Supported, &Current)
	return Supported, Current, ret
}

// nvml.SetVgpuVersion()
func SetVgpuVersion(VgpuVersion *VgpuVersion) Return {
	return nvmlSetVgpuVersion(VgpuVersion)
}

// nvml.VgpuInstanceClearAccountingPids()
func VgpuInstanceClearAccountingPids(VgpuInstance VgpuInstance) Return {
	return nvmlVgpuInstanceClearAccountingPids(VgpuInstance)
}

func (VgpuInstance VgpuInstance) ClearAccountingPids() Return {
	return VgpuInstanceClearAccountingPids(VgpuInstance)
}

// nvml.VgpuInstanceGetMdevUUID()
func VgpuInstanceGetMdevUUID(VgpuInstance VgpuInstance) (string, Return) {
	MdevUuid := make([]byte, DEVICE_UUID_BUFFER_SIZE)
	ret := nvmlVgpuInstanceGetMdevUUID(VgpuInstance, &MdevUuid[0], DEVICE_UUID_BUFFER_SIZE)
	return string(MdevUuid[:clen(MdevUuid)]), ret
}

func (VgpuInstance VgpuInstance) GetMdevUUID() (string, Return) {
	return VgpuInstanceGetMdevUUID(VgpuInstance)
}

// nvml.VgpuTypeGetCapabilities()
func VgpuTypeGetCapabilities(VgpuTypeId VgpuTypeId, Capability VgpuCapability) (bool, Return) {
	var CapResult uint32
	ret := nvmlVgpuTypeGetCapabilities(VgpuTypeId, Capability, &CapResult)
	return (CapResult != 0), ret
}

func (VgpuTypeId VgpuTypeId) GetCapabilities(Capability VgpuCapability) (bool, Return) {
	return VgpuTypeGetCapabilities(VgpuTypeId, Capability)
}

// nvml.GetVgpuDriverCapabilities()
func GetVgpuDriverCapabilities(Capability VgpuDriverCapability) (bool, Return) {
	var CapResult uint32
	ret := nvmlGetVgpuDriverCapabilities(Capability, &CapResult)
	return (CapResult != 0), ret
}
