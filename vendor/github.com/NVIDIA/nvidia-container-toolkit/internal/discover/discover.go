/*
# Copyright (c) 2021-2022, NVIDIA CORPORATION.  All rights reserved.
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

// Device represents a discovered character device.
type Device struct {
	HostPath string
	Path     string
}

// Mount represents a discovered mount.
type Mount struct {
	HostPath string
	Path     string
	Options  []string
}

// Hook represents a discovered hook.
type Hook struct {
	Lifecycle string
	Path      string
	Args      []string
}

// Discover defines an interface for discovering the devices, mounts, and hooks available on a system
//
//go:generate moq -stub -out discover_mock.go . Discover
type Discover interface {
	Devices() ([]Device, error)
	Mounts() ([]Mount, error)
	Hooks() ([]Hook, error)
}
