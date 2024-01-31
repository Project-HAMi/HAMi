/*
 * Copyright (c) NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package device

import (
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// Identifier can be used to refer to a GPU or MIG device.
// This includes a device index or UUID.
type Identifier string

// IsGpuIndex checks if an identifier is a full GPU index
func (i Identifier) IsGpuIndex() bool {
	if _, err := strconv.ParseUint(string(i), 10, 0); err != nil {
		return false
	}
	return true
}

// IsMigIndex checks if an identifier is a MIG index
func (i Identifier) IsMigIndex() bool {
	split := strings.Split(string(i), ":")
	if len(split) != 2 {
		return false
	}
	for _, s := range split {
		if !Identifier(s).IsGpuIndex() {
			return false
		}
	}
	return true
}

// IsUUID checks if an identifier is a UUID
func (i Identifier) IsUUID() bool {
	return i.IsGpuUUID() || i.IsMigUUID()
}

// IsGpuUUID checks if an identifier is a GPU UUID
// A GPU UUID must be of the form GPU-b1028956-cfa2-0990-bf4a-5da9abb51763
func (i Identifier) IsGpuUUID() bool {
	if !strings.HasPrefix(string(i), "GPU-") {
		return false
	}
	_, err := uuid.Parse(strings.TrimPrefix(string(i), "GPU-"))
	return err == nil
}

// IsMigUUID checks if an identifier is a MIG UUID
// A MIG UUID can be of one of two forms:
//   - MIG-b1028956-cfa2-0990-bf4a-5da9abb51763
//   - MIG-GPU-b1028956-cfa2-0990-bf4a-5da9abb51763/3/0
func (i Identifier) IsMigUUID() bool {
	if !strings.HasPrefix(string(i), "MIG-") {
		return false
	}
	suffix := strings.TrimPrefix(string(i), "MIG-")
	_, err := uuid.Parse(suffix)
	if err == nil {
		return true
	}
	split := strings.Split(suffix, "/")
	if len(split) != 3 {
		return false
	}
	if !Identifier(split[0]).IsGpuUUID() {
		return false
	}
	for _, s := range split[1:] {
		_, err := strconv.ParseUint(s, 10, 0)
		if err != nil {
			return false
		}
	}
	return true
}
