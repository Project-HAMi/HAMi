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
	"tags.cncf.io/container-device-interface/specs-go"
)

type dedupe struct{}

var _ Transformer = (*dedupe)(nil)

// NewDedupe creates a transformer that deduplicates container edits.
func NewDedupe() (Transformer, error) {
	return &dedupe{}, nil
}

// Transform removes duplicate entris from devices and common container edits.
func (d dedupe) Transform(spec *specs.Spec) error {
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
	spec.Devices = updatedDevices
	return nil
}

func (d dedupe) transformEdits(edits *specs.ContainerEdits) error {
	deviceNodes, err := d.deduplicateDeviceNodes(edits.DeviceNodes)
	if err != nil {
		return err
	}
	edits.DeviceNodes = deviceNodes

	envs, err := d.deduplicateEnvs(edits.Env)
	if err != nil {
		return err
	}
	edits.Env = envs

	hooks, err := d.deduplicateHooks(edits.Hooks)
	if err != nil {
		return err
	}
	edits.Hooks = hooks

	mounts, err := d.deduplicateMounts(edits.Mounts)
	if err != nil {
		return err
	}
	edits.Mounts = mounts

	return nil
}

func (d dedupe) deduplicateDeviceNodes(entities []*specs.DeviceNode) ([]*specs.DeviceNode, error) {
	seen := make(map[string]bool)
	var deviceNodes []*specs.DeviceNode
	for _, e := range entities {
		if e == nil {
			continue
		}
		id, err := deviceNode(*e).id()
		if err != nil {
			return nil, err
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		deviceNodes = append(deviceNodes, e)
	}
	return deviceNodes, nil
}

func (d dedupe) deduplicateEnvs(entities []string) ([]string, error) {
	seen := make(map[string]bool)
	var envs []string
	for _, e := range entities {
		id := e
		if seen[id] {
			continue
		}
		seen[id] = true
		envs = append(envs, e)
	}
	return envs, nil
}

func (d dedupe) deduplicateHooks(entities []*specs.Hook) ([]*specs.Hook, error) {
	seen := make(map[string]bool)
	var hooks []*specs.Hook
	for _, e := range entities {
		if e == nil {
			continue
		}
		id, err := hook(*e).id()
		if err != nil {
			return nil, err
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		hooks = append(hooks, e)
	}
	return hooks, nil
}

func (d dedupe) deduplicateMounts(entities []*specs.Mount) ([]*specs.Mount, error) {
	seen := make(map[string]bool)
	var mounts []*specs.Mount
	for _, e := range entities {
		if e == nil {
			continue
		}
		id, err := mount(*e).id()
		if err != nil {
			return nil, err
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		mounts = append(mounts, e)
	}
	return mounts, nil
}
