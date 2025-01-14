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

package manager

import (
	"fmt"

	"github.com/Project-HAMi/HAMi/pkg/abt/device-plugins/nvidiadevice"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/plugin"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/rm"
)

type nvmlmanager manager

// GetPlugins returns the plugins associated with the NVML resources available on the node
func (m *nvmlmanager) GetPlugins() ([]plugin.Interface, error) {
	rms, err := rm.NewNVMLResourceManagers(m.nvmllib, m.config)
	if err != nil {
		return nil, fmt.Errorf("failed to construct NVML resource managers: %v", err)
	}

	var plugins []plugin.Interface
	for _, r := range rms {
		plugins = append(plugins, plugin.NewNvidiaDevicePlugin(m.config, r, m.cdiHandler, m.cdiEnabled))
	}
	return plugins, nil
}

// CreateCDISpecFile creates forwards the request to the CDI handler
func (m *nvmlmanager) CreateCDISpecFile() error {
	return m.cdiHandler.CreateSpecFile()
}

func (m *nvmlmanager) GetVirtualPlugins() ([]plugin.Interface, error) {
	var plugins []plugin.Interface
	memoryPlugin, err := nvidiadevice.NewNvidiaMemoryPlugin(m.nvmllib)
	if err != nil {
		return nil, fmt.Errorf("failed to new nvidia memory plugin: %w", err)
	}
	corePlugin, err := nvidiadevice.NewNvidiaCorePlugin(m.nvmllib)
	if err != nil {
		return nil, fmt.Errorf("failed to new nvidia core plugin: %w", err)
	}
	plugins = append(plugins, memoryPlugin, corePlugin)
	return plugins, nil
}
