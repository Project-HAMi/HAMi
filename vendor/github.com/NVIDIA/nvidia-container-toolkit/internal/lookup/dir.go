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

package lookup

import (
	"fmt"
	"os"
)

// NewDirectoryLocator creates a Locator that can be used to find directories at the specified root.
func NewDirectoryLocator(opts ...Option) Locator {
	return NewFileLocator(
		append(
			opts,
			WithFilter(assertDirectory),
		)...,
	)
}

// assertDirectory checks wither the specified path is a directory.
func assertDirectory(filename string) error {
	info, err := os.Stat(filename)
	if err != nil {
		return fmt.Errorf("error getting info for %v: %v", filename, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("specified path '%v' is not a directory", filename)
	}

	return nil
}
