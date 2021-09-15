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
	"encoding/json"
	"fmt"
	"os"

	oci "github.com/opencontainers/runtime-spec/specs-go"
)

// SpecModifier is a function that accepts a pointer to an OCI Srec and returns an
// error. The intention is that the function would modify the spec in-place.
type SpecModifier func(*oci.Spec) error

// Spec defines the operations to be performed on an OCI specification
type Spec interface {
	Load() error
	Flush() error
	Modify(SpecModifier) error
}

type fileSpec struct {
	*oci.Spec
	path string
}

var _ Spec = (*fileSpec)(nil)

// NewSpecFromFile creates an object that encapsulates a file-backed OCI spec.
// This can be used to read from the file, modify the spec, and write to the
// same file.
func NewSpecFromFile(filepath string) Spec {
	oci := fileSpec{
		path: filepath,
	}

	return &oci
}

// Load reads the contents of an OCI spec from file to be referenced internally.
// The file is opened "read-only"
func (s *fileSpec) Load() error {
	specFile, err := os.Open(s.path)
	if err != nil {
		return fmt.Errorf("error opening OCI specification file: %v", err)
	}
	defer specFile.Close()

	decoder := json.NewDecoder(specFile)

	var spec oci.Spec
	err = decoder.Decode(&spec)
	if err != nil {
		return fmt.Errorf("error reading OCI specification from file: %v", err)
	}

	s.Spec = &spec
	return nil
}

// Modify applies the specified SpecModifier to the stored OCI specification.
func (s *fileSpec) Modify(f SpecModifier) error {
	if s.Spec == nil {
		return fmt.Errorf("no spec loaded for modification")
	}
	return f(s.Spec)
}

// Flush writes the stored OCI specification to the filepath specifed by the path member.
// The file is truncated upon opening, overwriting any existing contents.
func (s fileSpec) Flush() error {
	specFile, err := os.Create(s.path)
	if err != nil {
		return fmt.Errorf("error opening OCI specification file: %v", err)
	}
	defer specFile.Close()

	encoder := json.NewEncoder(specFile)

	err = encoder.Encode(s.Spec)
	if err != nil {
		return fmt.Errorf("error writing OCI specification to file: %v", err)
	}

	return nil
}
