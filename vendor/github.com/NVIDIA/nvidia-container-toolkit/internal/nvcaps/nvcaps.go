/*
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
*/

package nvcaps

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	nvidiaProcDriverPath   = "/proc/driver/nvidia"
	nvidiaCapabilitiesPath = nvidiaProcDriverPath + "/capabilities"

	nvcapsProcDriverPath = "/proc/driver/nvidia-caps"
	nvcapsMigMinorsPath  = nvcapsProcDriverPath + "/mig-minors"
	nvcapsDevicePath     = "/dev/nvidia-caps"
)

// MigMinor represents the minor number of a MIG device
type MigMinor int

// MigCap represents the path to a MIG cap file
type MigCap string

// MigCaps stores a map of MIG cap file paths to MIG minors
type MigCaps map[MigCap]MigMinor

// NewGPUInstanceCap creates a MigCap for the specified MIG GPU instance.
// A GPU instance is uniquely defined by the GPU minor number and GI instance ID.
func NewGPUInstanceCap(gpu, gi int) MigCap {
	return MigCap(fmt.Sprintf("gpu%d/gi%d/access", gpu, gi))
}

// NewComputeInstanceCap creates a MigCap for the specified MIG Compute instance.
// A GPU instance is uniquely defined by the GPU minor number, GI instance ID, and CI instance ID.
func NewComputeInstanceCap(gpu, gi, ci int) MigCap {
	return MigCap(fmt.Sprintf("gpu%d/gi%d/ci%d/access", gpu, gi, ci))
}

// GetCapDevicePath returns the path to the cap device for the specified cap.
// An error is returned if the cap is invalid.
func (m MigCaps) GetCapDevicePath(cap MigCap) (string, error) {
	minor, exists := m[cap]
	if !exists {
		return "", fmt.Errorf("invalid MIG capability path %v", cap)
	}
	return minor.DevicePath(), nil
}

// NewMigCaps creates a MigCaps structure based on the contents of the MIG minors file.
func NewMigCaps() (MigCaps, error) {
	// Open nvcapsMigMinorsPath for walking.
	// If the nvcapsMigMinorsPath does not exist, then we are not on a MIG
	// capable machine, so there is nothing to do.
	// The format of this file is discussed in:
	//     https://docs.nvidia.com/datacenter/tesla/mig-user-guide/index.html#unique_1576522674
	minorsFile, err := os.Open(nvcapsMigMinorsPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("error opening MIG minors file: %v", err)
	}
	defer minorsFile.Close()

	return processMinorsFile(minorsFile), nil
}

func processMinorsFile(minorsFile io.Reader) MigCaps {
	// Walk each line of nvcapsMigMinorsPath and construct a mapping of nvidia
	// capabilities path to device minor for that capability
	migCaps := make(MigCaps)
	scanner := bufio.NewScanner(minorsFile)
	for scanner.Scan() {
		cap, minor, err := processMigMinorsLine(scanner.Text())
		if err != nil {
			log.Printf("Skipping line in MIG minors file: %v", err)
			continue
		}
		migCaps[cap] = minor
	}
	return migCaps
}

func processMigMinorsLine(line string) (MigCap, MigMinor, error) {
	parts := strings.Split(line, " ")
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("error processing line: %v", line)
	}

	migCap := MigCap(parts[0])
	if !migCap.isValid() {
		return "", 0, fmt.Errorf("invalid MIG minors line: '%v'", line)
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, fmt.Errorf("error reading MIG minor from '%v': %v", line, err)
	}

	return migCap, MigMinor(minor), nil
}

func (m MigCap) isValid() bool {
	cap := string(m)
	switch cap {
	case "config", "monitor":
		return true
	default:
		var gpu int
		var gi int
		var ci int
		// Look for a CI access file
		n, _ := fmt.Sscanf(cap, "gpu%d/gi%d/ci%d/access", &gpu, &gi, &ci)
		if n == 3 {
			return true
		}
		// Look for a GI access file
		n, _ = fmt.Sscanf(cap, "gpu%d/gi%d/access %d", &gpu, &gi)
		if n == 2 {
			return true
		}
	}
	return false
}

// ProcPath returns the proc path associated with the MIG capability
func (m MigCap) ProcPath() string {
	id := string(m)

	var path string
	switch id {
	case "config", "monitor":
		path = "mig/" + id
	default:
		parts := strings.SplitN(id, "/", 2)
		path = strings.Join([]string{parts[0], "mig", parts[1]}, "/")
	}
	return filepath.Join(nvidiaCapabilitiesPath, path)
}

// DevicePath returns the path for the nvidia-caps device with the specified
// minor number
func (m MigMinor) DevicePath() string {
	return fmt.Sprintf(nvcapsDevicePath+"/nvidia-cap%d", m)
}
