/**
# Copyright (c) 2021-2022, NVIDIA CORPORATION.  All rights reserved.
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

package vgpu

import (
	"fmt"
	"strings"
)

// Interface allows us to get a list of vGPU specific PCI devices
type Interface interface {
	Devices() ([]*Device, error)
}

// Device is just an alias to a PCIDevice
type Device struct {
	pci            *PCIDevice
	vGPUCapability []byte
}

// Info represents vGPU driver info running on underlying hypervisor host.
type Info struct {
	HostDriverVersion string
	HostDriverBranch  string
}

const (
	// VGPUCapabilityRecordStart indicates offset of beginning vGPU capability record
	VGPUCapabilityRecordStart uint8 = 5
	// HostDriverVersionLength indicates max length of driver version
	HostDriverVersionLength = 10
	// HostDriverBranchLength indicates max length of driver branch
	HostDriverBranchLength = 10
)

// Lib implements the NvidiaVGPU interface
type Lib struct {
	pci NvidiaPCI
}

// NewVGPULib returns an instance of Lib implementing the VGPU interface
func NewVGPULib(pci NvidiaPCI) Interface {
	return &Lib{pci: pci}
}

// NewMockVGPU initializes and returns mock Interface interface type
func NewMockVGPU() Interface {
	return NewVGPULib(NewMockNvidiaPCI())
}

// Devices returns all vGPU devices attached to the guest
func (v *Lib) Devices() ([]*Device, error) {
	pciDevices, err := v.pci.Devices()
	if err != nil {
		return nil, fmt.Errorf("error getting NVIDIA specific PCI devices: %v", err)
	}

	var vgpus []*Device
	for _, device := range pciDevices {
		capability, err := device.GetVendorSpecificCapability()
		if err != nil {
			return nil, fmt.Errorf("unable to read vendor specific capability for %s: %v", device.Address, err)
		}
		if capability == nil {
			continue
		}
		if exists := v.IsVGPUDevice(capability); exists {
			vgpu := &Device{
				pci:            device,
				vGPUCapability: capability,
			}
			vgpus = append(vgpus, vgpu)
		}
	}
	return vgpus, nil
}

// IsVGPUDevice returns true if the device is of type vGPU
func (v *Lib) IsVGPUDevice(capability []byte) bool {
	if len(capability) < 5 {
		return false
	}
	// check for vGPU signature, 0x56, 0x46 i.e "VF"
	if capability[3] != 0x56 {
		return false
	}
	if capability[4] != 0x46 {
		return false
	}
	return true
}

// GetInfo returns information about vGPU manager running on the underlying hypervisor host
func (d *Device) GetInfo() (*Info, error) {
	if len(d.vGPUCapability) == 0 {
		return nil, fmt.Errorf("vendor capability record is not populated for device %s", d.pci.Address)
	}

	// traverse vGPU vendor capability records until host driver version record(id: 0) is found
	var hostDriverVersion string
	var hostDriverBranch string
	foundDriverVersionRecord := false
	pos := VGPUCapabilityRecordStart
	record := GetByte(d.vGPUCapability, VGPUCapabilityRecordStart)
	for record != 0 && int(pos) < len(d.vGPUCapability) {
		// find next record
		recordLength := GetByte(d.vGPUCapability, pos+1)
		pos += recordLength
		record = GetByte(d.vGPUCapability, pos)
	}

	if record == 0 && int(pos+2+HostDriverVersionLength+HostDriverBranchLength) <= len(d.vGPUCapability) {
		foundDriverVersionRecord = true
		// found vGPU host driver version record type
		// initialized at record data byte, i.e pos + 1(record id byte) + 1(record lengh byte)
		i := pos + 2
		// 10 bytes of driver version
		for ; i < pos+2+HostDriverVersionLength; i++ {
			hostDriverVersion += string(GetByte(d.vGPUCapability, i))
		}
		hostDriverVersion = strings.Trim(hostDriverVersion, "\x00")
		// 10 bytes of driver branch
		for ; i < pos+2+HostDriverVersionLength+HostDriverBranchLength; i++ {
			hostDriverBranch += string(GetByte(d.vGPUCapability, i))
		}
		hostDriverBranch = strings.Trim(hostDriverBranch, "\x00")
	}

	if !foundDriverVersionRecord {
		return nil, fmt.Errorf("cannot find driver version record in vendor specific capability for device %s", d.pci.Address)
	}

	info := &Info{
		HostDriverVersion: hostDriverVersion,
		HostDriverBranch:  hostDriverBranch,
	}

	return info, nil
}
