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

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"4pd.io/k8s-vgpu/pkg/oci"
)

const (
	ociSpecFileName = "config.json"
)

// newRuntime is a factory method that constructs a runtime based on the selected configuration.
func newRuntime(argv []string) (oci.Runtime, error) {
	ociSpec, err := newOCISpec(argv)
	if err != nil {
		return nil, fmt.Errorf("error constructing OCI specification: %v", err)
	}

	runc, err := newRuncRuntime()
	if err != nil {
		return nil, fmt.Errorf("error constructing runc runtime: %v", err)
	}

	r, err := newNvidiaContainerRuntimeWithLogger(logger.Logger, runc, ociSpec)
	if err != nil {
		return nil, fmt.Errorf("error constructing NVIDIA Container Runtime: %v", err)
	}

	return r, nil
}

// newOCISpec constructs an OCI spec for the provided arguments
func newOCISpec(argv []string) (oci.Spec, error) {
	bundlePath, err := getBundlePath(argv)
	if err != nil {
		return nil, fmt.Errorf("error parsing command line arguments: %v", err)
	}

	ociSpecPath, err := getOCISpecFilePath(bundlePath)
	if err != nil {
		return nil, fmt.Errorf("error getting OCI specification file path: %v", err)
	}
	ociSpec := oci.NewSpecFromFile(ociSpecPath)

	return ociSpec, nil
}

// newRuncRuntime locates the runc binary and wraps it in a SyscallExecRuntime
func newRuncRuntime() (oci.Runtime, error) {
	runtimePath, err := findRunc()
	if err != nil {
		return nil, fmt.Errorf("error locating runtime: %v", err)
	}

	runc, err := oci.NewSyscallExecRuntimeWithLogger(logger.Logger, runtimePath)
	if err != nil {
		return nil, fmt.Errorf("error constructing runtime: %v", err)
	}

	return runc, nil
}

// getBundlePath checks the specified slice of strings (argv) for a 'bundle' flag as allowed by runc.
// The following are supported:
// --bundle{{SEP}}BUNDLE_PATH
// -bundle{{SEP}}BUNDLE_PATH
// -b{{SEP}}BUNDLE_PATH
// where {{SEP}} is either ' ' or '='
func getBundlePath(argv []string) (string, error) {
	var bundlePath string

	for i := 0; i < len(argv); i++ {
		param := argv[i]

		parts := strings.SplitN(param, "=", 2)
		if !isBundleFlag(parts[0]) {
			continue
		}

		// The flag has the format --bundle=/path
		if len(parts) == 2 {
			bundlePath = parts[1]
			continue
		}

		// The flag has the format --bundle /path
		if i+1 < len(argv) {
			bundlePath = argv[i+1]
			i++
			continue
		}

		// --bundle / -b was the last element of argv
		return "", fmt.Errorf("bundle option requires an argument")
	}

	return bundlePath, nil
}

// findRunc locates runc in the path, returning the full path to the
// binary or an error.
func findRunc() (string, error) {
	runtimeCandidates := []string{
		"docker-runc",
		"runc",
	}

	return findRuntime(runtimeCandidates)
}

func findRuntime(runtimeCandidates []string) (string, error) {
	for _, candidate := range runtimeCandidates {
		logger.Infof("Looking for runtime binary '%v'", candidate)
		runcPath, err := exec.LookPath(candidate)
		if err == nil {
			logger.Infof("Found runtime binary '%v'", runcPath)
			return runcPath, nil
		}
		logger.Warnf("Runtime binary '%v' not found: %v", candidate, err)
	}

	return "", fmt.Errorf("no runtime binary found from candidate list: %v", runtimeCandidates)
}

func isBundleFlag(arg string) bool {
	if !strings.HasPrefix(arg, "-") {
		return false
	}

	trimmed := strings.TrimLeft(arg, "-")
	return trimmed == "b" || trimmed == "bundle"
}

// getOCISpecFilePath returns the expected path to the OCI specification file for the given
// bundle directory or the current working directory if not specified.
func getOCISpecFilePath(bundleDir string) (string, error) {
	if bundleDir == "" {
		logger.Infof("Bundle directory path is empty, using working directory.")
		workingDirectory, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("error getting working directory: %v", err)
		}
		bundleDir = workingDirectory
	}

	logger.Infof("Using bundle directory: %v", bundleDir)

	OCISpecFilePath := filepath.Join(bundleDir, ociSpecFileName)

	logger.Infof("Using OCI specification file path: %v", OCISpecFilePath)

	return OCISpecFilePath, nil
}
