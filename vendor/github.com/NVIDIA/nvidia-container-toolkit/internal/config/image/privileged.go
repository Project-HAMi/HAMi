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

package image

import (
	"github.com/opencontainers/runtime-spec/specs-go"
)

const (
	capSysAdmin = "CAP_SYS_ADMIN"
)

// IsPrivileged returns true if the container is a privileged container.
func IsPrivileged(s *specs.Spec) bool {
	if s.Process.Capabilities == nil {
		return false
	}

	// We only make sure that the bounding capabibility set has
	// CAP_SYS_ADMIN. This allows us to make sure that the container was
	// actually started as '--privileged', but also allow non-root users to
	// access the privileged NVIDIA capabilities.
	for _, c := range s.Process.Capabilities.Bounding {
		if c == capSysAdmin {
			return true
		}
	}
	return false
}
