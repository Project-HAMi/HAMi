/*
<<<<<<< HEAD
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
=======
 * Copyright (c) 2019-2022, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
<<<<<<< HEAD
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
=======
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY Type, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
 */

package rm

import (
<<<<<<< HEAD
	"errors"
	"fmt"
	"strings"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvlib/pkg/nvlib/info"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
=======
	"fmt"
	"strings"

	"4pd.io/k8s-vgpu/pkg/util"
	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvlib/pkg/nvlib/info"
	"github.com/NVIDIA/go-nvlib/pkg/nvml"
	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
<<<<<<< HEAD
	"gitlab.com/nvidia/cloud-native/go-nvlib/pkg/nvlib/device"
	"gitlab.com/nvidia/cloud-native/go-nvlib/pkg/nvlib/info"
	"gitlab.com/nvidia/cloud-native/go-nvlib/pkg/nvml"
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
=======
>>>>>>> c7a3893 (Remake this repo to HAMi)
	"k8s.io/klog/v2"
)

// resourceManager forms the base type for specific resource manager implementations
type resourceManager struct {
<<<<<<< HEAD
	config   *spec.Config
=======
	config   *util.DeviceConfig
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
	resource spec.ResourceName
	devices  Devices
}

// ResourceManager provides an interface for listing a set of Devices and checking health on them
type ResourceManager interface {
	Resource() spec.ResourceName
	Devices() Devices
	GetDevicePaths([]string) []string
	GetPreferredAllocation(available, required []string, size int) ([]string, error)
<<<<<<< HEAD
	CheckHealth(stop <-chan interface{}, unhealthy chan<- *Device, disableNVML <-chan bool, ackDisableHealthChecks chan<- bool) error
	ValidateRequest(AnnotatedIDs) error
=======
	CheckHealth(stop <-chan interface{}, unhealthy chan<- *Device) error
}

// NewResourceManagers returns a []ResourceManager, one for each resource in 'config'.
func NewResourceManagers(nvmllib nvml.Interface, config *util.DeviceConfig) ([]ResourceManager, error) {
	// logWithReason logs the output of the has* / is* checks from the info.Interface
	logWithReason := func(f func() (bool, string), tag string) bool {
		is, reason := f()
		if !is {
			tag = "non-" + tag
		}
		klog.Infof("Detected %v platform: %v", tag, reason)
		return is
	}

	infolib := info.New()

	hasNVML := logWithReason(infolib.HasNvml, "NVML")
	isTegra := logWithReason(infolib.IsTegraSystem, "Tegra")

	if !hasNVML && !isTegra {
		klog.Error("Incompatible platform detected")
		klog.Error("If this is a GPU node, did you configure the NVIDIA Container Toolkit?")
		klog.Error("You can check the prerequisites at: https://github.com/NVIDIA/k8s-device-plugin#prerequisites")
		klog.Error("You can learn how to set the runtime at: https://github.com/NVIDIA/k8s-device-plugin#quick-start")
		klog.Error("If this is not a GPU node, you should set up a toleration or nodeSelector to only deploy this plugin on GPU nodes")
		if *config.Flags.FailOnInitError {
			return nil, fmt.Errorf("platform detection failed")
		}
		return nil, nil
	}

	// The NVIDIA container stack does not yet support the use of integrated AND discrete GPUs on the same node.
	if hasNVML && isTegra {
		klog.Warning("Disabling Tegra-based resources on NVML system")
		isTegra = false
	}

	var resourceManagers []ResourceManager

	if hasNVML {
		nvmlManagers, err := NewNVMLResourceManagers(nvmllib, config)
		if err != nil {
			return nil, fmt.Errorf("failed to construct NVML resource managers: %v", err)
		}
		resourceManagers = append(resourceManagers, nvmlManagers...)
	}

	if isTegra {
		tegraManagers, err := NewTegraResourceManagers(config)
		if err != nil {
			return nil, fmt.Errorf("failed to construct Tegra resource managers: %v", err)
		}
		resourceManagers = append(resourceManagers, tegraManagers...)
	}

	return resourceManagers, nil
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
}

// Resource gets the resource name associated with the ResourceManager
func (r *resourceManager) Resource() spec.ResourceName {
	return r.resource
}

// Resource gets the devices managed by the ResourceManager
func (r *resourceManager) Devices() Devices {
	return r.devices
}

<<<<<<< HEAD
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
=======
// AddDefaultResourcesToConfig adds default resource matching rules to config.Resources
func AddDefaultResourcesToConfig(config *util.DeviceConfig) error {
	//config.Resources.AddGPUResource("*", "gpu")
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
	config.Resources.GPUs = append(config.Resources.GPUs, spec.Resource{
		Pattern: "*",
		Name:    spec.ResourceName(*config.ResourceName),
	})
<<<<<<< HEAD
	klog.V(4).InfoS("AddDefaultResourceToConfig", "config", config.Resources.GPUs)
	if config.Flags.MigStrategy == nil {
		return nil
	}
=======
	fmt.Println("config=", config.Resources.GPUs)
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
	switch *config.Flags.MigStrategy {
	case spec.MigStrategySingle:
		return config.Resources.AddMIGResource("*", "gpu")
	case spec.MigStrategyMixed:
<<<<<<< HEAD
		hasNVML, reason := infolib.HasNvml()
=======
		hasNVML, reason := info.New().HasNvml()
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
		if !hasNVML {
			klog.Warningf("mig-strategy=%q is only supported with NVML", spec.MigStrategyMixed)
			klog.Warningf("NVML not detected: %v", reason)
			return nil
		}

<<<<<<< HEAD
=======
		nvmllib := nvml.New()
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
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

<<<<<<< HEAD
=======
		devicelib := device.New(
			device.WithNvml(nvmllib),
		)
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
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
