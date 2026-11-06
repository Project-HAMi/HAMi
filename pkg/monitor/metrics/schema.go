/*
Copyright 2026 The HAMi Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package metrics defines the shared local and remote GPU monitoring schema.
package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	HostMemory = prometheus.NewDesc(
		"hami_host_gpu_memory_used_bytes",
		"GPU device memory usage in bytes",
		[]string{"node", "device_index", "device_uuid", "device_type"}, nil,
	)

	HostUtilization = prometheus.NewDesc(
		"hami_host_gpu_utilization_ratio",
		"GPU core utilization ratio (0-100)",
		[]string{"node", "device_index", "device_uuid", "device_type"}, nil,
	)

	ContainerMemory = prometheus.NewDesc(
		"hami_vgpu_memory_used_bytes",
		"vGPU device memory usage in bytes",
		[]string{"namespace", "pod", "container", "vdevice_index", "device_uuid"}, nil,
	)

	ContainerMemoryLimit = prometheus.NewDesc(
		"hami_vgpu_memory_limit_bytes",
		"vGPU device memory limit in bytes",
		[]string{"namespace", "pod", "container", "vdevice_index", "device_uuid"}, nil,
	)

	ContainerDeviceMemory = prometheus.NewDesc(
		"hami_container_device_memory_bytes",
		`Container device memory usage in bytes (DEPRECATED: use hami_vgpu_memory_used_bytes, deprecated in v2.11.0, removed in v2.12.0)`,
		[]string{"namespace", "pod", "container", "vdevice_index", "device_uuid"}, nil,
	)

	ContainerUtilization = prometheus.NewDesc(
		"hami_container_device_utilization_ratio",
		"Container device SM utilization ratio",
		[]string{"namespace", "pod", "container", "vdevice_index", "device_uuid"}, nil,
	)
)
