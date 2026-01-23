<<<<<<< HEAD
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
=======
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
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)

package cdi

import (
<<<<<<< HEAD
<<<<<<< HEAD
	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/imex"
=======
	"gitlab.com/nvidia/cloud-native/go-nvlib/pkg/nvml"
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
=======
	"github.com/NVIDIA/go-nvlib/pkg/nvml"
>>>>>>> c7a3893 (Remake this repo to HAMi)
)

// Option defines a function for passing options to the New() call
type Option func(*cdiHandler)

<<<<<<< HEAD
// WithDeviceListStrategies provides an Option to set the enabled flag used by the 'cdi' interface
func WithDeviceListStrategies(deviceListStrategies spec.DeviceListStrategies) Option {
	return func(c *cdiHandler) {
		c.deviceListStrategies = deviceListStrategies
	}
}

// WithDriverRoot provides an Option to set the driver root used by the 'cdi' interface.
=======
// WithEnabled provides an Option to set the enabled flag used by the 'cdi' interface
func WithEnabled(enabled bool) Option {
	return func(c *cdiHandler) {
		c.enabled = enabled
	}
}

// WithDriverRoot provides an Option to set the driver root used by the 'cdi' interface
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
func WithDriverRoot(root string) Option {
	return func(c *cdiHandler) {
		c.driverRoot = root
	}
}

<<<<<<< HEAD
// WithDevRoot sets the dev root for the `cdi` interface.
func WithDevRoot(root string) Option {
	return func(c *cdiHandler) {
		c.devRoot = root
	}
}

// WithTargetDriverRoot provides an Option to set the target (host) driver root used by the 'cdi' interface
=======
// WithTargetDriverRoot provides an Option to set the target driver root used by the 'cdi' interface
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
func WithTargetDriverRoot(root string) Option {
	return func(c *cdiHandler) {
		c.targetDriverRoot = root
	}
}

<<<<<<< HEAD
// WithTargetDevRoot provides an Option to set the target (host) dev root used by the 'cdi' interface
func WithTargetDevRoot(root string) Option {
	return func(c *cdiHandler) {
		c.targetDevRoot = root
	}
}

=======
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
// WithNvidiaCTKPath provides an Option to set the nvidia-ctk path used by the 'cdi' interface
func WithNvidiaCTKPath(path string) Option {
	return func(c *cdiHandler) {
		c.nvidiaCTKPath = path
	}
}

<<<<<<< HEAD
=======
// WithNvml provides an Option to set the NVML library used by the 'cdi' interface
func WithNvml(nvml nvml.Interface) Option {
	return func(c *cdiHandler) {
		c.nvml = nvml
	}
}

>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
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

<<<<<<< HEAD
// WithGdrcopyEnabled provides an option to set whether a GDS CDI spec should be generated
func WithGdrcopyEnabled(enabled bool) Option {
	return func(c *cdiHandler) {
		c.gdrcopyEnabled = enabled
	}
}

// WithGdsEnabled provides an option to set whether a GDS CDI spec should be generated
=======
// WithGdsEnabled provides and option to set whether a GDS CDI spec should be generated
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
func WithGdsEnabled(enabled bool) Option {
	return func(c *cdiHandler) {
		c.gdsEnabled = enabled
	}
}

<<<<<<< HEAD
// WithMofedEnabled provides an option to set whether a MOFED CDI spec should be generated
=======
// WithMofedEnabled provides and option to set whether a MOFED CDI spec should be generated
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
func WithMofedEnabled(enabled bool) Option {
	return func(c *cdiHandler) {
		c.mofedEnabled = enabled
	}
}
<<<<<<< HEAD

// WithImexChannels sets the IMEX channels for which CDI specs should be generated.
func WithImexChannels(imexChannels imex.Channels) Option {
	return func(c *cdiHandler) {
		c.imexChannels = imexChannels
	}
}
=======
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
