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

package image

import (
	"sort"
	"strings"
)

// DriverCapability represents the possible values of NVIDIA_DRIVER_CAPABILITIES
type DriverCapability string

// Constants for the supported driver capabilities
const (
	DriverCapabilityAll      DriverCapability = "all"
	DriverCapabilityNone     DriverCapability = "none"
	DriverCapabilityCompat32 DriverCapability = "compat32"
	DriverCapabilityCompute  DriverCapability = "compute"
	DriverCapabilityDisplay  DriverCapability = "display"
	DriverCapabilityGraphics DriverCapability = "graphics"
	DriverCapabilityNgx      DriverCapability = "ngx"
	DriverCapabilityUtility  DriverCapability = "utility"
	DriverCapabilityVideo    DriverCapability = "video"
)

var (
	driverCapabilitiesNone = NewDriverCapabilities()
	driverCapabilitiesAll  = NewDriverCapabilities("all")

	// DefaultDriverCapabilities sets the value for driver capabilities if no value is set.
	DefaultDriverCapabilities = NewDriverCapabilities("utility,compute")
	// SupportedDriverCapabilities defines the set of all supported driver capabilities.
	SupportedDriverCapabilities = NewDriverCapabilities("compute,compat32,graphics,utility,video,display,ngx")
)

// NewDriverCapabilities creates a set of driver capabilities from the specified capabilities
func NewDriverCapabilities(capabilities ...string) DriverCapabilities {
	dc := make(DriverCapabilities)
	for _, capability := range capabilities {
		for _, c := range strings.Split(capability, ",") {
			trimmed := strings.TrimSpace(c)
			if trimmed == "" {
				continue
			}
			dc[DriverCapability(trimmed)] = true
		}
	}
	return dc
}

// DriverCapabilities represents the NVIDIA_DRIVER_CAPABILITIES set for the specified image.
type DriverCapabilities map[DriverCapability]bool

// Has check whether the specified capability is selected.
func (c DriverCapabilities) Has(capability DriverCapability) bool {
	if c.IsAll() {
		return true
	}
	return c[capability]
}

// Any checks whether any of the specified capabilities are set
func (c DriverCapabilities) Any(capabilities ...DriverCapability) bool {
	if c.IsAll() {
		return true
	}
	for _, cap := range capabilities {
		if c.Has(cap) {
			return true
		}
	}
	return false
}

// List returns the list of driver capabilities.
// The list is sorted.
func (c DriverCapabilities) List() []string {
	var capabilities []string
	for capability := range c {
		capabilities = append(capabilities, string(capability))
	}
	sort.Strings(capabilities)
	return capabilities
}

// String returns the string repesentation of the driver capabilities.
func (c DriverCapabilities) String() string {
	if c.IsAll() {
		return "all"
	}
	return strings.Join(c.List(), ",")
}

// IsAll indicates whether the set of capabilities is `all`
func (c DriverCapabilities) IsAll() bool {
	return c[DriverCapabilityAll]
}

// Intersection returns a new set which includes the item in BOTH d and s2.
// For example: d = {a1, a2} s2 = {a2, a3} s1.Intersection(s2) = {a2}
func (c DriverCapabilities) Intersection(s2 DriverCapabilities) DriverCapabilities {
	if s2.IsAll() {
		return c
	}
	if c.IsAll() {
		return s2
	}

	intersection := make(DriverCapabilities)
	for capability := range s2 {
		if c[capability] {
			intersection[capability] = true
		}
	}

	return intersection
}

// IsSuperset returns true if and only if d is a superset of s2.
func (c DriverCapabilities) IsSuperset(s2 DriverCapabilities) bool {
	if c.IsAll() {
		return true
	}

	for capability := range s2 {
		if !c[capability] {
			return false
		}
	}

	return true
}
