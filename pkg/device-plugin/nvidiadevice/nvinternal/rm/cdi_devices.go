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
	"k8s.io/klog/v2"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdiparser "tags.cncf.io/container-device-interface/pkg/parser"
)

const (
	// CDIVendor is the CDI vendor of the NVIDIA device-plugin CDI specs. It must
	// match the vendor used when constructing qualified device names for
	// injection (see the cdi handler) and the vendor of the CDI spec generated
	// by the NVIDIA Container Toolkit / GPU Operator on the node.
	CDIVendor = "k8s.device-plugin.nvidia.com"
	// CDIClass is the CDI class for full GPU devices.
	CDIClass = "gpu"
	// cdiAllDevice is the meta-device that references every GPU in a CDI spec.
	// It is excluded from discovery so that each physical device is advertised
	// individually.
	cdiAllDevice = "all"
)

// cdiSpecDirs is the default set of directories scanned for CDI specs. It
// mirrors the CDI library defaults (/etc/cdi and /var/run/cdi).
var cdiSpecDirs = cdiapi.DefaultSpecDirs

// CDISpecDirs returns the directories scanned for CDI specs.
func CDISpecDirs() []string {
	return cdiSpecDirs
}

// HasCDISpecs reports whether any GPU device for the NVIDIA device-plugin
// vendor/class is present in the CDI specs under the supplied directories. It
// is used to auto-detect CDI-only accelerators (e.g. the GB10 iGPU) when NVML
// discovery is unavailable.
func HasCDISpecs(specDirs []string) bool {
	names, err := listCDIGPUDevices(specDirs)
	if err != nil {
		klog.V(3).InfoS("failed to scan CDI specs", "dirs", specDirs, "err", err)
		return false
	}
	return len(names) > 0
}

// listCDIGPUDevices returns the CDI device names (e.g. "0", a GPU UUID) for the
// NVIDIA device-plugin GPU class found in the CDI specs under specDirs. The
// "all" meta-device is excluded.
func listCDIGPUDevices(specDirs []string) ([]string, error) {
	cache, err := cdiapi.NewCache(
		cdiapi.WithSpecDirs(specDirs...),
		cdiapi.WithAutoRefresh(false),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create CDI cache: %w", err)
	}

	var names []string
	for _, qualified := range cache.ListDevices() {
		vendor, class, name, err := cdiparser.ParseQualifiedName(qualified)
		if err != nil {
			klog.V(5).InfoS("skipping malformed CDI device", "device", qualified, "err", err)
			continue
		}
		if vendor != CDIVendor || class != CDIClass || name == cdiAllDevice {
			continue
		}
		names = append(names, name)
	}
	return names, nil
}

// buildCDIDeviceMap creates a DeviceMap from the GPU devices described by the
// externally-managed CDI specs under specDirs. Devices are discovered without
// NVML, which is what allows CDI-only accelerators such as the GB10 to be
// advertised.
func buildCDIDeviceMap(config *spec.Config, specDirs []string) (DeviceMap, error) {
	cdiNames, err := listCDIGPUDevices(specDirs)
	if err != nil {
		return nil, err
	}

	devices := make(DeviceMap)
	i := 0
	for _, cdiName := range cdiNames {
		for _, resource := range config.Resources.GPUs {
			if !resource.Pattern.Matches(cdiName) {
				continue
			}
			index := fmt.Sprintf("%d", i)
			if err := devices.setEntry(resource.Name, index, &cdiDevice{name: cdiName}); err != nil {
				return nil, err
			}
			i++
			break
		}
	}
	return devices, nil
}

// cdiDevice represents a single GPU discovered from a CDI spec.
type cdiDevice struct {
	// name is the CDI device name (e.g. "0" or a GPU UUID). It is used as the
	// device UUID so that the allocation path can build a matching qualified
	// CDI device name for injection.
	name string
}

var _ deviceInfo = (*cdiDevice)(nil)

// GetUUID returns the CDI device name used to reference this device.
func (d *cdiDevice) GetUUID() (string, error) {
	return d.name, nil
}

// GetPaths returns no paths: device access is configured through CDI injection.
func (d *cdiDevice) GetPaths() ([]string, error) {
	return nil, nil
}

// GetNumaNode is unsupported for a CDI-discovered device.
func (d *cdiDevice) GetNumaNode() (bool, int, error) {
	return false, -1, nil
}

// GetTotalMemory is unsupported for a CDI-discovered device; the total memory
// is provided out of band via configuration (preConfiguredDeviceMemory).
func (d *cdiDevice) GetTotalMemory() (uint64, error) {
	return 0, nil
}

// GetComputeCapability is unsupported for a CDI-discovered device.
func (d *cdiDevice) GetComputeCapability() (string, error) {
	return "0.0", nil
}
