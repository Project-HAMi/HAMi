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
	"github.com/NVIDIA/go-nvml/pkg/nvml"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/cdi"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

// Option is a function that configures a manager
type Option func(*manager)

// WithCDIEnabled sets whether CDI is enabled for the manager
func WithCDIEnabled(enabled bool) Option {
	return func(m *manager) {
		m.cdiEnabled = enabled
	}
}

// WithCDIHandler sets the CDI handler for the manager
func WithCDIHandler(handler cdi.Interface) Option {
	return func(m *manager) {
		m.cdiHandler = handler
	}
}

// WithNVML sets the NVML handler for the manager
func WithNVML(nvmllib nvml.Interface) Option {
	return func(m *manager) {
		m.nvmllib = nvmllib
	}
}

// WithFailOnInitError sets whether the manager should fail on initialization errors
func WithFailOnInitError(failOnInitError bool) Option {
	return func(m *manager) {
		m.failOnInitError = failOnInitError
	}
}

// WithMigStrategy sets the MIG strategy for the manager
func WithMigStrategy(migStrategy string) Option {
	return func(m *manager) {
		m.migStrategy = migStrategy
	}
}

// WithConfig sets the config reference for the manager
func WithConfig(config *nvidia.DeviceConfig) Option {
	return func(m *manager) {
		m.config = config
	}
}
