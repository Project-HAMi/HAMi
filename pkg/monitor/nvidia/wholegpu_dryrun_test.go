/*
Copyright 2025 The HAMi Authors.

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

package nvidia

import (
	"context"
	"strings"
	"testing"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	mock "github.com/NVIDIA/go-nvml/pkg/nvml/mock"
	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/Project-HAMi/HAMi/pkg/device"
	nv "github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

func TestRunWholeGPUDryRun(t *testing.T) {
	const nodeName = "node-a"
	const gpuUUID = "GPU-aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			Annotations: map[string]string{nv.RegisterAnnos: device.MarshalNodeDevices([]*device.DeviceInfo{{
				ID: gpuUUID, Devmem: 8000, Devcore: 100,
			}})},
		},
	}
	wholePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "workloads", Name: "whole", UID: types.UID("whole-uid"),
			Annotations: wholeGPUAnnotation(device.ContainerDevices{{UUID: gpuUUID, Type: nv.NvidiaGPUDevice, Usedmem: 8000, Usedcores: 100}}),
		},
		Spec: corev1.PodSpec{NodeName: nodeName, Containers: []corev1.Container{{Name: "main"}}},
	}
	otherNodePod := wholePod.DeepCopy()
	otherNodePod.Name = "other-node"
	otherNodePod.Spec.NodeName = "node-b"

	t.Run("reports metrics, filters other nodes, and shuts NVML down", func(t *testing.T) {
		client := fake.NewClientset(node, wholePod, otherNodePod)
		initialized, shutdown := 0, 0
		nvmllib := &mock.Interface{
			InitFunc:     func() nvml.Return { initialized++; return nvml.SUCCESS },
			ShutdownFunc: func() nvml.Return { shutdown++; return nvml.SUCCESS },
			DeviceGetHandleByUUIDFunc: func(uuid string) (nvml.Device, nvml.Return) {
				assert.Equal(t, uuid, gpuUUID)
				return &mock.Device{
					GetMemoryInfoFunc: func() (nvml.Memory, nvml.Return) {
						return nvml.Memory{Used: 4096, Total: 8000 * 1024 * 1024}, nvml.SUCCESS
					},
					GetUtilizationRatesFunc: func() (nvml.Utilization, nvml.Return) { return nvml.Utilization{Gpu: 42}, nvml.SUCCESS },
				}, nvml.SUCCESS
			},
		}

		report, err := RunWholeGPUDryRun(context.Background(), client, nvmllib, nodeName, WholeGPUDryRunOptions{})
		assert.NilError(t, err)
		assert.Equal(t, report.ScannedPods, 1)
		assert.Equal(t, report.CandidateContainers, 1)
		assert.Equal(t, report.ConfirmedContainers, 1)
		assert.Equal(t, len(report.Containers), 1)
		assert.Equal(t, report.Containers[0].Namespace, "workloads")
		assert.Equal(t, report.Containers[0].Devices[0].MemoryUsed, uint64(4096))
		assert.Equal(t, report.Containers[0].Devices[0].MemoryTotal, uint64(8000*1024*1024))
		assert.Equal(t, report.Containers[0].Devices[0].SMUtil, uint32(42))
		assert.Equal(t, initialized, 1)
		assert.Equal(t, shutdown, 1)
		assertWholeGPUDryRunReadActions(t, client.Actions())
	})

	t.Run("applies namespace pod and container filters", func(t *testing.T) {
		filteredPod := wholePod.DeepCopy()
		filteredPod.Spec.Containers = append(filteredPod.Spec.Containers, corev1.Container{Name: "sidecar"})
		filteredPod.Annotations = wholeGPUAnnotation(
			device.ContainerDevices{{UUID: gpuUUID, Type: nv.NvidiaGPUDevice, Usedmem: 8000, Usedcores: 100}},
			device.ContainerDevices{{UUID: gpuUUID, Type: nv.NvidiaGPUDevice, Usedmem: 8000, Usedcores: 100}},
		)
		client := fake.NewClientset(node, filteredPod)
		nvmllib := successfulWholeGPUNVML(gpuUUID)
		report, err := RunWholeGPUDryRun(context.Background(), client, nvmllib, nodeName, WholeGPUDryRunOptions{Namespace: "workloads", Pod: "whole", Container: "sidecar"})
		assert.NilError(t, err)
		assert.Equal(t, report.ScannedPods, 1)
		assert.Equal(t, report.CandidateContainers, 1)
		assert.Equal(t, report.Containers[0].Container, "sidecar")
	})

	t.Run("maps init and application container allocation indexes", func(t *testing.T) {
		pod := wholePod.DeepCopy()
		pod.Spec.InitContainers = []corev1.Container{{Name: "prepare"}}
		pod.Spec.Containers = []corev1.Container{{Name: "main"}}
		pod.Annotations = wholeGPUAnnotation(
			device.ContainerDevices{{UUID: gpuUUID, Type: nv.NvidiaGPUDevice, Usedmem: 8000, Usedcores: 100}},
			device.ContainerDevices{{UUID: gpuUUID, Type: nv.NvidiaGPUDevice, Usedmem: 8000, Usedcores: 100}},
		)
		report, err := RunWholeGPUDryRun(context.Background(), fake.NewClientset(node, pod), successfulWholeGPUNVML(gpuUUID), nodeName, WholeGPUDryRunOptions{})
		assert.NilError(t, err)
		assert.Equal(t, report.ConfirmedContainers, 2)
		assert.Equal(t, report.Containers[0].Container, "main")
		assert.Equal(t, report.Containers[1].Container, "prepare")
	})

	t.Run("reports non-whole and missing registered devices without NVML", func(t *testing.T) {
		partialPod := wholePod.DeepCopy()
		partialPod.Name = "partial"
		partialPod.Annotations = wholeGPUAnnotation(device.ContainerDevices{{UUID: gpuUUID, Type: nv.NvidiaGPUDevice, Usedmem: 4000}})
		unknownPod := wholePod.DeepCopy()
		unknownPod.Name = "unknown"
		unknownPod.Annotations = wholeGPUAnnotation(device.ContainerDevices{{UUID: "GPU-missing", Type: nv.NvidiaGPUDevice, Usedmem: 8000, Usedcores: 100}})
		client := fake.NewClientset(node, partialPod, unknownPod)
		initialized := 0
		nvmllib := &mock.Interface{InitFunc: func() nvml.Return { initialized++; return nvml.SUCCESS }}
		report, err := RunWholeGPUDryRun(context.Background(), client, nvmllib, nodeName, WholeGPUDryRunOptions{})
		assert.NilError(t, err)
		assert.Equal(t, report.ConfirmedContainers, 0)
		assert.Equal(t, initialized, 0)
		assert.Equal(t, len(report.Diagnostics), 2)
		assert.Equal(t, report.Diagnostics[0].Status, "not-whole-gpu")
		assert.Equal(t, report.Diagnostics[1].Status, "unregistered-device")
	})

	t.Run("an unreadable device handle keeps the candidate unconfirmed", func(t *testing.T) {
		client := fake.NewClientset(node, wholePod)
		initialized, shutdown := 0, 0
		nvmllib := &mock.Interface{
			InitFunc:     func() nvml.Return { initialized++; return nvml.SUCCESS },
			ShutdownFunc: func() nvml.Return { shutdown++; return nvml.SUCCESS },
			DeviceGetHandleByUUIDFunc: func(string) (nvml.Device, nvml.Return) {
				return nil, nvml.ERROR_NOT_SUPPORTED
			},
		}
		report, err := RunWholeGPUDryRun(context.Background(), client, nvmllib, nodeName, WholeGPUDryRunOptions{})
		assert.NilError(t, err)
		assert.Equal(t, report.ConfirmedContainers, 0)
		assert.Equal(t, len(report.Diagnostics), 1)
		assert.Equal(t, report.Diagnostics[0].Status, "nvml-unreadable")
		// The registry-clean candidate still brought NVML up for its
		// physical check, so the library must also be shut down.
		assert.Equal(t, initialized, 1)
		assert.Equal(t, shutdown, 1)
	})

	t.Run("returns a system error when NVML initialization fails", func(t *testing.T) {
		client := fake.NewClientset(node, wholePod)
		_, err := RunWholeGPUDryRun(context.Background(), client, &mock.Interface{InitFunc: func() nvml.Return { return nvml.ERROR_LIBRARY_NOT_FOUND }}, nodeName, WholeGPUDryRunOptions{})
		assert.ErrorContains(t, err, "NVML initialization failed")
	})

	t.Run("returns a system error for an invalid node registry annotation", func(t *testing.T) {
		badNode := node.DeepCopy()
		badNode.Annotations[nv.RegisterAnnos] = "not-json"
		client := fake.NewClientset(badNode, wholePod)
		_, err := RunWholeGPUDryRun(context.Background(), client, successfulWholeGPUNVML(gpuUUID), nodeName, WholeGPUDryRunOptions{})
		assert.ErrorContains(t, err, "failed to unmarshal node")
	})
}

func TestRunWholeGPUDryRunAnnotationError(t *testing.T) {
	const nodeName = "node-a"
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "workloads", Name: "bad", Annotations: map[string]string{nv.AllocatedDevicesAnnotation: "not,a,valid"}},
		Spec:       corev1.PodSpec{NodeName: nodeName, Containers: []corev1.Container{{Name: "main"}}},
	}
	client := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}, pod)
	report, err := RunWholeGPUDryRun(context.Background(), client, successfulWholeGPUNVML("unused"), nodeName, WholeGPUDryRunOptions{})
	assert.NilError(t, err)
	assert.Equal(t, len(report.Diagnostics), 1)
	assert.Equal(t, report.Diagnostics[0].Status, "annotation-error")
}

func assertWholeGPUDryRunReadActions(t *testing.T, actions []k8stesting.Action) {
	t.Helper()
	assert.Equal(t, len(actions), 2)
	assert.Equal(t, actions[0].GetVerb(), "get")
	assert.Equal(t, actions[0].GetResource().Resource, "nodes")
	assert.Equal(t, actions[1].GetVerb(), "list")
	assert.Equal(t, actions[1].GetResource().Resource, "pods")
}

func successfulWholeGPUNVML(uuid string) nvml.Interface {
	return &mock.Interface{
		InitFunc:     func() nvml.Return { return nvml.SUCCESS },
		ShutdownFunc: func() nvml.Return { return nvml.SUCCESS },
		DeviceGetHandleByUUIDFunc: func(got string) (nvml.Device, nvml.Return) {
			if got != uuid {
				return nil, nvml.ERROR_NOT_FOUND
			}
			return &mock.Device{
				GetMemoryInfoFunc: func() (nvml.Memory, nvml.Return) {
					return nvml.Memory{Total: 8000 * 1024 * 1024}, nvml.SUCCESS
				},
				GetUtilizationRatesFunc: func() (nvml.Utilization, nvml.Return) { return nvml.Utilization{}, nvml.SUCCESS },
			}, nvml.SUCCESS
		},
	}
}

func Test_sampleWholeGPUDevice(t *testing.T) {
	const uuid = "GPU-1"
	healthyMemory := func() (nvml.Memory, nvml.Return) { return nvml.Memory{Used: 1024, Total: 8000}, nvml.SUCCESS }
	healthyRates := func() (nvml.Utilization, nvml.Return) { return nvml.Utilization{Gpu: 7}, nvml.SUCCESS }
	libFor := func(dev nvml.Device, ret nvml.Return) *mock.Interface {
		return &mock.Interface{DeviceGetHandleByUUIDFunc: func(string) (nvml.Device, nvml.Return) { return dev, ret }}
	}

	t.Run("handle lookup failure is reported", func(t *testing.T) {
		report := &WholeGPUDeviceReport{UUID: uuid}
		sampleWholeGPUDevice(libFor(nil, nvml.ERROR_UNKNOWN), report)
		assert.Assert(t, strings.Contains(report.Error, "DeviceGetHandleByUUID failed"), report.Error)
	})

	t.Run("memory query failure is reported without samples", func(t *testing.T) {
		dev := &mock.Device{
			GetMemoryInfoFunc:       func() (nvml.Memory, nvml.Return) { return nvml.Memory{}, nvml.ERROR_UNKNOWN },
			GetUtilizationRatesFunc: healthyRates,
		}
		report := &WholeGPUDeviceReport{UUID: uuid}
		sampleWholeGPUDevice(libFor(dev, nvml.SUCCESS), report)
		assert.Assert(t, strings.Contains(report.Error, "GetMemoryInfo failed"), report.Error)
		assert.Equal(t, report.MemoryUsed, uint64(0))
	})

	t.Run("utilization failure keeps the memory samples", func(t *testing.T) {
		dev := &mock.Device{
			GetMemoryInfoFunc: healthyMemory,
			GetUtilizationRatesFunc: func() (nvml.Utilization, nvml.Return) {
				return nvml.Utilization{}, nvml.ERROR_UNKNOWN
			},
		}
		report := &WholeGPUDeviceReport{UUID: uuid}
		sampleWholeGPUDevice(libFor(dev, nvml.SUCCESS), report)
		assert.Assert(t, strings.Contains(report.Error, "GetUtilizationRates failed"), report.Error)
		assert.Equal(t, report.MemoryUsed, uint64(1024))
		assert.Equal(t, report.SMUtil, uint32(0))
	})
}

func Test_sortWholeGPUReport(t *testing.T) {
	report := &WholeGPUDryRunReport{
		Containers: []WholeGPUContainerReport{
			{Namespace: "b", Pod: "p1", Container: "c1"},
			{Namespace: "a", Pod: "p2", Container: "c1"},
			{Namespace: "a", Pod: "p1", Container: "c2"},
			{Namespace: "a", Pod: "p1", Container: "c1"},
		},
		Diagnostics: []WholeGPUDiagnostic{
			{Namespace: "b", Pod: "p1", Status: "not-whole-gpu"},
			{Namespace: "a", Pod: "p1", Container: "x", Status: "nvml-unreadable"},
			{Namespace: "a", Pod: "p1", Container: "x", Status: "not-whole-gpu"},
			{Namespace: "a", Pod: "p1", Status: "unregistered-device"},
		},
	}
	sortWholeGPUReport(report)

	containers := make([]string, 0, len(report.Containers))
	for _, c := range report.Containers {
		containers = append(containers, c.Namespace+"/"+c.Pod+"/"+c.Container)
	}
	assert.DeepEqual(t, containers, []string{"a/p1/c1", "a/p1/c2", "a/p2/c1", "b/p1/c1"})

	diagnostics := make([]string, 0, len(report.Diagnostics))
	for _, d := range report.Diagnostics {
		diagnostics = append(diagnostics, d.Namespace+"/"+d.Pod+"/"+d.Container+"/"+d.Status)
	}
	assert.DeepEqual(t, diagnostics, []string{
		"a/p1//unregistered-device",
		"a/p1/x/not-whole-gpu",
		"a/p1/x/nvml-unreadable",
		"b/p1//not-whole-gpu",
	})
}

func Test_wholeGPUVerdictDiagnostic_messages(t *testing.T) {
	mkCandidate := func(devs device.ContainerDevices) wholeGPUCandidate {
		return wholeGPUCandidate{
			pod:       &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "p"}},
			container: "c",
			devices:   devs,
		}
	}
	nodeDevs := map[string]*device.DeviceInfo{
		"GPU-mig":  {ID: "GPU-mig", Devmem: 8000, Devcore: 100, Mode: nv.MigMode},
		"GPU-full": {ID: "GPU-full", Devmem: 8000, Devcore: 100},
	}

	t.Run("mig device", func(t *testing.T) {
		got := wholeGPUVerdictDiagnostic(mkCandidate(device.ContainerDevices{{UUID: "GPU-mig", Usedmem: 8000}}), notWholeGPU, causeNotIndeterminate, nodeDevs)
		assert.Equal(t, got.Status, "not-whole-gpu")
		assert.Assert(t, strings.Contains(got.Message, "registered as MIG"), got.Message)
	})

	t.Run("short memory shows both figures", func(t *testing.T) {
		got := wholeGPUVerdictDiagnostic(mkCandidate(device.ContainerDevices{{UUID: "GPU-full", Usedmem: 4000}}), notWholeGPU, causeNotIndeterminate, nodeDevs)
		assert.Assert(t, strings.Contains(got.Message, "below registered memory"), got.Message)
	})

	t.Run("generic not-whole fallback", func(t *testing.T) {
		got := wholeGPUVerdictDiagnostic(mkCandidate(device.ContainerDevices{{UUID: "GPU-full", Usedmem: 8000}}), notWholeGPU, causeNotIndeterminate, nodeDevs)
		assert.Equal(t, got.Message, "allocation does not represent one or more whole GPUs")
	})

	t.Run("phys unreadable", func(t *testing.T) {
		got := wholeGPUVerdictDiagnostic(mkCandidate(device.ContainerDevices{{UUID: "GPU-full", Usedmem: 8000}}), indeterminate, causePhysUnreadable, nodeDevs)
		assert.Equal(t, got.Status, "nvml-unreadable")
	})
}

func Test_getWholeGPUNodeDevices_missingNode(t *testing.T) {
	_, err := getWholeGPUNodeDevices(context.Background(), fake.NewClientset(), "node-a")
	assert.ErrorContains(t, err, "failed to get node")
}
