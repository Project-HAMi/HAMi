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

import "github.com/Project-HAMi/HAMi/pkg/device/nvidia"

// Option defines a functional option for configuring an MPS manager.
type Option func(*manager)

// WithConfig sets the config associated with the MPS manager.
func WithConfig(config *nvidia.DeviceConfig) Option {
	return func(m *manager) {
		m.config = config
	}
}
