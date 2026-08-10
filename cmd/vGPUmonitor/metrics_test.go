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

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	nvmlmock "github.com/NVIDIA/go-nvml/pkg/nvml/mock"
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

<<<<<<< HEAD
func TestHostGPUMetricsDescriptorsIncludeNodeLabel(t *testing.T) {
	initLegacyDescriptors()

	// Verify standard host GPU descriptors include "node" label
	hostGPUString := hostGPUdesc.String()
	if !strings.Contains(hostGPUString, `"node"`) && !strings.Contains(hostGPUString, `node`) {
		t.Errorf("hostGPUdesc does not contain 'node' label: %s", hostGPUString)
	}

	hostGPUUtilString := hostGPUUtilizationdesc.String()
	if !strings.Contains(hostGPUUtilString, `"node"`) && !strings.Contains(hostGPUUtilString, `node`) {
		t.Errorf("hostGPUUtilizationdesc does not contain 'node' label: %s", hostGPUUtilString)
	}

	// Verify legacy host GPU descriptors include "nodeid" label
	legacyHostGPUString := legacyHostGPUdesc.String()
	if !strings.Contains(legacyHostGPUString, `"nodeid"`) && !strings.Contains(legacyHostGPUString, `nodeid`) {
		t.Errorf("legacyHostGPUdesc does not contain 'nodeid' label: %s", legacyHostGPUString)
	}

	legacyHostGPUUtilString := legacyHostGPUUtilizationdesc.String()
	if !strings.Contains(legacyHostGPUUtilString, `"nodeid"`) && !strings.Contains(legacyHostGPUUtilString, `nodeid`) {
		t.Errorf("legacyHostGPUUtilizationdesc does not contain 'nodeid' label: %s", legacyHostGPUUtilString)
	}
}

func TestHostGPUMetricsMissingNodeName(t *testing.T) {
	t.Setenv(util.NodeNameEnvName, "")

	cc := ClusterManagerCollector{}

	// Test collectGPUUtilizationMetrics with missing NODE_NAME
	err := cc.collectGPUUtilizationMetrics(nil, nil, 0)
	if err == nil || !strings.Contains(err.Error(), "node name environment variable") {
		t.Errorf("expected missing node name error from collectGPUUtilizationMetrics, got: %v", err)
	}

	// Test collectGPUMemoryMetrics with missing NODE_NAME
	mockDev := &nvmlmock.Device{
		GetMemoryInfoFunc: func() (nvml.Memory, nvml.Return) {
			return nvml.Memory{Used: 100}, nvml.SUCCESS
		},
		GetUUIDFunc: func() (string, nvml.Return) {
			return "GPU-1234", nvml.SUCCESS
		},
		GetNameFunc: func() (string, nvml.Return) {
			return "Tesla T4", nvml.SUCCESS
		},
	}
	err = cc.collectGPUMemoryMetrics(nil, mockDev, 0)
	if err == nil || !strings.Contains(err.Error(), "node name environment variable") {
		t.Errorf("expected missing node name error from collectGPUMemoryMetrics, got: %v", err)
	}
}

func TestHostGPUMetricsCollectionSuccess(t *testing.T) {
	t.Setenv(util.NodeNameEnvName, "test-node")

	mockDev := &nvmlmock.Device{
		GetMemoryInfoFunc: func() (nvml.Memory, nvml.Return) {
			return nvml.Memory{Used: 1024}, nvml.SUCCESS
		},
		GetUtilizationRatesFunc: func() (nvml.Utilization, nvml.Return) {
			return nvml.Utilization{Gpu: 50, Memory: 20}, nvml.SUCCESS
		},
		GetUUIDFunc: func() (string, nvml.Return) {
			return "GPU-12345678-1234-1234-1234-123456789012", nvml.SUCCESS
		},
		GetNameFunc: func() (string, nvml.Return) {
			return "Tesla T4", nvml.SUCCESS
		},
	}

	initLegacyDescriptors()
	cc := ClusterManagerCollector{
		ClusterManager: &ClusterManager{LegacyMetrics: true},
	}

	ch := make(chan prometheus.Metric, 10)

	if err := cc.collectGPUMemoryMetrics(ch, mockDev, 0); err != nil {
		t.Fatalf("collectGPUMemoryMetrics failed: %v", err)
	}

	if err := cc.collectGPUUtilizationMetrics(ch, mockDev, 0); err != nil {
		t.Fatalf("collectGPUUtilizationMetrics failed: %v", err)
	}

	close(ch)

	count := 0
	for range ch {
		count++
	}
	if count < 4 {
		t.Errorf("expected at least 4 metrics, got %d", count)
=======
func TestDescribeRegistersMemoryControllerUtilization(t *testing.T) {
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

	descCh := make(chan *prometheus.Desc, 32)
	cc.Describe(descCh)
	close(descCh)

	found := false
	for desc := range descCh {
		if desc == hostGPUMemoryUtilizationdesc {
			found = true
			break
		}
	}
	if !found {
		t.Error("Describe did not emit hostGPUMemoryUtilizationdesc; hami_host_gpu_memory_controller_utilization_ratio will be missing from scrape output")
>>>>>>> a861173 (feat: add hami_host_gpu_memory_controller_utilization_ratio metric)
	}
}
