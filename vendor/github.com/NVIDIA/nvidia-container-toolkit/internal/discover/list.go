/*
# Copyright (c) 2021, NVIDIA CORPORATION.  All rights reserved.
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
*/

package discover

import "fmt"

// list is a discoverer that contains a list of Discoverers. The output of the
// Mounts functions is the concatenation of the output for each of the
// elements in the list.
type list struct {
	discoverers []Discover
}

var _ Discover = (*list)(nil)

// Merge creates a discoverer that is the composite of a list of discoverers.
func Merge(d ...Discover) Discover {
	l := list{
		discoverers: d,
	}

	return &l
}

// Devices returns all devices from the included discoverers
func (d list) Devices() ([]Device, error) {
	var allDevices []Device

	for i, di := range d.discoverers {
		devices, err := di.Devices()
		if err != nil {
			return nil, fmt.Errorf("error discovering devices for discoverer %v: %v", i, err)
		}
		allDevices = append(allDevices, devices...)
	}

	return allDevices, nil
}

// Mounts returns all mounts from the included discoverers
func (d list) Mounts() ([]Mount, error) {
	var allMounts []Mount

	for i, di := range d.discoverers {
		mounts, err := di.Mounts()
		if err != nil {
			return nil, fmt.Errorf("error discovering mounts for discoverer %v: %v", i, err)
		}
		allMounts = append(allMounts, mounts...)
	}

	return allMounts, nil
}

// Hooks returns all Hooks from the included discoverers
func (d list) Hooks() ([]Hook, error) {
	var allHooks []Hook

	for i, di := range d.discoverers {
		hooks, err := di.Hooks()
		if err != nil {
			return nil, fmt.Errorf("error discovering hooks for discoverer %v: %v", i, err)
		}
		allHooks = append(allHooks, hooks...)
	}

	return allHooks, nil
}
