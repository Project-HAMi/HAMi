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

package v1

import (
	"errors"
	"fmt"
)

const (
	ImexChannelEnvVar = "NVIDIA_IMEX_CHANNELS"
)

var errInvalidImexConfig = errors.New("invalid IMEX config")

// Imex stores the configuration options for fabric-attached devices.
type Imex struct {
	// ChannelIDs defines a list of channel IDs to inject into containers that request NVIDIA devices.
	// If a channel ID is specified and the associated channel device node exists, the corresponding
	// channel will be added to the ContainerAllocateResponse for containers with access to NVIDIA
	// devices.
	ChannelIDs []int `json:"channelIDs,omitempty" yaml:"channelIDs,omitempty"`
	// Required specifies whether the requested IMEX channel IDs are required or not.
	// If a channel is required, it is expected to exist as the device plugin starts.
	// If it is not required its injection is skipped if the device nodes do not exist or if its
	// existence cannot be queried.
	Required bool `json:"required,omitempty" yaml:"required,omitempty"`
}

// AssertChannelIDsIsValid checks whether the specified list of channel IDs is valid.
func AssertChannelIDsValid(ids []int) error {
	switch {
	case len(ids) == 0:
		return nil
	case len(ids) == 1 && ids[0] == 0:
		return nil
	}
	return fmt.Errorf("%w: channelIDs must be [] or [0]; found %v", errInvalidImexConfig, ids)
}
