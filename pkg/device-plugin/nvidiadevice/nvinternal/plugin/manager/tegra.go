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

package manager

import (
	"fmt"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/plugin"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/rm"
)

type tegramanager manager

// GetPlugins returns the plugins associated with the NVML resources available on the node
func (m *tegramanager) GetPlugins() ([]plugin.Interface, error) {
	rms, err := rm.NewTegraResourceManagers(m.config)
	if err != nil {
		return nil, fmt.Errorf("failed to construct NVML resource managers: %v", err)
	}

	var plugins []plugin.Interface
	for _, r := range rms {
		plugins = append(plugins, plugin.NewNvidiaDevicePlugin(m.config, r, m.cdiHandler, m.cdiEnabled))
	}
	return plugins, nil
}

// CreateCDISpecFile creates the spec is a no-op for the tegra plugin
func (m *tegramanager) CreateCDISpecFile() error {
	return nil
}
