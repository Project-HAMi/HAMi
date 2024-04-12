/*
 * Copyright (c) 2022, NVIDIA CORPORATION.  All rights reserved.
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

package nvml

import (
	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

type nvmlGpuInstance nvml.GpuInstance

var _ GpuInstance = (*nvmlGpuInstance)(nil)

// GetInfo returns info about a GPU Intsance
func (gi nvmlGpuInstance) GetInfo() (GpuInstanceInfo, Return) {
	i, r := nvml.GpuInstance(gi).GetInfo()
	info := GpuInstanceInfo{
		Device:    nvmlDevice(i.Device),
		Id:        i.Id,
		ProfileId: i.ProfileId,
		Placement: GpuInstancePlacement(i.Placement),
	}
	return info, Return(r)
}

// GetComputeInstanceById returns the Compute Instance associated with a particular ID.
func (gi nvmlGpuInstance) GetComputeInstanceById(id int) (ComputeInstance, Return) {
	ci, r := nvml.GpuInstance(gi).GetComputeInstanceById(id)
	return nvmlComputeInstance(ci), Return(r)
}

// GetComputeInstanceProfileInfo returns info about a given Compute Instance profile
func (gi nvmlGpuInstance) GetComputeInstanceProfileInfo(profile int, engProfile int) (ComputeInstanceProfileInfo, Return) {
	p, r := nvml.GpuInstance(gi).GetComputeInstanceProfileInfo(profile, engProfile)
	return ComputeInstanceProfileInfo(p), Return(r)
}

// CreateComputeInstance creates a Compute Instance within the GPU Instance
func (gi nvmlGpuInstance) CreateComputeInstance(info *ComputeInstanceProfileInfo) (ComputeInstance, Return) {
	ci, r := nvml.GpuInstance(gi).CreateComputeInstance((*nvml.ComputeInstanceProfileInfo)(info))
	return nvmlComputeInstance(ci), Return(r)
}

// GetComputeInstances returns the set of Compute Instances associated with a GPU Instance
func (gi nvmlGpuInstance) GetComputeInstances(info *ComputeInstanceProfileInfo) ([]ComputeInstance, Return) {
	nvmlCis, r := nvml.GpuInstance(gi).GetComputeInstances((*nvml.ComputeInstanceProfileInfo)(info))
	var cis []ComputeInstance
	for _, ci := range nvmlCis {
		cis = append(cis, nvmlComputeInstance(ci))
	}
	return cis, Return(r)
}

// Destroy destroys a GPU Instance
func (gi nvmlGpuInstance) Destroy() Return {
	r := nvml.GpuInstance(gi).Destroy()
	return Return(r)
}
