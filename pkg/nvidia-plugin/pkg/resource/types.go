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

package resource

// Manager defines an interface for managing devices
//
//go:generate moq -rm -out manager_mock.go . Manager
type Manager interface {
	Init() error
	Shutdown() error
	GetDevices() ([]Device, error)
	GetDriverVersion() (string, error)
	GetCudaDriverVersion() (int, int, error)
}

// Device defines an interface for a device with which labels are associated
//
//go:generate moq -out device_mock.go . Device
type Device interface {
	IsFabricAttached() (bool, error)
	IsMigEnabled() (bool, error)
	IsMigCapable() (bool, error)
	GetMigDevices() ([]Device, error)
	GetAttributes() (map[string]interface{}, error)
	GetName() (string, error)
	GetTotalMemoryMB() (uint64, error)
	GetDeviceHandleFromMigDeviceHandle() (Device, error)
	GetCudaComputeCapability() (int, int, error)
	GetPCIClass() (uint32, error)
	GetFabricIDs() (string, string, error)
}
