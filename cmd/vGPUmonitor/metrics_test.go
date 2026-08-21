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

	hostGPUMemoryUtilString := hostGPUMemoryUtilizationdesc.String()
	if !strings.Contains(hostGPUMemoryUtilString, `"node"`) && !strings.Contains(hostGPUMemoryUtilString, `node`) {
		t.Errorf("hostGPUMemoryUtilizationdesc does not contain 'node' label: %s", hostGPUMemoryUtilString)
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

// testGPUIdentity builds a gpuDeviceIdentity for tests that call one of the
// per-metric collectors directly, bypassing resolveGPUDeviceIdentity (whose
// own behavior - including the missing-node-name and NVML GetUUID/GetName
// failure paths - is covered separately by TestResolveGPUDeviceIdentity*
// below, now that identity resolution is centralized there instead of being
// repeated in every collector).
func testGPUIdentity(node, uuid, name string) gpuDeviceIdentity {
	return gpuDeviceIdentity{nodeName: node, uuid: uuid, deviceName: name}
}

func TestResolveGPUDeviceIdentityMissingNodeName(t *testing.T) {
	t.Setenv(util.NodeNameEnvName, "")

	cc := ClusterManagerCollector{}
	mockDev := &nvmlmock.Device{
		GetUUIDFunc: func() (string, nvml.Return) {
			return "GPU-1234", nvml.SUCCESS
		},
		GetNameFunc: func() (string, nvml.Return) {
			return "Tesla T4", nvml.SUCCESS
		},
	}

	if _, err := cc.resolveGPUDeviceIdentity(mockDev); err == nil || !strings.Contains(err.Error(), "node name environment variable") {
		t.Errorf("expected missing node name error from resolveGPUDeviceIdentity, got: %v", err)
	}
}

func TestResolveGPUDeviceIdentityNVMLErrors(t *testing.T) {
	t.Setenv(util.NodeNameEnvName, "test-node")
	cc := ClusterManagerCollector{}

	uuidErrDev := &nvmlmock.Device{
		GetUUIDFunc: func() (string, nvml.Return) { return "", nvml.ERROR_UNKNOWN },
	}
	if _, err := cc.resolveGPUDeviceIdentity(uuidErrDev); err == nil || !strings.Contains(err.Error(), "GetUUID") {
		t.Errorf("expected a GetUUID error from resolveGPUDeviceIdentity, got: %v", err)
	}

	nameErrDev := &nvmlmock.Device{
		GetUUIDFunc: func() (string, nvml.Return) { return "GPU-1234", nvml.SUCCESS },
		GetNameFunc: func() (string, nvml.Return) { return "", nvml.ERROR_UNKNOWN },
	}
	if _, err := cc.resolveGPUDeviceIdentity(nameErrDev); err == nil || !strings.Contains(err.Error(), "GetName") {
		t.Errorf("expected a GetName error from resolveGPUDeviceIdentity, got: %v", err)
	}
}

func TestResolveGPUDeviceIdentitySuccess(t *testing.T) {
	t.Setenv(util.NodeNameEnvName, "test-node")
	cc := ClusterManagerCollector{}
	mockDev := &nvmlmock.Device{
		GetUUIDFunc: func() (string, nvml.Return) { return "GPU-1234", nvml.SUCCESS },
		GetNameFunc: func() (string, nvml.Return) { return "Tesla T4", nvml.SUCCESS },
	}

	identity, err := cc.resolveGPUDeviceIdentity(mockDev)
	if err != nil {
		t.Fatalf("resolveGPUDeviceIdentity failed: %v", err)
	}
	if identity.nodeName != "test-node" || identity.uuid != "GPU-1234" || identity.deviceName != "NVIDIA-Tesla T4" {
		t.Errorf("unexpected identity: %+v", identity)
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
		GetTemperatureFunc: func(nvml.TemperatureSensors) (uint32, nvml.Return) {
			return 45, nvml.SUCCESS
		},
		GetPowerUsageFunc: func() (uint32, nvml.Return) {
			return 150000, nvml.SUCCESS
		},
		GetTotalEccErrorsFunc: func(nvml.MemoryErrorType, nvml.EccCounterType) (uint64, nvml.Return) {
			return 5, nvml.SUCCESS
		},
	}

	initLegacyDescriptors()
	cc := ClusterManagerCollector{
		ClusterManager: &ClusterManager{LegacyMetrics: true},
	}
	identity := testGPUIdentity("test-node", "GPU-12345678-1234-1234-1234-123456789012", "NVIDIA-Tesla T4")

	ch := make(chan prometheus.Metric, 10)

	if err := cc.collectGPUMemoryMetrics(ch, mockDev, 0, identity); err != nil {
		t.Fatalf("collectGPUMemoryMetrics failed: %v", err)
	}

	if err := cc.collectGPUUtilizationMetrics(ch, mockDev, 0, identity); err != nil {
		t.Fatalf("collectGPUUtilizationMetrics failed: %v", err)
	}

	if err := cc.collectGPUTemperatureMetrics(ch, mockDev, 0, identity); err != nil {
		t.Fatalf("collectGPUTemperatureMetrics failed: %v", err)
	}

	if err := cc.collectGPUPowerMetrics(ch, mockDev, 0, identity); err != nil {
		t.Fatalf("collectGPUPowerMetrics failed: %v", err)
	}

	if err := cc.collectGPUEccErrorMetrics(ch, mockDev, 0, identity); err != nil {
		t.Fatalf("collectGPUEccErrorMetrics failed: %v", err)
	}

	close(ch)

	count := 0
	for range ch {
		count++
	}
	if count < 8 {
		t.Errorf("expected at least 8 metrics, got %d", count)
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
	identity := testGPUIdentity("test-node", "GPU-abc123", "NVIDIA-A100")
	if err := cc.collectGPUUtilizationMetrics(ch, mockDev, 0, identity); err != nil {
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
	identity := testGPUIdentity("test-node", "GPU-1234", "NVIDIA-Tesla T4")

	if err := cc.collectGPUMemoryMetrics(ch, mockDev, 0, identity); err != nil {
		t.Errorf("expected collectGPUMemoryMetrics to return nil on ERROR_NOT_SUPPORTED, got: %v", err)
	}

	if err := cc.collectGPUUtilizationMetrics(ch, mockDev, 0, identity); err != nil {
		t.Errorf("expected collectGPUUtilizationMetrics to return nil on ERROR_NOT_SUPPORTED, got: %v", err)
	}

	close(ch)
	if count := len(ch); count != 0 {
		t.Errorf("expected 0 metrics emitted for unsupported device, got: %d", count)
	}
}

func TestHostGPUMetricsUnsupported(t *testing.T) {
	t.Setenv(util.NodeNameEnvName, "test-node")

	mockDev := &nvmlmock.Device{
		GetUUIDFunc: func() (string, nvml.Return) { return "GPU-1234", nvml.SUCCESS },
		GetNameFunc: func() (string, nvml.Return) { return "Tesla T4", nvml.SUCCESS },
		GetTemperatureFunc: func(nvml.TemperatureSensors) (uint32, nvml.Return) {
			return 0, nvml.ERROR_NOT_SUPPORTED
		},
		GetPowerUsageFunc: func() (uint32, nvml.Return) {
			return 0, nvml.ERROR_NOT_SUPPORTED
		},
		GetTotalEccErrorsFunc: func(nvml.MemoryErrorType, nvml.EccCounterType) (uint64, nvml.Return) {
			return 0, nvml.ERROR_NOT_SUPPORTED
		},
	}

	cc := ClusterManagerCollector{}
	ch := make(chan prometheus.Metric, 10)
	identity := testGPUIdentity("test-node", "GPU-1234", "NVIDIA-Tesla T4")

	if err := cc.collectGPUTemperatureMetrics(ch, mockDev, 0, identity); err != nil {
		t.Fatalf("expected nil error for unsupported temperature, got: %v", err)
	}
	if err := cc.collectGPUPowerMetrics(ch, mockDev, 0, identity); err != nil {
		t.Fatalf("expected nil error for unsupported power, got: %v", err)
	}
	if err := cc.collectGPUEccErrorMetrics(ch, mockDev, 0, identity); err != nil {
		t.Fatalf("expected nil error for unsupported ECC, got: %v", err)
	}

	close(ch)
	for range ch {
		t.Fatalf("expected no metrics emitted for unsupported hardware")
	}
}

func TestHostGPUMetricsError(t *testing.T) {
	t.Setenv(util.NodeNameEnvName, "test-node")

	mockDev := &nvmlmock.Device{
		GetUUIDFunc: func() (string, nvml.Return) { return "GPU-1234", nvml.SUCCESS },
		GetNameFunc: func() (string, nvml.Return) { return "Tesla T4", nvml.SUCCESS },
		GetTemperatureFunc: func(nvml.TemperatureSensors) (uint32, nvml.Return) {
			return 0, nvml.ERROR_UNKNOWN
		},
		GetPowerUsageFunc: func() (uint32, nvml.Return) {
			return 0, nvml.ERROR_UNKNOWN
		},
		GetTotalEccErrorsFunc: func(nvml.MemoryErrorType, nvml.EccCounterType) (uint64, nvml.Return) {
			return 0, nvml.ERROR_UNKNOWN
		},
	}

	cc := ClusterManagerCollector{}
	identity := testGPUIdentity("test-node", "GPU-1234", "NVIDIA-Tesla T4")

	if err := cc.collectGPUTemperatureMetrics(nil, mockDev, 0, identity); err == nil {
		t.Fatalf("expected error for temperature, got nil")
	}
	if err := cc.collectGPUPowerMetrics(nil, mockDev, 0, identity); err == nil {
		t.Fatalf("expected error for power, got nil")
	}

	// A failing temperature/power collector above must not prevent ECC from
	// still being collected independently - see the review discussion on
	// PR #2732 (collectGPUDeviceMetrics used to return on the first error,
	// which skipped every collector after it for that device).
	ch := make(chan prometheus.Metric, 10)
	if err := cc.collectGPUEccErrorMetrics(ch, mockDev, 0, identity); err != nil {
		t.Fatalf("expected nil error for ECC even when unknown error occurs (it continues), got: %v", err)
	}
	close(ch)
	for range ch {
		t.Fatalf("expected no metrics emitted for errored hardware")
	}
}
