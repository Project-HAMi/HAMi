/**
# Copyright 2023 NVIDIA CORPORATION
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

package nvml

// options represents the options that could be passed to the nvml contructor.
type options struct {
	libraryPath string
}

// Option represents a functional option to control behaviour.
type Option func(*options)

// WithLibraryPath sets the NVML library name to use.
func WithLibraryPath(libraryPath string) Option {
	return func(o *options) {
		o.libraryPath = libraryPath
	}
}
