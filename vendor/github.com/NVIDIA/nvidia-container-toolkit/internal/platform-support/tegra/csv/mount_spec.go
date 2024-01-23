/**
# Copyright (c) 2021-2022, NVIDIA CORPORATION.  All rights reserved.
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

package csv

import (
	"fmt"
	"strings"
)

// MountSpecType defines the mount types allowed in a CSV file
type MountSpecType string

const (
	// MountSpecDev is used for character devices
	MountSpecDev = MountSpecType("dev")
	// MountSpecDir is used for directories
	MountSpecDir = MountSpecType("dir")
	// MountSpecLib is used for libraries or regular files
	MountSpecLib = MountSpecType("lib")
	// MountSpecSym is used for symlinks.
	MountSpecSym = MountSpecType("sym")
)

// MountSpec represents a Jetson mount consisting of a type and a path.
type MountSpec struct {
	Type MountSpecType
	Path string
}

// NewMountSpecFromLine parses the specified line and returns the MountSpec or an error if the line is malformed
func NewMountSpecFromLine(line string) (*MountSpec, error) {
	parts := strings.SplitN(strings.TrimSpace(line), ",", 2)
	if len(parts) < 2 {
		return nil, fmt.Errorf("failed to parse line: %v", line)
	}
	mountType := strings.TrimSpace(parts[0])
	path := strings.TrimSpace(parts[1])

	return NewMountSpec(mountType, path)
}

// NewMountSpec creates a MountSpec with the specified type and path. An error is returned if the type is invalid.
func NewMountSpec(mountType string, path string) (*MountSpec, error) {
	mt := MountSpecType(mountType)
	switch mt {
	case MountSpecDev, MountSpecLib, MountSpecSym, MountSpecDir:
	default:
		return nil, fmt.Errorf("unexpected mount type: %v", mt)
	}
	if path == "" {
		return nil, fmt.Errorf("invalid path: %v", path)
	}

	mount := MountSpec{
		Type: mt,
		Path: path,
	}

	return &mount, nil
}
