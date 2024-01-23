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

// None is a null discoverer that returns an empty list of devices and
// mounts.
type None struct{}

var _ Discover = (*None)(nil)

// Devices returns an empty list of devices
func (e None) Devices() ([]Device, error) {
	return []Device{}, nil
}

// Mounts returns an empty list of mounts
func (e None) Mounts() ([]Mount, error) {
	return []Mount{}, nil
}

// Hooks returns an empty list of hooks
func (e None) Hooks() ([]Hook, error) {
	return []Hook{}, nil
}
