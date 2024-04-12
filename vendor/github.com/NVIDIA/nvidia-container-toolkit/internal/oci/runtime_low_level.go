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
	"github.com/NVIDIA/nvidia-container-toolkit/internal/lookup"
)

// NewLowLevelRuntime creates a Runtime that wraps a low-level runtime executable.
// The executable specified is taken from the list of supplied candidates, with the first match
// present in the PATH being selected. A logger is also specified.
func NewLowLevelRuntime(logger logger.Interface, candidates []string) (Runtime, error) {
	runtimePath, err := findRuntime(logger, candidates)
	if err != nil {
		return nil, fmt.Errorf("error locating runtime: %v", err)
	}

	logger.Infof("Using low-level runtime %v", runtimePath)
	return NewRuntimeForPath(logger, runtimePath)
}

// findRuntime checks elements in a list of supplied candidates for a matching executable in the PATH.
// The absolute path to the first match is returned.
func findRuntime(logger logger.Interface, candidates []string) (string, error) {
	if len(candidates) == 0 {
		return "", fmt.Errorf("at least one runtime candidate must be specified")
	}

	locator := lookup.NewExecutableLocator(logger, "/")
	for _, candidate := range candidates {
		logger.Debugf("Looking for runtime binary '%v'", candidate)
		targets, err := locator.Locate(candidate)
		if err == nil && len(targets) > 0 {
			logger.Debugf("Found runtime binary '%v'", targets)
			return targets[0], nil
		}
		logger.Debugf("Runtime binary '%v' not found: %v (targets=%v)", candidate, err, targets)
	}

	return "", fmt.Errorf("no runtime binary found from candidate list: %v", candidates)
}
