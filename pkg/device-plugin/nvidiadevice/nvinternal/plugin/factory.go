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
	"fmt"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvlib/pkg/nvlib/info"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"k8s.io/klog/v2"

	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/cdi"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/imex"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/rm"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

type options struct {
	infolib   info.Interface
	nvmllib   nvml.Interface
	devicelib device.Interface

	failOnInitError bool

	cdiHandler cdi.Interface
	config     *nvidia.DeviceConfig

	deviceListStrategies spec.DeviceListStrategies

	imexChannels imex.Channels
}

// New a new set of plugins with the supplied options.
func New(ctx context.Context, infolib info.Interface, nvmllib nvml.Interface, devicelib device.Interface, opts ...Option) ([]Interface, error) {
	o := &options{
		infolib:   infolib,
		nvmllib:   nvmllib,
		devicelib: devicelib,
	}
	for _, opt := range opts {
		opt(o)
	}

	if o.config == nil {
		klog.Warning("no config provided, returning a null manager")
		return nil, nil
	}

	if o.cdiHandler == nil {
		o.cdiHandler = cdi.NewNullHandler()
	}

	resourceManagers, err := o.getResourceManagers()
	if err != nil {
		return nil, fmt.Errorf("failed to construct resource managers: %w", err)
	}

	sConfig, mode, err := LoadNvidiaDevicePluginConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load nvidia plugin config: %v", err)
	}

	var plugins []Interface
	for _, resourceManager := range resourceManagers {
		plugin, err := o.devicePluginForResource(ctx, o.config, resourceManager, sConfig, mode)
		if err != nil {
			return nil, fmt.Errorf("failed to create plugin: %w", err)
		}
		plugins = append(plugins, plugin)
	}
	return plugins, nil
}

// getResourceManager constructs a set of resource managers.
// Each resource manager maps to a specific named extended resource and may
// include full GPUs or MIG devices.
func (o *options) getResourceManagers() ([]rm.ResourceManager, error) {
	strategy := o.resolveStrategy(*o.config.Flags.DeviceDiscoveryStrategy)
	switch strategy {
	case "nvml":
		ret := o.nvmllib.Init()
		if ret != nvml.SUCCESS {
			klog.Errorf("Failed to initialize NVML: %v.", ret)
			klog.Errorf("If this is a GPU node, did you set the docker default runtime to `nvidia`?")
			klog.Errorf("You can check the prerequisites at: https://github.com/NVIDIA/k8s-device-plugin#prerequisites")
			klog.Errorf("You can learn how to set the runtime at: https://github.com/NVIDIA/k8s-device-plugin#quick-start")
			klog.Errorf("If this is not a GPU node, you should set up a toleration or nodeSelector to only deploy this plugin on GPU nodes")
			if o.failOnInitError {
				return nil, fmt.Errorf("nvml init failed: %v", ret)
			}
			klog.Warningf("nvml init failed: %v", ret)
			return nil, nil
		}
		defer func() {
			_ = o.nvmllib.Shutdown()
		}()

		return rm.NewNVMLResourceManagers(o.infolib, o.nvmllib, o.devicelib, o.config.Config)
	case "tegra":
		return rm.NewTegraResourceManagers(o.config.Config)
	default:
		klog.Errorf("Incompatible strategy detected %v", strategy)
		klog.Error("If this is a GPU node, did you configure the NVIDIA Container Toolkit?")
		klog.Error("You can check the prerequisites at: https://github.com/NVIDIA/k8s-device-plugin#prerequisites")
		klog.Error("You can learn how to set the runtime at: https://github.com/NVIDIA/k8s-device-plugin#quick-start")
		klog.Error("If this is not a GPU node, you should set up a toleration or nodeSelector to only deploy this plugin on GPU nodes")
		if o.failOnInitError {
			return nil, fmt.Errorf("invalid device discovery strategy")
		}
		return nil, nil
	}
}

func (o *options) resolveStrategy(strategy string) string {
	if strategy != "" && strategy != "auto" {
		return strategy
	}

	platform := o.infolib.ResolvePlatform()
	switch platform {
	case info.PlatformNVML, info.PlatformWSL:
		return "nvml"
	case info.PlatformTegra:
		return "tegra"
	}
	return strategy
}
