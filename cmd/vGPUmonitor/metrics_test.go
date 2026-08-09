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
	reg := prometheus.NewPedanticRegistry()

	// Set NODE_NAME env var
	nodeName := "test-node-123"
	t.Setenv(util.NodeNameEnvName, nodeName)

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
		t.Fatalf("Failed to register: %v", err)
	}

	metrics, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	// Track which expected host GPU metrics were found
	metricsFound := make(map[string]bool)
	expectedMetrics := []string{
		"hami_host_gpu_memory_used_bytes",
		"hami_host_gpu_utilization_ratio",
		"hami_host_gpu_memory_total_bytes",
	}

	// Verify host metrics have 4 labels: node, device_index, device_uuid, device_type
	for _, mf := range metrics {
		metricName := mf.GetName()
		
		// Track if we found each expected metric
		for _, expected := range expectedMetrics {
			if metricName == expected {
				metricsFound[expected] = true
			}
		}

		if metricName == "hami_host_gpu_memory_used_bytes" ||
			metricName == "hami_host_gpu_utilization_ratio" ||
			metricName == "hami_host_gpu_memory_total_bytes" {
			for _, m := range mf.GetMetric() {
				labels := m.GetLabel()
				if len(labels) != 4 {
					t.Errorf("%s has %d labels, expected 4", metricName, len(labels))
				}

				// Verify node label exists and has correct value
				hasNode := false
				for _, label := range labels {
					if label.GetName() == "node" {
						hasNode = true
						if label.GetValue() != nodeName {
							t.Errorf("node label = %s, want %s", label.GetValue(), nodeName)
						}
					}
				}
				if !hasNode {
					t.Errorf("%s missing 'node' label", metricName)
				}
			}
		}
	}

	// Verify all expected metrics were found (skip if no metrics gathered)
	if len(metricsFound) == 0 {
		t.Logf("No host GPU metrics gathered - this is expected without GPU hardware")
		return
	}
	for _, expected := range expectedMetrics {
		if !metricsFound[expected] {
			t.Errorf("Expected metric %s not found in gathered metrics", expected)
		}
	}
}
