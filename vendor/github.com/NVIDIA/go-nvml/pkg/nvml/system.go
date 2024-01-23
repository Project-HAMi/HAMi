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

// nvml.SystemGetDriverVersion()
func SystemGetDriverVersion() (string, Return) {
	Version := make([]byte, SYSTEM_DRIVER_VERSION_BUFFER_SIZE)
	ret := nvmlSystemGetDriverVersion(&Version[0], SYSTEM_DRIVER_VERSION_BUFFER_SIZE)
	return string(Version[:clen(Version)]), ret
}

// nvml.SystemGetNVMLVersion()
func SystemGetNVMLVersion() (string, Return) {
	Version := make([]byte, SYSTEM_NVML_VERSION_BUFFER_SIZE)
	ret := nvmlSystemGetNVMLVersion(&Version[0], SYSTEM_NVML_VERSION_BUFFER_SIZE)
	return string(Version[:clen(Version)]), ret
}

// nvml.SystemGetCudaDriverVersion()
func SystemGetCudaDriverVersion() (int, Return) {
	var CudaDriverVersion int32
	ret := nvmlSystemGetCudaDriverVersion(&CudaDriverVersion)
	return int(CudaDriverVersion), ret
}

// nvml.SystemGetCudaDriverVersion_v2()
func SystemGetCudaDriverVersion_v2() (int, Return) {
	var CudaDriverVersion int32
	ret := nvmlSystemGetCudaDriverVersion_v2(&CudaDriverVersion)
	return int(CudaDriverVersion), ret
}

// nvml.SystemGetProcessName()
func SystemGetProcessName(Pid int) (string, Return) {
	Name := make([]byte, SYSTEM_PROCESS_NAME_BUFFER_SIZE)
	ret := nvmlSystemGetProcessName(uint32(Pid), &Name[0], SYSTEM_PROCESS_NAME_BUFFER_SIZE)
	return string(Name[:clen(Name)]), ret
}

// nvml.SystemGetHicVersion()
func SystemGetHicVersion() ([]HwbcEntry, Return) {
	var HwbcCount uint32 = 1 // Will be reduced upon returning
	for {
		HwbcEntries := make([]HwbcEntry, HwbcCount)
		ret := nvmlSystemGetHicVersion(&HwbcCount, &HwbcEntries[0])
		if ret == SUCCESS {
			return HwbcEntries[:HwbcCount], ret
		}
		if ret != ERROR_INSUFFICIENT_SIZE {
			return nil, ret
		}
		HwbcCount *= 2
	}
}

// nvml.SystemGetTopologyGpuSet()
func SystemGetTopologyGpuSet(CpuNumber int) ([]Device, Return) {
	var Count uint32
	ret := nvmlSystemGetTopologyGpuSet(uint32(CpuNumber), &Count, nil)
	if ret != SUCCESS {
		return nil, ret
	}
	if Count == 0 {
		return []Device{}, ret
	}
	DeviceArray := make([]Device, Count)
	ret = nvmlSystemGetTopologyGpuSet(uint32(CpuNumber), &Count, &DeviceArray[0])
	return DeviceArray, ret
}
