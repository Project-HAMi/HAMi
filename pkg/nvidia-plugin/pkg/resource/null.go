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
	"fmt"
)

type null struct{}

var _ Manager = (*null)(nil)

// NewNullManager returns an instance of a CUDA-based library that can be used
// when no operations are required.
// This returns no devices and the Init and Shutdown methods are no-ops.
func NewNullManager() Manager {
	return &null{}
}

// Init is a no-op for the null manager
func (l *null) Init() error {
	return nil
}

// Shutdown is a no-op for the null manager
func (l *null) Shutdown() (err error) {
	return nil
}

// GetDevices returns a nil slice for the null manager
func (l *null) GetDevices() ([]Device, error) {
	return nil, nil
}

// GetCudaDriverVersion is not supported
func (l *null) GetCudaDriverVersion() (int, int, error) {
	return 0, 0, fmt.Errorf("GetCudaDriverVersion is unsupported")
}

// GetDriverVersion is not supported
func (l *null) GetDriverVersion() (string, error) {
	return "", fmt.Errorf("GetDriverVersion is unsupported")
}
