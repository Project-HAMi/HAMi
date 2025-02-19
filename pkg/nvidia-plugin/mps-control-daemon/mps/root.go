/**
# Copyright 2024 NVIDIA CORPORATION
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

package mps

import (
	"path/filepath"

	spec "github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/api/config/v1"
)

const (
	ContainerRoot = Root("/mps")
)

// Root represents an MPS root.
// This is where per-resource pipe and log directories are created.
// For containerised applications the host root is typically mounted to /mps in the container.
type Root string

// LogDir returns the per-resource pipe dir for the specified root.
func (r Root) LogDir(resourceName spec.ResourceName) string {
	return r.Path(string(resourceName), "log")
}

// PipeDir returns the per-resource pipe dir for the specified root.
func (r Root) PipeDir(resourceName spec.ResourceName) string {
	return r.Path(string(resourceName), "pipe")
}

// ShmDir returns the shm dir associated with the root.
// Note that the shm dir is the same for all resources.
func (r Root) ShmDir(resourceName spec.ResourceName) string {
	return r.Path("shm")
}

// startedFile returns the per-resource .started file name for the specified root.
func (r Root) startedFile(resourceName spec.ResourceName) string {
	return r.Path(string(resourceName), ".started")
}

// Path returns a path relative to the MPS root.
func (r Root) Path(parts ...string) string {
	pathparts := append([]string{string(r)}, parts...)
	return filepath.Join(pathparts...)
}
