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
	"errors"
	"fmt"
	"strings"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvlib/pkg/nvlib/info"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"k8s.io/klog/v2"
)

// resourceManager forms the base type for specific resource manager implementations
type resourceManager struct {
	config   *spec.Config
	resource spec.ResourceName
	devices  Devices
}

// ResourceManager provides an interface for listing a set of Devices and checking health on them
type ResourceManager interface {
	Resource() spec.ResourceName
	Devices() Devices
	GetDevicePaths([]string) []string
	GetPreferredAllocation(available, required []string, size int) ([]string, error)
	CheckHealth(stop <-chan interface{}, unhealthy chan<- *Device, disableNVML <-chan bool, ackDisableHealthChecks chan<- bool) error
	ValidateRequest(AnnotatedIDs) error
}

// Resource gets the resource name associated with the ResourceManager
func (r *resourceManager) Resource() spec.ResourceName {
	return r.resource
}

// Resource gets the devices managed by the ResourceManager
func (r *resourceManager) Devices() Devices {
	return r.devices
}

var errInvalidRequest = errors.New("invalid request")

// ValidateRequest checks the requested IDs against the resource manager configuration.
// It asserts that all requested IDs are known to the resource manager and that the request is
// valid for a specified sharing configuration.
func (r *resourceManager) ValidateRequest(ids AnnotatedIDs) error {
	// Assert that all requested IDs are known to the resource manager
	for _, id := range ids {
		if !r.devices.Contains(id) {
			return fmt.Errorf("%w: unknown device: %s", errInvalidRequest, id)
		}
	}

	// If the devices being allocated are replicas, then (conditionally)
	// error out if more than one resource is being allocated.
	includesReplicas := ids.AnyHasAnnotations()
	numRequestedDevices := len(ids)
	switch r.config.Sharing.SharingStrategy() {
	case spec.SharingStrategyTimeSlicing:
		if includesReplicas && numRequestedDevices > 1 && r.config.Sharing.ReplicatedResources().FailRequestsGreaterThanOne {
			return fmt.Errorf("%w: maximum request size for shared resources is 1; found %d", errInvalidRequest, numRequestedDevices)
		}
	case spec.SharingStrategyMPS:
		// For MPS sharing, we explicitly ignore the FailRequestsGreaterThanOne
		// value in the sharing settings.
		// This setting was added to timeslicing after the initial release and
		// is set to `false` to maintain backward compatibility with existing
		// deployments. If we do extend MPS to allow multiple devices to be
		// requested, the MPS API will be extended separately from the
		// time-slicing API.
		if includesReplicas && numRequestedDevices > 1 {
			return fmt.Errorf("%w: maximum request size for shared resources is 1; found %d", errInvalidRequest, numRequestedDevices)
		}
	}
	return nil
}

// AddDefaultResourcesToConfig adds default resource matching rules to config.Resources
func AddDefaultResourcesToConfig(infolib info.Interface, nvmllib nvml.Interface, devicelib device.Interface, config *nvidia.DeviceConfig) error {
	config.Resources.GPUs = append(config.Resources.GPUs, spec.Resource{
		Pattern: "*",
		Name:    spec.ResourceName(*config.ResourceName),
	})
	klog.V(4).InfoS("AddDefaultResourceToConfig", "config", config.Resources.GPUs)
	if config.Flags.MigStrategy == nil {
		return nil
	}
	switch *config.Flags.MigStrategy {
	case spec.MigStrategySingle:
		return config.Resources.AddMIGResource("*", "gpu")
	case spec.MigStrategyMixed:
		hasNVML, reason := infolib.HasNvml()
		if !hasNVML {
			klog.Warningf("mig-strategy=%q is only supported with NVML", spec.MigStrategyMixed)
			klog.Warningf("NVML not detected: %v", reason)
			return nil
		}

		ret := nvmllib.Init()
		if ret != nvml.SUCCESS {
			if *config.Flags.FailOnInitError {
				return fmt.Errorf("failed to initialize NVML: %v", ret)
			}
			return nil
		}
		defer func() {
			ret := nvmllib.Shutdown()
			if ret != nvml.SUCCESS {
				klog.Errorf("Error shutting down NVML: %v", ret)
			}
		}()

		return devicelib.VisitMigProfiles(func(p device.MigProfile) error {
			info := p.GetInfo()
			if info.C != info.G {
				return nil
			}
			resourceName := strings.ReplaceAll("mig-"+p.String(), "+", ".")
			return config.Resources.AddMIGResource(p.String(), resourceName)
		})
	}
	return nil
}
