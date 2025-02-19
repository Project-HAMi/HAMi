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
	"strconv"

	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/pkg/vgpu"
)

// vgpuLabeler manages VGPUs labels for the node
type vgpuLabeler struct {
	lib vgpu.Interface
}

// NewVGPULabeler creates a new VGP label manager using the provided vgpu library
// and config.
func NewVGPULabeler(vgpu vgpu.Interface) Labeler {
	return vgpuLabeler{lib: vgpu}
}

// Labels generates the VGPU labels for the node
func (manager vgpuLabeler) Labels() (Labels, error) {
	devices, err := manager.lib.Devices()
	if err != nil {
		klog.ErrorS(err, "unable to get vGPU devices")
		return nil, nil
	}
	labels := Labels{
		"nvidia.com/vgpu.present": strconv.FormatBool(len(devices) > 0),
	}
	for _, device := range devices {
		info, err := device.GetInfo()
		if err != nil {
			return nil, fmt.Errorf("error getting vGPU device info: %v", err)
		}
		labels["nvidia.com/vgpu.host-driver-version"] = info.HostDriverVersion
		labels["nvidia.com/vgpu.host-driver-branch"] = info.HostDriverBranch
	}
	return labels, nil
}
