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

package info

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NVIDIA/go-nvml/pkg/dl"
)

// Interface provides the API to the info package
type Interface interface {
	HasDXCore() (bool, string)
	HasNvml() (bool, string)
	IsTegraSystem() (bool, string)
}

type infolib struct {
	root string
}

var _ Interface = &infolib{}

// HasDXCore returns true if DXCore is detected on the system.
func (i *infolib) HasDXCore() (bool, string) {
	const (
		libraryName = "libdxcore.so"
	)
	if err := assertHasLibrary(libraryName); err != nil {
		return false, fmt.Sprintf("could not load DXCore library: %v", err)
	}

	return true, "found DXCore library"
}

// HasNvml returns true if NVML is detected on the system
func (i *infolib) HasNvml() (bool, string) {
	const (
		libraryName = "libnvidia-ml.so.1"
	)
	if err := assertHasLibrary(libraryName); err != nil {
		return false, fmt.Sprintf("could not load NVML library: %v", err)
	}

	return true, "found NVML library"
}

// IsTegraSystem returns true if the system is detected as a Tegra-based system
func (i *infolib) IsTegraSystem() (bool, string) {
	tegraReleaseFile := filepath.Join(i.root, "/etc/nv_tegra_release")
	tegraFamilyFile := filepath.Join(i.root, "/sys/devices/soc0/family")

	if info, err := os.Stat(tegraReleaseFile); err == nil && !info.IsDir() {
		return true, fmt.Sprintf("%v found", tegraReleaseFile)
	}

	if info, err := os.Stat(tegraFamilyFile); err != nil || info.IsDir() {
		return false, fmt.Sprintf("%v file not found", tegraFamilyFile)
	}

	contents, err := os.ReadFile(tegraFamilyFile)
	if err != nil {
		return false, fmt.Sprintf("could not read %v", tegraFamilyFile)
	}

	if strings.HasPrefix(strings.ToLower(string(contents)), "tegra") {
		return true, fmt.Sprintf("%v has 'tegra' prefix", tegraFamilyFile)
	}

	return false, fmt.Sprintf("%v has no 'tegra' prefix", tegraFamilyFile)
}

// assertHasLibrary returns an error if the specified library cannot be loaded
func assertHasLibrary(libraryName string) error {
	const (
		libraryLoadFlags = dl.RTLD_LAZY
	)
	lib := dl.New(libraryName, libraryLoadFlags)
	if err := lib.Open(); err != nil {
		return err
	}
	defer lib.Close()

	return nil
}
