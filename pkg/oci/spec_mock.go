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

package oci

import (
	oci "github.com/opencontainers/runtime-spec/specs-go"
)

// MockSpec provides a simple mock for an OCI spec to be used in testing.
// It also implements the SpecModifier interface.
type MockSpec struct {
	*oci.Spec
	MockLoad   mockFunc
	MockFlush  mockFunc
	MockModify mockFunc
}

var _ Spec = (*MockSpec)(nil)

// NewMockSpec constructs a MockSpec to be used in testing as a Spec
func NewMockSpec(spec *oci.Spec, flushResult error, modifyResult error) *MockSpec {
	s := MockSpec{
		Spec:       spec,
		MockFlush:  mockFunc{result: flushResult},
		MockModify: mockFunc{result: modifyResult},
	}

	return &s
}

// Load invokes the mocked Load function to return the predefined error / result
func (s *MockSpec) Load() error {
	return s.MockLoad.call()
}

// Flush invokes the mocked Load function to return the predefined error / result
func (s *MockSpec) Flush() error {
	return s.MockFlush.call()
}

// Modify applies the specified SpecModifier to the spec and invokes the
// mocked modify function to return the predefined error / result.
func (s *MockSpec) Modify(f SpecModifier) error {
	f(s.Spec)
	return s.MockModify.call()
}

type mockFunc struct {
	Callcount int
	result    error
}

func (m *mockFunc) call() error {
	m.Callcount++
	return m.result
}
