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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/opencontainers/runtime-spec/specs-go"
)

// State stores an OCI container state. This includes the spec path and the environment
type State specs.State

// LoadContainerState loads the container state from the specified filename. If the filename is empty or '-' the state is loaded from STDIN
func LoadContainerState(filename string) (*State, error) {
	if filename == "" || filename == "-" {
		return ReadContainerState(os.Stdin)
	}

	inputFile, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %v", err)
	}
	defer inputFile.Close()

	return ReadContainerState(inputFile)
}

// ReadContainerState reads the container state from the specified reader
func ReadContainerState(reader io.Reader) (*State, error) {
	var s State

	d := json.NewDecoder(reader)
	if err := d.Decode(&s); err != nil {
		return nil, fmt.Errorf("failed to decode container state: %v", err)
	}

	return &s, nil
}

// LoadSpec loads the OCI spec associated with the container state
func (s *State) LoadSpec() (*specs.Spec, error) {
	specFilePath := GetSpecFilePath(s.Bundle)
	specFile, err := os.Open(specFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open OCI spec file: %v", err)
	}
	defer specFile.Close()

	spec, err := LoadFrom(specFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load OCI spec: %v", err)
	}
	return spec, nil
}

// GetContainerRoot returns the root for the container from the associated spec. If the spec is not yet loaded, it is
// loaded and cached.
func (s *State) GetContainerRoot() (string, error) {
	spec, err := s.LoadSpec()
	if err != nil {
		return "", err
	}

	var containerRoot string
	if spec.Root != nil {
		containerRoot = spec.Root.Path
	}

	if filepath.IsAbs(containerRoot) {
		return containerRoot, nil
	}

	return filepath.Join(s.Bundle, containerRoot), nil
}
