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

	"github.com/Project-HAMi/HAMi/pkg/device"
	schedulerpkg "github.com/Project-HAMi/HAMi/pkg/scheduler"
)

// fakeMetricsProvider satisfies the schedulerMetricsProvider interface
// with empty data for testing.
type fakeMetricsProvider struct{}

func (f *fakeMetricsProvider) InspectAllNodesUsage() *map[string]*schedulerpkg.NodeUsage {
	m := make(map[string]*schedulerpkg.NodeUsage)
	return &m
}

func (f *fakeMetricsProvider) GetQuotaManager() *device.QuotaManager {
	qm := device.NewQuotaManager()
	return qm
}

func (f *fakeMetricsProvider) GetPodManager() *device.PodManager {
	pm := device.NewPodManager()
	return pm
}

func TestSchedulerDescribeCollectSync(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()

	c := &ClusterManager{
		Zone:          "test-zone",
		LegacyMetrics: false,
	}
	cc := ClusterManagerCollector{
		ClusterManager:  c,
		metricsProvider: &fakeMetricsProvider{},
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
		ClusterManager:  cLegacy,
		metricsProvider: &fakeMetricsProvider{},
	}

	if err := regLegacy.Register(ccLegacy); err != nil {
		t.Fatalf("Failed to register ClusterManagerCollector (legacy): %v", err)
	}
	if _, err := regLegacy.Gather(); err != nil {
		t.Errorf("Gather failed (legacy): %v", err)
	}
}
