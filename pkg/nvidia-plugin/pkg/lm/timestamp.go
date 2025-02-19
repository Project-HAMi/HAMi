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

package lm

import (
	"fmt"
	"time"

	spec "github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/api/config/v1"
)

// NewTimestampLabeler creates a new label manager for generating timestamp
// labels from the specified config. If the noTimestamp option is set an empty
// label manager is returned.
func NewTimestampLabeler(config *spec.Config) Labeler {
	if *config.Flags.GFD.NoTimestamp {
		return empty{}
	}

	return Labels{
		"nvidia.com/gfd.timestamp": fmt.Sprintf("%d", time.Now().Unix()),
	}
}
