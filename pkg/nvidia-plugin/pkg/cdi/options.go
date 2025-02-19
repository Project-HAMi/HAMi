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

package cdi

import (
	spec "github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/api/config/v1"
	"github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/pkg/imex"
)

// Option defines a function for passing options to the New() call
type Option func(*cdiHandler)

// WithDeviceListStrategies provides an Option to set the enabled flag used by the 'cdi' interface
func WithDeviceListStrategies(deviceListStrategies spec.DeviceListStrategies) Option {
	return func(c *cdiHandler) {
		c.deviceListStrategies = deviceListStrategies
	}
}

// WithDriverRoot provides an Option to set the driver root used by the 'cdi' interface.
func WithDriverRoot(root string) Option {
	return func(c *cdiHandler) {
		c.driverRoot = root
	}
}

// WithDevRoot sets the dev root for the `cdi` interface.
func WithDevRoot(root string) Option {
	return func(c *cdiHandler) {
		c.devRoot = root
	}
}

// WithTargetDriverRoot provides an Option to set the target (host) driver root used by the 'cdi' interface
func WithTargetDriverRoot(root string) Option {
	return func(c *cdiHandler) {
		c.targetDriverRoot = root
	}
}

// WithTargetDevRoot provides an Option to set the target (host) dev root used by the 'cdi' interface
func WithTargetDevRoot(root string) Option {
	return func(c *cdiHandler) {
		c.targetDevRoot = root
	}
}

// WithNvidiaCTKPath provides an Option to set the nvidia-ctk path used by the 'cdi' interface
func WithNvidiaCTKPath(path string) Option {
	return func(c *cdiHandler) {
		c.nvidiaCTKPath = path
	}
}

// WithDeviceIDStrategy provides an Option to set the device ID strategy used by the 'cdi' interface
func WithDeviceIDStrategy(strategy string) Option {
	return func(c *cdiHandler) {
		c.deviceIDStrategy = strategy
	}
}

// WithVendor provides an Option to set the vendor used by the 'cdi' interface
func WithVendor(vendor string) Option {
	return func(c *cdiHandler) {
		c.vendor = vendor
	}
}

// WithGdsEnabled provides and option to set whether a GDS CDI spec should be generated
func WithGdsEnabled(enabled bool) Option {
	return func(c *cdiHandler) {
		c.gdsEnabled = enabled
	}
}

// WithMofedEnabled provides and option to set whether a MOFED CDI spec should be generated
func WithMofedEnabled(enabled bool) Option {
	return func(c *cdiHandler) {
		c.mofedEnabled = enabled
	}
}

// WithImexChannels sets the IMEX channels for which CDI specs should be generated.
func WithImexChannels(imexChannels imex.Channels) Option {
	return func(c *cdiHandler) {
		c.imexChannels = imexChannels
	}
}
