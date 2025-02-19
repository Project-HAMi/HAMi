/**
# Copyright 2024 NVIDIA CORPORATION
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

package mps

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/pkg/rm"
)

var errInvalidDevice = errors.New("invalid device")

// mpsDevice represents an MPS-specific alias for an rm.Device.
type mpsDevice rm.Device

// assertReplicas checks whether the number of replicas specified is valid.
func (d *mpsDevice) assertReplicas() error {
	maxClients := d.maxClients()
	if d.Replicas > maxClients {
		return fmt.Errorf("%w maximum allowed replicas exceeded: %d > %d", errInvalidDevice, d.Replicas, maxClients)
	}
	return nil
}

// maxClients returns the maximum number of clients supported by an MPS server.
func (d *mpsDevice) maxClients() int {
	if d.isAtLeastVolta() {
		return 48
	}
	return 16
}

// isAtLeastVolta checks whether the specified device is a volta device or newer.
func (d *mpsDevice) isAtLeastVolta() bool {
	vCc := "v" + strings.TrimPrefix(d.ComputeCapability, "v")
	return semver.Compare(semver.Canonical(vCc), semver.Canonical("v7.5")) >= 0
}
