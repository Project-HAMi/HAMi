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

package plugin

import (
	"github.com/NVIDIA/go-nvlib/pkg/nvlib/info"
	"github.com/NVIDIA/go-nvml/pkg/nvml"

	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	spec "github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/api/config/v1"
	"github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/pkg/cdi"
	"github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/pkg/imex"
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
