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
	"fmt"

	"github.com/NVIDIA/nvidia-container-toolkit/internal/logger"
	"github.com/opencontainers/runtime-spec/specs-go"
)

// SpecModifier defines an interface for modifying a (raw) OCI spec
type SpecModifier interface {
	// Modify is a method that accepts a pointer to an OCI Srec and returns an
	// error. The intention is that the function would modify the spec in-place.
	Modify(*specs.Spec) error
}

// Spec defines the operations to be performed on an OCI specification
//
//go:generate moq -stub -out spec_mock.go . Spec
type Spec interface {
	Load() (*specs.Spec, error)
	Flush() error
	Modify(SpecModifier) error
	LookupEnv(string) (string, bool)
}

// NewSpec creates fileSpec based on the command line arguments passed to the
// application using the specified logger.
func NewSpec(logger logger.Interface, args []string) (Spec, error) {
	bundleDir, err := GetBundleDir(args)
	if err != nil {
		return nil, fmt.Errorf("error getting bundle directory: %v", err)
	}
	logger.Debugf("Using bundle directory: %v", bundleDir)

	ociSpecPath := GetSpecFilePath(bundleDir)
	logger.Infof("Using OCI specification file path: %v", ociSpecPath)

	ociSpec := NewFileSpec(ociSpecPath)

	return ociSpec, nil
}
