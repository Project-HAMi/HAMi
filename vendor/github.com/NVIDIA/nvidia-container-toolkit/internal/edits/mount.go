/**
# Copyright (c) 2022, NVIDIA CORPORATION.  All rights reserved.
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

package edits

import (
	"github.com/NVIDIA/nvidia-container-toolkit/internal/discover"
	"tags.cncf.io/container-device-interface/pkg/cdi"
	"tags.cncf.io/container-device-interface/specs-go"
)

type mount discover.Mount

// toEdits converts a discovered mount to CDI Container Edits.
func (d mount) toEdits() *cdi.ContainerEdits {
	e := cdi.ContainerEdits{
		ContainerEdits: &specs.ContainerEdits{
			Mounts: []*specs.Mount{d.toSpec()},
		},
	}
	return &e
}

// toSpec converts a discovered Mount to a CDI Spec Mount. Note
// that missing info is filled in when edits are applied by querying the Mount node.
func (d mount) toSpec() *specs.Mount {
	s := specs.Mount{
		HostPath:      d.HostPath,
		ContainerPath: d.Path,
		Options:       d.Options,
	}

	return &s
}
