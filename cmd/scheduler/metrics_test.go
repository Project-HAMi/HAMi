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

func TestNormalizeAMDCoreMetrics(t *testing.T) {
	tests := []struct {
		name          string
		deviceType    string
		total         int32
		allocated     int32
		wantTotal     float64
		wantAllocated float64
	}{
		{
			name:          "non-AMD device is passed through unchanged",
			deviceType:    "NVIDIA",
			total:         4,
			allocated:     3,
			wantTotal:     4,
			wantAllocated: 3,
		},
		{
			name:          "AMD device with non-positive total is passed through unchanged",
			deviceType:    "AMD",
			total:         0,
			allocated:     0,
			wantTotal:     0,
			wantAllocated: 0,
		},
		{
			name:          "AMD device is normalized to a 0-100 percentage",
			deviceType:    "AMD",
			total:         64,
			allocated:     32,
			wantTotal:     normalizedCoreLimit,
			wantAllocated: 50,
		},
		{
			name:          "AMD device type matching is case-insensitive and normalization rounds up",
			deviceType:    "amd-instinct",
			total:         3,
			allocated:     1,
			wantTotal:     normalizedCoreLimit,
			wantAllocated: 34,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTotal, gotAllocated := normalizeAMDCoreMetrics(tt.deviceType, tt.total, tt.allocated)
			if gotTotal != tt.wantTotal || gotAllocated != tt.wantAllocated {
				t.Errorf("normalizeAMDCoreMetrics(%q, %d, %d) = (%v, %v), want (%v, %v)",
					tt.deviceType, tt.total, tt.allocated, gotTotal, gotAllocated, tt.wantTotal, tt.wantAllocated)
			}
		})
	}
}

func TestSendMetric(t *testing.T) {
	desc := prometheus.NewDesc("hami_test_metric", "test metric", []string{"label"}, nil)
	ch := make(chan prometheus.Metric, 1)

	if err := sendMetric(ch, desc, prometheus.GaugeValue, 1, "value"); err != nil {
		t.Fatalf("sendMetric returned unexpected error: %v", err)
	}
	select {
	case <-ch:
	default:
		t.Fatal("expected a metric to be sent on the channel")
	}

	// Supplying the wrong number of label values makes NewConstMetric fail,
	// and sendMetric must surface that error instead of sending on the channel.
	if err := sendMetric(ch, desc, prometheus.GaugeValue, 1); err == nil {
		t.Fatal("expected sendMetric to return an error for mismatched labels")
	}
	select {
	case <-ch:
		t.Fatal("did not expect a metric to be sent on error")
	default:
	}
}

func TestSendLegacyMetric(t *testing.T) {
	ch := make(chan prometheus.Metric, 1)

	// A nil descriptor means the legacy metric is disabled; sendLegacyMetric
	// must no-op rather than panic or send a metric.
	sendLegacyMetric(ch, nil, prometheus.GaugeValue, 1, "value")
	select {
	case <-ch:
		t.Fatal("did not expect a metric to be sent for a nil descriptor")
	default:
	}

	desc := prometheus.NewDesc("hami_test_legacy_metric", "test legacy metric", []string{"label"}, nil)

	// sendLegacyMetric logs and swallows errors from sendMetric rather than panicking.
	sendLegacyMetric(ch, desc, prometheus.GaugeValue, 1)
	select {
	case <-ch:
		t.Fatal("did not expect a metric to be sent when sendMetric errors")
	default:
	}

	sendLegacyMetric(ch, desc, prometheus.GaugeValue, 1, "value")
	select {
	case <-ch:
	default:
		t.Fatal("expected a metric to be sent on the channel")
	}
}
