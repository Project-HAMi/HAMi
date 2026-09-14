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
				ID: gpuUUID, Devmem: 8000,
			}})},
		},
	}
	wholePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "workloads", Name: "whole", UID: types.UID("whole-uid"),
			Annotations: wholeGPUAnnotation(device.ContainerDevices{{UUID: gpuUUID, Type: nv.NvidiaGPUDevice, Usedmem: 8000}}),
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
					GetMemoryInfoFunc:       func() (nvml.Memory, nvml.Return) { return nvml.Memory{Used: 4096, Total: 8192}, nvml.SUCCESS },
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
		assert.Equal(t, report.Containers[0].Devices[0].MemoryTotal, uint64(8192))
		assert.Equal(t, report.Containers[0].Devices[0].SMUtil, uint32(42))
		assert.Equal(t, initialized, 1)
		assert.Equal(t, shutdown, 1)
		assertWholeGPUDryRunReadActions(t, client.Actions())
	})

	t.Run("applies namespace pod and container filters", func(t *testing.T) {
		filteredPod := wholePod.DeepCopy()
		filteredPod.Spec.Containers = append(filteredPod.Spec.Containers, corev1.Container{Name: "sidecar"})
		filteredPod.Annotations = wholeGPUAnnotation(
			device.ContainerDevices{{UUID: gpuUUID, Type: nv.NvidiaGPUDevice, Usedmem: 8000}},
			device.ContainerDevices{{UUID: gpuUUID, Type: nv.NvidiaGPUDevice, Usedmem: 8000}},
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
			device.ContainerDevices{{UUID: gpuUUID, Type: nv.NvidiaGPUDevice, Usedmem: 8000}},
			device.ContainerDevices{{UUID: gpuUUID, Type: nv.NvidiaGPUDevice, Usedmem: 8000}},
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
		unknownPod.Annotations = wholeGPUAnnotation(device.ContainerDevices{{UUID: "GPU-missing", Type: nv.NvidiaGPUDevice, Usedmem: 8000}})
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

	t.Run("keeps confirmed container and marks device query errors", func(t *testing.T) {
		client := fake.NewClientset(node, wholePod)
		nvmllib := &mock.Interface{
			InitFunc:     func() nvml.Return { return nvml.SUCCESS },
			ShutdownFunc: func() nvml.Return { return nvml.SUCCESS },
			DeviceGetHandleByUUIDFunc: func(string) (nvml.Device, nvml.Return) {
				return nil, nvml.ERROR_NOT_SUPPORTED
			},
		}
		report, err := RunWholeGPUDryRun(context.Background(), client, nvmllib, nodeName, WholeGPUDryRunOptions{})
		assert.NilError(t, err)
		assert.Equal(t, report.ConfirmedContainers, 1)
		assert.Equal(t, report.Containers[0].Devices[0].MemoryUsed, uint64(0))
		assert.Assert(t, report.Containers[0].Devices[0].Error != "")
	})

	t.Run("returns a system error when NVML initialization fails", func(t *testing.T) {
		client := fake.NewClientset(node, wholePod)
		_, err := RunWholeGPUDryRun(context.Background(), client, &mock.Interface{InitFunc: func() nvml.Return { return nvml.ERROR_LIBRARY_NOT_FOUND }}, nodeName, WholeGPUDryRunOptions{})
		assert.ErrorContains(t, err, "NVML initialization failed")
	})

	t.Run("treats MIG instance allocation as whole at instance level", func(t *testing.T) {
		const migUUID = "MIG-GPU-aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa/0/0"
		migNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName,
				Annotations: map[string]string{nv.RegisterAnnos: device.MarshalNodeDevices([]*device.DeviceInfo{{
					ID: migUUID, Devmem: 2000, Mode: nv.MigMode,
				}})},
			},
		}
		migPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "workloads", Name: "mig-pod", UID: types.UID("mig-uid"),
				Annotations: wholeGPUAnnotation(device.ContainerDevices{{UUID: migUUID, Type: nv.NvidiaGPUDevice, Usedmem: 2000}}),
			},
			Spec: corev1.PodSpec{NodeName: nodeName, Containers: []corev1.Container{{Name: "trainer"}}},
		}
		report, err := RunWholeGPUDryRun(context.Background(), fake.NewClientset(migNode, migPod), successfulWholeGPUNVML(migUUID), nodeName, WholeGPUDryRunOptions{})
		assert.NilError(t, err)
		assert.Equal(t, report.ConfirmedContainers, 1)
		assert.Equal(t, report.Containers[0].Container, "trainer")
		assert.Equal(t, report.Containers[0].Devices[0].UUID, migUUID)
		assert.Equal(t, len(report.Diagnostics), 0)
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
				GetMemoryInfoFunc:       func() (nvml.Memory, nvml.Return) { return nvml.Memory{}, nvml.SUCCESS },
				GetUtilizationRatesFunc: func() (nvml.Utilization, nvml.Return) { return nvml.Utilization{}, nvml.SUCCESS },
			}, nvml.SUCCESS
		},
	}
}
