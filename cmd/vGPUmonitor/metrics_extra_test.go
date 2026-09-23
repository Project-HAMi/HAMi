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

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	nvmlmock "github.com/NVIDIA/go-nvml/pkg/nvml/mock"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Project-HAMi/HAMi/pkg/monitor/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

// mockUsageInfo implements nvidia.UsageInfo for testing purposes.
type mockUsageInfo struct{}

func (m *mockUsageInfo) DeviceMax() int                       { return 1 }
func (m *mockUsageInfo) DeviceNum() int                       { return 1 }
func (m *mockUsageInfo) DeviceMemoryContextSize(_ int) uint64 { return 100 }
func (m *mockUsageInfo) DeviceMemoryModuleSize(_ int) uint64  { return 200 }
func (m *mockUsageInfo) DeviceMemoryBufferSize(_ int) uint64  { return 300 }
func (m *mockUsageInfo) DeviceMemoryOffset(_ int) uint64      { return 0 }
func (m *mockUsageInfo) DeviceMemoryTotal(_ int) uint64       { return 1024 }
func (m *mockUsageInfo) DeviceSmUtil(_ int) uint64            { return 50 }
func (m *mockUsageInfo) SetDeviceSmLimit(_ uint64)            {}
func (m *mockUsageInfo) IsValidUUID(_ int) bool               { return true }
func (m *mockUsageInfo) DeviceUUID(_ int) string {
	// Must be at least 40 chars; collectContainerMetrics slices [0:40]
	return "GPU-12345678-1234-1234-1234-123456789012"
}
func (m *mockUsageInfo) DeviceMemoryLimit(_ int) uint64 { return 2048 }
func (m *mockUsageInfo) SetDeviceMemoryLimit(_ uint64)  {}
func (m *mockUsageInfo) LastKernelTime() int64          { return 0 }
func (m *mockUsageInfo) GetPriority() int               { return 0 }
func (m *mockUsageInfo) GetRecentKernel() int32         { return 0 }
func (m *mockUsageInfo) SetRecentKernel(_ int32)        {}
func (m *mockUsageInfo) GetUtilizationSwitch() int32    { return 0 }
func (m *mockUsageInfo) SetUtilizationSwitch(_ int32)   {}

func TestCollectPodAndContainerInfo_InitContainers(t *testing.T) {
	nodeName := "test-node"
	t.Setenv(util.NodeNameEnvName, nodeName)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       "test-uid",
			Annotations: map[string]string{
				util.AssignedNodeAnnotations: nodeName,
			},
			Labels: map[string]string{
				util.AssignedNodeAnnotations: nodeName,
			},
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{
				{Name: "init-c1"},
			},
			Containers: []corev1.Container{
				{Name: "c1"},
			},
		},
	}

	client := fake.NewSimpleClientset(pod)
	informerFactory := informers.NewSharedInformerFactory(client, 0)
	podInformer := informerFactory.Core().V1().Pods()
	if err := podInformer.Informer().GetIndexer().Add(pod); err != nil {
		t.Fatalf("failed to add pod to indexer: %v", err)
	}

	mockInfo := &mockUsageInfo{}
	lister := &nvidia.ContainerLister{}
	// Inject both init-container and regular container with valid Info
	lister.SetContainersForTest(map[string]*nvidia.ContainerUsage{
		"init-c1": {
			PodUID:        "test-uid",
			ContainerName: "init-c1",
			Info:          mockInfo,
		},
		"c1": {
			PodUID:        "test-uid",
			ContainerName: "c1",
			Info:          mockInfo,
		},
	})

	c := &ClusterManager{
		Zone:            "test-zone",
		LegacyMetrics:   false,
		PodLister:       podInformer.Lister(),
		containerLister: lister,
	}
	cc := ClusterManagerCollector{ClusterManager: c}

	ch := make(chan prometheus.Metric, 100)
	if err := cc.collectPodAndContainerInfo(ch); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	close(ch)

	// Drain the channel and assert that at least one metric is labeled "init-c1"
	foundInitC1 := false
	for m := range ch {
		pb := &dto.Metric{}
		if err := m.Write(pb); err != nil {
			continue
		}
		for _, lp := range pb.GetLabel() {
			if lp.GetValue() == "init-c1" {
				foundInitC1 = true
				break
			}
		}
	}
	if !foundInitC1 {
		t.Errorf("expected at least one metric labeled init-c1 for the init container, but none found")
	}
}

func TestHostGPUMetrics_SendMetricError(t *testing.T) {
	nodeName := "test-node"
	t.Setenv(util.NodeNameEnvName, nodeName)

	origHostGPUDesc := hostGPUdesc
	origHostGPUUtilDesc := hostGPUUtilizationdesc
	defer func() {
		hostGPUdesc = origHostGPUDesc
		hostGPUUtilizationdesc = origHostGPUUtilDesc
	}()

	// Inject descriptors with wrong label count to trigger sendMetric error path
	badDesc := prometheus.NewDesc("bad_desc", "bad desc", []string{"l1", "l2", "l3", "l4", "l5", "l6", "l7", "l8"}, nil)
	hostGPUdesc = badDesc
	hostGPUUtilizationdesc = badDesc

	mockDev := &nvmlmock.Device{
		GetMemoryInfoFunc: func() (nvml.Memory, nvml.Return) {
			return nvml.Memory{Used: 1024}, nvml.SUCCESS
		},
		GetUtilizationRatesFunc: func() (nvml.Utilization, nvml.Return) {
			return nvml.Utilization{Gpu: 50, Memory: 20}, nvml.SUCCESS
		},
		GetUUIDFunc: func() (string, nvml.Return) {
			return "GPU-123", nvml.SUCCESS
		},
		GetNameFunc: func() (string, nvml.Return) {
			return "Tesla", nvml.SUCCESS
		},
	}

	cc := ClusterManagerCollector{}
	ch := make(chan prometheus.Metric, 10)
	identity := testGPUIdentity(nodeName, "GPU-123", "NVIDIA-Tesla")

	if err := cc.collectGPUMemoryMetrics(ch, mockDev, 0, identity); err != nil {
		t.Errorf("expected no error from collectGPUMemoryMetrics even if descriptor is bad")
	}

	if err := cc.collectGPUUtilizationMetrics(ch, mockDev, 0, identity); err != nil {
		t.Errorf("expected no error from collectGPUUtilizationMetrics even if descriptor is bad")
	}
}
