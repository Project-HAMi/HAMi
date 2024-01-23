/*
*
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
	"encoding/json"

	"tags.cncf.io/container-device-interface/specs-go"
)

type containerEdits specs.ContainerEdits

// IsEmpty returns true if the edits are empty.
func (e containerEdits) IsEmpty() bool {
	// Devices with empty edits are invalid
	if len(e.DeviceNodes) > 0 {
		return false
	}
	if len(e.Env) > 0 {
		return false
	}
	if len(e.Hooks) > 0 {
		return false
	}
	if len(e.Mounts) > 0 {
		return false
	}

	return true
}

func (e *containerEdits) getEntityIds() ([]string, error) {
	if e == nil {
		return nil, nil
	}
	uniqueIDs := make(map[string]bool)

	deviceNodes, err := e.getDeviceNodeIDs()
	if err != nil {
		return nil, err
	}
	for k := range deviceNodes {
		uniqueIDs[k] = true
	}

	envs, err := e.getEnvIDs()
	if err != nil {
		return nil, err
	}
	for k := range envs {
		uniqueIDs[k] = true
	}

	hooks, err := e.getHookIDs()
	if err != nil {
		return nil, err
	}
	for k := range hooks {
		uniqueIDs[k] = true
	}

	mounts, err := e.getMountIDs()
	if err != nil {
		return nil, err
	}
	for k := range mounts {
		uniqueIDs[k] = true
	}

	var ids []string
	for k := range uniqueIDs {
		ids = append(ids, k)
	}

	return ids, nil
}

func (e *containerEdits) getDeviceNodeIDs() (map[string]bool, error) {
	deviceIDs := make(map[string]bool)
	for _, entity := range e.DeviceNodes {
		id, err := deviceNode(*entity).id()
		if err != nil {
			return nil, err
		}
		deviceIDs[id] = true
	}
	return deviceIDs, nil
}

func (e *containerEdits) getEnvIDs() (map[string]bool, error) {
	envIDs := make(map[string]bool)
	for _, entity := range e.Env {
		id, err := env(entity).id()
		if err != nil {
			return nil, err
		}
		envIDs[id] = true
	}
	return envIDs, nil
}

func (e *containerEdits) getHookIDs() (map[string]bool, error) {
	hookIDs := make(map[string]bool)
	for _, entity := range e.Hooks {
		id, err := hook(*entity).id()
		if err != nil {
			return nil, err
		}
		hookIDs[id] = true
	}
	return hookIDs, nil
}

func (e *containerEdits) getMountIDs() (map[string]bool, error) {
	mountIDs := make(map[string]bool)
	for _, entity := range e.Mounts {
		id, err := mount(*entity).id()
		if err != nil {
			return nil, err
		}
		mountIDs[id] = true
	}
	return mountIDs, nil
}

type deviceNode specs.DeviceNode

func (dn deviceNode) id() (string, error) {
	b, err := json.Marshal(dn)
	return string(b), err
}

type env string

func (e env) id() (string, error) {
	return string(e), nil
}

type mount specs.Mount

func (m mount) id() (string, error) {
	b, err := json.Marshal(m)
	return string(b), err
}

type hook specs.Hook

func (m hook) id() (string, error) {
	b, err := json.Marshal(m)
	return string(b), err
}
