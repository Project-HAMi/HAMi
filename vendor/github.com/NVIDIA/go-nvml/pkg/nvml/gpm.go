// Copyright (c) 2022, NVIDIA CORPORATION.  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package nvml

// nvml.GpmMetricsGet()
type GpmMetricsGetVType struct {
	metricsGet *GpmMetricsGetType
}

func GpmMetricsGetV(MetricsGet *GpmMetricsGetType) GpmMetricsGetVType {
	return GpmMetricsGetVType{MetricsGet}
}

func (MetricsGetV GpmMetricsGetVType) V1() Return {
	MetricsGetV.metricsGet.Version = 1
	return nvmlGpmMetricsGet(MetricsGetV.metricsGet)
}

func GpmMetricsGet(MetricsGet *GpmMetricsGetType) Return {
	MetricsGet.Version = GPM_METRICS_GET_VERSION
	return nvmlGpmMetricsGet(MetricsGet)
}

// nvml.GpmSampleFree()
func GpmSampleFree(GpmSample GpmSample) Return {
	return nvmlGpmSampleFree(GpmSample)
}

// nvml.GpmSampleAlloc()
func GpmSampleAlloc(GpmSample *GpmSample) Return {
	return nvmlGpmSampleAlloc(GpmSample)
}

// nvml.GpmSampleGet()
func GpmSampleGet(Device Device, GpmSample GpmSample) Return {
	return nvmlGpmSampleGet(Device, GpmSample)
}

func (Device Device) GpmSampleGet(GpmSample GpmSample) Return {
	return GpmSampleGet(Device, GpmSample)
}

// nvml.GpmQueryDeviceSupport()
type GpmSupportV struct {
	device Device
}

func GpmQueryDeviceSupportV(Device Device) GpmSupportV {
	return GpmSupportV{Device}
}

func (Device Device) GpmQueryDeviceSupportV() GpmSupportV {
	return GpmSupportV{Device}
}

func (GpmSupportV GpmSupportV) V1() (GpmSupport, Return) {
	var GpmSupport GpmSupport
	GpmSupport.Version = 1
	ret := nvmlGpmQueryDeviceSupport(GpmSupportV.device, &GpmSupport)
	return GpmSupport, ret
}

func GpmQueryDeviceSupport(Device Device) (GpmSupport, Return) {
	var GpmSupport GpmSupport
	GpmSupport.Version = GPM_SUPPORT_VERSION
	ret := nvmlGpmQueryDeviceSupport(Device, &GpmSupport)
	return GpmSupport, ret
}

func (Device Device) GpmQueryDeviceSupport() (GpmSupport, Return) {
	return GpmQueryDeviceSupport(Device)
}

// nvml.GpmMigSampleGet()
func GpmMigSampleGet(Device Device, GpuInstanceId int, GpmSample GpmSample) Return {
	return nvmlGpmMigSampleGet(Device, uint32(GpuInstanceId), GpmSample)
}

func (Device Device) GpmMigSampleGet(GpuInstanceId int, GpmSample GpmSample) Return {
	return GpmMigSampleGet(Device, GpuInstanceId, GpmSample)
}
