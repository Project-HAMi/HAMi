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

package cuda

import (
	"github.com/NVIDIA/nvidia-container-toolkit/internal/logger"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/lookup"
)

type cudaLocator struct {
	lookup.Locator
	logger     logger.Interface
	driverRoot string
}

// Options is a function that configures a cudaLocator.
type Options func(*cudaLocator)

// WithLogger is an option that configures the logger used by the locator.
func WithLogger(logger logger.Interface) Options {
	return func(c *cudaLocator) {
		c.logger = logger
	}
}

// WithDriverRoot is an option that configures the driver root used by the locator.
func WithDriverRoot(driverRoot string) Options {
	return func(c *cudaLocator) {
		c.driverRoot = driverRoot
	}
}

// New creates a new CUDA library locator.
func New(opts ...Options) lookup.Locator {
	c := &cudaLocator{}
	for _, opt := range opts {
		opt(c)
	}

	if c.logger == nil {
		c.logger = logger.New()
	}
	if c.driverRoot == "" {
		c.driverRoot = "/"
	}

	// TODO: Do we want to set the Count to 1 here?
	l, _ := lookup.NewLibraryLocator(
		c.logger,
		c.driverRoot,
	)

	c.Locator = l
	return c
}

// Locate returns the path to the libcuda.so.RMVERSION file.
// libcuda.so is prefixed to the specified pattern.
func (l *cudaLocator) Locate(pattern string) ([]string, error) {
	return l.Locator.Locate("libcuda.so" + pattern)
}
