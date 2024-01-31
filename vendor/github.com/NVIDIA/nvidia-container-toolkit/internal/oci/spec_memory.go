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

package oci

import (
	"fmt"
	"strings"

	"github.com/opencontainers/runtime-spec/specs-go"
)

type memorySpec struct {
	*specs.Spec
}

// NewMemorySpec creates a Spec instance from the specified OCI spec
func NewMemorySpec(spec *specs.Spec) Spec {
	s := memorySpec{
		Spec: spec,
	}

	return &s
}

// Load is a no-op for the memorySpec spec
func (s *memorySpec) Load() (*specs.Spec, error) {
	return s.Spec, nil
}

// Flush is a no-op for the memorySpec spec
func (s *memorySpec) Flush() error {
	return nil
}

// Modify applies the specified SpecModifier to the stored OCI specification.
func (s *memorySpec) Modify(m SpecModifier) error {
	if s.Spec == nil {
		return fmt.Errorf("cannot modify nil spec")
	}
	return m.Modify(s.Spec)
}

// LookupEnv mirrors os.LookupEnv for the OCI specification. It
// retrieves the value of the environment variable named
// by the key. If the variable is present in the environment the
// value (which may be empty) is returned and the boolean is true.
// Otherwise the returned value will be empty and the boolean will
// be false.
func (s memorySpec) LookupEnv(key string) (string, bool) {
	if s.Spec == nil || s.Spec.Process == nil {
		return "", false
	}

	for _, env := range s.Spec.Process.Env {
		if !strings.HasPrefix(env, key) {
			continue
		}

		parts := strings.SplitN(env, "=", 2)
		if parts[0] == key {
			if len(parts) < 2 {
				return "", true
			}
			return parts[1], true
		}
	}

	return "", false
}
