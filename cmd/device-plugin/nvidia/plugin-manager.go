/*
<<<<<<< HEAD
Copyright 2024 The HAMi Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
=======
 * Copyright (c) 2020, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)

package main

import (
<<<<<<< HEAD
	"context"
	"fmt"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/cdi"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/imex"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/plugin"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvlib/pkg/nvlib/info"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
)

// GetPlugins returns a set of plugins for the specified configuration.
func GetPlugins(ctx context.Context, infolib info.Interface, nvmllib nvml.Interface, devicelib device.Interface, config *nvidia.DeviceConfig) ([]plugin.Interface, error) {
	// TODO: We could consider passing this as an argument since it should already be used to construct nvmllib.
	driverRoot := root(*config.Flags.Plugin.ContainerDriverRoot)
=======
	"fmt"

	"4pd.io/k8s-vgpu/pkg/device-plugin/nvidiadevice/nvinternal/cdi"
	"4pd.io/k8s-vgpu/pkg/device-plugin/nvidiadevice/nvinternal/plugin/manager"
	"4pd.io/k8s-vgpu/pkg/util"
	"github.com/NVIDIA/go-nvlib/pkg/nvml"
	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
)

// NewPluginManager creates an NVML-based plugin manager
func NewPluginManager(config *util.DeviceConfig) (manager.Interface, error) {
	var err error
	switch *config.Flags.MigStrategy {
	case spec.MigStrategyNone:
	case spec.MigStrategySingle:
	case spec.MigStrategyMixed:
	default:
		return nil, fmt.Errorf("unknown strategy: %v", *config.Flags.MigStrategy)
	}

	nvmllib := nvml.New()
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)

	deviceListStrategies, err := spec.NewDeviceListStrategies(*config.Flags.Plugin.DeviceListStrategy)
	if err != nil {
		return nil, fmt.Errorf("invalid device list strategy: %v", err)
	}

<<<<<<< HEAD
	imexChannels, err := imex.GetChannels(config.Config, driverRoot.getDevRoot())
	if err != nil {
		return nil, fmt.Errorf("error querying IMEX channels: %w", err)
	}

	cdiHandler, err := cdi.New(infolib, nvmllib, devicelib,
		cdi.WithDeviceListStrategies(deviceListStrategies),
		cdi.WithDriverRoot(string(driverRoot)),
		cdi.WithDevRoot(driverRoot.getDevRoot()),
		cdi.WithTargetDriverRoot(*config.Flags.NvidiaDriverRoot),
		cdi.WithTargetDevRoot(*config.Flags.NvidiaDevRoot),
		cdi.WithNvidiaCTKPath(*config.Flags.Plugin.NvidiaCTKPath),
		cdi.WithDeviceIDStrategy(*config.Flags.Plugin.DeviceIDStrategy),
		cdi.WithVendor("k8s.device-plugin.nvidia.com"),
		cdi.WithGdrcopyEnabled(*config.Flags.GDRCopyEnabled),
		cdi.WithGdsEnabled(*config.Flags.GDSEnabled),
		cdi.WithMofedEnabled(*config.Flags.MOFEDEnabled),
		cdi.WithImexChannels(imexChannels),
=======
	cdiEnabled := deviceListStrategies.IsCDIEnabled()

	cdiHandler, err := cdi.New(
		cdi.WithEnabled(cdiEnabled),
		cdi.WithDriverRoot(*config.Flags.Plugin.ContainerDriverRoot),
		cdi.WithTargetDriverRoot(*config.Flags.NvidiaDriverRoot),
		cdi.WithNvidiaCTKPath(*config.Flags.Plugin.NvidiaCTKPath),
		cdi.WithNvml(nvmllib),
		cdi.WithDeviceIDStrategy(*config.Flags.Plugin.DeviceIDStrategy),
		cdi.WithVendor("k8s.device-plugin.nvidia.com"),
		cdi.WithGdsEnabled(*config.Flags.GDSEnabled),
		cdi.WithMofedEnabled(*config.Flags.MOFEDEnabled),
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
	)
	if err != nil {
		return nil, fmt.Errorf("unable to create cdi handler: %v", err)
	}

<<<<<<< HEAD
	plugins, err := plugin.New(ctx, infolib, nvmllib, devicelib,
		plugin.WithCDIHandler(cdiHandler),
		plugin.WithConfig(config),
		plugin.WithDeviceListStrategies(deviceListStrategies),
		plugin.WithFailOnInitError(*config.Flags.FailOnInitError),
		plugin.WithImexChannels(imexChannels),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to create plugins: %w", err)
	}

	if err := cdiHandler.CreateSpecFile(); err != nil {
		return nil, fmt.Errorf("unable to create cdi spec file: %v", err)
	}

	return plugins, nil
=======
	m, err := manager.New(
		manager.WithNVML(nvmllib),
		manager.WithCDIEnabled(cdiEnabled),
		manager.WithCDIHandler(cdiHandler),
		manager.WithConfig(config),
		manager.WithFailOnInitError(*config.Flags.FailOnInitError),
		manager.WithMigStrategy(*config.Flags.MigStrategy),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to create plugin manager: %v", err)
	}

	if err := m.CreateCDISpecFile(); err != nil {
		return nil, fmt.Errorf("unable to create cdi spec file: %v", err)
	}

	return m, nil
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
}
