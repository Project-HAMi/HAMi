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

type nvmlComputeInstance nvml.ComputeInstance

var _ ComputeInstance = (*nvmlComputeInstance)(nil)

// GetInfo() returns info about a Compute Instance
func (ci nvmlComputeInstance) GetInfo() (ComputeInstanceInfo, Return) {
	i, r := nvml.ComputeInstance(ci).GetInfo()
	info := ComputeInstanceInfo{
		Device:      nvmlDevice(i.Device),
		GpuInstance: nvmlGpuInstance(i.GpuInstance),
		Id:          i.Id,
		ProfileId:   i.ProfileId,
		Placement:   ComputeInstancePlacement(i.Placement),
	}
	return info, Return(r)
}

// Destroy() destroys a Compute Instance
func (ci nvmlComputeInstance) Destroy() Return {
	r := nvml.ComputeInstance(ci).Destroy()
	return Return(r)
}
