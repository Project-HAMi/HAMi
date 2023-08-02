/**
# Copyright (c) NVIDIA CORPORATION.  All rights reserved.
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

package manager

import (
	"4pd.io/k8s-vgpu/pkg/device-plugin/nvidiadevice/nvinternal/plugin"
)

type null struct{}

// GetPlugins returns an empty set of Plugins for the null manager
func (m *null) GetPlugins() ([]plugin.Interface, error) {
	return nil, nil
}

// CreateCDISpecFile creates the spec is a no-op for the null plugin
func (m *null) CreateCDISpecFile() error {
	return nil
}
