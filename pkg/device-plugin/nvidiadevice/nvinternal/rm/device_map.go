<<<<<<< HEAD
/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * The HAMi Contributors require contributions made to
 * this file be licensed under the Apache-2.0 license or a
 * compatible open source license.
 */

/*
 * Licensed to NVIDIA CORPORATION under one or more contributor
 * license agreements. See the NOTICE file distributed with
 * this work for additional information regarding copyright
 * ownership. NVIDIA CORPORATION licenses this file to you under
 * the Apache License, Version 2.0 (the "License"); you may
 * not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/*
 * Modifications Copyright The HAMi Authors. See
 * GitHub history for details.
 */
=======
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
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)

package rm

import (
	"fmt"

<<<<<<< HEAD
	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvlib/pkg/nvlib/info"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"k8s.io/klog/v2"

	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
=======
	"4pd.io/k8s-vgpu/pkg/util"
	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"gitlab.com/nvidia/cloud-native/go-nvlib/pkg/nvlib/device"
	"gitlab.com/nvidia/cloud-native/go-nvlib/pkg/nvml"
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
)

type deviceMapBuilder struct {
	device.Interface
<<<<<<< HEAD
	migStrategy         *string
	resources           *spec.Resources
	replicatedResources *spec.ReplicatedResources

	newGPUDevice func(i int, gpu nvml.Device) (string, deviceInfo)
=======
	config *util.DeviceConfig
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
}

// DeviceMap stores a set of devices per resource name.
type DeviceMap map[spec.ResourceName]Devices

// NewDeviceMap creates a device map for the specified NVML library and config.
<<<<<<< HEAD
func NewDeviceMap(infolib info.Interface, devicelib device.Interface, config *spec.Config) (DeviceMap, error) {
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

=======
func NewDeviceMap(nvmllib nvml.Interface, config *util.DeviceConfig) (DeviceMap, error) {
	b := deviceMapBuilder{
		Interface: device.New(device.WithNvml(nvmllib)),
		config:    config,
	}
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
	return b.build()
}

// build builds a map of resource names to devices.
func (b *deviceMapBuilder) build() (DeviceMap, error) {
	devices, err := b.buildDeviceMapFromConfigResources()
	if err != nil {
		return nil, fmt.Errorf("error building device map from config.resources: %v", err)
	}
<<<<<<< HEAD
	devices, err = updateDeviceMapWithReplicas(b.replicatedResources, devices)
	if err != nil {
		return nil, fmt.Errorf("error updating device map with replicas from replicatedResources config: %v", err)
=======
	devices, err = updateDeviceMapWithReplicas(b.config, devices)
	if err != nil {
		return nil, fmt.Errorf("error updating device map with replicas from config.sharing.timeSlicing.resources: %v", err)
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
	}
	return devices, nil
}

// buildDeviceMapFromConfigResources builds a map of resource names to devices from spec.Config.Resources
func (b *deviceMapBuilder) buildDeviceMapFromConfigResources() (DeviceMap, error) {
	deviceMap, err := b.buildGPUDeviceMap()
	if err != nil {
		return nil, fmt.Errorf("error building GPU device map: %v", err)
	}

<<<<<<< HEAD
	if *b.migStrategy == spec.MigStrategyNone {
=======
	if *b.config.Flags.MigStrategy == spec.MigStrategyNone {
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
		return deviceMap, nil
	}

	migDeviceMap, err := b.buildMigDeviceMap()
	if err != nil {
		return nil, fmt.Errorf("error building MIG device map: %v", err)
	}

	var requireUniformMIGDevices bool
<<<<<<< HEAD
	if *b.migStrategy == spec.MigStrategySingle {
=======
	if *b.config.Flags.MigStrategy == spec.MigStrategySingle {
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
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

<<<<<<< HEAD
	err := b.VisitDevices(func(i int, gpu device.Device) error {
=======
	b.VisitDevices(func(i int, gpu device.Device) error {
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
		name, ret := gpu.GetName()
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting product name for GPU: %v", ret)
		}
		migEnabled, err := gpu.IsMigEnabled()
		if err != nil {
			return fmt.Errorf("error checking if MIG is enabled on GPU: %v", err)
		}
<<<<<<< HEAD
		if migEnabled && *b.migStrategy != spec.MigStrategyNone {
			return nil
		}
		for _, resource := range b.resources.GPUs {
			if resource.Pattern.Matches(name) {
				index, info := b.newGPUDevice(i, gpu)
=======
		if migEnabled && *b.config.Flags.MigStrategy != spec.MigStrategyNone {
			return nil
		}
		for _, resource := range b.config.Resources.GPUs {
			if resource.Pattern.Matches(name) {
				index, info := newGPUDevice(i, gpu)
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
				return devices.setEntry(resource.Name, index, info)
			}
		}
		return fmt.Errorf("GPU name '%v' does not match any resource patterns", name)
	})
<<<<<<< HEAD
	return devices, err
=======
	return devices, nil
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
}

// buildMigDeviceMap builds a map of resource names to MIG devices
func (b *deviceMapBuilder) buildMigDeviceMap() (DeviceMap, error) {
	devices := make(DeviceMap)
	err := b.VisitMigDevices(func(i int, d device.Device, j int, mig device.MigDevice) error {
		migProfile, err := mig.GetProfile()
		if err != nil {
			return fmt.Errorf("error getting MIG profile for MIG device at index '(%v, %v)': %v", i, j, err)
		}
<<<<<<< HEAD
		for _, resource := range b.resources.MIGs {
=======
		for _, resource := range b.config.Resources.MIGs {
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
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
<<<<<<< HEAD
		if uniform && len(migDevices) == 0 {
			return fmt.Errorf("device %v has no MIG devices configured", i)
		}
		if !uniform && len(migDevices) == 0 {
			klog.Warningf("device %v has no MIG devices configured", i)
=======
		if len(migDevices) == 0 {
			i := 0
			return fmt.Errorf("device %v has an invalid MIG configuration", i)
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
		}
		return nil
	})
	if err != nil {
<<<<<<< HEAD
		return fmt.Errorf("at least one device with migEnabled=true was not configured correctly: %v", err)
=======
		return fmt.Errorf("At least one device with migEnabled=true was not configured correctly: %v", err)
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
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

<<<<<<< HEAD
// updateDeviceMapWithReplicas returns an updated map of resource names to devices with replica
// information from the active replicated resources config.
func updateDeviceMapWithReplicas(replicatedResources *spec.ReplicatedResources, oDevices DeviceMap) (DeviceMap, error) {
	devices := make(DeviceMap)

	// Begin by walking replicatedResources.Resources and building a map of just the resource names.
	names := make(map[spec.ResourceName]bool)
	for _, r := range replicatedResources.Resources {
=======
// updateDeviceMapWithReplicas returns an updated map of resource names to devices with replica information from spec.Config.Sharing.TimeSlicing.Resources
func updateDeviceMapWithReplicas(config *util.DeviceConfig, oDevices DeviceMap) (DeviceMap, error) {
	devices := make(DeviceMap)

	// Begin by walking config.Sharing.TimeSlicing.Resources and building a map of just the resource names.
	names := make(map[spec.ResourceName]bool)
	for _, r := range config.Sharing.TimeSlicing.Resources {
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
		names[r.Name] = true
	}

	// Copy over all devices from oDevices without a resource reference in TimeSlicing.Resources.
	for r, ds := range oDevices {
		if !names[r] {
			devices[r] = ds
		}
	}

<<<<<<< HEAD
	// Walk shared Resources and update devices in the device map as appropriate.
	for _, resource := range replicatedResources.Resources {
		r := resource
=======
	// Walk TimeSlicing.Resources and update devices in the device map as appropriate.
	for _, r := range config.Sharing.TimeSlicing.Resources {
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
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
<<<<<<< HEAD
				replicatedDevice.Replicas = r.Replicas
=======
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
				devices.insert(name, &replicatedDevice)
			}
		}
	}

	return devices, nil
}
