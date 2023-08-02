/*
<<<<<<< HEAD
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
=======
 * Copyright (c) 2019-2022, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
<<<<<<< HEAD
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
=======
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY Type, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
 */

package rm

import (
	"fmt"
	"strconv"
	"strings"

<<<<<<< HEAD
	"k8s.io/klog/v2"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

// Device wraps kubeletdevicepluginv1beta1.Device with extra metadata and functions.
type Device struct {
	kubeletdevicepluginv1beta1.Device
	Paths             []string
	Index             string
	TotalMemory       uint64
	ComputeCapability string
	// Replicas stores the total number of times this device is replicated.
	// If this is 0 or 1 then the device is not shared.
	Replicas int
=======
	"4pd.io/k8s-vgpu/pkg/util"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

// Device wraps pluginapi.Device with extra metadata and functions.
type Device struct {
	pluginapi.Device
	Paths []string
	Index string
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
}

// deviceInfo defines the information the required to construct a Device
type deviceInfo interface {
	GetUUID() (string, error)
	GetPaths() ([]string, error)
	GetNumaNode() (bool, int, error)
<<<<<<< HEAD
	GetTotalMemory() (uint64, error)
	GetComputeCapability() (string, error)
=======
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
}

// Devices wraps a map[string]*Device with some functions.
type Devices map[string]*Device

// AnnotatedID represents an ID with a replica number embedded in it.
type AnnotatedID string

// AnnotatedIDs can be used to treat a []string as a []AnnotatedID.
type AnnotatedIDs []string

// BuildDevice builds an rm.Device with the specified index and deviceInfo
func BuildDevice(index string, d deviceInfo) (*Device, error) {
	uuid, err := d.GetUUID()
	if err != nil {
		return nil, fmt.Errorf("error getting UUID device: %v", err)
	}

	paths, err := d.GetPaths()
	if err != nil {
		return nil, fmt.Errorf("error getting device paths: %v", err)
	}

	hasNuma, numa, err := d.GetNumaNode()
	if err != nil {
		return nil, fmt.Errorf("error getting device NUMA node: %v", err)
	}

<<<<<<< HEAD
	totalMemory, err := d.GetTotalMemory()
	if err != nil {
		klog.Warningf("Ignoring error getting device memory: %v", err)
	}

	computeCapability, err := d.GetComputeCapability()
	if err != nil {
		return nil, fmt.Errorf("error getting device compute capability: %w", err)
	}

	dev := Device{
		TotalMemory:       totalMemory,
		ComputeCapability: computeCapability,
	}
	dev.ID = uuid
	dev.Index = index
	dev.Paths = paths
	dev.Health = kubeletdevicepluginv1beta1.Healthy
	if hasNuma {
		dev.Topology = &kubeletdevicepluginv1beta1.TopologyInfo{
			Nodes: []*kubeletdevicepluginv1beta1.NUMANode{
=======
	dev := Device{}
	dev.ID = uuid
	dev.Index = index
	dev.Paths = paths
	dev.Health = pluginapi.Healthy
	if hasNuma {
		dev.Topology = &pluginapi.TopologyInfo{
			Nodes: []*pluginapi.NUMANode{
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
				{
					ID: int64(numa),
				},
			},
		}
	}

	return &dev, nil
}

// Contains checks if Devices contains devices matching all ids.
func (ds Devices) Contains(ids ...string) bool {
	for _, id := range ids {
		if _, exists := ds[id]; !exists {
			return false
		}
	}
	return true
}

// GetByID returns a reference to the device matching the specified ID (nil otherwise).
func (ds Devices) GetByID(id string) *Device {
	return ds[id]
}

// GetByIndex returns a reference to the device matching the specified Index (nil otherwise).
func (ds Devices) GetByIndex(index string) *Device {
	for _, d := range ds {
		if d.Index == index {
			return d
		}
	}
	return nil
}

// Subset returns the subset of devices in Devices matching the provided ids.
// If any id in ids is not in Devices, then the subset that did match will be returned.
func (ds Devices) Subset(ids []string) Devices {
	res := make(Devices)
	for _, id := range ids {
		if ds.Contains(id) {
			res[id] = ds[id]
		}
	}
	return res
}

// Difference returns the set of devices contained in ds but not in ods.
func (ds Devices) Difference(ods Devices) Devices {
	res := make(Devices)
	for id := range ds {
		if !ods.Contains(id) {
			res[id] = ds[id]
		}
	}
	return res
}

// GetIDs returns the ids from all devices in the Devices
func (ds Devices) GetIDs() []string {
	var res []string
	for _, d := range ds {
		res = append(res, d.ID)
	}
	return res
}

<<<<<<< HEAD
// GetUUIDs returns the uuids associated with the Device in the set.
func (ds Devices) GetUUIDs() []string {
	var res []string
	seen := make(map[string]bool)
	for _, d := range ds {
		uuid := d.GetUUID()
		if seen[uuid] {
			continue
		}
		seen[uuid] = true
		res = append(res, uuid)
	}
	return res
}

// GetPluginDevices returns the plugin Devices from all devices in the Devices
func (ds Devices) GetPluginDevices(count uint) []*kubeletdevicepluginv1beta1.Device {
	var res []*kubeletdevicepluginv1beta1.Device

	if !strings.Contains(ds.GetIDs()[0], "MIG") {
		for _, dev := range ds {
			for i := uint(0); i < count; i++ {
				id := fmt.Sprintf("%v-%v", dev.ID, i)
				res = append(res, &kubeletdevicepluginv1beta1.Device{
=======
// GetPluginDevices returns the plugin Devices from all devices in the Devices
func (ds Devices) GetPluginDevices() []*pluginapi.Device {
	var res []*pluginapi.Device

	if !strings.Contains(ds.GetIDs()[0], "MIG") {
		for _, dev := range ds {
			for i := uint(0); i < *util.DeviceSplitCount; i++ {
				id := fmt.Sprintf("%v-%v", dev.ID, i)
				res = append(res, &pluginapi.Device{
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
					ID:       id,
					Health:   dev.Health,
					Topology: nil,
				})
			}
		}
	} else {
		for _, d := range ds {
			res = append(res, &d.Device)
		}

	}

	return res
}

// GetIndices returns the Indices from all devices in the Devices
func (ds Devices) GetIndices() []string {
	var res []string
	for _, d := range ds {
		res = append(res, d.Index)
	}
	return res
}

// GetPaths returns the Paths from all devices in the Devices
func (ds Devices) GetPaths() []string {
	var res []string
	for _, d := range ds {
		res = append(res, d.Paths...)
	}
	return res
}

<<<<<<< HEAD
// AlignedAllocationSupported checks whether all devices support an aligned allocation
=======
// AlignedAllocationSupported checks whether all devices support an alligned allocation
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
func (ds Devices) AlignedAllocationSupported() bool {
	for _, d := range ds {
		if !d.AlignedAllocationSupported() {
			return false
		}
	}
	return true
}

<<<<<<< HEAD
// AlignedAllocationSupported checks whether the device supports an aligned allocation
=======
// AlignedAllocationSupported checks whether the device supports an alligned allocation
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
func (d Device) AlignedAllocationSupported() bool {
	if d.IsMigDevice() {
		return false
	}

	for _, p := range d.Paths {
		if p == "/dev/dxg" {
			return false
		}
	}

	return true
}

// IsMigDevice returns checks whether d is a MIG device or not.
func (d Device) IsMigDevice() bool {
	return strings.Contains(d.Index, ":")
}

// GetUUID returns the UUID for the device from the annotated ID.
func (d Device) GetUUID() string {
	return AnnotatedID(d.ID).GetID()
}

// NewAnnotatedID creates a new AnnotatedID from an ID and a replica number.
func NewAnnotatedID(id string, replica int) AnnotatedID {
	return AnnotatedID(fmt.Sprintf("%s::%d", id, replica))
}

// HasAnnotations checks if an AnnotatedID has any annotations or not.
func (r AnnotatedID) HasAnnotations() bool {
	split := strings.SplitN(string(r), "::", 2)
<<<<<<< HEAD
	return len(split) == 2
=======
	if len(split) != 2 {
		return false
	}
	return true
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
}

// Split splits a AnnotatedID into its ID and replica number parts.
func (r AnnotatedID) Split() (string, int) {
	split := strings.SplitN(string(r), "::", 2)
	if len(split) != 2 {
		return string(r), 0
	}
	replica, _ := strconv.ParseInt(split[1], 10, 0)
	return split[0], int(replica)
}

// GetID returns just the ID part of the replicated ID
func (r AnnotatedID) GetID() string {
	id, _ := r.Split()
	return id
}

// AnyHasAnnotations checks if any ID has annotations or not.
func (rs AnnotatedIDs) AnyHasAnnotations() bool {
	for _, r := range rs {
		if AnnotatedID(r).HasAnnotations() {
			return true
		}
	}
	return false
}

// GetIDs returns just the ID parts of the annotated IDs as a []string
func (rs AnnotatedIDs) GetIDs() []string {
	res := make([]string, len(rs))
	for i, r := range rs {
		res[i] = AnnotatedID(r).GetID()
	}
	return res
}
