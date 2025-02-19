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

package rm

import (
	"fmt"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvlib/pkg/nvlib/info"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	spec "github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/api/config/v1"
)

type deviceMapBuilder struct {
	device.Interface
	migStrategy         *string
	resources           *spec.Resources
	replicatedResources *spec.ReplicatedResources

	newGPUDevice func(i int, gpu nvml.Device) (string, deviceInfo)
}

// DeviceMap stores a set of devices per resource name.
type DeviceMap map[spec.ResourceName]Devices

// NewDeviceMap creates a device map for the specified NVML library and config.
func NewDeviceMap(infolib info.Interface, devicelib device.Interface, config *nvidia.DeviceConfig) (DeviceMap, error) {
	b := deviceMapBuilder{
		Interface:           devicelib,
		migStrategy:         config.Flags.MigStrategy,
		resources:           &config.Resources,
		replicatedResources: config.Sharing.ReplicatedResources(),
		newGPUDevice:        newNvmlGPUDevice,
	}

	if infolib.ResolvePlatform() == info.PlatformWSL {
		b.newGPUDevice = newWslGPUDevice
	}

	return b.build()
}

// build builds a map of resource names to devices.
func (b *deviceMapBuilder) build() (DeviceMap, error) {
	devices, err := b.buildDeviceMapFromConfigResources()
	if err != nil {
		return nil, fmt.Errorf("error building device map from config.resources: %v", err)
	}
	devices, err = updateDeviceMapWithReplicas(b.replicatedResources, devices)
	if err != nil {
		return nil, fmt.Errorf("error updating device map with replicas from replicatedResources config: %v", err)
	}
	return devices, nil
}

// buildDeviceMapFromConfigResources builds a map of resource names to devices from spec.Config.Resources
func (b *deviceMapBuilder) buildDeviceMapFromConfigResources() (DeviceMap, error) {
	deviceMap, err := b.buildGPUDeviceMap()
	if err != nil {
		return nil, fmt.Errorf("error building GPU device map: %v", err)
	}

	if *b.migStrategy == spec.MigStrategyNone {
		return deviceMap, nil
	}

	migDeviceMap, err := b.buildMigDeviceMap()
	if err != nil {
		return nil, fmt.Errorf("error building MIG device map: %v", err)
	}

	var requireUniformMIGDevices bool
	if *b.migStrategy == spec.MigStrategySingle {
		requireUniformMIGDevices = true
	}

	err = b.assertAllMigDevicesAreValid(requireUniformMIGDevices)
	if err != nil {
		return nil, fmt.Errorf("invalid MIG configuration: %v", err)
	}

	if requireUniformMIGDevices && !deviceMap.isEmpty() && !migDeviceMap.isEmpty() {
		return nil, fmt.Errorf("all devices on the node must be configured with the same migEnabled value")
	}

	deviceMap.merge(migDeviceMap)

	return deviceMap, nil
}

// buildGPUDeviceMap builds a map of resource names to GPU devices
func (b *deviceMapBuilder) buildGPUDeviceMap() (DeviceMap, error) {
	devices := make(DeviceMap)

	err := b.VisitDevices(func(i int, gpu device.Device) error {
		name, ret := gpu.GetName()
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting product name for GPU: %v", ret)
		}
		migEnabled, err := gpu.IsMigEnabled()
		if err != nil {
			return fmt.Errorf("error checking if MIG is enabled on GPU: %v", err)
		}
		if migEnabled && *b.migStrategy != spec.MigStrategyNone {
			return nil
		}
		for _, resource := range b.resources.GPUs {
			if resource.Pattern.Matches(name) {
				index, info := b.newGPUDevice(i, gpu)
				return devices.setEntry(resource.Name, index, info)
			}
		}
		return fmt.Errorf("GPU name '%v' does not match any resource patterns", name)
	})
	return devices, err
}

// buildMigDeviceMap builds a map of resource names to MIG devices
func (b *deviceMapBuilder) buildMigDeviceMap() (DeviceMap, error) {
	devices := make(DeviceMap)
	err := b.VisitMigDevices(func(i int, d device.Device, j int, mig device.MigDevice) error {
		migProfile, err := mig.GetProfile()
		if err != nil {
			return fmt.Errorf("error getting MIG profile for MIG device at index '(%v, %v)': %v", i, j, err)
		}
		for _, resource := range b.resources.MIGs {
			if resource.Pattern.Matches(migProfile.String()) {
				index, info := newMigDevice(i, j, mig)
				return devices.setEntry(resource.Name, index, info)
			}
		}
		return fmt.Errorf("MIG profile '%v' does not match any resource patterns", migProfile)
	})
	return devices, err
}

// assertAllMigDevicesAreValid ensures that each MIG-enabled device has at least one MIG device
// associated with it.
func (b *deviceMapBuilder) assertAllMigDevicesAreValid(uniform bool) error {
	err := b.VisitDevices(func(i int, d device.Device) error {
		isMigEnabled, err := d.IsMigEnabled()
		if err != nil {
			return err
		}
		if !isMigEnabled {
			return nil
		}
		migDevices, err := d.GetMigDevices()
		if err != nil {
			return err
		}
		if uniform && len(migDevices) == 0 {
			return fmt.Errorf("device %v has no MIG devices configured", i)
		}
		if !uniform && len(migDevices) == 0 {
			klog.Warningf("device %v has no MIG devices configured", i)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("at least one device with migEnabled=true was not configured correctly: %v", err)
	}

	if !uniform {
		return nil
	}

	var previousAttributes *nvml.DeviceAttributes
	return b.VisitMigDevices(func(i int, d device.Device, j int, m device.MigDevice) error {
		attrs, ret := m.GetAttributes()
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting device attributes: %v", ret)
		}
		if previousAttributes == nil {
			previousAttributes = &attrs
		} else if attrs != *previousAttributes {
			return fmt.Errorf("more than one MIG device type present on node")
		}

		return nil
	})
}

// setEntry sets the DeviceMap entry for the specified resource
func (d DeviceMap) setEntry(name spec.ResourceName, index string, device deviceInfo) error {
	dev, err := BuildDevice(index, device)
	if err != nil {
		return fmt.Errorf("error building Device: %v", err)
	}
	d.insert(name, dev)
	return nil
}

// insert adds the specified device to the device map
func (d DeviceMap) insert(name spec.ResourceName, dev *Device) {
	if d[name] == nil {
		d[name] = make(Devices)
	}
	d[name][dev.ID] = dev
}

// merge merges two devices maps
func (d DeviceMap) merge(o DeviceMap) {
	for name, devices := range o {
		for _, device := range devices {
			d.insert(name, device)
		}
	}
}

// isEmpty checks whether a device map is empty
func (d DeviceMap) isEmpty() bool {
	for _, devices := range d {
		if len(devices) > 0 {
			return false
		}
	}
	return true
}

// getIDsOfDevicesToReplicate returns a list of dervice IDs that we want to replicate.
func (d DeviceMap) getIDsOfDevicesToReplicate(r *spec.ReplicatedResource) ([]string, error) {
	devices, exists := d[r.Name]
	if !exists {
		return nil, nil
	}

	// If all devices for this resource type are to be replicated.
	if r.Devices.All {
		return devices.GetIDs(), nil
	}

	// If a specific number of devices for this resource type are to be replicated.
	if r.Devices.Count > 0 {
		if r.Devices.Count > len(devices) {
			return nil, fmt.Errorf("requested %d devices to be replicated, but only %d devices available", r.Devices.Count, len(devices))
		}
		return devices.GetIDs()[:r.Devices.Count], nil
	}

	// If a specific set of devices for this resource type are to be replicated.
	if len(r.Devices.List) > 0 {
		var ids []string
		for _, ref := range r.Devices.List {
			if ref.IsUUID() {
				d := devices.GetByID(string(ref))
				if d == nil {
					return nil, fmt.Errorf("no matching device with UUID: %v", ref)
				}
				ids = append(ids, d.ID)
			}
			if ref.IsGPUIndex() || ref.IsMigIndex() {
				d := devices.GetByIndex(string(ref))
				if d == nil {
					return nil, fmt.Errorf("no matching device at index: %v", ref)
				}
				ids = append(ids, d.ID)
			}
		}
		return ids, nil
	}

	return nil, fmt.Errorf("unexpected error")
}

// updateDeviceMapWithReplicas returns an updated map of resource names to devices with replica
// information from the active replicated resources config.
func updateDeviceMapWithReplicas(replicatedResources *spec.ReplicatedResources, oDevices DeviceMap) (DeviceMap, error) {
	devices := make(DeviceMap)

	// Begin by walking replicatedResources.Resources and building a map of just the resource names.
	names := make(map[spec.ResourceName]bool)
	for _, r := range replicatedResources.Resources {
		names[r.Name] = true
	}

	// Copy over all devices from oDevices without a resource reference in TimeSlicing.Resources.
	for r, ds := range oDevices {
		if !names[r] {
			devices[r] = ds
		}
	}

	// Walk shared Resources and update devices in the device map as appropriate.
	for _, resource := range replicatedResources.Resources {
		r := resource
		// Get the IDs of the devices we want to replicate from oDevices
		ids, err := oDevices.getIDsOfDevicesToReplicate(&r)
		if err != nil {
			return nil, fmt.Errorf("unable to get IDs of devices to replicate for '%v' resource: %v", r.Name, err)
		}
		// Skip any resources not matched in oDevices
		if len(ids) == 0 {
			continue
		}

		// Add any devices we don't want replicated directly into the device map.
		for _, d := range oDevices[r.Name].Difference(oDevices[r.Name].Subset(ids)) {
			devices.insert(r.Name, d)
		}

		// Create replicated devices add them to the device map.
		// Rename the resource for replicated devices as requested.
		name := r.Name
		if r.Rename != "" {
			name = r.Rename
		}
		for _, id := range ids {
			for i := 0; i < r.Replicas; i++ {
				annotatedID := string(NewAnnotatedID(id, i))
				replicatedDevice := *(oDevices[r.Name][id])
				replicatedDevice.ID = annotatedID
				replicatedDevice.Replicas = r.Replicas
				devices.insert(name, &replicatedDevice)
			}
		}
	}

	return devices, nil
}
