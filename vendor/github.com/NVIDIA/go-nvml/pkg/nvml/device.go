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

// EccBitType
type EccBitType = MemoryErrorType

// nvml.DeviceGetCount()
func DeviceGetCount() (int, Return) {
	var DeviceCount uint32
	ret := nvmlDeviceGetCount(&DeviceCount)
	return int(DeviceCount), ret
}

// nvml.DeviceGetHandleByIndex()
func DeviceGetHandleByIndex(Index int) (Device, Return) {
	var Device Device
	ret := nvmlDeviceGetHandleByIndex(uint32(Index), &Device)
	return Device, ret
}

// nvml.DeviceGetHandleBySerial()
func DeviceGetHandleBySerial(Serial string) (Device, Return) {
	var Device Device
	ret := nvmlDeviceGetHandleBySerial(Serial+string(rune(0)), &Device)
	return Device, ret
}

// nvml.DeviceGetHandleByUUID()
func DeviceGetHandleByUUID(Uuid string) (Device, Return) {
	var Device Device
	ret := nvmlDeviceGetHandleByUUID(Uuid+string(rune(0)), &Device)
	return Device, ret
}

// nvml.DeviceGetHandleByPciBusId()
func DeviceGetHandleByPciBusId(PciBusId string) (Device, Return) {
	var Device Device
	ret := nvmlDeviceGetHandleByPciBusId(PciBusId+string(rune(0)), &Device)
	return Device, ret
}

// nvml.DeviceGetName()
func DeviceGetName(Device Device) (string, Return) {
	Name := make([]byte, DEVICE_NAME_V2_BUFFER_SIZE)
	ret := nvmlDeviceGetName(Device, &Name[0], DEVICE_NAME_V2_BUFFER_SIZE)
	return string(Name[:clen(Name)]), ret
}

func (Device Device) GetName() (string, Return) {
	return DeviceGetName(Device)
}

// nvml.DeviceGetBrand()
func DeviceGetBrand(Device Device) (BrandType, Return) {
	var _type BrandType
	ret := nvmlDeviceGetBrand(Device, &_type)
	return _type, ret
}

func (Device Device) GetBrand() (BrandType, Return) {
	return DeviceGetBrand(Device)
}

// nvml.DeviceGetIndex()
func DeviceGetIndex(Device Device) (int, Return) {
	var Index uint32
	ret := nvmlDeviceGetIndex(Device, &Index)
	return int(Index), ret
}

func (Device Device) GetIndex() (int, Return) {
	return DeviceGetIndex(Device)
}

// nvml.DeviceGetSerial()
func DeviceGetSerial(Device Device) (string, Return) {
	Serial := make([]byte, DEVICE_SERIAL_BUFFER_SIZE)
	ret := nvmlDeviceGetSerial(Device, &Serial[0], DEVICE_SERIAL_BUFFER_SIZE)
	return string(Serial[:clen(Serial)]), ret
}

func (Device Device) GetSerial() (string, Return) {
	return DeviceGetSerial(Device)
}

// nvml.DeviceGetCpuAffinity()
func DeviceGetCpuAffinity(Device Device, NumCPUs int) ([]uint, Return) {
	CpuSetSize := uint32((NumCPUs-1)/int(unsafe.Sizeof(uint(0))) + 1)
	CpuSet := make([]uint, CpuSetSize)
	ret := nvmlDeviceGetCpuAffinity(Device, CpuSetSize, &CpuSet[0])
	return CpuSet, ret
}

func (Device Device) GetCpuAffinity(NumCPUs int) ([]uint, Return) {
	return DeviceGetCpuAffinity(Device, NumCPUs)
}

// nvml.DeviceSetCpuAffinity()
func DeviceSetCpuAffinity(Device Device) Return {
	return nvmlDeviceSetCpuAffinity(Device)
}

func (Device Device) SetCpuAffinity() Return {
	return DeviceSetCpuAffinity(Device)
}

// nvml.DeviceClearCpuAffinity()
func DeviceClearCpuAffinity(Device Device) Return {
	return nvmlDeviceClearCpuAffinity(Device)
}

func (Device Device) ClearCpuAffinity() Return {
	return DeviceClearCpuAffinity(Device)
}

// nvml.DeviceGetMemoryAffinity()
func DeviceGetMemoryAffinity(Device Device, NumNodes int, Scope AffinityScope) ([]uint, Return) {
	NodeSetSize := uint32((NumNodes-1)/int(unsafe.Sizeof(uint(0))) + 1)
	NodeSet := make([]uint, NodeSetSize)
	ret := nvmlDeviceGetMemoryAffinity(Device, NodeSetSize, &NodeSet[0], Scope)
	return NodeSet, ret
}

func (Device Device) GetMemoryAffinity(NumNodes int, Scope AffinityScope) ([]uint, Return) {
	return DeviceGetMemoryAffinity(Device, NumNodes, Scope)
}

// nvml.DeviceGetCpuAffinityWithinScope()
func DeviceGetCpuAffinityWithinScope(Device Device, NumCPUs int, Scope AffinityScope) ([]uint, Return) {
	CpuSetSize := uint32((NumCPUs-1)/int(unsafe.Sizeof(uint(0))) + 1)
	CpuSet := make([]uint, CpuSetSize)
	ret := nvmlDeviceGetCpuAffinityWithinScope(Device, CpuSetSize, &CpuSet[0], Scope)
	return CpuSet, ret
}

func (Device Device) GetCpuAffinityWithinScope(NumCPUs int, Scope AffinityScope) ([]uint, Return) {
	return DeviceGetCpuAffinityWithinScope(Device, NumCPUs, Scope)
}

// nvml.DeviceGetTopologyCommonAncestor()
func DeviceGetTopologyCommonAncestor(Device1 Device, Device2 Device) (GpuTopologyLevel, Return) {
	var PathInfo GpuTopologyLevel
	ret := nvmlDeviceGetTopologyCommonAncestor(Device1, Device2, &PathInfo)
	return PathInfo, ret
}

func (Device1 Device) GetTopologyCommonAncestor(Device2 Device) (GpuTopologyLevel, Return) {
	return DeviceGetTopologyCommonAncestor(Device1, Device2)
}

// nvml.DeviceGetTopologyNearestGpus()
func DeviceGetTopologyNearestGpus(device Device, Level GpuTopologyLevel) ([]Device, Return) {
	var Count uint32
	ret := nvmlDeviceGetTopologyNearestGpus(device, Level, &Count, nil)
	if ret != SUCCESS {
		return nil, ret
	}
	if Count == 0 {
		return []Device{}, ret
	}
	DeviceArray := make([]Device, Count)
	ret = nvmlDeviceGetTopologyNearestGpus(device, Level, &Count, &DeviceArray[0])
	return DeviceArray, ret
}

func (Device Device) GetTopologyNearestGpus(Level GpuTopologyLevel) ([]Device, Return) {
	return DeviceGetTopologyNearestGpus(Device, Level)
}

// nvml.DeviceGetP2PStatus()
func DeviceGetP2PStatus(Device1 Device, Device2 Device, P2pIndex GpuP2PCapsIndex) (GpuP2PStatus, Return) {
	var P2pStatus GpuP2PStatus
	ret := nvmlDeviceGetP2PStatus(Device1, Device2, P2pIndex, &P2pStatus)
	return P2pStatus, ret
}

func (Device1 Device) GetP2PStatus(Device2 Device, P2pIndex GpuP2PCapsIndex) (GpuP2PStatus, Return) {
	return DeviceGetP2PStatus(Device1, Device2, P2pIndex)
}

// nvml.DeviceGetUUID()
func DeviceGetUUID(Device Device) (string, Return) {
	Uuid := make([]byte, DEVICE_UUID_V2_BUFFER_SIZE)
	ret := nvmlDeviceGetUUID(Device, &Uuid[0], DEVICE_UUID_V2_BUFFER_SIZE)
	return string(Uuid[:clen(Uuid)]), ret
}

func (Device Device) GetUUID() (string, Return) {
	return DeviceGetUUID(Device)
}

// nvml.DeviceGetMinorNumber()
func DeviceGetMinorNumber(Device Device) (int, Return) {
	var MinorNumber uint32
	ret := nvmlDeviceGetMinorNumber(Device, &MinorNumber)
	return int(MinorNumber), ret
}

func (Device Device) GetMinorNumber() (int, Return) {
	return DeviceGetMinorNumber(Device)
}

// nvml.DeviceGetBoardPartNumber()
func DeviceGetBoardPartNumber(Device Device) (string, Return) {
	PartNumber := make([]byte, DEVICE_PART_NUMBER_BUFFER_SIZE)
	ret := nvmlDeviceGetBoardPartNumber(Device, &PartNumber[0], DEVICE_PART_NUMBER_BUFFER_SIZE)
	return string(PartNumber[:clen(PartNumber)]), ret
}

func (Device Device) GetBoardPartNumber() (string, Return) {
	return DeviceGetBoardPartNumber(Device)
}

// nvml.DeviceGetInforomVersion()
func DeviceGetInforomVersion(Device Device, Object InforomObject) (string, Return) {
	Version := make([]byte, DEVICE_INFOROM_VERSION_BUFFER_SIZE)
	ret := nvmlDeviceGetInforomVersion(Device, Object, &Version[0], DEVICE_INFOROM_VERSION_BUFFER_SIZE)
	return string(Version[:clen(Version)]), ret
}

func (Device Device) GetInforomVersion(Object InforomObject) (string, Return) {
	return DeviceGetInforomVersion(Device, Object)
}

// nvml.DeviceGetInforomImageVersion()
func DeviceGetInforomImageVersion(Device Device) (string, Return) {
	Version := make([]byte, DEVICE_INFOROM_VERSION_BUFFER_SIZE)
	ret := nvmlDeviceGetInforomImageVersion(Device, &Version[0], DEVICE_INFOROM_VERSION_BUFFER_SIZE)
	return string(Version[:clen(Version)]), ret
}

func (Device Device) GetInforomImageVersion() (string, Return) {
	return DeviceGetInforomImageVersion(Device)
}

// nvml.DeviceGetInforomConfigurationChecksum()
func DeviceGetInforomConfigurationChecksum(Device Device) (uint32, Return) {
	var Checksum uint32
	ret := nvmlDeviceGetInforomConfigurationChecksum(Device, &Checksum)
	return Checksum, ret
}

func (Device Device) GetInforomConfigurationChecksum() (uint32, Return) {
	return DeviceGetInforomConfigurationChecksum(Device)
}

// nvml.DeviceValidateInforom()
func DeviceValidateInforom(Device Device) Return {
	return nvmlDeviceValidateInforom(Device)
}

func (Device Device) ValidateInforom() Return {
	return DeviceValidateInforom(Device)
}

// nvml.DeviceGetDisplayMode()
func DeviceGetDisplayMode(Device Device) (EnableState, Return) {
	var Display EnableState
	ret := nvmlDeviceGetDisplayMode(Device, &Display)
	return Display, ret
}

func (Device Device) GetDisplayMode() (EnableState, Return) {
	return DeviceGetDisplayMode(Device)
}

// nvml.DeviceGetDisplayActive()
func DeviceGetDisplayActive(Device Device) (EnableState, Return) {
	var IsActive EnableState
	ret := nvmlDeviceGetDisplayActive(Device, &IsActive)
	return IsActive, ret
}

func (Device Device) GetDisplayActive() (EnableState, Return) {
	return DeviceGetDisplayActive(Device)
}

// nvml.DeviceGetPersistenceMode()
func DeviceGetPersistenceMode(Device Device) (EnableState, Return) {
	var Mode EnableState
	ret := nvmlDeviceGetPersistenceMode(Device, &Mode)
	return Mode, ret
}

func (Device Device) GetPersistenceMode() (EnableState, Return) {
	return DeviceGetPersistenceMode(Device)
}

// nvml.DeviceGetPciInfo()
func DeviceGetPciInfo(Device Device) (PciInfo, Return) {
	var Pci PciInfo
	ret := nvmlDeviceGetPciInfo(Device, &Pci)
	return Pci, ret
}

func (Device Device) GetPciInfo() (PciInfo, Return) {
	return DeviceGetPciInfo(Device)
}

// nvml.DeviceGetMaxPcieLinkGeneration()
func DeviceGetMaxPcieLinkGeneration(Device Device) (int, Return) {
	var MaxLinkGen uint32
	ret := nvmlDeviceGetMaxPcieLinkGeneration(Device, &MaxLinkGen)
	return int(MaxLinkGen), ret
}

func (Device Device) GetMaxPcieLinkGeneration() (int, Return) {
	return DeviceGetMaxPcieLinkGeneration(Device)
}

// nvml.DeviceGetMaxPcieLinkWidth()
func DeviceGetMaxPcieLinkWidth(Device Device) (int, Return) {
	var MaxLinkWidth uint32
	ret := nvmlDeviceGetMaxPcieLinkWidth(Device, &MaxLinkWidth)
	return int(MaxLinkWidth), ret
}

func (Device Device) GetMaxPcieLinkWidth() (int, Return) {
	return DeviceGetMaxPcieLinkWidth(Device)
}

// nvml.DeviceGetCurrPcieLinkGeneration()
func DeviceGetCurrPcieLinkGeneration(Device Device) (int, Return) {
	var CurrLinkGen uint32
	ret := nvmlDeviceGetCurrPcieLinkGeneration(Device, &CurrLinkGen)
	return int(CurrLinkGen), ret
}

func (Device Device) GetCurrPcieLinkGeneration() (int, Return) {
	return DeviceGetCurrPcieLinkGeneration(Device)
}

// nvml.DeviceGetCurrPcieLinkWidth()
func DeviceGetCurrPcieLinkWidth(Device Device) (int, Return) {
	var CurrLinkWidth uint32
	ret := nvmlDeviceGetCurrPcieLinkWidth(Device, &CurrLinkWidth)
	return int(CurrLinkWidth), ret
}

func (Device Device) GetCurrPcieLinkWidth() (int, Return) {
	return DeviceGetCurrPcieLinkWidth(Device)
}

// nvml.DeviceGetPcieThroughput()
func DeviceGetPcieThroughput(Device Device, Counter PcieUtilCounter) (uint32, Return) {
	var Value uint32
	ret := nvmlDeviceGetPcieThroughput(Device, Counter, &Value)
	return Value, ret
}

func (Device Device) GetPcieThroughput(Counter PcieUtilCounter) (uint32, Return) {
	return DeviceGetPcieThroughput(Device, Counter)
}

// nvml.DeviceGetPcieReplayCounter()
func DeviceGetPcieReplayCounter(Device Device) (int, Return) {
	var Value uint32
	ret := nvmlDeviceGetPcieReplayCounter(Device, &Value)
	return int(Value), ret
}

func (Device Device) GetPcieReplayCounter() (int, Return) {
	return DeviceGetPcieReplayCounter(Device)
}

// nvml.nvmlDeviceGetClockInfo()
func DeviceGetClockInfo(Device Device, _type ClockType) (uint32, Return) {
	var Clock uint32
	ret := nvmlDeviceGetClockInfo(Device, _type, &Clock)
	return Clock, ret
}

func (Device Device) GetClockInfo(_type ClockType) (uint32, Return) {
	return DeviceGetClockInfo(Device, _type)
}

// nvml.DeviceGetMaxClockInfo()
func DeviceGetMaxClockInfo(Device Device, _type ClockType) (uint32, Return) {
	var Clock uint32
	ret := nvmlDeviceGetMaxClockInfo(Device, _type, &Clock)
	return Clock, ret
}

func (Device Device) GetMaxClockInfo(_type ClockType) (uint32, Return) {
	return DeviceGetMaxClockInfo(Device, _type)
}

// nvml.DeviceGetApplicationsClock()
func DeviceGetApplicationsClock(Device Device, ClockType ClockType) (uint32, Return) {
	var ClockMHz uint32
	ret := nvmlDeviceGetApplicationsClock(Device, ClockType, &ClockMHz)
	return ClockMHz, ret
}

func (Device Device) GetApplicationsClock(ClockType ClockType) (uint32, Return) {
	return DeviceGetApplicationsClock(Device, ClockType)
}

// nvml.DeviceGetDefaultApplicationsClock()
func DeviceGetDefaultApplicationsClock(Device Device, ClockType ClockType) (uint32, Return) {
	var ClockMHz uint32
	ret := nvmlDeviceGetDefaultApplicationsClock(Device, ClockType, &ClockMHz)
	return ClockMHz, ret
}

func (Device Device) GetDefaultApplicationsClock(ClockType ClockType) (uint32, Return) {
	return DeviceGetDefaultApplicationsClock(Device, ClockType)
}

// nvml.DeviceResetApplicationsClocks()
func DeviceResetApplicationsClocks(Device Device) Return {
	return nvmlDeviceResetApplicationsClocks(Device)
}

func (Device Device) ResetApplicationsClocks() Return {
	return DeviceResetApplicationsClocks(Device)
}

// nvml.DeviceGetClock()
func DeviceGetClock(Device Device, ClockType ClockType, ClockId ClockId) (uint32, Return) {
	var ClockMHz uint32
	ret := nvmlDeviceGetClock(Device, ClockType, ClockId, &ClockMHz)
	return ClockMHz, ret
}

func (Device Device) GetClock(ClockType ClockType, ClockId ClockId) (uint32, Return) {
	return DeviceGetClock(Device, ClockType, ClockId)
}

// nvml.DeviceGetMaxCustomerBoostClock()
func DeviceGetMaxCustomerBoostClock(Device Device, ClockType ClockType) (uint32, Return) {
	var ClockMHz uint32
	ret := nvmlDeviceGetMaxCustomerBoostClock(Device, ClockType, &ClockMHz)
	return ClockMHz, ret
}

func (Device Device) GetMaxCustomerBoostClock(ClockType ClockType) (uint32, Return) {
	return DeviceGetMaxCustomerBoostClock(Device, ClockType)
}

// nvml.DeviceGetSupportedMemoryClocks()
func DeviceGetSupportedMemoryClocks(Device Device) (int, uint32, Return) {
	var Count, ClocksMHz uint32
	ret := nvmlDeviceGetSupportedMemoryClocks(Device, &Count, &ClocksMHz)
	return int(Count), ClocksMHz, ret
}

func (Device Device) GetSupportedMemoryClocks() (int, uint32, Return) {
	return DeviceGetSupportedMemoryClocks(Device)
}

// nvml.DeviceGetSupportedGraphicsClocks()
func DeviceGetSupportedGraphicsClocks(Device Device, MemoryClockMHz int) (int, uint32, Return) {
	var Count, ClocksMHz uint32
	ret := nvmlDeviceGetSupportedGraphicsClocks(Device, uint32(MemoryClockMHz), &Count, &ClocksMHz)
	return int(Count), ClocksMHz, ret
}

func (Device Device) GetSupportedGraphicsClocks(MemoryClockMHz int) (int, uint32, Return) {
	return DeviceGetSupportedGraphicsClocks(Device, MemoryClockMHz)
}

// nvml.DeviceGetAutoBoostedClocksEnabled()
func DeviceGetAutoBoostedClocksEnabled(Device Device) (EnableState, EnableState, Return) {
	var IsEnabled, DefaultIsEnabled EnableState
	ret := nvmlDeviceGetAutoBoostedClocksEnabled(Device, &IsEnabled, &DefaultIsEnabled)
	return IsEnabled, DefaultIsEnabled, ret
}

func (Device Device) GetAutoBoostedClocksEnabled() (EnableState, EnableState, Return) {
	return DeviceGetAutoBoostedClocksEnabled(Device)
}

// nvml.DeviceSetAutoBoostedClocksEnabled()
func DeviceSetAutoBoostedClocksEnabled(Device Device, Enabled EnableState) Return {
	return nvmlDeviceSetAutoBoostedClocksEnabled(Device, Enabled)
}

func (Device Device) SetAutoBoostedClocksEnabled(Enabled EnableState) Return {
	return DeviceSetAutoBoostedClocksEnabled(Device, Enabled)
}

// nvml.DeviceSetDefaultAutoBoostedClocksEnabled()
func DeviceSetDefaultAutoBoostedClocksEnabled(Device Device, Enabled EnableState, Flags uint32) Return {
	return nvmlDeviceSetDefaultAutoBoostedClocksEnabled(Device, Enabled, Flags)
}

func (Device Device) SetDefaultAutoBoostedClocksEnabled(Enabled EnableState, Flags uint32) Return {
	return DeviceSetDefaultAutoBoostedClocksEnabled(Device, Enabled, Flags)
}

// nvml.DeviceGetFanSpeed()
func DeviceGetFanSpeed(Device Device) (uint32, Return) {
	var Speed uint32
	ret := nvmlDeviceGetFanSpeed(Device, &Speed)
	return Speed, ret
}

func (Device Device) GetFanSpeed() (uint32, Return) {
	return DeviceGetFanSpeed(Device)
}

// nvml.DeviceGetFanSpeed_v2()
func DeviceGetFanSpeed_v2(Device Device, Fan int) (uint32, Return) {
	var Speed uint32
	ret := nvmlDeviceGetFanSpeed_v2(Device, uint32(Fan), &Speed)
	return Speed, ret
}

func (Device Device) GetFanSpeed_v2(Fan int) (uint32, Return) {
	return DeviceGetFanSpeed_v2(Device, Fan)
}

// nvml.DeviceGetNumFans()
func DeviceGetNumFans(Device Device) (int, Return) {
	var NumFans uint32
	ret := nvmlDeviceGetNumFans(Device, &NumFans)
	return int(NumFans), ret
}

func (Device Device) GetNumFans() (int, Return) {
	return DeviceGetNumFans(Device)
}

// nvml.DeviceGetTemperature()
func DeviceGetTemperature(Device Device, SensorType TemperatureSensors) (uint32, Return) {
	var Temp uint32
	ret := nvmlDeviceGetTemperature(Device, SensorType, &Temp)
	return Temp, ret
}

func (Device Device) GetTemperature(SensorType TemperatureSensors) (uint32, Return) {
	return DeviceGetTemperature(Device, SensorType)
}

// nvml.DeviceGetTemperatureThreshold()
func DeviceGetTemperatureThreshold(Device Device, ThresholdType TemperatureThresholds) (uint32, Return) {
	var Temp uint32
	ret := nvmlDeviceGetTemperatureThreshold(Device, ThresholdType, &Temp)
	return Temp, ret
}

func (Device Device) GetTemperatureThreshold(ThresholdType TemperatureThresholds) (uint32, Return) {
	return DeviceGetTemperatureThreshold(Device, ThresholdType)
}

// nvml.DeviceSetTemperatureThreshold()
func DeviceSetTemperatureThreshold(Device Device, ThresholdType TemperatureThresholds, Temp int) Return {
	t := int32(Temp)
	ret := nvmlDeviceSetTemperatureThreshold(Device, ThresholdType, &t)
	return ret
}

func (Device Device) SetTemperatureThreshold(ThresholdType TemperatureThresholds, Temp int) Return {
	return DeviceSetTemperatureThreshold(Device, ThresholdType, Temp)
}

// nvml.DeviceGetPerformanceState()
func DeviceGetPerformanceState(Device Device) (Pstates, Return) {
	var PState Pstates
	ret := nvmlDeviceGetPerformanceState(Device, &PState)
	return PState, ret
}

func (Device Device) GetPerformanceState() (Pstates, Return) {
	return DeviceGetPerformanceState(Device)
}

// nvml.DeviceGetCurrentClocksThrottleReasons()
func DeviceGetCurrentClocksThrottleReasons(Device Device) (uint64, Return) {
	var ClocksThrottleReasons uint64
	ret := nvmlDeviceGetCurrentClocksThrottleReasons(Device, &ClocksThrottleReasons)
	return ClocksThrottleReasons, ret
}

func (Device Device) GetCurrentClocksThrottleReasons() (uint64, Return) {
	return DeviceGetCurrentClocksThrottleReasons(Device)
}

// nvml.DeviceGetSupportedClocksThrottleReasons()
func DeviceGetSupportedClocksThrottleReasons(Device Device) (uint64, Return) {
	var SupportedClocksThrottleReasons uint64
	ret := nvmlDeviceGetSupportedClocksThrottleReasons(Device, &SupportedClocksThrottleReasons)
	return SupportedClocksThrottleReasons, ret
}

func (Device Device) GetSupportedClocksThrottleReasons() (uint64, Return) {
	return DeviceGetSupportedClocksThrottleReasons(Device)
}

// nvml.DeviceGetPowerState()
func DeviceGetPowerState(Device Device) (Pstates, Return) {
	var PState Pstates
	ret := nvmlDeviceGetPowerState(Device, &PState)
	return PState, ret
}

func (Device Device) GetPowerState() (Pstates, Return) {
	return DeviceGetPowerState(Device)
}

// nvml.DeviceGetPowerManagementMode()
func DeviceGetPowerManagementMode(Device Device) (EnableState, Return) {
	var Mode EnableState
	ret := nvmlDeviceGetPowerManagementMode(Device, &Mode)
	return Mode, ret
}

func (Device Device) GetPowerManagementMode() (EnableState, Return) {
	return DeviceGetPowerManagementMode(Device)
}

// nvml.DeviceGetPowerManagementLimit()
func DeviceGetPowerManagementLimit(Device Device) (uint32, Return) {
	var Limit uint32
	ret := nvmlDeviceGetPowerManagementLimit(Device, &Limit)
	return Limit, ret
}

func (Device Device) GetPowerManagementLimit() (uint32, Return) {
	return DeviceGetPowerManagementLimit(Device)
}

// nvml.DeviceGetPowerManagementLimitConstraints()
func DeviceGetPowerManagementLimitConstraints(Device Device) (uint32, uint32, Return) {
	var MinLimit, MaxLimit uint32
	ret := nvmlDeviceGetPowerManagementLimitConstraints(Device, &MinLimit, &MaxLimit)
	return MinLimit, MaxLimit, ret
}

func (Device Device) GetPowerManagementLimitConstraints() (uint32, uint32, Return) {
	return DeviceGetPowerManagementLimitConstraints(Device)
}

// nvml.DeviceGetPowerManagementDefaultLimit()
func DeviceGetPowerManagementDefaultLimit(Device Device) (uint32, Return) {
	var DefaultLimit uint32
	ret := nvmlDeviceGetPowerManagementDefaultLimit(Device, &DefaultLimit)
	return DefaultLimit, ret
}

func (Device Device) GetPowerManagementDefaultLimit() (uint32, Return) {
	return DeviceGetPowerManagementDefaultLimit(Device)
}

// nvml.DeviceGetPowerUsage()
func DeviceGetPowerUsage(Device Device) (uint32, Return) {
	var Power uint32
	ret := nvmlDeviceGetPowerUsage(Device, &Power)
	return Power, ret
}

func (Device Device) GetPowerUsage() (uint32, Return) {
	return DeviceGetPowerUsage(Device)
}

// nvml.DeviceGetTotalEnergyConsumption()
func DeviceGetTotalEnergyConsumption(Device Device) (uint64, Return) {
	var Energy uint64
	ret := nvmlDeviceGetTotalEnergyConsumption(Device, &Energy)
	return Energy, ret
}

func (Device Device) GetTotalEnergyConsumption() (uint64, Return) {
	return DeviceGetTotalEnergyConsumption(Device)
}

// nvml.DeviceGetEnforcedPowerLimit()
func DeviceGetEnforcedPowerLimit(Device Device) (uint32, Return) {
	var Limit uint32
	ret := nvmlDeviceGetEnforcedPowerLimit(Device, &Limit)
	return Limit, ret
}

func (Device Device) GetEnforcedPowerLimit() (uint32, Return) {
	return DeviceGetEnforcedPowerLimit(Device)
}

// nvml.DeviceGetGpuOperationMode()
func DeviceGetGpuOperationMode(Device Device) (GpuOperationMode, GpuOperationMode, Return) {
	var Current, Pending GpuOperationMode
	ret := nvmlDeviceGetGpuOperationMode(Device, &Current, &Pending)
	return Current, Pending, ret
}

func (Device Device) GetGpuOperationMode() (GpuOperationMode, GpuOperationMode, Return) {
	return DeviceGetGpuOperationMode(Device)
}

// nvml.DeviceGetMemoryInfo()
func DeviceGetMemoryInfo(Device Device) (Memory, Return) {
	var Memory Memory
	ret := nvmlDeviceGetMemoryInfo(Device, &Memory)
	return Memory, ret
}

func (Device Device) GetMemoryInfo() (Memory, Return) {
	return DeviceGetMemoryInfo(Device)
}

// nvml.DeviceGetMemoryInfo_v2()
func DeviceGetMemoryInfo_v2(Device Device) (Memory_v2, Return) {
	var Memory Memory_v2
	Memory.Version = STRUCT_VERSION(Memory, 2)
	ret := nvmlDeviceGetMemoryInfo_v2(Device, &Memory)
	return Memory, ret
}

func (Device Device) GetMemoryInfo_v2() (Memory_v2, Return) {
	return DeviceGetMemoryInfo_v2(Device)
}

// nvml.DeviceGetComputeMode()
func DeviceGetComputeMode(Device Device) (ComputeMode, Return) {
	var Mode ComputeMode
	ret := nvmlDeviceGetComputeMode(Device, &Mode)
	return Mode, ret
}

func (Device Device) GetComputeMode() (ComputeMode, Return) {
	return DeviceGetComputeMode(Device)
}

// nvml.DeviceGetCudaComputeCapability()
func DeviceGetCudaComputeCapability(Device Device) (int, int, Return) {
	var Major, Minor int32
	ret := nvmlDeviceGetCudaComputeCapability(Device, &Major, &Minor)
	return int(Major), int(Minor), ret
}

func (Device Device) GetCudaComputeCapability() (int, int, Return) {
	return DeviceGetCudaComputeCapability(Device)
}

// nvml.DeviceGetEccMode()
func DeviceGetEccMode(Device Device) (EnableState, EnableState, Return) {
	var Current, Pending EnableState
	ret := nvmlDeviceGetEccMode(Device, &Current, &Pending)
	return Current, Pending, ret
}

func (Device Device) GetEccMode() (EnableState, EnableState, Return) {
	return DeviceGetEccMode(Device)
}

// nvml.DeviceGetBoardId()
func DeviceGetBoardId(Device Device) (uint32, Return) {
	var BoardId uint32
	ret := nvmlDeviceGetBoardId(Device, &BoardId)
	return BoardId, ret
}

func (Device Device) GetBoardId() (uint32, Return) {
	return DeviceGetBoardId(Device)
}

// nvml.DeviceGetMultiGpuBoard()
func DeviceGetMultiGpuBoard(Device Device) (int, Return) {
	var MultiGpuBool uint32
	ret := nvmlDeviceGetMultiGpuBoard(Device, &MultiGpuBool)
	return int(MultiGpuBool), ret
}

func (Device Device) GetMultiGpuBoard() (int, Return) {
	return DeviceGetMultiGpuBoard(Device)
}

// nvml.DeviceGetTotalEccErrors()
func DeviceGetTotalEccErrors(Device Device, ErrorType MemoryErrorType, CounterType EccCounterType) (uint64, Return) {
	var EccCounts uint64
	ret := nvmlDeviceGetTotalEccErrors(Device, ErrorType, CounterType, &EccCounts)
	return EccCounts, ret
}

func (Device Device) GetTotalEccErrors(ErrorType MemoryErrorType, CounterType EccCounterType) (uint64, Return) {
	return DeviceGetTotalEccErrors(Device, ErrorType, CounterType)
}

// nvml.DeviceGetDetailedEccErrors()
func DeviceGetDetailedEccErrors(Device Device, ErrorType MemoryErrorType, CounterType EccCounterType) (EccErrorCounts, Return) {
	var EccCounts EccErrorCounts
	ret := nvmlDeviceGetDetailedEccErrors(Device, ErrorType, CounterType, &EccCounts)
	return EccCounts, ret
}

func (Device Device) GetDetailedEccErrors(ErrorType MemoryErrorType, CounterType EccCounterType) (EccErrorCounts, Return) {
	return DeviceGetDetailedEccErrors(Device, ErrorType, CounterType)
}

// nvml.DeviceGetMemoryErrorCounter()
func DeviceGetMemoryErrorCounter(Device Device, ErrorType MemoryErrorType, CounterType EccCounterType, LocationType MemoryLocation) (uint64, Return) {
	var Count uint64
	ret := nvmlDeviceGetMemoryErrorCounter(Device, ErrorType, CounterType, LocationType, &Count)
	return Count, ret
}

func (Device Device) GetMemoryErrorCounter(ErrorType MemoryErrorType, CounterType EccCounterType, LocationType MemoryLocation) (uint64, Return) {
	return DeviceGetMemoryErrorCounter(Device, ErrorType, CounterType, LocationType)
}

// nvml.DeviceGetUtilizationRates()
func DeviceGetUtilizationRates(Device Device) (Utilization, Return) {
	var Utilization Utilization
	ret := nvmlDeviceGetUtilizationRates(Device, &Utilization)
	return Utilization, ret
}

func (Device Device) GetUtilizationRates() (Utilization, Return) {
	return DeviceGetUtilizationRates(Device)
}

// nvml.DeviceGetEncoderUtilization()
func DeviceGetEncoderUtilization(Device Device) (uint32, uint32, Return) {
	var Utilization, SamplingPeriodUs uint32
	ret := nvmlDeviceGetEncoderUtilization(Device, &Utilization, &SamplingPeriodUs)
	return Utilization, SamplingPeriodUs, ret
}

func (Device Device) GetEncoderUtilization() (uint32, uint32, Return) {
	return DeviceGetEncoderUtilization(Device)
}

// nvml.DeviceGetEncoderCapacity()
func DeviceGetEncoderCapacity(Device Device, EncoderQueryType EncoderType) (int, Return) {
	var EncoderCapacity uint32
	ret := nvmlDeviceGetEncoderCapacity(Device, EncoderQueryType, &EncoderCapacity)
	return int(EncoderCapacity), ret
}

func (Device Device) GetEncoderCapacity(EncoderQueryType EncoderType) (int, Return) {
	return DeviceGetEncoderCapacity(Device, EncoderQueryType)
}

// nvml.DeviceGetEncoderStats()
func DeviceGetEncoderStats(Device Device) (int, uint32, uint32, Return) {
	var SessionCount, AverageFps, AverageLatency uint32
	ret := nvmlDeviceGetEncoderStats(Device, &SessionCount, &AverageFps, &AverageLatency)
	return int(SessionCount), AverageFps, AverageLatency, ret
}

func (Device Device) GetEncoderStats() (int, uint32, uint32, Return) {
	return DeviceGetEncoderStats(Device)
}

// nvml.DeviceGetEncoderSessions()
func DeviceGetEncoderSessions(Device Device) ([]EncoderSessionInfo, Return) {
	var SessionCount uint32 = 1 // Will be reduced upon returning
	for {
		SessionInfos := make([]EncoderSessionInfo, SessionCount)
		ret := nvmlDeviceGetEncoderSessions(Device, &SessionCount, &SessionInfos[0])
		if ret == SUCCESS {
			return SessionInfos[:SessionCount], ret
		}
		if ret != ERROR_INSUFFICIENT_SIZE {
			return nil, ret
		}
		SessionCount *= 2
	}
}

func (Device Device) GetEncoderSessions() ([]EncoderSessionInfo, Return) {
	return DeviceGetEncoderSessions(Device)
}

// nvml.DeviceGetDecoderUtilization()
func DeviceGetDecoderUtilization(Device Device) (uint32, uint32, Return) {
	var Utilization, SamplingPeriodUs uint32
	ret := nvmlDeviceGetDecoderUtilization(Device, &Utilization, &SamplingPeriodUs)
	return Utilization, SamplingPeriodUs, ret
}

func (Device Device) GetDecoderUtilization() (uint32, uint32, Return) {
	return DeviceGetDecoderUtilization(Device)
}

// nvml.DeviceGetFBCStats()
func DeviceGetFBCStats(Device Device) (FBCStats, Return) {
	var FbcStats FBCStats
	ret := nvmlDeviceGetFBCStats(Device, &FbcStats)
	return FbcStats, ret
}

func (Device Device) GetFBCStats() (FBCStats, Return) {
	return DeviceGetFBCStats(Device)
}

// nvml.DeviceGetFBCSessions()
func DeviceGetFBCSessions(Device Device) ([]FBCSessionInfo, Return) {
	var SessionCount uint32 = 1 // Will be reduced upon returning
	for {
		SessionInfo := make([]FBCSessionInfo, SessionCount)
		ret := nvmlDeviceGetFBCSessions(Device, &SessionCount, &SessionInfo[0])
		if ret == SUCCESS {
			return SessionInfo[:SessionCount], ret
		}
		if ret != ERROR_INSUFFICIENT_SIZE {
			return nil, ret
		}
		SessionCount *= 2
	}
}

func (Device Device) GetFBCSessions() ([]FBCSessionInfo, Return) {
	return DeviceGetFBCSessions(Device)
}

// nvml.DeviceGetDriverModel()
func DeviceGetDriverModel(Device Device) (DriverModel, DriverModel, Return) {
	var Current, Pending DriverModel
	ret := nvmlDeviceGetDriverModel(Device, &Current, &Pending)
	return Current, Pending, ret
}

func (Device Device) GetDriverModel() (DriverModel, DriverModel, Return) {
	return DeviceGetDriverModel(Device)
}

// nvml.DeviceGetVbiosVersion()
func DeviceGetVbiosVersion(Device Device) (string, Return) {
	Version := make([]byte, DEVICE_VBIOS_VERSION_BUFFER_SIZE)
	ret := nvmlDeviceGetVbiosVersion(Device, &Version[0], DEVICE_VBIOS_VERSION_BUFFER_SIZE)
	return string(Version[:clen(Version)]), ret
}

func (Device Device) GetVbiosVersion() (string, Return) {
	return DeviceGetVbiosVersion(Device)
}

// nvml.DeviceGetBridgeChipInfo()
func DeviceGetBridgeChipInfo(Device Device) (BridgeChipHierarchy, Return) {
	var BridgeHierarchy BridgeChipHierarchy
	ret := nvmlDeviceGetBridgeChipInfo(Device, &BridgeHierarchy)
	return BridgeHierarchy, ret
}

func (Device Device) GetBridgeChipInfo() (BridgeChipHierarchy, Return) {
	return DeviceGetBridgeChipInfo(Device)
}

// nvml.DeviceGetComputeRunningProcesses()
func deviceGetComputeRunningProcesses_v1(Device Device) ([]ProcessInfo, Return) {
	var InfoCount uint32 = 1 // Will be reduced upon returning
	for {
		Infos := make([]ProcessInfo_v1, InfoCount)
		ret := nvmlDeviceGetComputeRunningProcesses_v1(Device, &InfoCount, &Infos[0])
		if ret == SUCCESS {
			return ProcessInfo_v1Slice(Infos[:InfoCount]).ToProcessInfoSlice(), ret
		}
		if ret != ERROR_INSUFFICIENT_SIZE {
			return nil, ret
		}
		InfoCount *= 2
	}
}

func deviceGetComputeRunningProcesses_v2(Device Device) ([]ProcessInfo, Return) {
	var InfoCount uint32 = 1 // Will be reduced upon returning
	for {
		Infos := make([]ProcessInfo_v2, InfoCount)
		ret := nvmlDeviceGetComputeRunningProcesses_v2(Device, &InfoCount, &Infos[0])
		if ret == SUCCESS {
			return ProcessInfo_v2Slice(Infos[:InfoCount]).ToProcessInfoSlice(), ret
		}
		if ret != ERROR_INSUFFICIENT_SIZE {
			return nil, ret
		}
		InfoCount *= 2
	}
}

func deviceGetComputeRunningProcesses_v3(Device Device) ([]ProcessInfo, Return) {
	var InfoCount uint32 = 1 // Will be reduced upon returning
	for {
		Infos := make([]ProcessInfo, InfoCount)
		ret := nvmlDeviceGetComputeRunningProcesses_v3(Device, &InfoCount, &Infos[0])
		if ret == SUCCESS {
			return Infos[:InfoCount], ret
		}
		if ret != ERROR_INSUFFICIENT_SIZE {
			return nil, ret
		}
		InfoCount *= 2
	}
}

func (Device Device) GetComputeRunningProcesses() ([]ProcessInfo, Return) {
	return DeviceGetComputeRunningProcesses(Device)
}

// nvml.DeviceGetGraphicsRunningProcesses()
func deviceGetGraphicsRunningProcesses_v1(Device Device) ([]ProcessInfo, Return) {
	var InfoCount uint32 = 1 // Will be reduced upon returning
	for {
		Infos := make([]ProcessInfo_v1, InfoCount)
		ret := nvmlDeviceGetGraphicsRunningProcesses_v1(Device, &InfoCount, &Infos[0])
		if ret == SUCCESS {
			return ProcessInfo_v1Slice(Infos[:InfoCount]).ToProcessInfoSlice(), ret
		}
		if ret != ERROR_INSUFFICIENT_SIZE {
			return nil, ret
		}
		InfoCount *= 2
	}
}

func deviceGetGraphicsRunningProcesses_v2(Device Device) ([]ProcessInfo, Return) {
	var InfoCount uint32 = 1 // Will be reduced upon returning
	for {
		Infos := make([]ProcessInfo_v2, InfoCount)
		ret := nvmlDeviceGetGraphicsRunningProcesses_v2(Device, &InfoCount, &Infos[0])
		if ret == SUCCESS {
			return ProcessInfo_v2Slice(Infos[:InfoCount]).ToProcessInfoSlice(), ret
		}
		if ret != ERROR_INSUFFICIENT_SIZE {
			return nil, ret
		}
		InfoCount *= 2
	}
}

func deviceGetGraphicsRunningProcesses_v3(Device Device) ([]ProcessInfo, Return) {
	var InfoCount uint32 = 1 // Will be reduced upon returning
	for {
		Infos := make([]ProcessInfo, InfoCount)
		ret := nvmlDeviceGetGraphicsRunningProcesses_v3(Device, &InfoCount, &Infos[0])
		if ret == SUCCESS {
			return Infos[:InfoCount], ret
		}
		if ret != ERROR_INSUFFICIENT_SIZE {
			return nil, ret
		}
		InfoCount *= 2
	}
}

func (Device Device) GetGraphicsRunningProcesses() ([]ProcessInfo, Return) {
	return DeviceGetGraphicsRunningProcesses(Device)
}

// nvml.DeviceGetMPSComputeRunningProcesses()
func deviceGetMPSComputeRunningProcesses_v1(Device Device) ([]ProcessInfo, Return) {
	var InfoCount uint32 = 1 // Will be reduced upon returning
	for {
		Infos := make([]ProcessInfo_v1, InfoCount)
		ret := nvmlDeviceGetMPSComputeRunningProcesses_v1(Device, &InfoCount, &Infos[0])
		if ret == SUCCESS {
			return ProcessInfo_v1Slice(Infos[:InfoCount]).ToProcessInfoSlice(), ret
		}
		if ret != ERROR_INSUFFICIENT_SIZE {
			return nil, ret
		}
		InfoCount *= 2
	}
}

func deviceGetMPSComputeRunningProcesses_v2(Device Device) ([]ProcessInfo, Return) {
	var InfoCount uint32 = 1 // Will be reduced upon returning
	for {
		Infos := make([]ProcessInfo_v2, InfoCount)
		ret := nvmlDeviceGetMPSComputeRunningProcesses_v2(Device, &InfoCount, &Infos[0])
		if ret == SUCCESS {
			return ProcessInfo_v2Slice(Infos[:InfoCount]).ToProcessInfoSlice(), ret
		}
		if ret != ERROR_INSUFFICIENT_SIZE {
			return nil, ret
		}
		InfoCount *= 2
	}
}

func deviceGetMPSComputeRunningProcesses_v3(Device Device) ([]ProcessInfo, Return) {
	var InfoCount uint32 = 1 // Will be reduced upon returning
	for {
		Infos := make([]ProcessInfo, InfoCount)
		ret := nvmlDeviceGetMPSComputeRunningProcesses_v3(Device, &InfoCount, &Infos[0])
		if ret == SUCCESS {
			return Infos[:InfoCount], ret
		}
		if ret != ERROR_INSUFFICIENT_SIZE {
			return nil, ret
		}
		InfoCount *= 2
	}
}

func (Device Device) GetMPSComputeRunningProcesses() ([]ProcessInfo, Return) {
	return DeviceGetMPSComputeRunningProcesses(Device)
}

// nvml.DeviceOnSameBoard()
func DeviceOnSameBoard(Device1 Device, Device2 Device) (int, Return) {
	var OnSameBoard int32
	ret := nvmlDeviceOnSameBoard(Device1, Device2, &OnSameBoard)
	return int(OnSameBoard), ret
}

func (Device1 Device) OnSameBoard(Device2 Device) (int, Return) {
	return DeviceOnSameBoard(Device1, Device2)
}

// nvml.DeviceGetAPIRestriction()
func DeviceGetAPIRestriction(Device Device, ApiType RestrictedAPI) (EnableState, Return) {
	var IsRestricted EnableState
	ret := nvmlDeviceGetAPIRestriction(Device, ApiType, &IsRestricted)
	return IsRestricted, ret
}

func (Device Device) GetAPIRestriction(ApiType RestrictedAPI) (EnableState, Return) {
	return DeviceGetAPIRestriction(Device, ApiType)
}

// nvml.DeviceGetSamples()
func DeviceGetSamples(Device Device, _type SamplingType, LastSeenTimeStamp uint64) (ValueType, []Sample, Return) {
	var SampleValType ValueType
	var SampleCount uint32
	ret := nvmlDeviceGetSamples(Device, _type, LastSeenTimeStamp, &SampleValType, &SampleCount, nil)
	if ret != SUCCESS {
		return SampleValType, nil, ret
	}
	if SampleCount == 0 {
		return SampleValType, []Sample{}, ret
	}
	Samples := make([]Sample, SampleCount)
	ret = nvmlDeviceGetSamples(Device, _type, LastSeenTimeStamp, &SampleValType, &SampleCount, &Samples[0])
	return SampleValType, Samples, ret
}

func (Device Device) GetSamples(_type SamplingType, LastSeenTimeStamp uint64) (ValueType, []Sample, Return) {
	return DeviceGetSamples(Device, _type, LastSeenTimeStamp)
}

// nvml.DeviceGetBAR1MemoryInfo()
func DeviceGetBAR1MemoryInfo(Device Device) (BAR1Memory, Return) {
	var Bar1Memory BAR1Memory
	ret := nvmlDeviceGetBAR1MemoryInfo(Device, &Bar1Memory)
	return Bar1Memory, ret
}

func (Device Device) GetBAR1MemoryInfo() (BAR1Memory, Return) {
	return DeviceGetBAR1MemoryInfo(Device)
}

// nvml.DeviceGetViolationStatus()
func DeviceGetViolationStatus(Device Device, PerfPolicyType PerfPolicyType) (ViolationTime, Return) {
	var ViolTime ViolationTime
	ret := nvmlDeviceGetViolationStatus(Device, PerfPolicyType, &ViolTime)
	return ViolTime, ret
}

func (Device Device) GetViolationStatus(PerfPolicyType PerfPolicyType) (ViolationTime, Return) {
	return DeviceGetViolationStatus(Device, PerfPolicyType)
}

// nvml.DeviceGetIrqNum()
func DeviceGetIrqNum(Device Device) (int, Return) {
	var IrqNum uint32
	ret := nvmlDeviceGetIrqNum(Device, &IrqNum)
	return int(IrqNum), ret
}

func (Device Device) GetIrqNum() (int, Return) {
	return DeviceGetIrqNum(Device)
}

// nvml.DeviceGetNumGpuCores()
func DeviceGetNumGpuCores(Device Device) (int, Return) {
	var NumCores uint32
	ret := nvmlDeviceGetNumGpuCores(Device, &NumCores)
	return int(NumCores), ret
}

func (Device Device) GetNumGpuCores() (int, Return) {
	return DeviceGetNumGpuCores(Device)
}

// nvml.DeviceGetPowerSource()
func DeviceGetPowerSource(Device Device) (PowerSource, Return) {
	var PowerSource PowerSource
	ret := nvmlDeviceGetPowerSource(Device, &PowerSource)
	return PowerSource, ret
}

func (Device Device) GetPowerSource() (PowerSource, Return) {
	return DeviceGetPowerSource(Device)
}

// nvml.DeviceGetMemoryBusWidth()
func DeviceGetMemoryBusWidth(Device Device) (uint32, Return) {
	var BusWidth uint32
	ret := nvmlDeviceGetMemoryBusWidth(Device, &BusWidth)
	return BusWidth, ret
}

func (Device Device) GetMemoryBusWidth() (uint32, Return) {
	return DeviceGetMemoryBusWidth(Device)
}

// nvml.DeviceGetPcieLinkMaxSpeed()
func DeviceGetPcieLinkMaxSpeed(Device Device) (uint32, Return) {
	var MaxSpeed uint32
	ret := nvmlDeviceGetPcieLinkMaxSpeed(Device, &MaxSpeed)
	return MaxSpeed, ret
}

func (Device Device) GetPcieLinkMaxSpeed() (uint32, Return) {
	return DeviceGetPcieLinkMaxSpeed(Device)
}

// nvml.DeviceGetAdaptiveClockInfoStatus()
func DeviceGetAdaptiveClockInfoStatus(Device Device) (uint32, Return) {
	var AdaptiveClockStatus uint32
	ret := nvmlDeviceGetAdaptiveClockInfoStatus(Device, &AdaptiveClockStatus)
	return AdaptiveClockStatus, ret
}

func (Device Device) GetAdaptiveClockInfoStatus() (uint32, Return) {
	return DeviceGetAdaptiveClockInfoStatus(Device)
}

// nvml.DeviceGetAccountingMode()
func DeviceGetAccountingMode(Device Device) (EnableState, Return) {
	var Mode EnableState
	ret := nvmlDeviceGetAccountingMode(Device, &Mode)
	return Mode, ret
}

func (Device Device) GetAccountingMode() (EnableState, Return) {
	return DeviceGetAccountingMode(Device)
}

// nvml.DeviceGetAccountingStats()
func DeviceGetAccountingStats(Device Device, Pid uint32) (AccountingStats, Return) {
	var Stats AccountingStats
	ret := nvmlDeviceGetAccountingStats(Device, Pid, &Stats)
	return Stats, ret
}

func (Device Device) GetAccountingStats(Pid uint32) (AccountingStats, Return) {
	return DeviceGetAccountingStats(Device, Pid)
}

// nvml.DeviceGetAccountingPids()
func DeviceGetAccountingPids(Device Device) ([]int, Return) {
	var Count uint32 = 1 // Will be reduced upon returning
	for {
		Pids := make([]uint32, Count)
		ret := nvmlDeviceGetAccountingPids(Device, &Count, &Pids[0])
		if ret == SUCCESS {
			return uint32SliceToIntSlice(Pids[:Count]), ret
		}
		if ret != ERROR_INSUFFICIENT_SIZE {
			return nil, ret
		}
		Count *= 2
	}
}

func (Device Device) GetAccountingPids() ([]int, Return) {
	return DeviceGetAccountingPids(Device)
}

// nvml.DeviceGetAccountingBufferSize()
func DeviceGetAccountingBufferSize(Device Device) (int, Return) {
	var BufferSize uint32
	ret := nvmlDeviceGetAccountingBufferSize(Device, &BufferSize)
	return int(BufferSize), ret
}

func (Device Device) GetAccountingBufferSize() (int, Return) {
	return DeviceGetAccountingBufferSize(Device)
}

// nvml.DeviceGetRetiredPages()
func DeviceGetRetiredPages(Device Device, Cause PageRetirementCause) ([]uint64, Return) {
	var PageCount uint32 = 1 // Will be reduced upon returning
	for {
		Addresses := make([]uint64, PageCount)
		ret := nvmlDeviceGetRetiredPages(Device, Cause, &PageCount, &Addresses[0])
		if ret == SUCCESS {
			return Addresses[:PageCount], ret
		}
		if ret != ERROR_INSUFFICIENT_SIZE {
			return nil, ret
		}
		PageCount *= 2
	}
}

func (Device Device) GetRetiredPages(Cause PageRetirementCause) ([]uint64, Return) {
	return DeviceGetRetiredPages(Device, Cause)
}

// nvml.DeviceGetRetiredPages_v2()
func DeviceGetRetiredPages_v2(Device Device, Cause PageRetirementCause) ([]uint64, []uint64, Return) {
	var PageCount uint32 = 1 // Will be reduced upon returning
	for {
		Addresses := make([]uint64, PageCount)
		Timestamps := make([]uint64, PageCount)
		ret := nvmlDeviceGetRetiredPages_v2(Device, Cause, &PageCount, &Addresses[0], &Timestamps[0])
		if ret == SUCCESS {
			return Addresses[:PageCount], Timestamps[:PageCount], ret
		}
		if ret != ERROR_INSUFFICIENT_SIZE {
			return nil, nil, ret
		}
		PageCount *= 2
	}
}

func (Device Device) GetRetiredPages_v2(Cause PageRetirementCause) ([]uint64, []uint64, Return) {
	return DeviceGetRetiredPages_v2(Device, Cause)
}

// nvml.DeviceGetRetiredPagesPendingStatus()
func DeviceGetRetiredPagesPendingStatus(Device Device) (EnableState, Return) {
	var IsPending EnableState
	ret := nvmlDeviceGetRetiredPagesPendingStatus(Device, &IsPending)
	return IsPending, ret
}

func (Device Device) GetRetiredPagesPendingStatus() (EnableState, Return) {
	return DeviceGetRetiredPagesPendingStatus(Device)
}

// nvml.DeviceSetPersistenceMode()
func DeviceSetPersistenceMode(Device Device, Mode EnableState) Return {
	return nvmlDeviceSetPersistenceMode(Device, Mode)
}

func (Device Device) SetPersistenceMode(Mode EnableState) Return {
	return DeviceSetPersistenceMode(Device, Mode)
}

// nvml.DeviceSetComputeMode()
func DeviceSetComputeMode(Device Device, Mode ComputeMode) Return {
	return nvmlDeviceSetComputeMode(Device, Mode)
}

func (Device Device) SetComputeMode(Mode ComputeMode) Return {
	return DeviceSetComputeMode(Device, Mode)
}

// nvml.DeviceSetEccMode()
func DeviceSetEccMode(Device Device, Ecc EnableState) Return {
	return nvmlDeviceSetEccMode(Device, Ecc)
}

func (Device Device) SetEccMode(Ecc EnableState) Return {
	return DeviceSetEccMode(Device, Ecc)
}

// nvml.DeviceClearEccErrorCounts()
func DeviceClearEccErrorCounts(Device Device, CounterType EccCounterType) Return {
	return nvmlDeviceClearEccErrorCounts(Device, CounterType)
}

func (Device Device) ClearEccErrorCounts(CounterType EccCounterType) Return {
	return DeviceClearEccErrorCounts(Device, CounterType)
}

// nvml.DeviceSetDriverModel()
func DeviceSetDriverModel(Device Device, DriverModel DriverModel, Flags uint32) Return {
	return nvmlDeviceSetDriverModel(Device, DriverModel, Flags)
}

func (Device Device) SetDriverModel(DriverModel DriverModel, Flags uint32) Return {
	return DeviceSetDriverModel(Device, DriverModel, Flags)
}

// nvml.DeviceSetGpuLockedClocks()
func DeviceSetGpuLockedClocks(Device Device, MinGpuClockMHz uint32, MaxGpuClockMHz uint32) Return {
	return nvmlDeviceSetGpuLockedClocks(Device, MinGpuClockMHz, MaxGpuClockMHz)
}

func (Device Device) SetGpuLockedClocks(MinGpuClockMHz uint32, MaxGpuClockMHz uint32) Return {
	return DeviceSetGpuLockedClocks(Device, MinGpuClockMHz, MaxGpuClockMHz)
}

// nvml.DeviceResetGpuLockedClocks()
func DeviceResetGpuLockedClocks(Device Device) Return {
	return nvmlDeviceResetGpuLockedClocks(Device)
}

func (Device Device) ResetGpuLockedClocks() Return {
	return DeviceResetGpuLockedClocks(Device)
}

// nvmlDeviceSetMemoryLockedClocks()
func DeviceSetMemoryLockedClocks(Device Device, MinMemClockMHz uint32, MaxMemClockMHz uint32) Return {
	return nvmlDeviceSetMemoryLockedClocks(Device, MinMemClockMHz, MaxMemClockMHz)
}

func (Device Device) SetMemoryLockedClocks(NinMemClockMHz uint32, MaxMemClockMHz uint32) Return {
	return DeviceSetMemoryLockedClocks(Device, NinMemClockMHz, MaxMemClockMHz)
}

// nvmlDeviceResetMemoryLockedClocks()
func DeviceResetMemoryLockedClocks(Device Device) Return {
	return nvmlDeviceResetMemoryLockedClocks(Device)
}

func (Device Device) ResetMemoryLockedClocks() Return {
	return DeviceResetMemoryLockedClocks(Device)
}

// nvml.DeviceGetClkMonStatus()
func DeviceGetClkMonStatus(Device Device) (ClkMonStatus, Return) {
	var Status ClkMonStatus
	ret := nvmlDeviceGetClkMonStatus(Device, &Status)
	return Status, ret
}

func (Device Device) GetClkMonStatus() (ClkMonStatus, Return) {
	return DeviceGetClkMonStatus(Device)
}

// nvml.DeviceSetApplicationsClocks()
func DeviceSetApplicationsClocks(Device Device, MemClockMHz uint32, GraphicsClockMHz uint32) Return {
	return nvmlDeviceSetApplicationsClocks(Device, MemClockMHz, GraphicsClockMHz)
}

func (Device Device) SetApplicationsClocks(MemClockMHz uint32, GraphicsClockMHz uint32) Return {
	return DeviceSetApplicationsClocks(Device, MemClockMHz, GraphicsClockMHz)
}

// nvml.DeviceSetPowerManagementLimit()
func DeviceSetPowerManagementLimit(Device Device, Limit uint32) Return {
	return nvmlDeviceSetPowerManagementLimit(Device, Limit)
}

func (Device Device) SetPowerManagementLimit(Limit uint32) Return {
	return DeviceSetPowerManagementLimit(Device, Limit)
}

// nvml.DeviceSetGpuOperationMode()
func DeviceSetGpuOperationMode(Device Device, Mode GpuOperationMode) Return {
	return nvmlDeviceSetGpuOperationMode(Device, Mode)
}

func (Device Device) SetGpuOperationMode(Mode GpuOperationMode) Return {
	return DeviceSetGpuOperationMode(Device, Mode)
}

// nvml.DeviceSetAPIRestriction()
func DeviceSetAPIRestriction(Device Device, ApiType RestrictedAPI, IsRestricted EnableState) Return {
	return nvmlDeviceSetAPIRestriction(Device, ApiType, IsRestricted)
}

func (Device Device) SetAPIRestriction(ApiType RestrictedAPI, IsRestricted EnableState) Return {
	return DeviceSetAPIRestriction(Device, ApiType, IsRestricted)
}

// nvml.DeviceSetAccountingMode()
func DeviceSetAccountingMode(Device Device, Mode EnableState) Return {
	return nvmlDeviceSetAccountingMode(Device, Mode)
}

func (Device Device) SetAccountingMode(Mode EnableState) Return {
	return DeviceSetAccountingMode(Device, Mode)
}

// nvml.DeviceClearAccountingPids()
func DeviceClearAccountingPids(Device Device) Return {
	return nvmlDeviceClearAccountingPids(Device)
}

func (Device Device) ClearAccountingPids() Return {
	return DeviceClearAccountingPids(Device)
}

// nvml.DeviceGetNvLinkState()
func DeviceGetNvLinkState(Device Device, Link int) (EnableState, Return) {
	var IsActive EnableState
	ret := nvmlDeviceGetNvLinkState(Device, uint32(Link), &IsActive)
	return IsActive, ret
}

func (Device Device) GetNvLinkState(Link int) (EnableState, Return) {
	return DeviceGetNvLinkState(Device, Link)
}

// nvml.DeviceGetNvLinkVersion()
func DeviceGetNvLinkVersion(Device Device, Link int) (uint32, Return) {
	var Version uint32
	ret := nvmlDeviceGetNvLinkVersion(Device, uint32(Link), &Version)
	return Version, ret
}

func (Device Device) GetNvLinkVersion(Link int) (uint32, Return) {
	return DeviceGetNvLinkVersion(Device, Link)
}

// nvml.DeviceGetNvLinkCapability()
func DeviceGetNvLinkCapability(Device Device, Link int, Capability NvLinkCapability) (uint32, Return) {
	var CapResult uint32
	ret := nvmlDeviceGetNvLinkCapability(Device, uint32(Link), Capability, &CapResult)
	return CapResult, ret
}

func (Device Device) GetNvLinkCapability(Link int, Capability NvLinkCapability) (uint32, Return) {
	return DeviceGetNvLinkCapability(Device, Link, Capability)
}

// nvml.DeviceGetNvLinkRemotePciInfo()
func DeviceGetNvLinkRemotePciInfo(Device Device, Link int) (PciInfo, Return) {
	var Pci PciInfo
	ret := nvmlDeviceGetNvLinkRemotePciInfo(Device, uint32(Link), &Pci)
	return Pci, ret
}

func (Device Device) GetNvLinkRemotePciInfo(Link int) (PciInfo, Return) {
	return DeviceGetNvLinkRemotePciInfo(Device, Link)
}

// nvml.DeviceGetNvLinkErrorCounter()
func DeviceGetNvLinkErrorCounter(Device Device, Link int, Counter NvLinkErrorCounter) (uint64, Return) {
	var CounterValue uint64
	ret := nvmlDeviceGetNvLinkErrorCounter(Device, uint32(Link), Counter, &CounterValue)
	return CounterValue, ret
}

func (Device Device) GetNvLinkErrorCounter(Link int, Counter NvLinkErrorCounter) (uint64, Return) {
	return DeviceGetNvLinkErrorCounter(Device, Link, Counter)
}

// nvml.DeviceResetNvLinkErrorCounters()
func DeviceResetNvLinkErrorCounters(Device Device, Link int) Return {
	return nvmlDeviceResetNvLinkErrorCounters(Device, uint32(Link))
}

func (Device Device) ResetNvLinkErrorCounters(Link int) Return {
	return DeviceResetNvLinkErrorCounters(Device, Link)
}

// nvml.DeviceSetNvLinkUtilizationControl()
func DeviceSetNvLinkUtilizationControl(Device Device, Link int, Counter int, Control *NvLinkUtilizationControl, Reset bool) Return {
	reset := uint32(0)
	if Reset {
		reset = 1
	}
	return nvmlDeviceSetNvLinkUtilizationControl(Device, uint32(Link), uint32(Counter), Control, reset)
}

func (Device Device) SetNvLinkUtilizationControl(Link int, Counter int, Control *NvLinkUtilizationControl, Reset bool) Return {
	return DeviceSetNvLinkUtilizationControl(Device, Link, Counter, Control, Reset)
}

// nvml.DeviceGetNvLinkUtilizationControl()
func DeviceGetNvLinkUtilizationControl(Device Device, Link int, Counter int) (NvLinkUtilizationControl, Return) {
	var Control NvLinkUtilizationControl
	ret := nvmlDeviceGetNvLinkUtilizationControl(Device, uint32(Link), uint32(Counter), &Control)
	return Control, ret
}

func (Device Device) GetNvLinkUtilizationControl(Link int, Counter int) (NvLinkUtilizationControl, Return) {
	return DeviceGetNvLinkUtilizationControl(Device, Link, Counter)
}

// nvml.DeviceGetNvLinkUtilizationCounter()
func DeviceGetNvLinkUtilizationCounter(Device Device, Link int, Counter int) (uint64, uint64, Return) {
	var Rxcounter, Txcounter uint64
	ret := nvmlDeviceGetNvLinkUtilizationCounter(Device, uint32(Link), uint32(Counter), &Rxcounter, &Txcounter)
	return Rxcounter, Txcounter, ret
}

func (Device Device) GetNvLinkUtilizationCounter(Link int, Counter int) (uint64, uint64, Return) {
	return DeviceGetNvLinkUtilizationCounter(Device, Link, Counter)
}

// nvml.DeviceFreezeNvLinkUtilizationCounter()
func DeviceFreezeNvLinkUtilizationCounter(Device Device, Link int, Counter int, Freeze EnableState) Return {
	return nvmlDeviceFreezeNvLinkUtilizationCounter(Device, uint32(Link), uint32(Counter), Freeze)
}

func (Device Device) FreezeNvLinkUtilizationCounter(Link int, Counter int, Freeze EnableState) Return {
	return DeviceFreezeNvLinkUtilizationCounter(Device, Link, Counter, Freeze)
}

// nvml.DeviceResetNvLinkUtilizationCounter()
func DeviceResetNvLinkUtilizationCounter(Device Device, Link int, Counter int) Return {
	return nvmlDeviceResetNvLinkUtilizationCounter(Device, uint32(Link), uint32(Counter))
}

func (Device Device) ResetNvLinkUtilizationCounter(Link int, Counter int) Return {
	return DeviceResetNvLinkUtilizationCounter(Device, Link, Counter)
}

// nvml.DeviceGetNvLinkRemoteDeviceType()
func DeviceGetNvLinkRemoteDeviceType(Device Device, Link int) (IntNvLinkDeviceType, Return) {
	var NvLinkDeviceType IntNvLinkDeviceType
	ret := nvmlDeviceGetNvLinkRemoteDeviceType(Device, uint32(Link), &NvLinkDeviceType)
	return NvLinkDeviceType, ret
}

func (Device Device) GetNvLinkRemoteDeviceType(Link int) (IntNvLinkDeviceType, Return) {
	return DeviceGetNvLinkRemoteDeviceType(Device, Link)
}

// nvml.DeviceRegisterEvents()
func DeviceRegisterEvents(Device Device, EventTypes uint64, Set EventSet) Return {
	return nvmlDeviceRegisterEvents(Device, EventTypes, Set)
}

func (Device Device) RegisterEvents(EventTypes uint64, Set EventSet) Return {
	return DeviceRegisterEvents(Device, EventTypes, Set)
}

// nvmlDeviceGetSupportedEventTypes()
func DeviceGetSupportedEventTypes(Device Device) (uint64, Return) {
	var EventTypes uint64
	ret := nvmlDeviceGetSupportedEventTypes(Device, &EventTypes)
	return EventTypes, ret
}

func (Device Device) GetSupportedEventTypes() (uint64, Return) {
	return DeviceGetSupportedEventTypes(Device)
}

// nvml.DeviceModifyDrainState()
func DeviceModifyDrainState(PciInfo *PciInfo, NewState EnableState) Return {
	return nvmlDeviceModifyDrainState(PciInfo, NewState)
}

// nvml.DeviceQueryDrainState()
func DeviceQueryDrainState(PciInfo *PciInfo) (EnableState, Return) {
	var CurrentState EnableState
	ret := nvmlDeviceQueryDrainState(PciInfo, &CurrentState)
	return CurrentState, ret
}

// nvml.DeviceRemoveGpu()
func DeviceRemoveGpu(PciInfo *PciInfo) Return {
	return nvmlDeviceRemoveGpu(PciInfo)
}

// nvml.DeviceRemoveGpu_v2()
func DeviceRemoveGpu_v2(PciInfo *PciInfo, GpuState DetachGpuState, LinkState PcieLinkState) Return {
	return nvmlDeviceRemoveGpu_v2(PciInfo, GpuState, LinkState)
}

// nvml.DeviceDiscoverGpus()
func DeviceDiscoverGpus() (PciInfo, Return) {
	var PciInfo PciInfo
	ret := nvmlDeviceDiscoverGpus(&PciInfo)
	return PciInfo, ret
}

// nvml.DeviceGetFieldValues()
func DeviceGetFieldValues(Device Device, Values []FieldValue) Return {
	ValuesCount := len(Values)
	return nvmlDeviceGetFieldValues(Device, int32(ValuesCount), &Values[0])
}

func (Device Device) GetFieldValues(Values []FieldValue) Return {
	return DeviceGetFieldValues(Device, Values)
}

// nvml.DeviceGetVirtualizationMode()
func DeviceGetVirtualizationMode(Device Device) (GpuVirtualizationMode, Return) {
	var PVirtualMode GpuVirtualizationMode
	ret := nvmlDeviceGetVirtualizationMode(Device, &PVirtualMode)
	return PVirtualMode, ret
}

func (Device Device) GetVirtualizationMode() (GpuVirtualizationMode, Return) {
	return DeviceGetVirtualizationMode(Device)
}

// nvml.DeviceGetHostVgpuMode()
func DeviceGetHostVgpuMode(Device Device) (HostVgpuMode, Return) {
	var PHostVgpuMode HostVgpuMode
	ret := nvmlDeviceGetHostVgpuMode(Device, &PHostVgpuMode)
	return PHostVgpuMode, ret
}

func (Device Device) GetHostVgpuMode() (HostVgpuMode, Return) {
	return DeviceGetHostVgpuMode(Device)
}

// nvml.DeviceSetVirtualizationMode()
func DeviceSetVirtualizationMode(Device Device, VirtualMode GpuVirtualizationMode) Return {
	return nvmlDeviceSetVirtualizationMode(Device, VirtualMode)
}

func (Device Device) SetVirtualizationMode(VirtualMode GpuVirtualizationMode) Return {
	return DeviceSetVirtualizationMode(Device, VirtualMode)
}

// nvml.DeviceGetGridLicensableFeatures()
func DeviceGetGridLicensableFeatures(Device Device) (GridLicensableFeatures, Return) {
	var PGridLicensableFeatures GridLicensableFeatures
	ret := nvmlDeviceGetGridLicensableFeatures(Device, &PGridLicensableFeatures)
	return PGridLicensableFeatures, ret
}

func (Device Device) GetGridLicensableFeatures() (GridLicensableFeatures, Return) {
	return DeviceGetGridLicensableFeatures(Device)
}

// nvml.DeviceGetProcessUtilization()
func DeviceGetProcessUtilization(Device Device, LastSeenTimeStamp uint64) ([]ProcessUtilizationSample, Return) {
	var ProcessSamplesCount uint32
	ret := nvmlDeviceGetProcessUtilization(Device, nil, &ProcessSamplesCount, LastSeenTimeStamp)
	if ret != ERROR_INSUFFICIENT_SIZE {
		return nil, ret
	}
	if ProcessSamplesCount == 0 {
		return []ProcessUtilizationSample{}, ret
	}
	Utilization := make([]ProcessUtilizationSample, ProcessSamplesCount)
	ret = nvmlDeviceGetProcessUtilization(Device, &Utilization[0], &ProcessSamplesCount, LastSeenTimeStamp)
	return Utilization[:ProcessSamplesCount], ret
}

func (Device Device) GetProcessUtilization(LastSeenTimeStamp uint64) ([]ProcessUtilizationSample, Return) {
	return DeviceGetProcessUtilization(Device, LastSeenTimeStamp)
}

// nvml.DeviceGetSupportedVgpus()
func DeviceGetSupportedVgpus(Device Device) ([]VgpuTypeId, Return) {
	var VgpuCount uint32 = 1 // Will be reduced upon returning
	for {
		VgpuTypeIds := make([]VgpuTypeId, VgpuCount)
		ret := nvmlDeviceGetSupportedVgpus(Device, &VgpuCount, &VgpuTypeIds[0])
		if ret == SUCCESS {
			return VgpuTypeIds[:VgpuCount], ret
		}
		if ret != ERROR_INSUFFICIENT_SIZE {
			return nil, ret
		}
		VgpuCount *= 2
	}
}

func (Device Device) GetSupportedVgpus() ([]VgpuTypeId, Return) {
	return DeviceGetSupportedVgpus(Device)
}

// nvml.DeviceGetCreatableVgpus()
func DeviceGetCreatableVgpus(Device Device) ([]VgpuTypeId, Return) {
	var VgpuCount uint32 = 1 // Will be reduced upon returning
	for {
		VgpuTypeIds := make([]VgpuTypeId, VgpuCount)
		ret := nvmlDeviceGetCreatableVgpus(Device, &VgpuCount, &VgpuTypeIds[0])
		if ret == SUCCESS {
			return VgpuTypeIds[:VgpuCount], ret
		}
		if ret != ERROR_INSUFFICIENT_SIZE {
			return nil, ret
		}
		VgpuCount *= 2
	}
}

func (Device Device) GetCreatableVgpus() ([]VgpuTypeId, Return) {
	return DeviceGetCreatableVgpus(Device)
}

// nvml.DeviceGetActiveVgpus()
func DeviceGetActiveVgpus(Device Device) ([]VgpuInstance, Return) {
	var VgpuCount uint32 = 1 // Will be reduced upon returning
	for {
		VgpuInstances := make([]VgpuInstance, VgpuCount)
		ret := nvmlDeviceGetActiveVgpus(Device, &VgpuCount, &VgpuInstances[0])
		if ret == SUCCESS {
			return VgpuInstances[:VgpuCount], ret
		}
		if ret != ERROR_INSUFFICIENT_SIZE {
			return nil, ret
		}
		VgpuCount *= 2
	}
}

func (Device Device) GetActiveVgpus() ([]VgpuInstance, Return) {
	return DeviceGetActiveVgpus(Device)
}

// nvml.DeviceGetVgpuMetadata()
func DeviceGetVgpuMetadata(Device Device) (VgpuPgpuMetadata, Return) {
	var VgpuPgpuMetadata VgpuPgpuMetadata
	OpaqueDataSize := unsafe.Sizeof(VgpuPgpuMetadata.nvmlVgpuPgpuMetadata.OpaqueData)
	VgpuPgpuMetadataSize := unsafe.Sizeof(VgpuPgpuMetadata.nvmlVgpuPgpuMetadata) - OpaqueDataSize
	for {
		BufferSize := uint32(VgpuPgpuMetadataSize + OpaqueDataSize)
		Buffer := make([]byte, BufferSize)
		nvmlVgpuPgpuMetadataPtr := (*nvmlVgpuPgpuMetadata)(unsafe.Pointer(&Buffer[0]))
		ret := nvmlDeviceGetVgpuMetadata(Device, nvmlVgpuPgpuMetadataPtr, &BufferSize)
		if ret == SUCCESS {
			VgpuPgpuMetadata.nvmlVgpuPgpuMetadata = *nvmlVgpuPgpuMetadataPtr
			VgpuPgpuMetadata.OpaqueData = Buffer[VgpuPgpuMetadataSize:BufferSize]
			return VgpuPgpuMetadata, ret
		}
		if ret != ERROR_INSUFFICIENT_SIZE {
			return VgpuPgpuMetadata, ret
		}
		OpaqueDataSize = 2 * OpaqueDataSize
	}
}

func (Device Device) GetVgpuMetadata() (VgpuPgpuMetadata, Return) {
	return DeviceGetVgpuMetadata(Device)
}

// nvml.DeviceGetPgpuMetadataString()
func DeviceGetPgpuMetadataString(Device Device) (string, Return) {
	var BufferSize uint32 = 1 // Will be reduced upon returning
	for {
		PgpuMetadata := make([]byte, BufferSize)
		ret := nvmlDeviceGetPgpuMetadataString(Device, &PgpuMetadata[0], &BufferSize)
		if ret == SUCCESS {
			return string(PgpuMetadata[:clen(PgpuMetadata)]), ret
		}
		if ret != ERROR_INSUFFICIENT_SIZE {
			return "", ret
		}
		BufferSize *= 2
	}
}

func (Device Device) GetPgpuMetadataString() (string, Return) {
	return DeviceGetPgpuMetadataString(Device)
}

// nvml.DeviceGetVgpuUtilization()
func DeviceGetVgpuUtilization(Device Device, LastSeenTimeStamp uint64) (ValueType, []VgpuInstanceUtilizationSample, Return) {
	var SampleValType ValueType
	var VgpuInstanceSamplesCount uint32 = 1 // Will be reduced upon returning
	for {
		UtilizationSamples := make([]VgpuInstanceUtilizationSample, VgpuInstanceSamplesCount)
		ret := nvmlDeviceGetVgpuUtilization(Device, LastSeenTimeStamp, &SampleValType, &VgpuInstanceSamplesCount, &UtilizationSamples[0])
		if ret == SUCCESS {
			return SampleValType, UtilizationSamples[:VgpuInstanceSamplesCount], ret
		}
		if ret != ERROR_INSUFFICIENT_SIZE {
			return SampleValType, nil, ret
		}
		VgpuInstanceSamplesCount *= 2
	}
}

func (Device Device) GetVgpuUtilization(LastSeenTimeStamp uint64) (ValueType, []VgpuInstanceUtilizationSample, Return) {
	return DeviceGetVgpuUtilization(Device, LastSeenTimeStamp)
}

// nvml.DeviceGetAttributes()
func DeviceGetAttributes(Device Device) (DeviceAttributes, Return) {
	var Attributes DeviceAttributes
	ret := nvmlDeviceGetAttributes(Device, &Attributes)
	return Attributes, ret
}

func (Device Device) GetAttributes() (DeviceAttributes, Return) {
	return DeviceGetAttributes(Device)
}

// nvml.DeviceGetRemappedRows()
func DeviceGetRemappedRows(Device Device) (int, int, bool, bool, Return) {
	var CorrRows, UncRows, IsPending, FailureOccured uint32
	ret := nvmlDeviceGetRemappedRows(Device, &CorrRows, &UncRows, &IsPending, &FailureOccured)
	return int(CorrRows), int(UncRows), (IsPending != 0), (FailureOccured != 0), ret
}

func (Device Device) GetRemappedRows() (int, int, bool, bool, Return) {
	return DeviceGetRemappedRows(Device)
}

// nvml.DeviceGetRowRemapperHistogram()
func DeviceGetRowRemapperHistogram(Device Device) (RowRemapperHistogramValues, Return) {
	var Values RowRemapperHistogramValues
	ret := nvmlDeviceGetRowRemapperHistogram(Device, &Values)
	return Values, ret
}

func (Device Device) GetRowRemapperHistogram() (RowRemapperHistogramValues, Return) {
	return DeviceGetRowRemapperHistogram(Device)
}

// nvml.DeviceGetArchitecture()
func DeviceGetArchitecture(Device Device) (DeviceArchitecture, Return) {
	var Arch DeviceArchitecture
	ret := nvmlDeviceGetArchitecture(Device, &Arch)
	return Arch, ret
}

func (Device Device) GetArchitecture() (DeviceArchitecture, Return) {
	return DeviceGetArchitecture(Device)
}

// nvml.DeviceGetVgpuProcessUtilization()
func DeviceGetVgpuProcessUtilization(Device Device, LastSeenTimeStamp uint64) ([]VgpuProcessUtilizationSample, Return) {
	var VgpuProcessSamplesCount uint32 = 1 // Will be reduced upon returning
	for {
		UtilizationSamples := make([]VgpuProcessUtilizationSample, VgpuProcessSamplesCount)
		ret := nvmlDeviceGetVgpuProcessUtilization(Device, LastSeenTimeStamp, &VgpuProcessSamplesCount, &UtilizationSamples[0])
		if ret == SUCCESS {
			return UtilizationSamples[:VgpuProcessSamplesCount], ret
		}
		if ret != ERROR_INSUFFICIENT_SIZE {
			return nil, ret
		}
		VgpuProcessSamplesCount *= 2
	}
}

func (Device Device) GetVgpuProcessUtilization(LastSeenTimeStamp uint64) ([]VgpuProcessUtilizationSample, Return) {
	return DeviceGetVgpuProcessUtilization(Device, LastSeenTimeStamp)
}

// nvml.GetExcludedDeviceCount()
func GetExcludedDeviceCount() (int, Return) {
	var DeviceCount uint32
	ret := nvmlGetExcludedDeviceCount(&DeviceCount)
	return int(DeviceCount), ret
}

// nvml.GetExcludedDeviceInfoByIndex()
func GetExcludedDeviceInfoByIndex(Index int) (ExcludedDeviceInfo, Return) {
	var Info ExcludedDeviceInfo
	ret := nvmlGetExcludedDeviceInfoByIndex(uint32(Index), &Info)
	return Info, ret
}

// nvml.DeviceSetMigMode()
func DeviceSetMigMode(Device Device, Mode int) (Return, Return) {
	var ActivationStatus Return
	ret := nvmlDeviceSetMigMode(Device, uint32(Mode), &ActivationStatus)
	return ActivationStatus, ret
}

func (Device Device) SetMigMode(Mode int) (Return, Return) {
	return DeviceSetMigMode(Device, Mode)
}

// nvml.DeviceGetMigMode()
func DeviceGetMigMode(Device Device) (int, int, Return) {
	var CurrentMode, PendingMode uint32
	ret := nvmlDeviceGetMigMode(Device, &CurrentMode, &PendingMode)
	return int(CurrentMode), int(PendingMode), ret
}

func (Device Device) GetMigMode() (int, int, Return) {
	return DeviceGetMigMode(Device)
}

// nvml.DeviceGetGpuInstanceProfileInfo()
func DeviceGetGpuInstanceProfileInfo(Device Device, Profile int) (GpuInstanceProfileInfo, Return) {
	var Info GpuInstanceProfileInfo
	ret := nvmlDeviceGetGpuInstanceProfileInfo(Device, uint32(Profile), &Info)
	return Info, ret
}

func (Device Device) GetGpuInstanceProfileInfo(Profile int) (GpuInstanceProfileInfo, Return) {
	return DeviceGetGpuInstanceProfileInfo(Device, Profile)
}

// nvml.DeviceGetGpuInstanceProfileInfoV()
type GpuInstanceProfileInfoV struct {
	device  Device
	profile int
}

func (InfoV GpuInstanceProfileInfoV) V1() (GpuInstanceProfileInfo, Return) {
	return DeviceGetGpuInstanceProfileInfo(InfoV.device, InfoV.profile)
}

func (InfoV GpuInstanceProfileInfoV) V2() (GpuInstanceProfileInfo_v2, Return) {
	var Info GpuInstanceProfileInfo_v2
	Info.Version = STRUCT_VERSION(Info, 2)
	ret := nvmlDeviceGetGpuInstanceProfileInfoV(InfoV.device, uint32(InfoV.profile), &Info)
	return Info, ret
}

func DeviceGetGpuInstanceProfileInfoV(Device Device, Profile int) GpuInstanceProfileInfoV {
	return GpuInstanceProfileInfoV{Device, Profile}
}

func (Device Device) GetGpuInstanceProfileInfoV(Profile int) GpuInstanceProfileInfoV {
	return DeviceGetGpuInstanceProfileInfoV(Device, Profile)
}

// nvml.DeviceGetGpuInstancePossiblePlacements()
func DeviceGetGpuInstancePossiblePlacements(Device Device, Info *GpuInstanceProfileInfo) ([]GpuInstancePlacement, Return) {
	if Info == nil {
		return nil, ERROR_INVALID_ARGUMENT
	}
	var Count uint32
	ret := nvmlDeviceGetGpuInstancePossiblePlacements(Device, Info.Id, nil, &Count)
	if ret != SUCCESS {
		return nil, ret
	}
	if Count == 0 {
		return []GpuInstancePlacement{}, ret
	}
	Placements := make([]GpuInstancePlacement, Count)
	ret = nvmlDeviceGetGpuInstancePossiblePlacements(Device, Info.Id, &Placements[0], &Count)
	return Placements[:Count], ret
}

func (Device Device) GetGpuInstancePossiblePlacements(Info *GpuInstanceProfileInfo) ([]GpuInstancePlacement, Return) {
	return DeviceGetGpuInstancePossiblePlacements(Device, Info)
}

// nvml.DeviceGetGpuInstanceRemainingCapacity()
func DeviceGetGpuInstanceRemainingCapacity(Device Device, Info *GpuInstanceProfileInfo) (int, Return) {
	if Info == nil {
		return 0, ERROR_INVALID_ARGUMENT
	}
	var Count uint32
	ret := nvmlDeviceGetGpuInstanceRemainingCapacity(Device, Info.Id, &Count)
	return int(Count), ret
}

func (Device Device) GetGpuInstanceRemainingCapacity(Info *GpuInstanceProfileInfo) (int, Return) {
	return DeviceGetGpuInstanceRemainingCapacity(Device, Info)
}

// nvml.DeviceCreateGpuInstance()
func DeviceCreateGpuInstance(Device Device, Info *GpuInstanceProfileInfo) (GpuInstance, Return) {
	if Info == nil {
		return GpuInstance{}, ERROR_INVALID_ARGUMENT
	}
	var GpuInstance GpuInstance
	ret := nvmlDeviceCreateGpuInstance(Device, Info.Id, &GpuInstance)
	return GpuInstance, ret
}

func (Device Device) CreateGpuInstance(Info *GpuInstanceProfileInfo) (GpuInstance, Return) {
	return DeviceCreateGpuInstance(Device, Info)
}

// nvml.DeviceCreateGpuInstanceWithPlacement()
func DeviceCreateGpuInstanceWithPlacement(Device Device, Info *GpuInstanceProfileInfo, Placement *GpuInstancePlacement) (GpuInstance, Return) {
	if Info == nil {
		return GpuInstance{}, ERROR_INVALID_ARGUMENT
	}
	var GpuInstance GpuInstance
	ret := nvmlDeviceCreateGpuInstanceWithPlacement(Device, Info.Id, Placement, &GpuInstance)
	return GpuInstance, ret
}

func (Device Device) CreateGpuInstanceWithPlacement(Info *GpuInstanceProfileInfo, Placement *GpuInstancePlacement) (GpuInstance, Return) {
	return DeviceCreateGpuInstanceWithPlacement(Device, Info, Placement)
}

// nvml.GpuInstanceDestroy()
func GpuInstanceDestroy(GpuInstance GpuInstance) Return {
	return nvmlGpuInstanceDestroy(GpuInstance)
}

func (GpuInstance GpuInstance) Destroy() Return {
	return GpuInstanceDestroy(GpuInstance)
}

// nvml.DeviceGetGpuInstances()
func DeviceGetGpuInstances(Device Device, Info *GpuInstanceProfileInfo) ([]GpuInstance, Return) {
	if Info == nil {
		return nil, ERROR_INVALID_ARGUMENT
	}
	var Count uint32 = Info.InstanceCount
	GpuInstances := make([]GpuInstance, Count)
	ret := nvmlDeviceGetGpuInstances(Device, Info.Id, &GpuInstances[0], &Count)
	return GpuInstances[:Count], ret
}

func (Device Device) GetGpuInstances(Info *GpuInstanceProfileInfo) ([]GpuInstance, Return) {
	return DeviceGetGpuInstances(Device, Info)
}

// nvml.DeviceGetGpuInstanceById()
func DeviceGetGpuInstanceById(Device Device, Id int) (GpuInstance, Return) {
	var GpuInstance GpuInstance
	ret := nvmlDeviceGetGpuInstanceById(Device, uint32(Id), &GpuInstance)
	return GpuInstance, ret
}

func (Device Device) GetGpuInstanceById(Id int) (GpuInstance, Return) {
	return DeviceGetGpuInstanceById(Device, Id)
}

// nvml.GpuInstanceGetInfo()
func GpuInstanceGetInfo(GpuInstance GpuInstance) (GpuInstanceInfo, Return) {
	var Info GpuInstanceInfo
	ret := nvmlGpuInstanceGetInfo(GpuInstance, &Info)
	return Info, ret
}

func (GpuInstance GpuInstance) GetInfo() (GpuInstanceInfo, Return) {
	return GpuInstanceGetInfo(GpuInstance)
}

// nvml.GpuInstanceGetComputeInstanceProfileInfo()
func GpuInstanceGetComputeInstanceProfileInfo(GpuInstance GpuInstance, Profile int, EngProfile int) (ComputeInstanceProfileInfo, Return) {
	var Info ComputeInstanceProfileInfo
	ret := nvmlGpuInstanceGetComputeInstanceProfileInfo(GpuInstance, uint32(Profile), uint32(EngProfile), &Info)
	return Info, ret
}

func (GpuInstance GpuInstance) GetComputeInstanceProfileInfo(Profile int, EngProfile int) (ComputeInstanceProfileInfo, Return) {
	return GpuInstanceGetComputeInstanceProfileInfo(GpuInstance, Profile, EngProfile)
}

// nvml.GpuInstanceGetComputeInstanceProfileInfoV()
type ComputeInstanceProfileInfoV struct {
	gpuInstance GpuInstance
	profile     int
	engProfile  int
}

func (InfoV ComputeInstanceProfileInfoV) V1() (ComputeInstanceProfileInfo, Return) {
	return GpuInstanceGetComputeInstanceProfileInfo(InfoV.gpuInstance, InfoV.profile, InfoV.engProfile)
}

func (InfoV ComputeInstanceProfileInfoV) V2() (ComputeInstanceProfileInfo_v2, Return) {
	var Info ComputeInstanceProfileInfo_v2
	Info.Version = STRUCT_VERSION(Info, 2)
	ret := nvmlGpuInstanceGetComputeInstanceProfileInfoV(InfoV.gpuInstance, uint32(InfoV.profile), uint32(InfoV.engProfile), &Info)
	return Info, ret
}

func GpuInstanceGetComputeInstanceProfileInfoV(GpuInstance GpuInstance, Profile int, EngProfile int) ComputeInstanceProfileInfoV {
	return ComputeInstanceProfileInfoV{GpuInstance, Profile, EngProfile}
}

func (GpuInstance GpuInstance) GetComputeInstanceProfileInfoV(Profile int, EngProfile int) ComputeInstanceProfileInfoV {
	return GpuInstanceGetComputeInstanceProfileInfoV(GpuInstance, Profile, EngProfile)
}

// nvml.GpuInstanceGetComputeInstanceRemainingCapacity()
func GpuInstanceGetComputeInstanceRemainingCapacity(GpuInstance GpuInstance, Info *ComputeInstanceProfileInfo) (int, Return) {
	if Info == nil {
		return 0, ERROR_INVALID_ARGUMENT
	}
	var Count uint32
	ret := nvmlGpuInstanceGetComputeInstanceRemainingCapacity(GpuInstance, Info.Id, &Count)
	return int(Count), ret
}

func (GpuInstance GpuInstance) GetComputeInstanceRemainingCapacity(Info *ComputeInstanceProfileInfo) (int, Return) {
	return GpuInstanceGetComputeInstanceRemainingCapacity(GpuInstance, Info)
}

// nvml.GpuInstanceCreateComputeInstance()
func GpuInstanceCreateComputeInstance(GpuInstance GpuInstance, Info *ComputeInstanceProfileInfo) (ComputeInstance, Return) {
	if Info == nil {
		return ComputeInstance{}, ERROR_INVALID_ARGUMENT
	}
	var ComputeInstance ComputeInstance
	ret := nvmlGpuInstanceCreateComputeInstance(GpuInstance, Info.Id, &ComputeInstance)
	return ComputeInstance, ret
}

func (GpuInstance GpuInstance) CreateComputeInstance(Info *ComputeInstanceProfileInfo) (ComputeInstance, Return) {
	return GpuInstanceCreateComputeInstance(GpuInstance, Info)
}

// nvml.ComputeInstanceDestroy()
func ComputeInstanceDestroy(ComputeInstance ComputeInstance) Return {
	return nvmlComputeInstanceDestroy(ComputeInstance)
}

func (ComputeInstance ComputeInstance) Destroy() Return {
	return ComputeInstanceDestroy(ComputeInstance)
}

// nvml.GpuInstanceGetComputeInstances()
func GpuInstanceGetComputeInstances(GpuInstance GpuInstance, Info *ComputeInstanceProfileInfo) ([]ComputeInstance, Return) {
	if Info == nil {
		return nil, ERROR_INVALID_ARGUMENT
	}
	var Count uint32 = Info.InstanceCount
	ComputeInstances := make([]ComputeInstance, Count)
	ret := nvmlGpuInstanceGetComputeInstances(GpuInstance, Info.Id, &ComputeInstances[0], &Count)
	return ComputeInstances[:Count], ret
}

func (GpuInstance GpuInstance) GetComputeInstances(Info *ComputeInstanceProfileInfo) ([]ComputeInstance, Return) {
	return GpuInstanceGetComputeInstances(GpuInstance, Info)
}

// nvml.GpuInstanceGetComputeInstanceById()
func GpuInstanceGetComputeInstanceById(GpuInstance GpuInstance, Id int) (ComputeInstance, Return) {
	var ComputeInstance ComputeInstance
	ret := nvmlGpuInstanceGetComputeInstanceById(GpuInstance, uint32(Id), &ComputeInstance)
	return ComputeInstance, ret
}

func (GpuInstance GpuInstance) GetComputeInstanceById(Id int) (ComputeInstance, Return) {
	return GpuInstanceGetComputeInstanceById(GpuInstance, Id)
}

// nvml.ComputeInstanceGetInfo()
func ComputeInstanceGetInfo(ComputeInstance ComputeInstance) (ComputeInstanceInfo, Return) {
	var Info ComputeInstanceInfo
	ret := nvmlComputeInstanceGetInfo(ComputeInstance, &Info)
	return Info, ret
}

func (ComputeInstance ComputeInstance) GetInfo() (ComputeInstanceInfo, Return) {
	return ComputeInstanceGetInfo(ComputeInstance)
}

// nvml.DeviceIsMigDeviceHandle()
func DeviceIsMigDeviceHandle(Device Device) (bool, Return) {
	var IsMigDevice uint32
	ret := nvmlDeviceIsMigDeviceHandle(Device, &IsMigDevice)
	return (IsMigDevice != 0), ret
}

func (Device Device) IsMigDeviceHandle() (bool, Return) {
	return DeviceIsMigDeviceHandle(Device)
}

// nvml DeviceGetGpuInstanceId()
func DeviceGetGpuInstanceId(Device Device) (int, Return) {
	var Id uint32
	ret := nvmlDeviceGetGpuInstanceId(Device, &Id)
	return int(Id), ret
}

func (Device Device) GetGpuInstanceId() (int, Return) {
	return DeviceGetGpuInstanceId(Device)
}

// nvml.DeviceGetComputeInstanceId()
func DeviceGetComputeInstanceId(Device Device) (int, Return) {
	var Id uint32
	ret := nvmlDeviceGetComputeInstanceId(Device, &Id)
	return int(Id), ret
}

func (Device Device) GetComputeInstanceId() (int, Return) {
	return DeviceGetComputeInstanceId(Device)
}

// nvml.DeviceGetMaxMigDeviceCount()
func DeviceGetMaxMigDeviceCount(Device Device) (int, Return) {
	var Count uint32
	ret := nvmlDeviceGetMaxMigDeviceCount(Device, &Count)
	return int(Count), ret
}

func (Device Device) GetMaxMigDeviceCount() (int, Return) {
	return DeviceGetMaxMigDeviceCount(Device)
}

// nvml.DeviceGetMigDeviceHandleByIndex()
func DeviceGetMigDeviceHandleByIndex(device Device, Index int) (Device, Return) {
	var MigDevice Device
	ret := nvmlDeviceGetMigDeviceHandleByIndex(device, uint32(Index), &MigDevice)
	return MigDevice, ret
}

func (Device Device) GetMigDeviceHandleByIndex(Index int) (Device, Return) {
	return DeviceGetMigDeviceHandleByIndex(Device, Index)
}

// nvml.DeviceGetDeviceHandleFromMigDeviceHandle()
func DeviceGetDeviceHandleFromMigDeviceHandle(MigDevice Device) (Device, Return) {
	var Device Device
	ret := nvmlDeviceGetDeviceHandleFromMigDeviceHandle(MigDevice, &Device)
	return Device, ret
}

func (MigDevice Device) GetDeviceHandleFromMigDeviceHandle() (Device, Return) {
	return DeviceGetDeviceHandleFromMigDeviceHandle(MigDevice)
}

// nvml.DeviceGetBusType()
func DeviceGetBusType(Device Device) (BusType, Return) {
	var Type BusType
	ret := nvmlDeviceGetBusType(Device, &Type)
	return Type, ret
}

func (Device Device) GetBusType() (BusType, Return) {
	return DeviceGetBusType(Device)
}

// nvml.DeviceSetDefaultFanSpeed_v2()
func DeviceSetDefaultFanSpeed_v2(Device Device, Fan int) Return {
	return nvmlDeviceSetDefaultFanSpeed_v2(Device, uint32(Fan))
}

func (Device Device) SetDefaultFanSpeed_v2(Fan int) Return {
	return DeviceSetDefaultFanSpeed_v2(Device, Fan)
}

// nvml.DeviceGetMinMaxFanSpeed()
func DeviceGetMinMaxFanSpeed(Device Device) (int, int, Return) {
	var MinSpeed, MaxSpeed uint32
	ret := nvmlDeviceGetMinMaxFanSpeed(Device, &MinSpeed, &MaxSpeed)
	return int(MinSpeed), int(MaxSpeed), ret
}

func (Device Device) GetMinMaxFanSpeed() (int, int, Return) {
	return DeviceGetMinMaxFanSpeed(Device)
}

// nvml.DeviceGetThermalSettings()
func DeviceGetThermalSettings(Device Device, SensorIndex uint32) (GpuThermalSettings, Return) {
	var PThermalSettings GpuThermalSettings
	ret := nvmlDeviceGetThermalSettings(Device, SensorIndex, &PThermalSettings)
	return PThermalSettings, ret
}

func (Device Device) GetThermalSettings(SensorIndex uint32) (GpuThermalSettings, Return) {
	return DeviceGetThermalSettings(Device, SensorIndex)
}

// nvml.DeviceGetDefaultEccMode()
func DeviceGetDefaultEccMode(Device Device) (EnableState, Return) {
	var DefaultMode EnableState
	ret := nvmlDeviceGetDefaultEccMode(Device, &DefaultMode)
	return DefaultMode, ret
}

func (Device Device) GetDefaultEccMode() (EnableState, Return) {
	return DeviceGetDefaultEccMode(Device)
}

// nvml.DeviceGetPcieSpeed()
func DeviceGetPcieSpeed(Device Device) (int, Return) {
	var PcieSpeed uint32
	ret := nvmlDeviceGetPcieSpeed(Device, &PcieSpeed)
	return int(PcieSpeed), ret
}

func (Device Device) GetPcieSpeed() (int, Return) {
	return DeviceGetPcieSpeed(Device)
}

// nvml.DeviceGetGspFirmwareVersion()
func DeviceGetGspFirmwareVersion(Device Device) (string, Return) {
	Version := make([]byte, GSP_FIRMWARE_VERSION_BUF_SIZE)
	ret := nvmlDeviceGetGspFirmwareVersion(Device, &Version[0])
	return string(Version[:clen(Version)]), ret
}

func (Device Device) GetGspFirmwareVersion() (string, Return) {
	return DeviceGetGspFirmwareVersion(Device)
}

// nvml.DeviceGetGspFirmwareMode()
func DeviceGetGspFirmwareMode(Device Device) (bool, bool, Return) {
	var IsEnabled, DefaultMode uint32
	ret := nvmlDeviceGetGspFirmwareMode(Device, &IsEnabled, &DefaultMode)
	return (IsEnabled != 0), (DefaultMode != 0), ret
}

func (Device Device) GetGspFirmwareMode() (bool, bool, Return) {
	return DeviceGetGspFirmwareMode(Device)
}

// nvml.DeviceGetDynamicPstatesInfo()
func DeviceGetDynamicPstatesInfo(Device Device) (GpuDynamicPstatesInfo, Return) {
	var PDynamicPstatesInfo GpuDynamicPstatesInfo
	ret := nvmlDeviceGetDynamicPstatesInfo(Device, &PDynamicPstatesInfo)
	return PDynamicPstatesInfo, ret
}

func (Device Device) GetDynamicPstatesInfo() (GpuDynamicPstatesInfo, Return) {
	return DeviceGetDynamicPstatesInfo(Device)
}

// nvml.DeviceSetFanSpeed_v2()
func DeviceSetFanSpeed_v2(Device Device, Fan int, Speed int) Return {
	return nvmlDeviceSetFanSpeed_v2(Device, uint32(Fan), uint32(Speed))
}

func (Device Device) SetFanSpeed_v2(Fan int, Speed int) Return {
	return DeviceSetFanSpeed_v2(Device, Fan, Speed)
}

// nvml.DeviceGetGpcClkVfOffset()
func DeviceGetGpcClkVfOffset(Device Device) (int, Return) {
	var Offset int32
	ret := nvmlDeviceGetGpcClkVfOffset(Device, &Offset)
	return int(Offset), ret
}

func (Device Device) GetGpcClkVfOffset() (int, Return) {
	return DeviceGetGpcClkVfOffset(Device)
}

// nvml.DeviceSetGpcClkVfOffset()
func DeviceSetGpcClkVfOffset(Device Device, Offset int) Return {
	return nvmlDeviceSetGpcClkVfOffset(Device, int32(Offset))
}

func (Device Device) SetGpcClkVfOffset(Offset int) Return {
	return DeviceSetGpcClkVfOffset(Device, Offset)
}

// nvml.DeviceGetMinMaxClockOfPState()
func DeviceGetMinMaxClockOfPState(Device Device, _type ClockType, Pstate Pstates) (uint32, uint32, Return) {
	var MinClockMHz, MaxClockMHz uint32
	ret := nvmlDeviceGetMinMaxClockOfPState(Device, _type, Pstate, &MinClockMHz, &MaxClockMHz)
	return MinClockMHz, MaxClockMHz, ret
}

func (Device Device) GetMinMaxClockOfPState(_type ClockType, Pstate Pstates) (uint32, uint32, Return) {
	return DeviceGetMinMaxClockOfPState(Device, _type, Pstate)
}

// nvml.DeviceGetSupportedPerformanceStates()
func DeviceGetSupportedPerformanceStates(Device Device) ([]Pstates, Return) {
	Pstates := make([]Pstates, MAX_GPU_PERF_PSTATES)
	ret := nvmlDeviceGetSupportedPerformanceStates(Device, &Pstates[0], MAX_GPU_PERF_PSTATES)
	for i := 0; i < MAX_GPU_PERF_PSTATES; i++ {
		if Pstates[i] == PSTATE_UNKNOWN {
			return Pstates[0:i], ret
		}
	}
	return Pstates, ret
}

func (Device Device) GetSupportedPerformanceStates() ([]Pstates, Return) {
	return DeviceGetSupportedPerformanceStates(Device)
}

// nvml.DeviceGetTargetFanSpeed()
func DeviceGetTargetFanSpeed(Device Device, Fan int) (int, Return) {
	var TargetSpeed uint32
	ret := nvmlDeviceGetTargetFanSpeed(Device, uint32(Fan), &TargetSpeed)
	return int(TargetSpeed), ret
}

func (Device Device) GetTargetFanSpeed(Fan int) (int, Return) {
	return DeviceGetTargetFanSpeed(Device, Fan)
}

// nvml.DeviceGetMemClkVfOffset()
func DeviceGetMemClkVfOffset(Device Device) (int, Return) {
	var Offset int32
	ret := nvmlDeviceGetMemClkVfOffset(Device, &Offset)
	return int(Offset), ret
}

func (Device Device) GetMemClkVfOffset() (int, Return) {
	return DeviceGetMemClkVfOffset(Device)
}

// nvml.DeviceSetMemClkVfOffset()
func DeviceSetMemClkVfOffset(Device Device, Offset int) Return {
	return nvmlDeviceSetMemClkVfOffset(Device, int32(Offset))
}

func (Device Device) SetMemClkVfOffset(Offset int) Return {
	return DeviceSetMemClkVfOffset(Device, Offset)
}

// nvml.DeviceGetGpcClkMinMaxVfOffset()
func DeviceGetGpcClkMinMaxVfOffset(Device Device) (int, int, Return) {
	var MinOffset, MaxOffset int32
	ret := nvmlDeviceGetGpcClkMinMaxVfOffset(Device, &MinOffset, &MaxOffset)
	return int(MinOffset), int(MaxOffset), ret
}

func (Device Device) GetGpcClkMinMaxVfOffset() (int, int, Return) {
	return DeviceGetGpcClkMinMaxVfOffset(Device)
}

// nvml.DeviceGetMemClkMinMaxVfOffset()
func DeviceGetMemClkMinMaxVfOffset(Device Device) (int, int, Return) {
	var MinOffset, MaxOffset int32
	ret := nvmlDeviceGetMemClkMinMaxVfOffset(Device, &MinOffset, &MaxOffset)
	return int(MinOffset), int(MaxOffset), ret
}

func (Device Device) GetMemClkMinMaxVfOffset() (int, int, Return) {
	return DeviceGetMemClkMinMaxVfOffset(Device)
}

// nvml.DeviceGetGpuMaxPcieLinkGeneration()
func DeviceGetGpuMaxPcieLinkGeneration(Device Device) (int, Return) {
	var MaxLinkGenDevice uint32
	ret := nvmlDeviceGetGpuMaxPcieLinkGeneration(Device, &MaxLinkGenDevice)
	return int(MaxLinkGenDevice), ret
}

func (Device Device) GetGpuMaxPcieLinkGeneration() (int, Return) {
	return DeviceGetGpuMaxPcieLinkGeneration(Device)
}

// nvml.DeviceGetFanControlPolicy_v2()
func DeviceGetFanControlPolicy_v2(Device Device, Fan int) (FanControlPolicy, Return) {
	var Policy FanControlPolicy
	ret := nvmlDeviceGetFanControlPolicy_v2(Device, uint32(Fan), &Policy)
	return Policy, ret
}

func (Device Device) GetFanControlPolicy_v2(Fan int) (FanControlPolicy, Return) {
	return DeviceGetFanControlPolicy_v2(Device, Fan)
}

// nvml.DeviceSetFanControlPolicy()
func DeviceSetFanControlPolicy(Device Device, Fan int, Policy FanControlPolicy) Return {
	return nvmlDeviceSetFanControlPolicy(Device, uint32(Fan), Policy)
}

func (Device Device) SetFanControlPolicy(Fan int, Policy FanControlPolicy) Return {
	return DeviceSetFanControlPolicy(Device, Fan, Policy)
}

// nvml.DeviceClearFieldValues()
func DeviceClearFieldValues(Device Device, Values []FieldValue) Return {
	ValuesCount := len(Values)
	return nvmlDeviceClearFieldValues(Device, int32(ValuesCount), &Values[0])
}

func (Device Device) ClearFieldValues(Values []FieldValue) Return {
	return DeviceClearFieldValues(Device, Values)
}

// nvml.DeviceGetVgpuCapabilities()
func DeviceGetVgpuCapabilities(Device Device, Capability DeviceVgpuCapability) (bool, Return) {
	var CapResult uint32
	ret := nvmlDeviceGetVgpuCapabilities(Device, Capability, &CapResult)
	return (CapResult != 0), ret
}

func (Device Device) GetVgpuCapabilities(Capability DeviceVgpuCapability) (bool, Return) {
	return DeviceGetVgpuCapabilities(Device, Capability)
}

// nvml.DeviceGetVgpuSchedulerLog()
func DeviceGetVgpuSchedulerLog(Device Device) (VgpuSchedulerLog, Return) {
	var PSchedulerLog VgpuSchedulerLog
	ret := nvmlDeviceGetVgpuSchedulerLog(Device, &PSchedulerLog)
	return PSchedulerLog, ret
}

func (Device Device) GetVgpuSchedulerLog() (VgpuSchedulerLog, Return) {
	return DeviceGetVgpuSchedulerLog(Device)
}

// nvml.DeviceGetVgpuSchedulerState()
func DeviceGetVgpuSchedulerState(Device Device) (VgpuSchedulerGetState, Return) {
	var PSchedulerState VgpuSchedulerGetState
	ret := nvmlDeviceGetVgpuSchedulerState(Device, &PSchedulerState)
	return PSchedulerState, ret
}

func (Device Device) GetVgpuSchedulerState() (VgpuSchedulerGetState, Return) {
	return DeviceGetVgpuSchedulerState(Device)
}

// nvml.DeviceSetVgpuSchedulerState()
func DeviceSetVgpuSchedulerState(Device Device, PSchedulerState *VgpuSchedulerSetState) Return {
	return nvmlDeviceSetVgpuSchedulerState(Device, PSchedulerState)
}

func (Device Device) SetVgpuSchedulerState(PSchedulerState *VgpuSchedulerSetState) Return {
	return DeviceSetVgpuSchedulerState(Device, PSchedulerState)
}

// nvml.DeviceGetVgpuSchedulerCapabilities()
func DeviceGetVgpuSchedulerCapabilities(Device Device) (VgpuSchedulerCapabilities, Return) {
	var PCapabilities VgpuSchedulerCapabilities
	ret := nvmlDeviceGetVgpuSchedulerCapabilities(Device, &PCapabilities)
	return PCapabilities, ret
}

func (Device Device) GetVgpuSchedulerCapabilities() (VgpuSchedulerCapabilities, Return) {
	return DeviceGetVgpuSchedulerCapabilities(Device)
}

// nvml.GpuInstanceGetComputeInstancePossiblePlacements()
func GpuInstanceGetComputeInstancePossiblePlacements(GpuInstance GpuInstance, Info *ComputeInstanceProfileInfo) ([]ComputeInstancePlacement, Return) {
	var Count uint32
	ret := nvmlGpuInstanceGetComputeInstancePossiblePlacements(GpuInstance, Info.Id, nil, &Count)
	if ret != SUCCESS {
		return nil, ret
	}
	if Count == 0 {
		return []ComputeInstancePlacement{}, ret
	}
	PlacementArray := make([]ComputeInstancePlacement, Count)
	ret = nvmlGpuInstanceGetComputeInstancePossiblePlacements(GpuInstance, Info.Id, &PlacementArray[0], &Count)
	return PlacementArray, ret
}

func (GpuInstance GpuInstance) GetComputeInstancePossiblePlacements(Info *ComputeInstanceProfileInfo) ([]ComputeInstancePlacement, Return) {
	return GpuInstanceGetComputeInstancePossiblePlacements(GpuInstance, Info)
}

// nvml.GpuInstanceCreateComputeInstanceWithPlacement()
func GpuInstanceCreateComputeInstanceWithPlacement(GpuInstance GpuInstance, Info *ComputeInstanceProfileInfo, Placement *ComputeInstancePlacement, ComputeInstance *ComputeInstance) Return {
	return nvmlGpuInstanceCreateComputeInstanceWithPlacement(GpuInstance, Info.Id, Placement, ComputeInstance)
}

func (GpuInstance GpuInstance) CreateComputeInstanceWithPlacement(Info *ComputeInstanceProfileInfo, Placement *ComputeInstancePlacement, ComputeInstance *ComputeInstance) Return {
	return GpuInstanceCreateComputeInstanceWithPlacement(GpuInstance, Info, Placement, ComputeInstance)
}

// nvml.DeviceGetGpuFabricInfo()
func DeviceGetGpuFabricInfo(Device Device) (GpuFabricInfo, Return) {
	var GpuFabricInfo GpuFabricInfo
	ret := nvmlDeviceGetGpuFabricInfo(Device, &GpuFabricInfo)
	return GpuFabricInfo, ret
}

func (Device Device) GetGpuFabricInfo() (GpuFabricInfo, Return) {
	return DeviceGetGpuFabricInfo(Device)
}

// nvml.DeviceCcuGetStreamState()
func DeviceCcuGetStreamState(Device Device) (int, Return) {
	var State uint32
	ret := nvmlDeviceCcuGetStreamState(Device, &State)
	return int(State), ret
}

func (Device Device) CcuGetStreamState() (int, Return) {
	return DeviceCcuGetStreamState(Device)
}

// nvml.DeviceCcuSetStreamState()
func DeviceCcuSetStreamState(Device Device, State int) Return {
	return nvmlDeviceCcuSetStreamState(Device, uint32(State))
}

func (Device Device) CcuSetStreamState(State int) Return {
	return DeviceCcuSetStreamState(Device, State)
}

// nvml.DeviceSetNvLinkDeviceLowPowerThreshold()
func DeviceSetNvLinkDeviceLowPowerThreshold(Device Device, Info *NvLinkPowerThres) Return {
	return nvmlDeviceSetNvLinkDeviceLowPowerThreshold(Device, Info)
}

func (Device Device) SetNvLinkDeviceLowPowerThreshold(Info *NvLinkPowerThres) Return {
	return DeviceSetNvLinkDeviceLowPowerThreshold(Device, Info)
}
