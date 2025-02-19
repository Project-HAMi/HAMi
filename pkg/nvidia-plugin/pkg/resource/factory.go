/**
# Copyright (c) 2022, NVIDIA CORPORATION.  All rights reserved.
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

package resource

import (
	"fmt"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvlib/pkg/nvlib/info"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"k8s.io/klog/v2"

	spec "github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/api/config/v1"
)

// NewManager is a factory method that creates a resource Manager based on the specified config.
func NewManager(infolib info.Interface, nvmllib nvml.Interface, devicelib device.Interface, config *spec.Config) (Manager, error) {
	manager, err := getManager(infolib, nvmllib, devicelib, *config.Flags.DeviceDiscoveryStrategy)
	if err != nil {
		if *config.Flags.FailOnInitError {
			return nil, err
		}
		klog.ErrorS(err, "using empty manager")
		return NewNullManager(), nil
	}
	return WithConfig(manager, config), nil
}

// WithConfig modifies a manager depending on the specified config.
// If failure on a call to init is allowed, the manager is wrapped to allow fallback to a Null manager.
func WithConfig(manager Manager, config *spec.Config) Manager {
	if *config.Flags.FailOnInitError {
		return manager
	}

	return NewFallbackToNullOnInitError(manager)
}

// getManager returns the resource manager depending on the system configuration.
func getManager(infolib info.Interface, nvmllib nvml.Interface, devicelib device.Interface, strategy string) (Manager, error) {
	resolved := resolveMode(infolib, strategy)
	switch resolved {
	case "nvml":
		klog.Info("Using NVML manager")
		return NewNVMLManager(nvmllib, devicelib), nil
	case "tegra":
		klog.Info("Using CUDA manager")
		return NewCudaManager(), nil
	case "vfio":
		klog.Info("Using Vfio manager")
		return NewVfioManager(), nil
	default:
		return nil, fmt.Errorf("unsupported strategy %v", resolved)
	}
}

func resolveMode(infolib info.Interface, strategy string) string {
	if strategy != "" && strategy != "auto" {
		return strategy
	}

	platform := infolib.ResolvePlatform()
	switch platform {
	case info.PlatformNVML, info.PlatformWSL:
		return "nvml"
	case info.PlatformTegra:
		return "tegra"
	}
	return strategy
}
