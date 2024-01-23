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

// Wait watches for an event with the specified timeout
func (e EventSet) Wait(Timeoutms uint32) (EventData, Return) {
	d, r := nvml.EventSet(e).Wait(Timeoutms)
	eventData := EventData{
		Device:            nvmlDevice(d.Device),
		EventType:         d.EventType,
		EventData:         d.EventData,
		GpuInstanceId:     d.GpuInstanceId,
		ComputeInstanceId: d.ComputeInstanceId,
	}
	return eventData, Return(r)
}

// Free deletes the event set
func (e EventSet) Free() Return {
	return Return(nvml.EventSet(e).Free())
}
