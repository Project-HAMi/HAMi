/*
 * Copyright (c) 2019-2022, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY Type, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package rm

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Project-HAMi/HAMi/pkg/util"

	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

// Device wraps kubeletdevicepluginv1beta1.Device with extra metadata and functions.
type Device struct {
	kubeletdevicepluginv1beta1.Device
	Paths []string
	Index string
}

// deviceInfo defines the information the required to construct a Device
type deviceInfo interface {
	GetUUID() (string, error)
	GetPaths() ([]string, error)
	GetNumaNode() (bool, int, error)
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

	dev := Device{}
	dev.ID = uuid
	dev.Index = index
	dev.Paths = paths
	dev.Health = kubeletdevicepluginv1beta1.Healthy
	if hasNuma {
		dev.Topology = &kubeletdevicepluginv1beta1.TopologyInfo{
			Nodes: []*kubeletdevicepluginv1beta1.NUMANode{
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

// GetPluginDevices returns the plugin Devices from all devices in the Devices
func (ds Devices) GetPluginDevices() []*kubeletdevicepluginv1beta1.Device {
	var res []*kubeletdevicepluginv1beta1.Device

	if !strings.Contains(ds.GetIDs()[0], "MIG") {
		for _, dev := range ds {
			for i := uint(0); i < *util.DeviceSplitCount; i++ {
				id := fmt.Sprintf("%v-%v", dev.ID, i)
				res = append(res, &kubeletdevicepluginv1beta1.Device{
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

// AlignedAllocationSupported checks whether all devices support an alligned allocation
func (ds Devices) AlignedAllocationSupported() bool {
	for _, d := range ds {
		if !d.AlignedAllocationSupported() {
			return false
		}
	}
	return true
}

// AlignedAllocationSupported checks whether the device supports an alligned allocation
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
	if len(split) != 2 {
		return false
	}
	return true
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
