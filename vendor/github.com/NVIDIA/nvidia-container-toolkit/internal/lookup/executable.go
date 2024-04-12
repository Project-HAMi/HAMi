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
	"strings"

	"github.com/NVIDIA/nvidia-container-toolkit/internal/logger"
)

type executable struct {
	file
}

// NewExecutableLocator creates a locator to fine executable files in the path. A logger can also be specified.
func NewExecutableLocator(logger logger.Interface, root string) Locator {
	paths := GetPaths(root)

	return newExecutableLocator(logger, root, paths...)
}

func newExecutableLocator(logger logger.Interface, root string, paths ...string) *executable {
	f := newFileLocator(
		WithLogger(logger),
		WithRoot(root),
		WithSearchPaths(paths...),
		WithFilter(assertExecutable),
		WithCount(1),
	)

	l := executable{
		file: *f,
	}

	return &l
}

var _ Locator = (*executable)(nil)

// Locate finds executable files with the specified pattern in the path.
// If a relative or absolute path is specified, the prefix paths are not considered.
func (p executable) Locate(pattern string) ([]string, error) {
	// For absolute paths we ensure that it is executable
	if strings.Contains(pattern, "/") {
		err := assertExecutable(pattern)
		if err != nil {
			return nil, fmt.Errorf("absolute path %v is not an executable file: %v", pattern, err)
		}
		return []string{pattern}, nil
	}

	return p.file.Locate(pattern)
}

// assertExecutable checks whether the specified path is an execuable file.
func assertExecutable(filename string) error {
	err := assertFile(filename)
	if err != nil {
		return err
	}
	info, err := os.Stat(filename)
	if err != nil {
		return err
	}

	if info.Mode()&0111 == 0 {
		return fmt.Errorf("specified file '%v' is not executable", filename)
	}

	return nil
}
