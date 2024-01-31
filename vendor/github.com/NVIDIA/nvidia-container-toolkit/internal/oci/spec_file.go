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
	"io"
	"os"

	"github.com/opencontainers/runtime-spec/specs-go"
)

type fileSpec struct {
	memorySpec
	path string
}

var _ Spec = (*fileSpec)(nil)

// NewFileSpec creates an object that encapsulates a file-backed OCI spec.
// This can be used to read from the file, modify the spec, and write to the
// same file.
func NewFileSpec(filepath string) Spec {
	oci := fileSpec{
		path: filepath,
	}

	return &oci
}

// Load reads the contents of an OCI spec from file to be referenced internally.
// The file is opened "read-only"
func (s *fileSpec) Load() (*specs.Spec, error) {
	specFile, err := os.Open(s.path)
	if err != nil {
		return nil, fmt.Errorf("error opening OCI specification file: %v", err)
	}
	defer specFile.Close()

	spec, err := LoadFrom(specFile)
	if err != nil {
		return nil, fmt.Errorf("error loading OCI specification from file: %v", err)
	}
	s.Spec = spec
	return s.Spec, nil
}

// LoadFrom reads the contents of the OCI spec from the specified io.Reader.
func LoadFrom(reader io.Reader) (*specs.Spec, error) {
	decoder := json.NewDecoder(reader)

	var spec specs.Spec

	err := decoder.Decode(&spec)
	if err != nil {
		return nil, fmt.Errorf("error reading OCI specification: %v", err)
	}

	return &spec, nil
}

// Modify applies the specified SpecModifier to the stored OCI specification.
func (s *fileSpec) Modify(m SpecModifier) error {
	return s.memorySpec.Modify(m)
}

// Flush writes the stored OCI specification to the filepath specified by the path member.
// The file is truncated upon opening, overwriting any existing contents.
func (s fileSpec) Flush() error {
	if s.Spec == nil {
		return fmt.Errorf("no OCI specification loaded")
	}

	specFile, err := os.Create(s.path)
	if err != nil {
		return fmt.Errorf("error opening OCI specification file: %v", err)
	}
	defer specFile.Close()

	return flushTo(s.Spec, specFile)
}

// flushTo writes the stored OCI specification to the specified io.Writer.
func flushTo(spec *specs.Spec, writer io.Writer) error {
	if spec == nil {
		return nil
	}
	encoder := json.NewEncoder(writer)

	err := encoder.Encode(spec)
	if err != nil {
		return fmt.Errorf("error writing OCI specification: %v", err)
	}

	return nil
}
