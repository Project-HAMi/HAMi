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

func TestHostGPUMetricsDescriptorsIncludeNodeLabel(t *testing.T) {
	initLegacyDescriptors()

	hostGPUString := hostGPUdesc.String()
	if !strings.Contains(hostGPUString, `"node"`) && !strings.Contains(hostGPUString, `node`) {
		t.Errorf("hostGPUdesc does not contain 'node' label: %s", hostGPUString)
	}

	hostGPUUtilString := hostGPUUtilizationdesc.String()
	if !strings.Contains(hostGPUUtilString, `"node"`) && !strings.Contains(hostGPUUtilString, `node`) {
		t.Errorf("hostGPUUtilizationdesc does not contain 'node' label: %s", hostGPUUtilString)
	}

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

	err := cc.collectGPUUtilizationMetrics(nil, nil, 0)
	if err == nil || !strings.Contains(err.Error(), "node name environment variable") {
		t.Errorf("expected missing node name error from collectGPUUtilizationMetrics, got: %v", err)
	}

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
	}
}

func TestDescribeRegistersMemoryControllerUtilization(t *testing.T) {
	c := &ClusterManager{Zone: "test-zone", LegacyMetrics: false}
	cc := ClusterManagerCollector{ClusterManager: c}
	descCh := make(chan *prometheus.Desc, 32)
	cc.Describe(descCh)
	close(descCh)
	for d := range descCh {
		if strings.Contains(d.String(), "hami_host_gpu_memory_controller_utilization_ratio") {
			return
		}
	}
	t.Error("hami_host_gpu_memory_controller_utilization_ratio not found in Describe output")
}

func TestCollectMemoryControllerUtilizationValue(t *testing.T) {
	t.Setenv(util.NodeNameEnvName, "test-node")
	const wantMemory = uint32(73)
	mockDev := &nvmlmock.Device{
		GetUtilizationRatesFunc: func() (nvml.Utilization, nvml.Return) {
			return nvml.Utilization{Gpu: 10, Memory: wantMemory}, nvml.SUCCESS
		},
		GetUUIDFunc: func() (string, nvml.Return) {
			return "GPU-abc123", nvml.SUCCESS
		},
		GetNameFunc: func() (string, nvml.Return) {
			return "A100", nvml.SUCCESS
		},
	}
	ch := make(chan prometheus.Metric, 10)
	c := &ClusterManager{Zone: "test-zone", LegacyMetrics: false}
	cc := ClusterManagerCollector{ClusterManager: c}
	if err := cc.collectGPUUtilizationMetrics(ch, mockDev, 0); err != nil {
		t.Fatalf("collectGPUUtilizationMetrics: %v", err)
	}
	close(ch)
	var found bool
	for m := range ch {
		var dm dto.Metric
		if err := m.Write(&dm); err != nil {
			continue
		}
		if dm.Gauge == nil {
			continue
		}
		if *dm.Gauge.Value == float64(wantMemory) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected memory controller utilization metric with value %v", wantMemory)
	}
}

func TestHostGPUMetricsNotSupportedGracefullySkipped(t *testing.T) {
	t.Setenv(util.NodeNameEnvName, "test-node")

	mockDev := &nvmlmock.Device{
		GetMemoryInfoFunc: func() (nvml.Memory, nvml.Return) {
			return nvml.Memory{}, nvml.ERROR_NOT_SUPPORTED
		},
		GetUtilizationRatesFunc: func() (nvml.Utilization, nvml.Return) {
			return nvml.Utilization{}, nvml.ERROR_NOT_SUPPORTED
		},
	}

	cc := ClusterManagerCollector{}
	ch := make(chan prometheus.Metric, 10)

	if err := cc.collectGPUMemoryMetrics(ch, mockDev, 0); err != nil {
		t.Errorf("expected collectGPUMemoryMetrics to return nil on ERROR_NOT_SUPPORTED, got: %v", err)
	}

	if err := cc.collectGPUUtilizationMetrics(ch, mockDev, 0); err != nil {
		t.Errorf("expected collectGPUUtilizationMetrics to return nil on ERROR_NOT_SUPPORTED, got: %v", err)
	}

	close(ch)
	if count := len(ch); count != 0 {
		t.Errorf("expected 0 metrics emitted for unsupported device, got: %d", count)
	}
}
