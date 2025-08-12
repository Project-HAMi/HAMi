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

package rm

import (
	"fmt"

	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"k8s.io/klog/v2"
)

type nvmlResourceManager struct {
	resourceManager
	nvml nvml.Interface
}

var _ ResourceManager = (*nvmlResourceManager)(nil)

// NewNVMLResourceManagers returns a set of ResourceManagers, one for each NVML resource in 'config'.
func NewNVMLResourceManagers(nvmllib nvml.Interface, config *nvidia.DeviceConfig) ([]ResourceManager, error) {
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
		for key, value := range devices {
			if nvidia.FilterDeviceToRegister(value.ID, value.Index) {
				klog.V(5).InfoS("Filtering device", "device", value.ID)
				delete(devices, key)
				continue
			}
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
func (r *nvmlResourceManager) CheckHealth(stop <-chan any, unhealthy chan<- *Device, disableNVML <-chan bool, ackDisableHealthChecks chan<- bool) error {
	for {
		// first check if disableNVML channel signal is pass close into checkHealth function
		// if signal is pass close, return error "close signal received"
		err := r.checkHealth(stop, r.devices, unhealthy, disableNVML)
		if err.Error() == "close signal received" {
			ackDisableHealthChecks <- true
			klog.Info("Check Health has been closed")
			// when disableNVML channel signal is pass restart, continue to restart checkHealth function
			// when disableNVML channel signal is not pass restart, wait for restart signal
			<-disableNVML
			klog.Info("Restarting Check Health")
			continue

		}
		return err
	}
}
