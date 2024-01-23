/**
# Copyright (c) NVIDIA CORPORATION.  All rights reserved.
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

package transform

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"tags.cncf.io/container-device-interface/specs-go"
)

type sorter struct{}

var _ Transformer = (*sorter)(nil)

// NewSorter creates a transformer that sorts container edits.
func NewSorter() Transformer {
	return nil
}

// Transform sorts the entities in the specified CDI specification.
func (d sorter) Transform(spec *specs.Spec) error {
	if spec == nil {
		return nil
	}
	if err := d.transformEdits(&spec.ContainerEdits); err != nil {
		return err
	}
	var updatedDevices []specs.Device
	for _, device := range spec.Devices {
		device := device
		if err := d.transformEdits(&device.ContainerEdits); err != nil {
			return err
		}
		updatedDevices = append(updatedDevices, device)
	}
	spec.Devices = d.sortDevices(updatedDevices)
	return nil
}

func (d sorter) transformEdits(edits *specs.ContainerEdits) error {
	edits.DeviceNodes = d.sortDeviceNodes(edits.DeviceNodes)
	edits.Mounts = d.sortMounts(edits.Mounts)
	return nil
}

func (d sorter) sortDevices(devices []specs.Device) []specs.Device {
	sort.Slice(devices, func(i, j int) bool {
		return devices[i].Name < devices[j].Name
	})
	return devices
}

// sortDeviceNodes sorts the specified device nodes by container path.
// If two device nodes have the same container path, the host path is used to break ties.
func (d sorter) sortDeviceNodes(entities []*specs.DeviceNode) []*specs.DeviceNode {
	sort.Slice(entities, func(i, j int) bool {
		ip := strings.Count(filepath.Clean(entities[i].Path), string(os.PathSeparator))
		jp := strings.Count(filepath.Clean(entities[j].Path), string(os.PathSeparator))
		if ip == jp {
			return entities[i].Path < entities[j].Path
		}
		return ip < jp
	})
	return entities
}

// sortMounts sorts the specified mounts by container path.
// If two mounts have the same mount path, the host path is used to break ties.
func (d sorter) sortMounts(entities []*specs.Mount) []*specs.Mount {
	sort.Slice(entities, func(i, j int) bool {
		ip := strings.Count(filepath.Clean(entities[i].ContainerPath), string(os.PathSeparator))
		jp := strings.Count(filepath.Clean(entities[j].ContainerPath), string(os.PathSeparator))
		if ip == jp {
			return entities[i].ContainerPath < entities[j].ContainerPath
		}
		return ip < jp
	})
	return entities
}
