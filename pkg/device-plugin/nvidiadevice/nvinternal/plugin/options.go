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

package plugin

import (
	"context"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/info"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/cdi"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/imex"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

// Option is a function that configures a options
type Option func(*options)

// WithCDIHandler sets the CDI handler for the options
func WithCDIHandler(handler cdi.Interface) Option {
	return func(m *options) {
		m.cdiHandler = handler
	}
}

// WithDeviceListStrategies sets the device list strategies.
func WithDeviceListStrategies(deviceListStrategies spec.DeviceListStrategies) Option {
	return func(m *options) {
		m.deviceListStrategies = deviceListStrategies
	}
}

// WithNVML sets the NVML handler for the options
func WithNVML(nvmllib nvml.Interface) Option {
	return func(m *options) {
		m.nvmllib = nvmllib
	}
}

// WithInfoLib sets the info lib for the options.
func WithInfoLib(infolib info.Interface) Option {
	return func(m *options) {
		m.infolib = infolib
	}
}

// WithFailOnInitError sets whether the options should fail on initialization errors
func WithFailOnInitError(failOnInitError bool) Option {
	return func(m *options) {
		m.failOnInitError = failOnInitError
	}
}

// WithConfig sets the config reference for the options
func WithConfig(config *nvidia.DeviceConfig) Option {
	return func(m *options) {
		m.config = config
	}
}

// WithImexChannels sets the imex channels for the manager.
func WithImexChannels(imexChannels imex.Channels) Option {
	return func(m *options) {
		m.imexChannels = imexChannels
	}
}

// WithNodePodList injects cached, read-only node Pod snapshots for allocation
// recovery and detecting cleanup candidates. Absence does not authorize deletion.
func WithNodePodList(list func() ([]*corev1.Pod, error)) Option {
	return func(m *options) {
		m.listNodePods = list
	}
}

// WithLiveNodePodList injects API confirmation for destructive MIG operations.
// Errors must preserve existing resources; callers must never fall back to cache.
func WithLiveNodePodList(list func() ([]*corev1.Pod, error)) Option {
	return func(m *options) { m.listLiveNodePods = list }
}

// WithVGPUCachePreparer injects Allocate-time preparation of a container's
// libvgpu cache directory.
func WithVGPUCachePreparer(prepare func(string, string) (string, error)) Option {
	return func(m *options) {
		m.prepareVGPUCache = prepare
	}
}

// WithNodePodSync injects the initial synchronization gate used only by MIG mode.
func WithNodePodSync(wait func(context.Context) error) Option {
	return func(m *options) { m.waitForNodePodSync = wait }
}
