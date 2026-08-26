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

func TestMibToBytes(t *testing.T) {
	tests := []struct {
		name string
		mib  int32
		want float64
	}{
		{name: "zero", mib: 0, want: 0},
		{name: "one mebibyte", mib: 1, want: 1024 * 1024},
		{name: "typical device memory", mib: 16384, want: 16384 * 1024 * 1024},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := mibToBytes(tc.mib); got != tc.want {
				t.Errorf("mibToBytes(%d) = %v, want %v", tc.mib, got, tc.want)
			}
		})
	}
}

func TestFindNodeDeviceUsage(t *testing.T) {
	nodeUsage := map[string]*schedulerpkg.NodeUsage{
		"node-1": {
			Devices: policy.DeviceUsageList{
				DeviceLists: []*policy.DeviceListsScore{
					{Device: &device.DeviceUsage{ID: "AMD-1", Totalcore: 64, Type: "AMDGPU"}},
					{Device: &device.DeviceUsage{ID: "NVIDIA-1", Totalcore: 100, Type: "NVIDIA"}},
				},
			},
		},
		"node-2": {
			Devices: policy.DeviceUsageList{
				DeviceLists: []*policy.DeviceListsScore{
					{Device: &device.DeviceUsage{ID: "AMD-2", Totalcore: 304, Type: "AMDGPU"}},
				},
			},
		},
	}

	tests := []struct {
		name           string
		uuid           string
		wantTotalcore  int32
		wantDeviceType string
		wantOk         bool
	}{
		{name: "found on first node", uuid: "AMD-1", wantTotalcore: 64, wantDeviceType: "AMDGPU", wantOk: true},
		{name: "found on second node", uuid: "AMD-2", wantTotalcore: 304, wantDeviceType: "AMDGPU", wantOk: true},
		{name: "non-AMD device", uuid: "NVIDIA-1", wantTotalcore: 100, wantDeviceType: "NVIDIA", wantOk: true},
		{name: "unknown device returns not ok", uuid: "missing", wantTotalcore: 0, wantDeviceType: "", wantOk: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotTotalcore, gotDeviceType, gotOk := findNodeDeviceUsage(&nodeUsage, tc.uuid)
			if gotTotalcore != tc.wantTotalcore || gotDeviceType != tc.wantDeviceType || gotOk != tc.wantOk {
				t.Errorf("findNodeDeviceUsage(%q) = (%d, %q, %v), want (%d, %q, %v)",
					tc.uuid, gotTotalcore, gotDeviceType, gotOk, tc.wantTotalcore, tc.wantDeviceType, tc.wantOk)
			}
		})
	}
}

func TestAMDCoreAllocatedRatioNormalization(t *testing.T) {
	// Regression test for issue #2518: AMD stores physical compute-unit (CU)
	// counts in Usedcores, so a 50% request on a 64-CU device is stored as 32 CUs.
	// Both the container-level hami_vgpu_core_allocated_ratio and the node-level
	// hami_gpu_core_allocated_ratio must be normalized to 50, while legacy metrics
	// keep the raw CU count (32) for backward compatibility.
	nodeUsage := map[string]*schedulerpkg.NodeUsage{
		"node-1": {
			Devices: policy.DeviceUsageList{
				DeviceLists: []*policy.DeviceListsScore{
					{
						Device: &device.DeviceUsage{
							ID:        "AMD-1",
							Index:     0,
							Count:     1,
							Totalmem:  192000,
							Totalcore: 64,
							Usedcores: 32,
							Type:      "AMDGPU",
							Mode:      "hami-core",
						},
					},
				},
			},
		},
	}

	pm := device.NewPodManager()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "amd-pod",
			Namespace: "default",
			UID:       k8stypes.UID("uid-amd"),
		},
	}
	// A 50% core request on a 64-CU device is stored as 32 physical CUs.
	podDevices := device.PodDevices{
		"AMDGPU": device.PodSingleDevice{
			device.ContainerDevices{
				{
					UUID:      "AMD-1",
					Type:      "AMDGPU",
					Usedmem:   96000,
					Usedcores: 32,
				},
			},
		},
	}
	pm.AddPod(pod, "node-1", podDevices)

	newCollector := ClusterManagerCollector{
		ClusterManager: &ClusterManager{LegacyMetrics: false},
		metricsProvider: &fakeMetricsProvider{
			nodeUsage:    nodeUsage,
			quotaManager: device.NewQuotaManager(),
			podManager:   pm,
		},
	}

	// Non-legacy metrics: both node- and container-level core allocation ratios
	// are normalized to a percentage for AMD devices.
	newWant := `
# HELP hami_gpu_core_allocated_ratio Device core allocated for a certain GPU
# TYPE hami_gpu_core_allocated_ratio gauge
hami_gpu_core_allocated_ratio{device_index="0",device_type="AMDGPU",device_uuid="AMD-1",node="node-1"} 50
# HELP hami_vgpu_core_allocated_ratio vGPU core allocated from a container
# TYPE hami_vgpu_core_allocated_ratio gauge
hami_vgpu_core_allocated_ratio{container_index="0",device_uuid="AMD-1",namespace="default",node="node-1",pod="amd-pod"} 50
`
	if err := promtestutil.CollectAndCompare(
		newCollector,
		strings.NewReader(newWant),
		"hami_gpu_core_allocated_ratio",
		"hami_vgpu_core_allocated_ratio",
	); err != nil {
		t.Fatalf("unexpected non-legacy collecting result:\n%s", err)
	}

	// Legacy metrics keep the raw CU count for backward compatibility.
	legacyCollector := ClusterManagerCollector{
		ClusterManager: &ClusterManager{LegacyMetrics: true},
		metricsProvider: &fakeMetricsProvider{
			nodeUsage:    nodeUsage,
			quotaManager: device.NewQuotaManager(),
			podManager:   pm,
		},
	}
	legacyWant := `
# HELP vGPUCoreAllocated vGPU core allocated from a container
# TYPE vGPUCoreAllocated gauge
vGPUCoreAllocated{containeridx="0",deviceuuid="AMD-1",nodename="node-1",podname="amd-pod",podnamespace="default"} 32
`
	if err := promtestutil.CollectAndCompare(
		legacyCollector,
		strings.NewReader(legacyWant),
		"vGPUCoreAllocated",
	); err != nil {
		t.Fatalf("unexpected legacy collecting result:\n%s", err)
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
# HELP hami_node_gpu_memory_allocated_ratio GPU memory allocated ratio on a certain GPU (0-1)
# TYPE hami_node_gpu_memory_allocated_ratio gauge
hami_node_gpu_memory_allocated_ratio{device_index="2",device_type="NVIDIA",device_uuid="normal-memory",node="node-1"} 0.25
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

func TestClusterManagerCollectorQuotaMetrics(t *testing.T) {
	// Regression coverage for collectQuotaMetrics: verify per-namespace quota
	// usage is exported as hami_resource_quota_used, and as legacy QuotaUsed
	// when LegacyMetrics is enabled.
	const (
		ns       = "team-a"
		memName  = "nvidia.com/gpumem"
		coreName = "nvidia.com/gpucore"
	)

	qm := device.NewQuotaManager()
	qm.Quotas[ns] = &device.DeviceQuota{
		memName:  &device.Quota{Used: 4096, Limit: 8192, LimitSet: true},
		coreName: &device.Quota{Used: 50, Limit: 100, LimitSet: true},
	}
	t.Cleanup(func() { delete(qm.Quotas, ns) })

	provider := &fakeMetricsProvider{
		nodeUsage:    map[string]*schedulerpkg.NodeUsage{},
		quotaManager: qm,
		podManager:   device.NewPodManager(),
	}

	t.Run("non-legacy", func(t *testing.T) {
		collector := ClusterManagerCollector{
			ClusterManager:  &ClusterManager{LegacyMetrics: false},
			metricsProvider: provider,
		}
		want := `
# HELP hami_resource_quota_limit resourcequota limit for a certain device
# TYPE hami_resource_quota_limit gauge
hami_resource_quota_limit{namespace="team-a",quota_name="nvidia.com/gpucore"} 100
hami_resource_quota_limit{namespace="team-a",quota_name="nvidia.com/gpumem"} 8192
# HELP hami_resource_quota_used resourcequota usage for a certain device
# TYPE hami_resource_quota_used gauge
hami_resource_quota_used{limit="100",namespace="team-a",quota_name="nvidia.com/gpucore"} 50
hami_resource_quota_used{limit="8192",namespace="team-a",quota_name="nvidia.com/gpumem"} 4096
`
		if err := promtestutil.CollectAndCompare(
			collector,
			strings.NewReader(want),
			"hami_resource_quota_used",
			"hami_resource_quota_limit",
		); err != nil {
			t.Fatalf("unexpected non-legacy collecting result:\n%s", err)
		}
	})

	t.Run("legacy", func(t *testing.T) {
		collector := ClusterManagerCollector{
			ClusterManager:  &ClusterManager{LegacyMetrics: true},
			metricsProvider: provider,
		}
		want := `
# HELP QuotaUsed resourcequota usage for a certain device
# TYPE QuotaUsed gauge
QuotaUsed{limit="100",quotaName="nvidia.com/gpucore",quotanamespace="team-a"} 50
QuotaUsed{limit="8192",quotaName="nvidia.com/gpumem",quotanamespace="team-a"} 4096
# HELP hami_resource_quota_limit resourcequota limit for a certain device
# TYPE hami_resource_quota_limit gauge
hami_resource_quota_limit{namespace="team-a",quota_name="nvidia.com/gpucore"} 100
hami_resource_quota_limit{namespace="team-a",quota_name="nvidia.com/gpumem"} 8192
# HELP hami_resource_quota_used resourcequota usage for a certain device
# TYPE hami_resource_quota_used gauge
hami_resource_quota_used{limit="100",namespace="team-a",quota_name="nvidia.com/gpucore"} 50
hami_resource_quota_used{limit="8192",namespace="team-a",quota_name="nvidia.com/gpumem"} 4096
`
		if err := promtestutil.CollectAndCompare(
			collector,
			strings.NewReader(want),
			"hami_resource_quota_used",
			"hami_resource_quota_limit",
			"QuotaUsed",
		); err != nil {
			t.Fatalf("unexpected legacy collecting result:\n%s", err)
		}
	})
}

func TestClusterManagerCollectorQuotaUnconfiguredLimit(t *testing.T) {
	const (
		ns      = "team-b"
		memName = "nvidia.com/gpumem"
	)

	unconfQm := device.NewQuotaManager()
	unconfQm.Quotas[ns] = &device.DeviceQuota{
		memName: &device.Quota{Used: 1024, Limit: 0, LimitSet: false},
	}
	t.Cleanup(func() { delete(unconfQm.Quotas, ns) })

	unconfProvider := &fakeMetricsProvider{
		nodeUsage:    map[string]*schedulerpkg.NodeUsage{},
		quotaManager: unconfQm,
		podManager:   device.NewPodManager(),
	}
	collector := ClusterManagerCollector{
		ClusterManager:  &ClusterManager{LegacyMetrics: false},
		metricsProvider: unconfProvider,
	}
	want := `
# HELP hami_resource_quota_used resourcequota usage for a certain device
# TYPE hami_resource_quota_used gauge
hami_resource_quota_used{limit="0",namespace="team-b",quota_name="nvidia.com/gpumem"} 1024
`
	if err := promtestutil.CollectAndCompare(
		collector,
		strings.NewReader(want),
		"hami_resource_quota_used",
		"hami_resource_quota_limit",
	); err != nil {
		t.Fatalf("unexpected unconfigured limit collecting result:\n%s", err)
	}
}
