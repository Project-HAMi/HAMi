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
	"os"

	"github.com/NVIDIA/nvidia-container-toolkit/internal/logger"
)

// pathRuntime wraps the path that a binary and defines the semantics for how to exec into it.
// This can be used to wrap an OCI-compliant low-level runtime binary, allowing it to be used through the
// Runtime internface.
type pathRuntime struct {
	logger      logger.Interface
	path        string
	execRuntime Runtime
}

var _ Runtime = (*pathRuntime)(nil)

// NewRuntimeForPath creates a Runtime for the specified logger and path
func NewRuntimeForPath(logger logger.Interface, path string) (Runtime, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path '%v': %v", path, err)
	}
	if info.IsDir() || info.Mode()&0111 == 0 {
		return nil, fmt.Errorf("specified path '%v' is not an executable file", path)
	}

	shim := pathRuntime{
		logger:      logger,
		path:        path,
		execRuntime: syscallExec{},
	}

	return &shim, nil
}

// Exec exces into the binary at the path from the pathRuntime struct, passing it the supplied arguments
// after ensuring that the first argument is the path of the target binary.
func (s pathRuntime) Exec(args []string) error {
	runtimeArgs := []string{s.path}
	if len(args) > 1 {
		runtimeArgs = append(runtimeArgs, args[1:]...)
	}

	return s.execRuntime.Exec(runtimeArgs)
}
