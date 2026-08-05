/*
Copyright 2024 The HAMi Authors.

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

	"github.com/prometheus/client_golang/prometheus"
	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	"github.com/Project-HAMi/HAMi/pkg/device"
	schedulerpkg "github.com/Project-HAMi/HAMi/pkg/scheduler"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
)

type fakeMetricsProvider struct {
	nodeUsage    map[string]*schedulerpkg.NodeUsage
	quotaManager *device.QuotaManager
	podManager   *device.PodManager
}

func (f *fakeMetricsProvider) InspectAllNodesUsage() *map[string]*schedulerpkg.NodeUsage {
	return &f.nodeUsage
}

func (f *fakeMetricsProvider) GetQuotaManager() *device.QuotaManager {
	return f.quotaManager
}

func (f *fakeMetricsProvider) GetPodManager() *device.PodManager {
	return f.podManager
}

func TestSchedulerDescribeCollectSync(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()

	c := &ClusterManager{
		Zone:          "test-zone",
		LegacyMetrics: false,
	}

	nodeUsage := map[string]*schedulerpkg.NodeUsage{
		"node-1": {
			Devices: policy.DeviceUsageList{
				DeviceLists: []*policy.DeviceListsScore{
					{
						Device: &device.DeviceUsage{
							ID:        "GPU-abc-123",
							Index:     0,
							Used:      2,
							Count:     4,
							Usedmem:   4096,
							Totalmem:  8192,
							Totalcore: 100,
							Usedcores: 50,
							Type:      "NVIDIA",
							Mode:      "hami-core",
						},
					},
				},
			},
		},
	}

	qm := device.NewQuotaManager()
	pm := device.NewPodManager()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       k8stypes.UID("uid-123"),
		},
	}
	podDevices := device.PodDevices{
		"NVIDIA": device.PodSingleDevice{
			device.ContainerDevices{
				{
					UUID:      "GPU-abc-123",
					Type:      "NVIDIA",
					Usedmem:   2048,
					Usedcores: 25,
				},
			},
		},
	}
	pm.AddPod(pod, "node-1", podDevices)

	cc := ClusterManagerCollector{
		ClusterManager: c,
		metricsProvider: &fakeMetricsProvider{
			nodeUsage:    nodeUsage,
			quotaManager: qm,
			podManager:   pm,
		},
	}

	if err := reg.Register(cc); err != nil {
		t.Fatalf("Failed to register ClusterManagerCollector (non-legacy): %v", err)
	}

	if _, err := reg.Gather(); err != nil {
		t.Errorf("Gather failed (non-legacy): %v", err)
	}

	regLegacy := prometheus.NewPedanticRegistry()
	cLegacy := &ClusterManager{
		Zone:          "test-zone-legacy",
		LegacyMetrics: true,
	}
	ccLegacy := ClusterManagerCollector{
		ClusterManager: cLegacy,
		metricsProvider: &fakeMetricsProvider{
			nodeUsage:    nodeUsage,
			quotaManager: qm,
			podManager:   pm,
		},
	}

	if err := regLegacy.Register(ccLegacy); err != nil {
		t.Fatalf("Failed to register ClusterManagerCollector (legacy): %v", err)
	}
	if _, err := regLegacy.Gather(); err != nil {
		t.Errorf("Gather failed (legacy): %v", err)
	}
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
		metricsProvider: &fakeMetricsProvider{
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

func TestClusterManagerCollectorExposesDynamicMigRuntimeIdentity(t *testing.T) {
	nodeUsage := map[string]*schedulerpkg.NodeUsage{
		"node-1": {
			Devices: policy.DeviceUsageList{DeviceLists: []*policy.DeviceListsScore{{
				Device: &device.DeviceUsage{
					ID: "GPU-parent", Index: 0, Mode: "mig", Type: "NVIDIA",
					MigAllocationsInUse: []device.MigAllocation{{
						Profile: "2g.10gb", Placement: device.MigPlacement{Start: 0, Size: 2},
						MigUUID: "MIG-runtime", GPUInstanceID: 4, ComputeInstanceID: 0, RuntimeReady: true,
					}},
				},
			}}},
		},
	}
	collector := ClusterManagerCollector{
		ClusterManager: &ClusterManager{},
		metricsProvider: &fakeMetricsProvider{
			nodeUsage: nodeUsage, quotaManager: device.NewQuotaManager(), podManager: device.NewPodManager(),
		},
	}

	want := `
# HELP hami_node_gpu_mig_instance_info Realized MIG instance identity and scheduler placement
# TYPE hami_node_gpu_mig_instance_info gauge
hami_node_gpu_mig_instance_info{compute_instance_id="0",device_index="0",device_uuid="GPU-parent",gpu_instance_id="4",mig_uuid="MIG-runtime",node="node-1",placement_size="2",placement_start="0",profile="2g.10gb"} 1
`
	if err := promtestutil.CollectAndCompare(collector, strings.NewReader(want), "hami_node_gpu_mig_instance_info"); err != nil {
		t.Fatalf("unexpected collecting result:\n%s", err)
	}
}
