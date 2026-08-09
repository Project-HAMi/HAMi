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
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Project-HAMi/HAMi/pkg/monitor/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

func TestDescribeCollectSync(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()

	t.Setenv(util.NodeNameEnvName, "test-node")
	client := fake.NewSimpleClientset()
	informerFactory := informers.NewSharedInformerFactory(client, 0)
	podLister := informerFactory.Core().V1().Pods().Lister()

	c := &ClusterManager{
		Zone:            "test-zone",
		LegacyMetrics:   false,
		PodLister:       podLister,
		containerLister: &nvidia.ContainerLister{},
	}
	cc := ClusterManagerCollector{ClusterManager: c}

	if err := reg.Register(cc); err != nil {
		t.Fatalf("Failed to register ClusterManagerCollector (non-legacy): %v", err)
	}

	if _, err := reg.Gather(); err != nil {
		t.Errorf("Gather failed (non-legacy): %v", err)
	}

	regLegacy := prometheus.NewPedanticRegistry()
	cLegacy := &ClusterManager{
		Zone:            "test-zone-legacy",
		LegacyMetrics:   true,
		PodLister:       podLister,
		containerLister: &nvidia.ContainerLister{},
	}
	initLegacyDescriptors()
	ccLegacy := ClusterManagerCollector{ClusterManager: cLegacy}

	if err := regLegacy.Register(ccLegacy); err != nil {
		t.Fatalf("Failed to register ClusterManagerCollector (legacy): %v", err)
	}
	if _, err := regLegacy.Gather(); err != nil {
		t.Errorf("Gather failed (legacy): %v", err)
	}
}

func TestHostMetricsIncludeNodeLabel(t *testing.T) {
	cases := []struct {
		name          string
		legacyMetrics bool
		setNodeName   bool
		wantNodeName  string
	}{
		{name: "non-legacy/env-set", legacyMetrics: false, setNodeName: true, wantNodeName: "test-node-123"},
		{name: "non-legacy/env-unset", legacyMetrics: false, setNodeName: false, wantNodeName: "unknown"},
		{name: "legacy/env-set", legacyMetrics: true, setNodeName: true, wantNodeName: "test-node-123"},
		{name: "legacy/env-unset", legacyMetrics: true, setNodeName: false, wantNodeName: "unknown"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg := prometheus.NewPedanticRegistry()

			if tc.setNodeName {
				t.Setenv(util.NodeNameEnvName, tc.wantNodeName)
			} else {
				// Empty value is equivalent to "unset" for the os.Getenv("") == ""
				// fallback check in collectGPUInfo.
				t.Setenv(util.NodeNameEnvName, "")
			}

			client := fake.NewSimpleClientset()
			informerFactory := informers.NewSharedInformerFactory(client, 0)
			podLister := informerFactory.Core().V1().Pods().Lister()

			if tc.legacyMetrics {
				initLegacyDescriptors()
			}

			c := &ClusterManager{
				Zone:            "test-zone",
				LegacyMetrics:   tc.legacyMetrics,
				PodLister:       podLister,
				containerLister: &nvidia.ContainerLister{},
			}
			cc := ClusterManagerCollector{ClusterManager: c}

			if err := reg.Register(cc); err != nil {
				t.Fatalf("Failed to register: %v", err)
			}

			metrics, err := reg.Gather()
			if err != nil {
				t.Fatalf("Gather failed: %v", err)
			}

			// Metrics may not be present if NVML initialization fails (no GPU
			// hardware); that's expected in test environments, so absence
			// doesn't fail the test, but any metric that IS present must
			// carry the correct node_name label.
			assertHostMetricNodeLabel(t, metrics, "hami_host_gpu_memory_used_bytes", tc.wantNodeName)
			assertHostMetricNodeLabel(t, metrics, "hami_host_gpu_utilization_ratio", tc.wantNodeName)
		})
	}
}

func assertHostMetricNodeLabel(t *testing.T, metrics []*dto.MetricFamily, metricName, wantNodeName string) {
	t.Helper()

	for _, mf := range metrics {
		if mf.GetName() != metricName {
			continue
		}
		for _, m := range mf.GetMetric() {
			labels := m.GetLabel()
			if len(labels) != 4 {
				t.Errorf("%s has %d labels, expected 4", metricName, len(labels))
			}

			hasNode := false
			for _, label := range labels {
				if label.GetName() == "node_name" {
					hasNode = true
					if label.GetValue() != wantNodeName {
						t.Errorf("node_name label = %s, want %s", label.GetValue(), wantNodeName)
					}
				}
			}
			if !hasNode {
				t.Errorf("%s missing 'node_name' label", metricName)
			}
		}
		t.Logf("%s found and validated", metricName)
	}
}
