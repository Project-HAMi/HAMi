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

package main

import (
	"strings"
	"testing"

	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/Project-HAMi/HAMi/pkg/device"
	schedulerpkg "github.com/Project-HAMi/HAMi/pkg/scheduler"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
)

type fakeSchedulerMetricsProvider struct {
	nodeUsage    map[string]*schedulerpkg.NodeUsage
	quotaManager *device.QuotaManager
	podManager   *device.PodManager
}

func (f *fakeSchedulerMetricsProvider) InspectAllNodesUsage() *map[string]*schedulerpkg.NodeUsage {
	return &f.nodeUsage
}

func (f *fakeSchedulerMetricsProvider) GetQuotaManager() *device.QuotaManager {
	return f.quotaManager
}

func (f *fakeSchedulerMetricsProvider) GetPodManager() *device.PodManager {
	return f.podManager
}

func TestClusterManagerCollectorSkipsMemoryRatioWithNonPositiveTotalMemory(t *testing.T) {
	nodeUsage := map[string]*schedulerpkg.NodeUsage{
		"node-1": {
			Devices: policy.DeviceUsageList{
				DeviceLists: []*policy.DeviceListsScore{
					{
						Device: &device.DeviceUsage{
							ID:        "zero-memory",
							Index:     0,
							Totalmem:  0,
							Totalcore: 2,
							Type:      "AWSNeuron",
						},
					},
					{
						Device: &device.DeviceUsage{
							ID:        "negative-memory",
							Index:     1,
							Totalmem:  -1,
							Totalcore: 2,
							Type:      "test-device",
						},
					},
					{
						Device: &device.DeviceUsage{
							ID:        "normal-memory",
							Index:     2,
							Usedmem:   1,
							Totalmem:  4,
							Totalcore: 2,
							Type:      "NVIDIA",
						},
					},
				},
			},
		},
	}

	collector := ClusterManagerCollector{
		ClusterManager: &ClusterManager{
			LegacyMetrics: true,
		},
		metricsProvider: &fakeSchedulerMetricsProvider{
			nodeUsage:    nodeUsage,
			quotaManager: device.NewQuotaManager(),
			podManager:   device.NewPodManager(),
		},
	}

	want := `
# HELP hami_gpu_core_limit_ratio Device core limit for a certain GPU
# TYPE hami_gpu_core_limit_ratio gauge
hami_gpu_core_limit_ratio{device_index="0",device_type="AWSNeuron",device_uuid="zero-memory",node="node-1"} 2
hami_gpu_core_limit_ratio{device_index="1",device_type="test-device",device_uuid="negative-memory",node="node-1"} 2
hami_gpu_core_limit_ratio{device_index="2",device_type="NVIDIA",device_uuid="normal-memory",node="node-1"} 2
# HELP hami_node_gpu_memory_allocated_ratio GPU Memory Allocated Percentage on a certain GPU
# TYPE hami_node_gpu_memory_allocated_ratio gauge
hami_node_gpu_memory_allocated_ratio{device_index="2",device_uuid="normal-memory",node="node-1"} 0.25
# HELP nodeGPUMemoryPercentage GPU Memory Allocated Percentage on a certain GPU
# TYPE nodeGPUMemoryPercentage gauge
nodeGPUMemoryPercentage{deviceidx="2",deviceuuid="normal-memory",nodeid="node-1"} 0.25
`

	if err := promtestutil.CollectAndCompare(
		collector,
		strings.NewReader(want),
		"hami_gpu_core_limit_ratio",
		"hami_node_gpu_memory_allocated_ratio",
		"nodeGPUMemoryPercentage",
	); err != nil {
		t.Fatalf("unexpected collecting result:\n%s", err)
	}
}
