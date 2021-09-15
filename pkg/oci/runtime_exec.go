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
	"syscall"

	log "github.com/sirupsen/logrus"
)

// SyscallExecRuntime wraps the path that a binary and defines the semanitcs for how to exec into it.
// This can be used to wrap an OCI-compliant low-level runtime binary, allowing it to be used through the
// Runtime internface.
type SyscallExecRuntime struct {
	logger *log.Logger
	path   string
	// exec is used for testing. This defaults to syscall.Exec
	exec func(argv0 string, argv []string, envv []string) error
}

var _ Runtime = (*SyscallExecRuntime)(nil)

// NewSyscallExecRuntime creates a SyscallExecRuntime for the specified path with the standard logger
func NewSyscallExecRuntime(path string) (Runtime, error) {
	return NewSyscallExecRuntimeWithLogger(log.StandardLogger(), path)
}

// NewSyscallExecRuntimeWithLogger creates a SyscallExecRuntime for the specified logger and path
func NewSyscallExecRuntimeWithLogger(logger *log.Logger, path string) (Runtime, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path '%v': %v", path, err)
	}
	if info.IsDir() || info.Mode()&0111 == 0 {
		return nil, fmt.Errorf("specified path '%v' is not an executable file", path)
	}

	shim := SyscallExecRuntime{
		logger: logger,
		path:   path,
		exec:   syscall.Exec,
	}

	return &shim, nil
}

// Exec exces into the binary at the path from the SyscallExecRuntime struct, passing it the supplied arguments
// after ensuring that the first argument is the path of the target binary.
func (s SyscallExecRuntime) Exec(args []string) error {
	runtimeArgs := []string{s.path}
	if len(args) > 1 {
		runtimeArgs = append(runtimeArgs, args[1:]...)
	}

	err := s.exec(s.path, runtimeArgs, os.Environ())
	if err != nil {
		return fmt.Errorf("could not exec '%v': %v", s.path, err)
	}

	// syscall.Exec is not expected to return. This is an error state regardless of whether
	// err is nil or not.
	return fmt.Errorf("unexpected return from exec '%v'", s.path)
}
