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
	"fmt"

	"tags.cncf.io/container-device-interface/specs-go"
)

type simplify struct{}

var _ Transformer = (*simplify)(nil)

// NewSimplifier creates a simplifier transformer.
// This transoformer ensures that entities in the spec are deduplicated and that common edits are removed from device-specific edits.
func NewSimplifier() Transformer {
	return Merge(
		dedupe{},
		simplify{},
		sorter{},
	)
}

// Transform simplifies the supplied spec.
// Edits that are present in the common edits are removed from device-specific edits.
func (s simplify) Transform(spec *specs.Spec) error {
	if spec == nil {
		return nil
	}

	dedupe := dedupe{}
	if err := dedupe.Transform(spec); err != nil {
		return err
	}

	commonEntityIDs, err := (*containerEdits)(&spec.ContainerEdits).getEntityIds()
	if err != nil {
		return err
	}

	toRemove := newRemover(commonEntityIDs...)
	var updatedDevices []specs.Device
	for _, device := range spec.Devices {
		deviceAsSpec := specs.Spec{
			ContainerEdits: device.ContainerEdits,
		}
		err := toRemove.Transform(&deviceAsSpec)
		if err != nil {
			return fmt.Errorf("failed to transform device edits: %w", err)
		}

		if !(containerEdits)(deviceAsSpec.ContainerEdits).IsEmpty() {
			// Devices with empty edits are invalid.
			// We only update the container edits for the device if this would
			// result in a valid device.
			device.ContainerEdits = deviceAsSpec.ContainerEdits
		}
		updatedDevices = append(updatedDevices, device)
	}
	spec.Devices = updatedDevices

	return nil
}
