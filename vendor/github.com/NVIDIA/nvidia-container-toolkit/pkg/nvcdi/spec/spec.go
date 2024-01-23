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

package spec

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"tags.cncf.io/container-device-interface/pkg/cdi"
	"tags.cncf.io/container-device-interface/specs-go"
)

type spec struct {
	*specs.Spec
	format      string
	permissions os.FileMode
}

var _ Interface = (*spec)(nil)

// New creates a new spec with the specified options.
func New(opts ...Option) (Interface, error) {
	return newBuilder(opts...).Build()
}

// Save writes the spec to the specified path and overwrites the file if it exists.
func (s *spec) Save(path string) error {
	path, err := s.normalizePath(path)
	if err != nil {
		return fmt.Errorf("failed to normalize path: %w", err)
	}

	specDir := filepath.Dir(path)
	registry := cdi.GetRegistry(
		cdi.WithAutoRefresh(false),
		cdi.WithSpecDirs(specDir),
	)

	if err := registry.SpecDB().WriteSpec(s.Raw(), filepath.Base(path)); err != nil {
		return fmt.Errorf("failed to write spec: %w", err)
	}

	if err := os.Chmod(path, s.permissions); err != nil {
		return fmt.Errorf("failed to set permissions on spec file: %w", err)
	}

	return nil
}

// WriteTo writes the spec to the specified writer.
func (s *spec) WriteTo(w io.Writer) (int64, error) {
	name, err := cdi.GenerateNameForSpec(s.Raw())
	if err != nil {
		return 0, err
	}

	path, _ := s.normalizePath(name)
	tmpFile, err := os.CreateTemp("", "*"+filepath.Base(path))
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmpFile.Name())

	if err := s.Save(tmpFile.Name()); err != nil {
		return 0, err
	}

	err = tmpFile.Close()
	if err != nil {
		return 0, fmt.Errorf("failed to close temporary file: %w", err)
	}

	r, err := os.Open(tmpFile.Name())
	if err != nil {
		return 0, fmt.Errorf("failed to open temporary file: %w", err)
	}
	defer r.Close()

	return io.Copy(w, r)
}

// Raw returns a pointer to the raw spec.
func (s *spec) Raw() *specs.Spec {
	return s.Spec
}

// normalizePath ensures that the specified path has a supported extension
func (s *spec) normalizePath(path string) (string, error) {
	if ext := filepath.Ext(path); ext != ".yaml" && ext != ".json" {
		path += s.extension()
	}

	if filepath.Clean(filepath.Dir(path)) == "." {
		pwd, err := os.Getwd()
		if err != nil {
			return path, fmt.Errorf("failed to get current working directory: %v", err)
		}
		path = filepath.Join(pwd, path)
	}

	return path, nil
}

func (s *spec) extension() string {
	switch s.format {
	case FormatJSON:
		return ".json"
	case FormatYAML:
		return ".yaml"
	}

	return ".yaml"
}
