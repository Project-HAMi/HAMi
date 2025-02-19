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

package cuda

import (
	"github.com/NVIDIA/go-nvml/pkg/dl"
)

const (
	libraryName      = "libcuda.so.1"
	libraryLoadFlags = dl.RTLD_LAZY | dl.RTLD_GLOBAL
)

// cuda stores a reference the cuda dynamic library
var cuda *dl.DynamicLibrary

// Init calls cuInit and initialized the library
func Init() Result {
	lib := dl.New(libraryName, libraryLoadFlags)
	if err := lib.Open(); err != nil {
		return ERROR_UNKNOWN
	}
	cuda = lib

	if err := cuda.Lookup("cuInit"); err != nil {
		return ERROR_UNKNOWN
	}

	return cuInit(0)
}

// Shutdown ensures that the CUDA library is unloaded.
func Shutdown() Result {
	if cuda == nil {
		return SUCCESS
	}
	if err := cuda.Close(); err != nil {
		return ERROR_UNKNOWN
	}
	return SUCCESS
}

// DriverGetVersion returns the driver version as an int.
func DriverGetVersion() (int, Result) {
	var version int32
	r := cuDriverGetVersion(&version)

	return int(version), r
}

// DeviceGet returns the device with the specified index.
func DeviceGet(index int) (Device, Result) {
	var device Device
	//nolint:gosec  // Since index is internal-only, we ignore possible overflow errors here.
	r := cuDeviceGet(&device, int32(index))

	return device, r
}

// DeviceGetAttribute returns the specified attribute for the specified device.
func DeviceGetAttribute(attribute DeviceAttribute, device Device) (int, Result) {
	var value int32
	r := cuDeviceGetAttribute(&value, attribute, device)
	return int(value), r
}

// DeviceGetCount returns the number of CUDA-capable devices available
func DeviceGetCount() (int, Result) {
	var count int32
	r := cuDeviceGetCount(&count)
	return int(count), r
}

// GetAttribute converts the DeviceGetAttribute function to a device method
func (device Device) GetAttribute(attribute DeviceAttribute) (int, Result) {
	return DeviceGetAttribute(attribute, device)
}

// DeviceGetName returns the name of the specified device.
func DeviceGetName(device Device) (string, Result) {
	len := int32(96)
	name := make([]byte, len)

	r := cuDeviceGetName(&name[0], len, device)

	return string(name[:clen(name)]), r
}

// GetName converts the DeviceGetname function to a device method
func (device Device) GetName() (string, Result) {
	return DeviceGetName(device)
}

// DeviceTotalMem returns the total memory for the specified device
func DeviceTotalMem(device Device) (uint64, Result) {
	var bytes uint64
	r := cuDeviceTotalMem(&bytes, device)

	return bytes, r
}

// TotalMem converts the DeviceTotalMem function to a device method
func (device Device) TotalMem() (uint64, Result) {
	return DeviceTotalMem(device)
}
