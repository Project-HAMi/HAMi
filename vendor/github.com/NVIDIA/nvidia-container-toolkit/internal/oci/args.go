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
	"path/filepath"
	"strings"
)

const (
	specFileName = "config.json"
)

// GetBundleDir returns the bundle directory or default depending on the
// supplied command line arguments.
func GetBundleDir(args []string) (string, error) {
	bundleDir, err := GetBundleDirFromArgs(args)
	if err != nil {
		return "", fmt.Errorf("error getting bundle dir from args: %v", err)
	}

	return bundleDir, nil
}

// GetBundleDirFromArgs checks the specified slice of strings (argv) for a 'bundle' flag as allowed by runc.
// The following are supported:
// --bundle{{SEP}}BUNDLE_PATH
// -bundle{{SEP}}BUNDLE_PATH
// -b{{SEP}}BUNDLE_PATH
// where {{SEP}} is either ' ' or '='
func GetBundleDirFromArgs(args []string) (string, error) {
	var bundleDir string

	for i := 0; i < len(args); i++ {
		param := args[i]

		parts := strings.SplitN(param, "=", 2)
		if !IsBundleFlag(parts[0]) {
			continue
		}

		// The flag has the format --bundle=/path
		if len(parts) == 2 {
			bundleDir = parts[1]
			continue
		}

		// The flag has the format --bundle /path
		if i+1 < len(args) {
			bundleDir = args[i+1]
			i++
			continue
		}

		// --bundle / -b was the last element of args
		return "", fmt.Errorf("bundle option requires an argument")
	}

	return bundleDir, nil
}

// GetSpecFilePath returns the expected path to the OCI specification file for the given
// bundle directory.
func GetSpecFilePath(bundleDir string) string {
	specFilePath := filepath.Join(bundleDir, specFileName)
	return specFilePath
}

// IsBundleFlag is a helper function that checks wither the specified argument represents
// a bundle flag (--bundle or -b)
func IsBundleFlag(arg string) bool {
	if !strings.HasPrefix(arg, "-") {
		return false
	}

	trimmed := strings.TrimLeft(arg, "-")
	return trimmed == "b" || trimmed == "bundle"
}

// HasCreateSubcommand checks the supplied arguments for a 'create' subcommand
func HasCreateSubcommand(args []string) bool {
	var previousWasBundle bool
	for _, a := range args {
		// We check for '--bundle create' explicitly to ensure that we
		// don't inadvertently trigger a modification if the bundle directory
		// is specified as `create`
		if !previousWasBundle && IsBundleFlag(a) {
			previousWasBundle = true
			continue
		}

		if !previousWasBundle && a == "create" {
			return true
		}

		previousWasBundle = false
	}

	return false
}
