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

type remove map[string]bool

func newRemover(ids ...string) Transformer {
	r := make(remove)
	for _, id := range ids {
		r[id] = true
	}
	return r
}

// Transform remove the specified entities from the spec.
func (r remove) Transform(spec *specs.Spec) error {
	if spec == nil {
		return nil
	}

	for _, device := range spec.Devices {
		device := device
		if err := r.transformEdits(&device.ContainerEdits); err != nil {
			return fmt.Errorf("failed to remove edits from device %q: %w", device.Name, err)
		}
	}

	return r.transformEdits(&spec.ContainerEdits)
}

func (r remove) transformEdits(edits *specs.ContainerEdits) error {
	if edits == nil {
		return nil
	}

	var deviceNodes []*specs.DeviceNode
	for _, entity := range edits.DeviceNodes {
		id, err := deviceNode(*entity).id()
		if err != nil {
			return err
		}
		if r[id] {
			continue
		}
		deviceNodes = append(deviceNodes, entity)
	}
	edits.DeviceNodes = deviceNodes

	var envs []string
	for _, entity := range edits.Env {
		id := entity
		if r[id] {
			continue
		}
		envs = append(envs, entity)
	}
	edits.Env = envs

	var hooks []*specs.Hook
	for _, entity := range edits.Hooks {
		id, err := hook(*entity).id()
		if err != nil {
			return err
		}
		if r[id] {
			continue
		}
		hooks = append(hooks, entity)
	}
	edits.Hooks = hooks

	var mounts []*specs.Mount
	for _, entity := range edits.Mounts {
		id, err := mount(*entity).id()
		if err != nil {
			return err
		}
		if r[id] {
			continue
		}
		mounts = append(mounts, entity)
	}
	edits.Mounts = mounts

	return nil
}
