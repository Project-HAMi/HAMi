/*
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

package rm

import (
	"fmt"

	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
)

type cdiResourceManager struct {
	resourceManager
}

var _ ResourceManager = (*cdiResourceManager)(nil)

// NewCDIResourceManagers returns a set of ResourceManagers for GPUs discovered
// from externally-managed CDI specs on the node. This path does not use NVML
// and is intended for CDI-only accelerators such as the GB10 (Grace-Blackwell
// iGPU).
func NewCDIResourceManagers(config *spec.Config) ([]ResourceManager, error) {
	deviceMap, err := buildCDIDeviceMap(config, cdiSpecDirs)
	if err != nil {
		return nil, fmt.Errorf("error building CDI device map: %v", err)
	}

	deviceMap, err = updateDeviceMapWithReplicas(config.Sharing.ReplicatedResources(), deviceMap)
	if err != nil {
		return nil, fmt.Errorf("error updating device map with replicas from sharing resources: %v", err)
	}

	var rms []ResourceManager
	for resourceName, devices := range deviceMap {
		if len(devices) == 0 {
			continue
		}
		r := &cdiResourceManager{
			resourceManager: resourceManager{
				config:   config,
				resource: resourceName,
				devices:  devices,
			},
		}
		rms = append(rms, r)
	}

	return rms, nil
}

// GetPreferredAllocation returns a standard allocation for the CDI resource manager.
func (r *cdiResourceManager) GetPreferredAllocation(available, required []string, size int) ([]string, error) {
	return r.distributedAlloc(available, required, size)
}

// GetDevicePaths returns an empty slice: CDI devices are injected via CDI, not
// through explicit device paths.
func (r *cdiResourceManager) GetDevicePaths(ids []string) []string {
	return nil
}

// CheckHealth is disabled for the cdiResourceManager: health checks require NVML.
func (r *cdiResourceManager) CheckHealth(stop <-chan any, unhealthy chan<- *Device, disableNVML <-chan bool, ackDisableHealthChecks chan<- bool) error {
	return nil
}
