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

package resource

import (
	"k8s.io/klog/v2"
)

type withFallBack struct {
	wraps    Manager
	fallback Manager
}

// NewFallbackToNullOnInitError creates a manager that becomes a Null manager on the first Init error.
func NewFallbackToNullOnInitError(m Manager) Manager {
	return &withFallBack{
		wraps:    m,
		fallback: NewNullManager(),
	}
}

// Init calls the Init function and if this does not succeed falls back to a Null manager.
func (m *withFallBack) Init() error {
	err := m.wraps.Init()
	if err != nil {
		klog.Warningf("Failed to initialize resource manager: %v", err)
		m.wraps = m.fallback
	}
	return nil
}

// Shutdown delegates to the wrapped manager
func (m *withFallBack) Shutdown() (err error) {
	return m.wraps.Shutdown()
}

// GetDevices delegates to the wrapped manager
func (m *withFallBack) GetDevices() ([]Device, error) {
	return m.wraps.GetDevices()
}

// GetCudaDriverVersion delegates to the wrapped manager
func (m *withFallBack) GetCudaDriverVersion() (int, int, error) {
	return m.wraps.GetCudaDriverVersion()
}

// GetDriverVersion delegates to the wrapped manager
func (m *withFallBack) GetDriverVersion() (string, error) {
	return m.wraps.GetDriverVersion()
}
