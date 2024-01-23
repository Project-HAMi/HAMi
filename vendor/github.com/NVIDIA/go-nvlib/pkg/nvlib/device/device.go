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

package device

import (
	"fmt"

	"github.com/NVIDIA/go-nvlib/pkg/nvml"
)

// Device defines the set of extended functions associated with a device.Device
type Device interface {
	nvml.Device
	GetArchitectureAsString() (string, error)
	GetBrandAsString() (string, error)
	GetCudaComputeCapabilityAsString() (string, error)
	GetMigDevices() ([]MigDevice, error)
	GetMigProfiles() ([]MigProfile, error)
	IsMigCapable() (bool, error)
	IsMigEnabled() (bool, error)
	VisitMigDevices(func(j int, m MigDevice) error) error
	VisitMigProfiles(func(p MigProfile) error) error
}

type device struct {
	nvml.Device
	lib         *devicelib
	migProfiles []MigProfile
}

var _ Device = &device{}

// NewDevice builds a new Device from an nvml.Device
func (d *devicelib) NewDevice(dev nvml.Device) (Device, error) {
	return d.newDevice(dev)
}

// NewDeviceByUUID builds a new Device from a UUID
func (d *devicelib) NewDeviceByUUID(uuid string) (Device, error) {
	dev, ret := d.nvml.DeviceGetHandleByUUID(uuid)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting device handle for uuid '%v': %v", uuid, ret)
	}
	return d.newDevice(dev)
}

// newDevice creates a device from an nvml.Device
func (d *devicelib) newDevice(dev nvml.Device) (*device, error) {
	return &device{dev, d, nil}, nil
}

// GetArchitectureAsString returns the Device architecture as a string
func (d *device) GetArchitectureAsString() (string, error) {
	arch, ret := d.GetArchitecture()
	if ret != nvml.SUCCESS {
		return "", fmt.Errorf("error getting device architecture: %v", ret)
	}
	switch arch {
	case nvml.DEVICE_ARCH_KEPLER:
		return "Kepler", nil
	case nvml.DEVICE_ARCH_MAXWELL:
		return "Maxwell", nil
	case nvml.DEVICE_ARCH_PASCAL:
		return "Pascal", nil
	case nvml.DEVICE_ARCH_VOLTA:
		return "Volta", nil
	case nvml.DEVICE_ARCH_TURING:
		return "Turing", nil
	case nvml.DEVICE_ARCH_AMPERE:
		return "Ampere", nil
	case nvml.DEVICE_ARCH_ADA:
		return "Ada", nil
	case nvml.DEVICE_ARCH_HOPPER:
		return "Hopper", nil
	case nvml.DEVICE_ARCH_UNKNOWN:
		return "Unknown", nil
	}
	return "", fmt.Errorf("error interpreting device architecture as string: %v", arch)
}

// GetBrandAsString returns the Device architecture as a string
func (d *device) GetBrandAsString() (string, error) {
	brand, ret := d.GetBrand()
	if ret != nvml.SUCCESS {
		return "", fmt.Errorf("error getting device brand: %v", ret)
	}
	switch brand {
	case nvml.BRAND_UNKNOWN:
		return "Unknown", nil
	case nvml.BRAND_QUADRO:
		return "Quadro", nil
	case nvml.BRAND_TESLA:
		return "Tesla", nil
	case nvml.BRAND_NVS:
		return "NVS", nil
	case nvml.BRAND_GRID:
		return "Grid", nil
	case nvml.BRAND_GEFORCE:
		return "GeForce", nil
	case nvml.BRAND_TITAN:
		return "Titan", nil
	case nvml.BRAND_NVIDIA_VAPPS:
		return "NvidiaVApps", nil
	case nvml.BRAND_NVIDIA_VPC:
		return "NvidiaVPC", nil
	case nvml.BRAND_NVIDIA_VCS:
		return "NvidiaVCS", nil
	case nvml.BRAND_NVIDIA_VWS:
		return "NvidiaVWS", nil
	// Deprecated in favor of nvml.BRAND_NVIDIA_CLOUD_GAMING
	//case nvml.BRAND_NVIDIA_VGAMING:
	//	return "VGaming", nil
	case nvml.BRAND_NVIDIA_CLOUD_GAMING:
		return "NvidiaCloudGaming", nil
	case nvml.BRAND_QUADRO_RTX:
		return "QuadroRTX", nil
	case nvml.BRAND_NVIDIA_RTX:
		return "NvidiaRTX", nil
	case nvml.BRAND_NVIDIA:
		return "Nvidia", nil
	case nvml.BRAND_GEFORCE_RTX:
		return "GeForceRTX", nil
	case nvml.BRAND_TITAN_RTX:
		return "TitanRTX", nil
	}
	return "", fmt.Errorf("error interpreting device brand as string: %v", brand)
}

// GetCudaComputeCapabilityAsString returns the Device's CUDA compute capability as a version string
func (d *device) GetCudaComputeCapabilityAsString() (string, error) {
	major, minor, ret := d.GetCudaComputeCapability()
	if ret != nvml.SUCCESS {
		return "", fmt.Errorf("error getting CUDA compute capability: %v", ret)
	}
	return fmt.Sprintf("%d.%d", major, minor), nil
}

// IsMigCapable checks if a device is capable of having MIG paprtitions created on it
func (d *device) IsMigCapable() (bool, error) {
	if !d.lib.hasSymbol("nvmlDeviceGetMigMode") {
		return false, nil
	}

	_, _, ret := nvml.Device(d).GetMigMode()
	if ret == nvml.ERROR_NOT_SUPPORTED {
		return false, nil
	}
	if ret != nvml.SUCCESS {
		return false, fmt.Errorf("error getting MIG mode: %v", ret)
	}

	return true, nil
}

// IsMigEnabled checks if a device has MIG mode currently enabled on it
func (d *device) IsMigEnabled() (bool, error) {
	if !d.lib.hasSymbol("nvmlDeviceGetMigMode") {
		return false, nil
	}

	mode, _, ret := nvml.Device(d).GetMigMode()
	if ret == nvml.ERROR_NOT_SUPPORTED {
		return false, nil
	}
	if ret != nvml.SUCCESS {
		return false, fmt.Errorf("error getting MIG mode: %v", ret)
	}

	return (mode == nvml.DEVICE_MIG_ENABLE), nil
}

// VisitMigDevices walks a top-level device and invokes a callback function for each MIG device configured on it
func (d *device) VisitMigDevices(visit func(int, MigDevice) error) error {
	capable, err := d.IsMigCapable()
	if err != nil {
		return fmt.Errorf("error checking if GPU is MIG capable: %v", err)
	}
	if !capable {
		return nil
	}

	count, ret := nvml.Device(d).GetMaxMigDeviceCount()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("error getting max MIG device count: %v", ret)
	}

	for i := 0; i < count; i++ {
		device, ret := nvml.Device(d).GetMigDeviceHandleByIndex(i)
		if ret == nvml.ERROR_NOT_FOUND {
			continue
		}
		if ret == nvml.ERROR_INVALID_ARGUMENT {
			continue
		}
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting MIG device handle at index '%v': %v", i, ret)
		}
		mig, err := d.lib.NewMigDevice(device)
		if err != nil {
			return fmt.Errorf("error creating new MIG device wrapper: %v", err)
		}
		err = visit(i, mig)
		if err != nil {
			return fmt.Errorf("error visiting MIG device: %v", err)
		}
	}
	return nil
}

// VisitMigProfiles walks a top-level device and invokes a callback function for each unique MIG Profile that can be configured on it
func (d *device) VisitMigProfiles(visit func(MigProfile) error) error {
	capable, err := d.IsMigCapable()
	if err != nil {
		return fmt.Errorf("error checking if GPU is MIG capable: %v", err)
	}

	if !capable {
		return nil
	}

	memory, ret := d.GetMemoryInfo()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("error getting device memory info: %v", ret)
	}

	for i := 0; i < nvml.GPU_INSTANCE_PROFILE_COUNT; i++ {
		giProfileInfo, ret := d.GetGpuInstanceProfileInfo(i)
		if ret == nvml.ERROR_NOT_SUPPORTED {
			continue
		}
		if ret == nvml.ERROR_INVALID_ARGUMENT {
			continue
		}
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting GPU Instance profile info: %v", ret)
		}

		for j := 0; j < nvml.COMPUTE_INSTANCE_PROFILE_COUNT; j++ {
			for k := 0; k < nvml.COMPUTE_INSTANCE_ENGINE_PROFILE_COUNT; k++ {
				p, err := d.lib.NewMigProfile(i, j, k, giProfileInfo.MemorySizeMB, memory.Total)
				if err != nil {
					return fmt.Errorf("error creating MIG profile: %v", err)
				}

				// NOTE: The NVML API doesn't currently let us query the set of
				// valid Compute Instance profiles without first instantiating
				// a GPU Instance to check against. In theory, it should be
				// possible to get this information without a reference to a
				// GPU instance, but no API is provided for that at the moment.
				// We run the checks below to weed out invalid profiles
				// heuristically, given what we know about how they are
				// physically constructed. In the future we should do this via
				// NVML once a proper API for this exists.
				pi := p.GetInfo()
				if pi.C > pi.G {
					continue
				}
				if (pi.C < pi.G) && ((pi.C * 2) > (pi.G + 1)) {
					continue
				}

				err = visit(p)
				if err != nil {
					return fmt.Errorf("error visiting MIG profile: %v", err)
				}
			}
		}
	}
	return nil
}

// GetMigDevices gets the set of MIG devices associated with a top-level device
func (d *device) GetMigDevices() ([]MigDevice, error) {
	var migs []MigDevice
	err := d.VisitMigDevices(func(j int, m MigDevice) error {
		migs = append(migs, m)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return migs, nil
}

// GetMigProfiles gets the set of unique MIG profiles associated with a top-level device
func (d *device) GetMigProfiles() ([]MigProfile, error) {
	// Return the cached list if available
	if d.migProfiles != nil {
		return d.migProfiles, nil
	}

	// Otherwise generate it...
	var profiles []MigProfile
	err := d.VisitMigProfiles(func(p MigProfile) error {
		profiles = append(profiles, p)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// And cache it before returning
	d.migProfiles = profiles
	return profiles, nil
}

// isSkipped checks whether the device should be skipped.
func (d *device) isSkipped() (bool, error) {
	name, ret := d.GetName()
	if ret != nvml.SUCCESS {
		return false, fmt.Errorf("error getting device name: %v", ret)
	}

	if _, exists := d.lib.skippedDevices[name]; exists {
		return true, nil
	}

	return false, nil
}

// VisitDevices visits each top-level device and invokes a callback function for it
func (d *devicelib) VisitDevices(visit func(int, Device) error) error {
	count, ret := d.nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("error getting device count: %v", ret)
	}

	for i := 0; i < count; i++ {
		device, ret := d.nvml.DeviceGetHandleByIndex(i)
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting device handle for index '%v': %v", i, ret)
		}
		dev, err := d.newDevice(device)
		if err != nil {
			return fmt.Errorf("error creating new device wrapper: %v", err)
		}

		isSkipped, err := dev.isSkipped()
		if err != nil {
			return fmt.Errorf("error checking whether device is skipped: %v", err)
		}
		if isSkipped {
			continue
		}

		err = visit(i, dev)
		if err != nil {
			return fmt.Errorf("error visiting device: %v", err)
		}
	}
	return nil
}

// VisitMigDevices walks a top-level device and invokes a callback function for each MIG device configured on it
func (d *devicelib) VisitMigDevices(visit func(int, Device, int, MigDevice) error) error {
	err := d.VisitDevices(func(i int, dev Device) error {
		err := dev.VisitMigDevices(func(j int, mig MigDevice) error {
			err := visit(i, dev, j, mig)
			if err != nil {
				return fmt.Errorf("error visiting MIG device: %v", err)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("error visiting device: %v", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("error visiting devices: %v", err)
	}
	return nil
}

// VisitMigProfiles walks a top-level device and invokes a callback function for each unique MIG profile found on them
func (d *devicelib) VisitMigProfiles(visit func(MigProfile) error) error {
	visited := make(map[string]bool)
	err := d.VisitDevices(func(i int, dev Device) error {
		err := dev.VisitMigProfiles(func(p MigProfile) error {
			if visited[p.String()] {
				return nil
			}

			err := visit(p)
			if err != nil {
				return fmt.Errorf("error visiting MIG profile: %v", err)
			}

			visited[p.String()] = true
			return nil
		})
		if err != nil {
			return fmt.Errorf("error visiting device: %v", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("error visiting devices: %v", err)
	}
	return nil
}

// GetDevices gets the set of all top-level devices
func (d *devicelib) GetDevices() ([]Device, error) {
	var devs []Device
	err := d.VisitDevices(func(i int, dev Device) error {
		devs = append(devs, dev)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return devs, nil
}

// GetMigDevices gets the set of MIG devices across all top-level devices
func (d *devicelib) GetMigDevices() ([]MigDevice, error) {
	var migs []MigDevice
	err := d.VisitMigDevices(func(i int, dev Device, j int, m MigDevice) error {
		migs = append(migs, m)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return migs, nil
}

// GetMigProfiles gets the set of unique MIG profiles across all top-level devices
func (d *devicelib) GetMigProfiles() ([]MigProfile, error) {
	// Return the cached list if available
	if d.migProfiles != nil {
		return d.migProfiles, nil
	}

	// Otherwise generate it...
	var profiles []MigProfile
	err := d.VisitMigProfiles(func(p MigProfile) error {
		profiles = append(profiles, p)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// And cache it before returning
	d.migProfiles = profiles
	return profiles, nil
}

// hasSymbol checks to see if the given symbol is present in the NVML library.
// If devicelib is configured to not verify symbols, then all symbols are assumed to exist.
func (d *devicelib) hasSymbol(symbol string) bool {
	if !*d.verifySymbols {
		return true
	}

	return d.nvml.Lookup(symbol) == nil
}
