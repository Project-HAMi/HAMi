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

package proc

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// GPUInfoField represents the field name for information specified in a GPU's information file
type GPUInfoField string

// The following constants define the fields of interest from the GPU information file
const (
	GPUInfoModel       = GPUInfoField("Model")
	GPUInfoGPUUUID     = GPUInfoField("GPU UUID")
	GPUInfoBusLocation = GPUInfoField("Bus Location")
	GPUInfoDeviceMinor = GPUInfoField("Device Minor")
)

// GPUInfo stores the information for a GPU as determined from its associated information file
type GPUInfo map[GPUInfoField]string

// GetInformationFilePaths returns the list of information files associated with NVIDIA GPUs.
func GetInformationFilePaths(root string) ([]string, error) {
	return filepath.Glob(filepath.Join(root, "/proc/driver/nvidia/gpus/*/information"))
}

// ParseGPUInformationFile parses the specified GPU information file and constructs a GPUInfo structure
func ParseGPUInformationFile(path string) (GPUInfo, error) {
	infoFile, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open %v: %v", path, err)
	}
	defer infoFile.Close()

	return gpuInfoFrom(infoFile), nil
}

// gpuInfoFrom parses a GPUInfo struct from the specified reader
// An information file has the following structure:
// $ cat /proc/driver/nvidia/gpus/0000\:06\:00.0/information
// Model:           Tesla V100-SXM2-16GB
// IRQ:             408
// GPU UUID:        GPU-edfee158-11c1-52b8-0517-92f30e7fac88
// Video BIOS:      88.00.41.00.01
// Bus Type:        PCIe
// DMA Size:        47 bits
// DMA Mask:        0x7fffffffffff
// Bus Location:    0000:06:00.0
// Device Minor:    0
// GPU Excluded:    No
func gpuInfoFrom(reader io.Reader) GPUInfo {
	info := make(GPUInfo)
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		field := GPUInfoField(parts[0])
		value := strings.TrimSpace(parts[1])

		info[field] = value
	}

	return info
}
