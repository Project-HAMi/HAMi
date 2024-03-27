/*
 * Copyright (c) 2019-2022, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY Type, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package rm

import (
	"fmt"

	"github.com/Project-HAMi/HAMi/pkg/util"

	"github.com/NVIDIA/go-nvlib/pkg/nvml"
	"k8s.io/klog/v2"
)

type nvmlResourceManager struct {
	resourceManager
	nvml nvml.Interface
}

var _ ResourceManager = (*nvmlResourceManager)(nil)

// NewNVMLResourceManagers returns a set of ResourceManagers, one for each NVML resource in 'config'.
func NewNVMLResourceManagers(nvmllib nvml.Interface, config *util.DeviceConfig) ([]ResourceManager, error) {
	ret := nvmllib.Init()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("failed to initialize NVML: %v", ret)
	}
	defer func() {
		ret := nvmllib.Shutdown()
		if ret != nvml.SUCCESS {
			klog.Infof("Error shutting down NVML: %v", ret)
		}
	}()

	deviceMap, err := NewDeviceMap(nvmllib, config)
	if err != nil {
		return nil, fmt.Errorf("error building device map: %v", err)
	}

	var rms []ResourceManager
	for resourceName, devices := range deviceMap {
		if len(devices) == 0 {
			continue
		}
		r := &nvmlResourceManager{
			resourceManager: resourceManager{
				config:   config,
				resource: resourceName,
				devices:  devices,
			},
			nvml: nvmllib,
		}
		rms = append(rms, r)
	}

	return rms, nil
}

// GetPreferredAllocation runs an allocation algorithm over the inputs.
// The algorithm chosen is based both on the incoming set of available devices and various config settings.
func (r *nvmlResourceManager) GetPreferredAllocation(available, required []string, size int) ([]string, error) {
	return r.getPreferredAllocation(available, required, size)
}

// GetDevicePaths returns the required and optional device nodes for the requested resources
func (r *nvmlResourceManager) GetDevicePaths(ids []string) []string {
	paths := []string{
		"/dev/nvidiactl",
		"/dev/nvidia-uvm",
		"/dev/nvidia-uvm-tools",
		"/dev/nvidia-modeset",
	}

	for _, p := range r.Devices().Subset(ids).GetPaths() {
		paths = append(paths, p)
	}

	return paths
}

// CheckHealth performs health checks on a set of devices, writing to the 'unhealthy' channel with any unhealthy devices
func (r *nvmlResourceManager) CheckHealth(stop <-chan interface{}, unhealthy chan<- *Device) error {
	return r.checkHealth(stop, r.devices, unhealthy)
}
