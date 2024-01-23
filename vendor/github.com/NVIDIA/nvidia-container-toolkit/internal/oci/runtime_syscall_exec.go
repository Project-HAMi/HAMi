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
)

type syscallExec struct{}

var _ Runtime = (*syscallExec)(nil)

func (r syscallExec) Exec(args []string) error {
	//nolint:gosec // TODO: Can we harden this so that there is less risk of command injection
	err := syscall.Exec(args[0], args, os.Environ())
	if err != nil {
		return fmt.Errorf("could not exec '%v': %v", args[0], err)
	}

	// syscall.Exec is not expected to return. This is an error state regardless of whether
	// err is nil or not.
	return fmt.Errorf("unexpected return from exec '%v'", args[0])
}
