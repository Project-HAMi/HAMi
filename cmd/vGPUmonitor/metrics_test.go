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
	"context"
	"os"
	"testing"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/NVIDIA/go-nvml/pkg/nvml/mock"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	nv "github.com/Project-HAMi/HAMi/pkg/device/nvidia"
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

func TestCollectGPUInfo_NilNVML(t *testing.T) {
	c := &ClusterManager{
		Zone:            "test-zone",
		nvmllib:         nil,
		containerLister: &nvidia.ContainerLister{},
	}
	cc := ClusterManagerCollector{ClusterManager: c}
	ch := make(chan prometheus.Metric, 10)

	// Under nil nvmllib, physical GPU metrics collection must gracefully skip without panicking.
	if err := cc.collectGPUInfo(ch); err != nil {
		t.Errorf("collectGPUInfo with nil nvmllib returned unexpected error: %v", err)
	}
}

func newMockNVMLDevice(uuid, name string, memTotal, memUsed uint64, utilGPU, utilMem uint32) *mock.Device {
	return &mock.Device{
		GetMemoryInfoFunc: func() (nvml.Memory, nvml.Return) {
			return nvml.Memory{Total: memTotal, Used: memUsed, Free: memTotal - memUsed}, nvml.SUCCESS
		},
		GetUtilizationRatesFunc: func() (nvml.Utilization, nvml.Return) {
			return nvml.Utilization{Gpu: utilGPU, Memory: utilMem}, nvml.SUCCESS
		},
		GetUUIDFunc: func() (string, nvml.Return) {
			return uuid, nvml.SUCCESS
		},
		GetNameFunc: func() (string, nvml.Return) {
			return name, nvml.SUCCESS
		},
	}
}

func newMockNVML(devices ...nvml.Device) *mock.Interface {
	return &mock.Interface{
		InitFunc: func() nvml.Return {
			return nvml.SUCCESS
		},
		ShutdownFunc: func() nvml.Return {
			return nvml.SUCCESS
		},
		DeviceGetCountFunc: func() (int, nvml.Return) {
			return len(devices), nvml.SUCCESS
		},
		DeviceGetHandleByIndexFunc: func(index int) (nvml.Device, nvml.Return) {
			if index >= 0 && index < len(devices) {
				return devices[index], nvml.SUCCESS
			}
			return nil, nvml.ERROR_INVALID_ARGUMENT
		},
	}
}

func TestNewClusterManager(t *testing.T) {
	containerLister := &nvidia.ContainerLister{}
	reg := prometheus.NewRegistry()
	mockNVML := newMockNVML()

	cm := NewClusterManager("test-zone", reg, containerLister, mockNVML, false)
	if cm == nil {
		t.Fatal("NewClusterManager returned nil")
	}
	if cm.nvmllib != mockNVML {
		t.Errorf("NewClusterManager nvmllib mismatch")
	}

	regLegacy := prometheus.NewRegistry()
	cmLegacy := NewClusterManager("test-zone-legacy", regLegacy, containerLister, mockNVML, true)
	if cmLegacy == nil {
		t.Fatal("NewClusterManager legacy returned nil")
	}
}

func TestInitMetrics(t *testing.T) {
	origAddr := metricsBindAddress
	metricsBindAddress = "127.0.0.1:0"
	defer func() { metricsBindAddress = origAddr }()

	containerLister := &nvidia.ContainerLister{}
	mockNVML := newMockNVML()

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- initMetrics(ctx, containerLister, mockNVML)
	}()

	cancel()
	if err := <-errCh; err != nil {
		t.Errorf("initMetrics returned unexpected error: %v", err)
	}
}

func TestCollectGPUInfo_Success(t *testing.T) {
	dev := newMockNVMLDevice("GPU-12345", "Tesla T4", 16000000000, 4000000000, 75, 50)
	mockNVML := newMockNVML(dev)

	c := &ClusterManager{
		Zone:            "test-zone",
		nvmllib:         mockNVML,
		containerLister: &nvidia.ContainerLister{},
	}
	cc := ClusterManagerCollector{ClusterManager: c}
	ch := make(chan prometheus.Metric, 10)

	if err := cc.collectGPUInfo(ch); err != nil {
		t.Fatalf("collectGPUInfo returned unexpected error: %v", err)
	}
	close(ch)

	metricCount := 0
	for range ch {
		metricCount++
	}
	if metricCount != 4 {
		t.Errorf("Expected 4 metrics, got %d", metricCount)
	}
}

func TestCollectGPUInfo_ErrorPaths(t *testing.T) {
	ch := make(chan prometheus.Metric, 10)

	// 1. Init error
	mockInitErr := &mock.Interface{
		InitFunc: func() nvml.Return { return nvml.ERROR_UNKNOWN },
	}
	ccInitErr := ClusterManagerCollector{ClusterManager: &ClusterManager{nvmllib: mockInitErr}}
	if err := ccInitErr.collectGPUInfo(ch); err == nil {
		t.Error("Expected error when Init fails, got nil")
	}

	// 2. DeviceGetCount error
	mockCountErr := &mock.Interface{
		InitFunc:           func() nvml.Return { return nvml.SUCCESS },
		ShutdownFunc:       func() nvml.Return { return nvml.SUCCESS },
		DeviceGetCountFunc: func() (int, nvml.Return) { return 0, nvml.ERROR_UNKNOWN },
	}
	ccCountErr := ClusterManagerCollector{ClusterManager: &ClusterManager{nvmllib: mockCountErr}}
	if err := ccCountErr.collectGPUInfo(ch); err == nil {
		t.Error("Expected error when DeviceGetCount fails, got nil")
	}

	// 3. DeviceGetHandleByIndex error
	mockHandleErr := &mock.Interface{
		InitFunc:                   func() nvml.Return { return nvml.SUCCESS },
		ShutdownFunc:               func() nvml.Return { return nvml.SUCCESS },
		DeviceGetCountFunc:         func() (int, nvml.Return) { return 1, nvml.SUCCESS },
		DeviceGetHandleByIndexFunc: func(int) (nvml.Device, nvml.Return) { return nil, nvml.ERROR_UNKNOWN },
	}
	ccHandleErr := ClusterManagerCollector{ClusterManager: &ClusterManager{nvmllib: mockHandleErr}}
	if err := ccHandleErr.collectGPUInfo(ch); err != nil {
		t.Errorf("Unexpected error when DeviceGetHandleByIndex fails: %v", err)
	}

	// 4. GetMemoryInfo error
	devMemErr := &mock.Device{
		GetMemoryInfoFunc: func() (nvml.Memory, nvml.Return) { return nvml.Memory{}, nvml.ERROR_UNKNOWN },
	}
	mockMemErr := newMockNVML(devMemErr)
	ccMemErr := ClusterManagerCollector{ClusterManager: &ClusterManager{nvmllib: mockMemErr}}
	_ = ccMemErr.collectGPUInfo(ch)

	// 5. GetMemoryInfo NOT_SUPPORTED (Unified Memory)
	devMemNotSupp := &mock.Device{
		GetMemoryInfoFunc:       func() (nvml.Memory, nvml.Return) { return nvml.Memory{}, nvml.ERROR_NOT_SUPPORTED },
		GetUtilizationRatesFunc: func() (nvml.Utilization, nvml.Return) { return nvml.Utilization{Gpu: 50}, nvml.SUCCESS },
		GetUUIDFunc:             func() (string, nvml.Return) { return "GPU-unified", nvml.SUCCESS },
		GetNameFunc:             func() (string, nvml.Return) { return "GH200", nvml.SUCCESS },
	}
	mockMemNotSupp := newMockNVML(devMemNotSupp)
	ccMemNotSupp := ClusterManagerCollector{ClusterManager: &ClusterManager{nvmllib: mockMemNotSupp}}
	if err := ccMemNotSupp.collectGPUInfo(ch); err != nil {
		t.Errorf("Unexpected error when MemoryInfo is NOT_SUPPORTED: %v", err)
	}

	// 6. GetUUID error in memory metrics
	devUUIDMemErr := &mock.Device{
		GetMemoryInfoFunc: func() (nvml.Memory, nvml.Return) { return nvml.Memory{Used: 100}, nvml.SUCCESS },
		GetUUIDFunc:       func() (string, nvml.Return) { return "", nvml.ERROR_UNKNOWN },
	}
	mockUUIDMemErr := newMockNVML(devUUIDMemErr)
	ccUUIDMemErr := ClusterManagerCollector{ClusterManager: &ClusterManager{nvmllib: mockUUIDMemErr}}
	_ = ccUUIDMemErr.collectGPUInfo(ch)

	// 7. GetName error in memory metrics
	devNameMemErr := &mock.Device{
		GetMemoryInfoFunc: func() (nvml.Memory, nvml.Return) { return nvml.Memory{Used: 100}, nvml.SUCCESS },
		GetUUIDFunc:       func() (string, nvml.Return) { return "GPU-123", nvml.SUCCESS },
		GetNameFunc:       func() (string, nvml.Return) { return "", nvml.ERROR_UNKNOWN },
	}
	mockNameMemErr := newMockNVML(devNameMemErr)
	ccNameMemErr := ClusterManagerCollector{ClusterManager: &ClusterManager{nvmllib: mockNameMemErr}}
	_ = ccNameMemErr.collectGPUInfo(ch)

	// 8. GetUtilizationRates error
	devUtilErr := &mock.Device{
		GetMemoryInfoFunc:       func() (nvml.Memory, nvml.Return) { return nvml.Memory{Used: 100}, nvml.SUCCESS },
		GetUUIDFunc:             func() (string, nvml.Return) { return "GPU-123", nvml.SUCCESS },
		GetNameFunc:             func() (string, nvml.Return) { return "Tesla", nvml.SUCCESS },
		GetUtilizationRatesFunc: func() (nvml.Utilization, nvml.Return) { return nvml.Utilization{}, nvml.ERROR_UNKNOWN },
	}
	mockUtilErr := newMockNVML(devUtilErr)
	ccUtilErr := ClusterManagerCollector{ClusterManager: &ClusterManager{nvmllib: mockUtilErr}}
	_ = ccUtilErr.collectGPUInfo(ch)

	// 9. GetUUID error in utilization metrics
	uuidCallCount := 0
	devUUIDUtilErr := &mock.Device{
		GetMemoryInfoFunc:       func() (nvml.Memory, nvml.Return) { return nvml.Memory{Used: 100}, nvml.SUCCESS },
		GetUtilizationRatesFunc: func() (nvml.Utilization, nvml.Return) { return nvml.Utilization{Gpu: 50}, nvml.SUCCESS },
		GetUUIDFunc: func() (string, nvml.Return) {
			uuidCallCount++
			if uuidCallCount > 1 {
				return "", nvml.ERROR_UNKNOWN
			}
			return "GPU-123", nvml.SUCCESS
		},
		GetNameFunc: func() (string, nvml.Return) { return "Tesla", nvml.SUCCESS },
	}
	mockUUIDUtilErr := newMockNVML(devUUIDUtilErr)
	ccUUIDUtilErr := ClusterManagerCollector{ClusterManager: &ClusterManager{nvmllib: mockUUIDUtilErr}}
	_ = ccUUIDUtilErr.collectGPUInfo(ch)

	// 10. GetName error in utilization metrics
	nameCallCount := 0
	devNameUtilErr := &mock.Device{
		GetMemoryInfoFunc:       func() (nvml.Memory, nvml.Return) { return nvml.Memory{Used: 100}, nvml.SUCCESS },
		GetUtilizationRatesFunc: func() (nvml.Utilization, nvml.Return) { return nvml.Utilization{Gpu: 50}, nvml.SUCCESS },
		GetUUIDFunc:             func() (string, nvml.Return) { return "GPU-123", nvml.SUCCESS },
		GetNameFunc: func() (string, nvml.Return) {
			nameCallCount++
			if nameCallCount > 1 {
				return "", nvml.ERROR_UNKNOWN
			}
			return "Tesla", nvml.SUCCESS
		},
	}
	mockNameUtilErr := newMockNVML(devNameUtilErr)
	ccNameUtilErr := ClusterManagerCollector{ClusterManager: &ClusterManager{nvmllib: mockNameUtilErr}}
	_ = ccNameUtilErr.collectGPUInfo(ch)
}

func TestClusterManagerCollector_NilGuards(t *testing.T) {
	ch := make(chan prometheus.Metric, 10)

	// 1. cc with nil ClusterManager
	ccNil := ClusterManagerCollector{ClusterManager: nil}
	ccNil.Collect(ch)

	if err := ccNil.collectGPUInfo(ch); err != nil {
		t.Errorf("collectGPUInfo with nil ClusterManager returned error: %v", err)
	}
	if err := ccNil.collectPodAndContainerInfo(ch); err != nil {
		t.Errorf("collectPodAndContainerInfo with nil ClusterManager returned error: %v", err)
	}
	if err := ccNil.collectPodAndContainerMigInfo(ch); err != nil {
		t.Errorf("collectPodAndContainerMigInfo with nil ClusterManager returned error: %v", err)
	}

	// 2. cc with ClusterManager containing nil PodLister / containerLister
	cmPartial := &ClusterManager{
		Zone: "test-zone",
	}
	ccPartial := ClusterManagerCollector{ClusterManager: cmPartial}
	if err := ccPartial.collectPodAndContainerInfo(ch); err != nil {
		t.Errorf("collectPodAndContainerInfo with nil PodLister returned error: %v", err)
	}
	if err := ccPartial.collectPodAndContainerMigInfo(ch); err != nil {
		t.Errorf("collectPodAndContainerMigInfo with nil PodLister returned error: %v", err)
	}

	// 3. NodeName environment variable not set
	t.Setenv(util.NodeNameEnvName, "")
	podLister := informers.NewSharedInformerFactory(fake.NewSimpleClientset(), 0).Core().V1().Pods().Lister()
	cmNoNodeEnv := &ClusterManager{
		Zone:            "test-zone",
		PodLister:       podLister,
		containerLister: &nvidia.ContainerLister{},
	}
	ccNoNodeEnv := ClusterManagerCollector{ClusterManager: cmNoNodeEnv}
	if err := ccNoNodeEnv.collectPodAndContainerInfo(ch); err == nil {
		t.Error("collectPodAndContainerInfo expected error when nodeName env is unset, got nil")
	}
	if err := ccNoNodeEnv.collectPodAndContainerMigInfo(ch); err == nil {
		t.Error("collectPodAndContainerMigInfo expected error when nodeName env is unset, got nil")
	}
}

func TestCollectPodAndContainerInfo_WithPod(t *testing.T) {
	t.Setenv(util.NodeNameEnvName, "test-node")

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "trainer-0",
			Namespace: "team-a",
			UID:       "uid-12345",
			Annotations: map[string]string{
				util.AssignedNodeAnnotations: "test-node",
				nv.MigAllocationsAnnotation:  `[{"ContainerIndex":0,"DeviceIndex":0,"GPUUUID":"GPU-1","MigUUID":"MIG-1","Profile":"1g.5gb","GPUInstanceID":1,"ComputeInstanceID":1}]`,
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
			Containers: []corev1.Container{
				{Name: "worker"},
			},
		},
	}

	client := fake.NewSimpleClientset(pod)
	informerFactory := informers.NewSharedInformerFactory(client, 0)
	podInformer := informerFactory.Core().V1().Pods().Informer()
	podInformer.GetIndexer().Add(pod)
	podLister := informerFactory.Core().V1().Pods().Lister()

	c := &ClusterManager{
		Zone:            "test-zone",
		PodLister:       podLister,
		containerLister: &nvidia.ContainerLister{},
		LegacyMetrics:   true,
	}
	cc := ClusterManagerCollector{ClusterManager: c}
	ch := make(chan prometheus.Metric, 50)

	if err := cc.collectPodAndContainerInfo(ch); err != nil {
		t.Errorf("collectPodAndContainerInfo returned error: %v", err)
	}

	if err := cc.collectPodAndContainerMigInfo(ch); err != nil {
		t.Errorf("collectPodAndContainerMigInfo returned error: %v", err)
	}

	// Test MIG allocation error decoding path
	podBadMig := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "trainer-bad",
			Namespace: "team-a",
			UID:       "uid-67890",
			Annotations: map[string]string{
				util.AssignedNodeAnnotations: "test-node",
				nv.MigAllocationsAnnotation:  "invalid-json",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "worker"}},
		},
	}
	podInformer.GetIndexer().Add(podBadMig)
	_ = cc.collectPodAndContainerMigInfo(ch)
}

func TestValidateEnvVars(t *testing.T) {
	t.Setenv("HOOK_PATH", "")
	os.Unsetenv("HOOK_PATH")
	if err := ValidateEnvVars(); err == nil {
		t.Error("expected error when HOOK_PATH is unset, got nil")
	}

	t.Setenv("HOOK_PATH", "/tmp")
	if err := ValidateEnvVars(); err != nil {
		t.Errorf("unexpected error when HOOK_PATH is set: %v", err)
	}
}

func TestSendMetric_Errors(t *testing.T) {
	ch := make(chan prometheus.Metric, 10)

	err := sendMetric(ch, ctrDeviceMigInfo, prometheus.GaugeValue, 1, "extra", "labels", "that", "exceed", "desc", "count", "extra1", "extra2", "extra3", "extra4")
	if err == nil {
		t.Error("sendMetric expected error for wrong label count, got nil")
	}

	sendLegacyMetric(ch, legacyHostGPUdesc, prometheus.GaugeValue, 1, "extra", "labels", "that", "exceed", "desc", "count", "extra1", "extra2")
}
