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

package lm

import (
	"fmt"
	"regexp"
	"strings"

	spec "github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/api/config/v1"
	"github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/pkg/resource"
)

const fullGPUResourceName = "nvidia.com/gpu"

// NewGPUResourceLabelerWithoutSharing creates a resource labeler for the specified device that does not apply sharing labels.
func NewGPUResourceLabelerWithoutSharing(device resource.Device, count int) (Labeler, error) {
	// NOTE: We use a nil config to signal that sharing is disabled.
	return NewGPUResourceLabeler(nil, device, count)
}

// NewGPUResourceLabeler creates a resource labeler for the specified full GPU device with the specified count
func NewGPUResourceLabeler(config *spec.Config, device resource.Device, count int) (Labeler, error) {
	if count == 0 {
		return empty{}, nil
	}

	model, err := device.GetName()
	if err != nil {
		return nil, fmt.Errorf("failed to get device model: %v", err)
	}

	totalMemoryMB, err := device.GetTotalMemoryMB()
	if err != nil {
		return nil, fmt.Errorf("failed to get memory info for device: %v", err)
	}

	resourceLabeler := newResourceLabeler(fullGPUResourceName, config)

	architectureLabels, err := newArchitectureLabels(resourceLabeler, device)
	if err != nil {
		return nil, fmt.Errorf("failed to create architecture labels: %v", err)
	}

	memoryLabeler := (Labeler)(&empty{})
	if totalMemoryMB != 0 {
		memoryLabeler = resourceLabeler.single("memory", totalMemoryMB)
	}

	labelers := Merge(
		resourceLabeler.baseLabeler(count, model),
		memoryLabeler,
		architectureLabels,
	)

	return labelers, nil
}

// NewMIGResourceLabeler creates a resource labeler for the specified full GPU device with the specified resource name.
func NewMIGResourceLabeler(resourceName spec.ResourceName, config *spec.Config, device resource.Device, count int) (Labeler, error) {
	if count == 0 {
		return empty{}, nil
	}

	parent, err := device.GetDeviceHandleFromMigDeviceHandle()
	if err != nil {
		return nil, fmt.Errorf("failed to get parent of MIG device: %v", err)
	}
	model, err := parent.GetName()
	if err != nil {
		return nil, fmt.Errorf("failed to get device model: %v", err)
	}

	migProfile, err := device.GetName()
	if err != nil {
		return nil, fmt.Errorf("failed to get MIG profile name: %v", err)
	}

	resourceLabeler := newResourceLabeler(resourceName, config)

	attributeLabels, err := newMigAttributeLabels(resourceLabeler, device)
	if err != nil {
		return nil, fmt.Errorf("faled to get MIG attribute labels: %v", err)
	}

	labelers := Merge(
		resourceLabeler.baseLabeler(count, model, "MIG", migProfile),
		attributeLabels,
	)

	return labelers, nil
}

func newResourceLabeler(resourceName spec.ResourceName, config *spec.Config) resourceLabeler {
	var sharing *spec.Sharing
	if config != nil {
		sharing = &config.Sharing
	}
	return resourceLabeler{
		resourceName: resourceName,
		sharing:      sharing,
	}

}

type resourceLabeler struct {
	resourceName spec.ResourceName
	sharing      *spec.Sharing
}

// single creates a single label for the resource. The label key is
// <fully-qualified-resource-name>.suffix
func (rl resourceLabeler) single(suffix string, value interface{}) Labels {
	return rl.labels(map[string]interface{}{suffix: value})

}

// labels creates a set of labels from the specified map for the resource.
// Each key in the map corresponds to a label <fully-qualified-resource-name>.key
func (rl resourceLabeler) labels(suffixValues map[string]interface{}) Labels {
	labels := make(Labels)
	for suffix, value := range suffixValues {
		rl.updateLabel(labels, suffix, value)
	}

	return labels
}

// updateLabel modifies the specified labels, updating <fully-qualified-resource-name>.suffix with
// the provided value.
func (rl resourceLabeler) updateLabel(labels Labels, suffix string, value interface{}) {
	key := rl.key(suffix)

	labels[key] = fmt.Sprintf("%v", value)
}

// key generates the label key for the specified suffix. The key is generated as
// <fully-qualified-resource-name>.suffix
func (rl resourceLabeler) key(suffix string) string {
	return string(rl.resourceName) + "." + suffix
}

// baseLabeler generates the product, count, and replicas labels for the resource
func (rl resourceLabeler) baseLabeler(count int, parts ...string) Labeler {
	replicas := rl.getReplicas()
	strategy := spec.SharingStrategyNone
	if rl.sharing != nil && replicas > 1 {
		strategy = rl.sharing.SharingStrategy()
	}
	rawLabels := map[string]interface{}{
		"product":          rl.getProductName(parts...),
		"count":            count,
		"replicas":         replicas,
		"sharing-strategy": strategy,
	}

	labels := make(Labels)
	for k, v := range rawLabels {
		labels[rl.key(k)] = fmt.Sprintf("%v", v)
	}
	return labels
}

// Deprecated
func (rl resourceLabeler) productLabel(parts ...string) Labels {
	name := rl.getProductName(parts...)
	if name == "" {
		return make(Labels)
	}
	return rl.single("product", name)
}

func (rl resourceLabeler) getProductName(parts ...string) string {
	var strippedParts []string
	for _, p := range parts {
		if p != "" {
			sanitisedPart := sanitise(p)
			strippedParts = append(strippedParts, sanitisedPart)
		}
	}

	if len(strippedParts) == 0 {
		return ""
	}

	if rl.isShared() && !rl.isRenamed() {
		strippedParts = append(strippedParts, "SHARED")
	}
	return strings.Join(strippedParts, "-")
}

func (rl resourceLabeler) getReplicas() int {
	if rl.sharingDisabled() {
		return 0
	} else if r := rl.replicationInfo(); r != nil && r.Replicas > 0 {
		return r.Replicas
	}
	return 1
}

// sharingDisabled checks whether the resourceLabeler has sharing disabled
// TODO: The nil check here is because we call NewGPUResourceLabeler with a nil config when sharing is disabled.
func (rl resourceLabeler) sharingDisabled() bool {
	return rl.sharing == nil
}

// isShared checks whether the resource is shared.
func (rl resourceLabeler) isShared() bool {
	if r := rl.replicationInfo(); r != nil && r.Replicas > 1 {
		return true
	}
	return false
}

// isRenamed checks whether the resource is renamed.
func (rl resourceLabeler) isRenamed() bool {
	if r := rl.replicationInfo(); r != nil && r.Rename != "" {
		return true
	}
	return false
}

// replicationInfo searches the associated config for the resource and returns the replication info
func (rl resourceLabeler) replicationInfo() *spec.ReplicatedResource {
	if rl.sharingDisabled() {
		return nil
	}
	for _, r := range rl.sharing.ReplicatedResources().Resources {
		if r.Name == rl.resourceName {
			return &r
		}
	}
	return nil
}

func newMigAttributeLabels(rl resourceLabeler, device resource.Device) (Labels, error) {
	attributes, err := device.GetAttributes()
	if err != nil {
		return nil, fmt.Errorf("unable to get attributes of MIG device: %v", err)
	}

	labels := rl.labels(attributes)

	return labels, nil
}

func newArchitectureLabels(rl resourceLabeler, device resource.Device) (Labels, error) {
	computeMajor, computeMinor, err := device.GetCudaComputeCapability()
	if err != nil {
		return nil, fmt.Errorf("failed to determine CUDA compute capability: %v", err)
	}

	if computeMajor == 0 {
		return make(Labels), nil
	}

	family := getArchFamily(computeMajor, computeMinor)

	labels := rl.labels(map[string]interface{}{
		"family":        family,
		"compute.major": computeMajor,
		"compute.minor": computeMinor,
	})

	return labels, nil
}

// TODO: This should a function in go-nvlib
func getArchFamily(computeMajor, computeMinor int) string {
	switch computeMajor {
	case 1:
		return "tesla"
	case 2:
		return "fermi"
	case 3:
		return "kepler"
	case 5:
		return "maxwell"
	case 6:
		return "pascal"
	case 7:
		if computeMinor < 5 {
			return "volta"
		}
		return "turing"
	case 8:
		if computeMinor < 9 {
			return "ampere"
		}
		return "ada-lovelace"
	case 9:
		return "hopper"
	}
	return "undefined"
}

func sanitise(input string) string {
	var sanitised string
	re := regexp.MustCompile("[^A-Za-z0-9-_. ]")
	input = re.ReplaceAllString(input, "")
	// remove redundant blank spaces
	sanitised = strings.Join(strings.Fields(input), "-")

	return sanitised
}
