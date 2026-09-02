/*
Copyright 2026 The HAMi Authors.

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

package scheduler

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/enflame"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

const (
	refitPodUID  = "refit-pod-uid"
	refitPodName = "refit-pod"
	refitNode    = "node-1"
)

// refitFixture builds a cache-only scheduler tracking one pod that reserves
// GPU-a, on a node carrying GPU-a (NUMA 1) and GPU-b (NUMA 0).
func refitFixture(t *testing.T, gpuBDevmem int32) (*Scheduler, *corev1.Pod) {
	t.Helper()
	nodes := newNodeManager()
	nodes.addNode(refitNode, &device.NodeInfo{
		ID: refitNode, Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: refitNode}},
		Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: {
			{ID: "GPU-a", Count: 10, Devmem: 40000, Devcore: 100, Numa: 1, Type: nvidia.NvidiaGPUDevice, Health: true},
			{ID: "GPU-b", Count: 10, Devmem: gpuBDevmem, Devcore: 100, Numa: 0, Type: nvidia.NvidiaGPUDevice, Health: true},
		}},
	})

	reserved := device.PodSingleDevice{{{UUID: "GPU-a", Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 30}}}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID: refitPodUID, Name: refitPodName, Namespace: "default",
			Annotations: map[string]string{
				device.InRequestDevices[nvidia.NvidiaGPUDevice]: device.EncodePodSingleDevice(reserved),
				device.SupportDevices[nvidia.NvidiaGPUDevice]:   device.EncodePodSingleDevice(reserved),
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
	}
	pods := device.NewPodManager()
	pods.AddPod(pod, refitNode, device.PodDevices{nvidia.NvidiaGPUDevice: reserved})

	s := &Scheduler{nodeManager: nodes, podManager: pods, quotaManager: device.NewQuotaManager()}
	s.quotaManager.Quotas = map[string]*device.DeviceQuota{}
	return s, pod
}

func phaseAwareRefitFixture(t *testing.T, initContainers, containers []corev1.Container, reserved device.PodSingleDevice, gpuBDevmem, externalGPUBMemory int32) (*Scheduler, *corev1.Pod) {
	t.Helper()
	if len(containers) == 0 {
		containers = []corev1.Container{{Name: "main"}}
	}
	nodes := newNodeManager()
	nodes.addNode(refitNode, &device.NodeInfo{
		ID: refitNode, Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: refitNode}},
		Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: {
			{ID: "GPU-a", Count: 10, Devmem: 40000, Devcore: 100, Numa: 1, Type: nvidia.NvidiaGPUDevice, Health: true},
			{ID: "GPU-b", Count: 10, Devmem: gpuBDevmem, Devcore: 100, Numa: 0, Type: nvidia.NvidiaGPUDevice, Health: true},
		}},
	})

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID: refitPodUID, Name: refitPodName, Namespace: "default",
			Annotations: map[string]string{
				device.InRequestDevices[nvidia.NvidiaGPUDevice]: device.EncodePodSingleDevice(reserved),
				device.SupportDevices[nvidia.NvidiaGPUDevice]:   device.EncodePodSingleDevice(reserved),
			},
		},
		Spec: corev1.PodSpec{
			InitContainers: initContainers,
			Containers:     containers,
		},
	}
	raw := device.PodDevices{nvidia.NvidiaGPUDevice: reserved}
	pods := device.NewPodManager()
	pods.AddPod(pod, refitNode, device.CollapseInitContainerUsage(pod, raw))
	if externalGPUBMemory > 0 {
		externalPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "external-pod", Name: "external-pod", Namespace: "default"}}
		pods.AddPod(externalPod, refitNode, device.PodDevices{nvidia.NvidiaGPUDevice: {{{
			UUID: "GPU-b", Type: nvidia.NvidiaGPUDevice, Usedmem: externalGPUBMemory, Usedcores: 10,
		}}}})
	}

	s := &Scheduler{nodeManager: nodes, podManager: pods, quotaManager: device.NewQuotaManager()}
	s.quotaManager.Quotas = map[string]*device.DeviceQuota{}
	return s, pod
}

// stubRefitPatch replaces the annotation patch with a capture; returns the
// captured map and a call counter.
func stubRefitPatch(t *testing.T, fail error) (map[string]string, *int) {
	t.Helper()
	captured := map[string]string{}
	calls := 0
	previous := patchPodAnnotations
	patchPodAnnotations = func(_ *corev1.Pod, annotations map[string]string) error {
		calls++
		if fail != nil {
			return fail
		}
		maps.Copy(captured, annotations)
		return nil
	}
	t.Cleanup(func() { patchPodAnnotations = previous })
	return captured, &calls
}

func TestRefitNumaAllocationPatchDeadlineReleasesFilter(t *testing.T) {
	s, pod := refitFixture(t, 40000)
	resourceNames := device.GetDevices()[nvidia.NvidiaGPUDevice].GetResourceNames()
	s.quotaManager.AddUsage(pod, device.PodDevices{nvidia.NvidiaGPUDevice: {{{
		UUID: "GPU-a", Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 30,
	}}}})

	patchStarted := make(chan struct{})
	releasePatch := make(chan struct{})
	filterStarted := make(chan struct{})
	filterDone := make(chan error, 1)
	previousPatch := patchPodAnnotations
	patchPodAnnotations = func(_ *corev1.Pod, _ map[string]string) error {
		close(patchStarted)
		<-releasePatch
		return context.DeadlineExceeded
	}
	t.Cleanup(func() { patchPodAnnotations = previousPatch })

	refitDone := make(chan device.NumaRefitResponse, 1)
	go func() {
		refitDone <- s.RefitNumaAllocation(refitTestRequestFor("GPU-b"))
	}()
	select {
	case <-patchStarted:
	case <-time.After(time.Second):
		t.Fatal("refit did not reach the annotation PATCH")
	}
	if s.allocLock.TryLock() {
		s.allocLock.Unlock()
		t.Fatal("refit did not hold allocLock while patching annotations")
	}

	filterPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{UID: "waiting-filter-uid", Name: "waiting-filter", Namespace: "default"},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "main",
			Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
				corev1.ResourceName(resourceNames.ResourceCountName): *resource.NewQuantity(1, resource.DecimalSI),
			}},
		}}},
	}
	go func() {
		close(filterStarted)
		_, err := s.Filter(extenderv1.ExtenderArgs{Pod: filterPod, NodeNames: &[]string{}})
		filterDone <- err
	}()
	<-filterStarted
	close(releasePatch)

	select {
	case response := <-refitDone:
		assert.Equal(t, response.Succeeded, false)
		assert.Assert(t, strings.Contains(response.FailureReason, context.DeadlineExceeded.Error()), "reason: %s", response.FailureReason)
	case <-time.After(time.Second):
		t.Fatal("refit did not stop after the annotation PATCH deadline")
	}
	select {
	case err := <-filterDone:
		assert.NilError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Filter did not continue after the refit released allocLock")
	}

	assert.Equal(t, trackedUUID(t, s), "GPU-a")
	quota := s.quotaManager.GetResourceQuota()[pod.Namespace]
	assert.Equal(t, (*quota)[resourceNames.ResourceMemoryName].Used, int64(20000))
	assert.Equal(t, (*quota)[resourceNames.ResourceCoreName].Used, int64(30))
}

func refitTestRequestFor(allowed ...string) device.NumaRefitRequest {
	return device.NumaRefitRequest{
		PodUID:             refitPodUID,
		PodNamespace:       "default",
		PodName:            refitPodName,
		NodeName:           refitNode,
		ContainerIndex:     0,
		DeviceType:         nvidia.NvidiaGPUDevice,
		AllowedDeviceUUIDs: allowed,
	}
}

func trackedUUID(t *testing.T, s *Scheduler) string {
	t.Helper()
	pi, ok := s.podManager.GetPod(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: refitPodUID}})
	assert.Equal(t, ok, true)
	return pi.Devices[nvidia.NvidiaGPUDevice][0][0].UUID
}

func TestRefitNumaAllocationMovesReservation(t *testing.T) {
	s, _ := refitFixture(t, 40000)
	captured, calls := stubRefitPatch(t, nil)

	response := s.RefitNumaAllocation(refitTestRequestFor("GPU-b"))

	assert.Equal(t, response.Succeeded, true, "refit failed: %s", response.FailureReason)
	devices, err := device.DecodeContainerDevices(response.ContainerDevices)
	assert.NilError(t, err)
	assert.Equal(t, len(devices), 1)
	assert.Equal(t, devices[0].UUID, "GPU-b")
	assert.Equal(t, devices[0].Usedmem, int32(20000))
	assert.Equal(t, devices[0].Usedcores, int32(30))

	// Both annotations were patched together onto the new device, with the
	// value shape preserved byte for byte (no phantom container entries).
	assert.Equal(t, *calls, 1)
	expected := device.EncodePodSingleDevice(device.PodSingleDevice{{{UUID: "GPU-b", Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 30}}})
	for _, key := range []string{device.InRequestDevices[nvidia.NvidiaGPUDevice], device.SupportDevices[nvidia.NvidiaGPUDevice]} {
		value, ok := captured[key]
		assert.Equal(t, ok, true, "annotation %s not patched", key)
		assert.Equal(t, value, expected, "annotation %s", key)
	}

	// In-memory accounting moved with it, without arming the init shrink.
	assert.Equal(t, trackedUUID(t, s), "GPU-b")
	pi, _ := s.podManager.GetPod(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: refitPodUID}})
	assert.Equal(t, pi.InitContainerResourceReleased, false)
}

func TestRefitNumaAllocationNoOpWhenAlreadyAllowed(t *testing.T) {
	s, _ := refitFixture(t, 40000)
	_, calls := stubRefitPatch(t, nil)

	response := s.RefitNumaAllocation(refitTestRequestFor("GPU-a", "GPU-b"))

	assert.Equal(t, response.Succeeded, true)
	devices, err := device.DecodeContainerDevices(response.ContainerDevices)
	assert.NilError(t, err)
	assert.Equal(t, devices[0].UUID, "GPU-a")
	assert.Equal(t, *calls, 0)
	assert.Equal(t, trackedUUID(t, s), "GPU-a")
}

func TestRefitNumaAllocationInsufficientCapacity(t *testing.T) {
	s, _ := refitFixture(t, 16000)
	_, calls := stubRefitPatch(t, nil)

	response := s.RefitNumaAllocation(refitTestRequestFor("GPU-b"))

	assert.Equal(t, response.Succeeded, false)
	assert.Assert(t, strings.Contains(response.FailureReason, "no allowed device fits"), "reason: %s", response.FailureReason)
	assert.Equal(t, *calls, 0)
	assert.Equal(t, trackedUUID(t, s), "GPU-a")
}

func TestRefitNumaAllocationUsesPhaseAwareCapacity(t *testing.T) {
	always := corev1.ContainerRestartPolicyAlways
	tests := []struct {
		name              string
		initContainers    []corev1.Container
		containers        []corev1.Container
		reserved          device.PodSingleDevice
		gpuBMemory        int32
		externalGPUMemory int32
		containerIndex    int
		containerName     string
		wantSucceeded     bool
		wantMemory        int32
		wantSlots         int32
	}{
		{
			name:           "regular init and app use the phase peak",
			initContainers: []corev1.Container{{Name: "init"}},
			reserved: device.PodSingleDevice{
				{{UUID: "GPU-b", Type: nvidia.NvidiaGPUDevice, Usedmem: 30000, Usedcores: 60}},
				{{UUID: "GPU-a", Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 30}},
			},
			gpuBMemory:     40000,
			containerIndex: 1,
			containerName:  "main",
			wantSucceeded:  true,
			wantMemory:     30000,
			wantSlots:      1,
		},
		{
			name:           "other pod usage is not released",
			initContainers: []corev1.Container{{Name: "init"}},
			reserved: device.PodSingleDevice{
				{{UUID: "GPU-b", Type: nvidia.NvidiaGPUDevice, Usedmem: 30000, Usedcores: 60}},
				{{UUID: "GPU-a", Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 30}},
			},
			gpuBMemory:        40000,
			externalGPUMemory: 15000,
			containerIndex:    1,
			containerName:     "main",
		},
		{
			name: "sidecar starting after a larger init does not inflate the peak",
			initContainers: []corev1.Container{
				{Name: "prepare"},
				{Name: "sidecar", RestartPolicy: &always},
			},
			reserved: device.PodSingleDevice{
				{{UUID: "GPU-b", Type: nvidia.NvidiaGPUDevice, Usedmem: 30000, Usedcores: 60}},
				{{UUID: "GPU-a", Type: nvidia.NvidiaGPUDevice, Usedmem: 10000, Usedcores: 10}},
				{{UUID: "GPU-b", Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 30}},
			},
			gpuBMemory:     30000,
			containerIndex: 1,
			containerName:  "sidecar",
			wantSucceeded:  true,
			wantMemory:     30000,
			wantSlots:      2,
		},
		{
			name:           "sidecar and two apps preserve three concurrent slots",
			initContainers: []corev1.Container{{Name: "sidecar", RestartPolicy: &always}},
			containers:     []corev1.Container{{Name: "main"}, {Name: "worker"}},
			reserved: device.PodSingleDevice{
				{{UUID: "GPU-b", Type: nvidia.NvidiaGPUDevice, Usedmem: 5000, Usedcores: 10}},
				{{UUID: "GPU-b", Type: nvidia.NvidiaGPUDevice, Usedmem: 10000, Usedcores: 20}},
				{{UUID: "GPU-a", Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 30}},
			},
			gpuBMemory:     40000,
			containerIndex: 2,
			containerName:  "worker",
			wantSucceeded:  true,
			wantMemory:     35000,
			wantSlots:      3,
		},
		{
			name:           "sidecar and app remain concurrent",
			initContainers: []corev1.Container{{Name: "sidecar", RestartPolicy: &always}},
			reserved: device.PodSingleDevice{
				{{UUID: "GPU-a", Type: nvidia.NvidiaGPUDevice, Usedmem: 30000, Usedcores: 60}},
				{{UUID: "GPU-b", Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 30}},
			},
			gpuBMemory:    40000,
			containerName: "sidecar",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, _ := phaseAwareRefitFixture(t, test.initContainers, test.containers, test.reserved, test.gpuBMemory, test.externalGPUMemory)
			captured, calls := stubRefitPatch(t, nil)

			request := refitTestRequestFor("GPU-b")
			request.ContainerIndex = test.containerIndex
			request.ContainerName = test.containerName
			response := s.RefitNumaAllocation(request)

			assert.Equal(t, response.Succeeded, test.wantSucceeded, "reason: %s", response.FailureReason)
			if !test.wantSucceeded {
				assert.Equal(t, *calls, 0)
				assert.Assert(t, strings.Contains(response.FailureReason, "CardInsufficientMemory"), "reason: %s", response.FailureReason)
				return
			}

			assert.Equal(t, *calls, 1)
			allocated, err := device.DecodePodDevices(device.SupportDevices, captured)
			assert.NilError(t, err)
			assert.Equal(t, allocated[nvidia.NvidiaGPUDevice][test.containerIndex][0].UUID, "GPU-b")

			pi, ok := s.podManager.GetPod(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: refitPodUID}})
			assert.Equal(t, ok, true)
			var usedMemory, usedSlots int32
			for _, containerDevices := range pi.Devices[nvidia.NvidiaGPUDevice] {
				for _, d := range containerDevices {
					if d.UUID == "GPU-b" {
						usedMemory += d.Usedmem
						usedSlots += max(d.Slots, 1)
					}
				}
			}
			assert.Equal(t, usedMemory, test.wantMemory)
			assert.Equal(t, usedSlots, test.wantSlots)
		})
	}
}

func TestRefitNumaAllocationUnmatchedAllowedSet(t *testing.T) {
	s, _ := refitFixture(t, 40000)
	stubRefitPatch(t, nil)

	response := s.RefitNumaAllocation(refitTestRequestFor("GPU-a-0", "GPU-a-1"))

	assert.Equal(t, response.Succeeded, false)
	assert.Assert(t, strings.Contains(response.FailureReason, AllowedSetUnmatched), "reason: %s", response.FailureReason)
}

func TestRefitNumaAllocationPatchFailureLeavesStateUntouched(t *testing.T) {
	s, _ := refitFixture(t, 40000)
	_, calls := stubRefitPatch(t, errors.New("apiserver unavailable"))

	response := s.RefitNumaAllocation(refitTestRequestFor("GPU-b"))

	assert.Equal(t, response.Succeeded, false)
	assert.Assert(t, strings.Contains(response.FailureReason, "apiserver unavailable"))
	assert.Equal(t, *calls, 1)
	assert.Equal(t, trackedUUID(t, s), "GPU-a")
}

func TestRefitNumaAllocationConsumedAllocation(t *testing.T) {
	s, pod := refitFixture(t, 40000)
	stubRefitPatch(t, nil)
	// Simulate Allocate having consumed the container's pending entry.
	pod.Annotations[device.InRequestDevices[nvidia.NvidiaGPUDevice]] = device.EncodePodSingleDevice(device.PodSingleDevice{{}})
	s.podManager.UpdatePod(pod)

	response := s.RefitNumaAllocation(refitTestRequestFor("GPU-b"))

	assert.Equal(t, response.Succeeded, false)
	assert.Assert(t, strings.Contains(response.FailureReason, "no pending"), "reason: %s", response.FailureReason)
	assert.Equal(t, trackedUUID(t, s), "GPU-a")
}

func TestRefitNumaAllocationValidation(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*device.NumaRefitRequest)
		wantReason string
	}{
		{
			name:       "unknown pod",
			mutate:     func(r *device.NumaRefitRequest) { r.PodUID = "other-uid" },
			wantReason: "not tracked",
		},
		{
			name:       "pod identity mismatch",
			mutate:     func(r *device.NumaRefitRequest) { r.PodName = "other-name" },
			wantReason: "does not match",
		},
		{
			name:       "wrong node",
			mutate:     func(r *device.NumaRefitRequest) { r.NodeName = "node-2" },
			wantReason: "tracked on node",
		},
		{
			name:       "unknown device type",
			mutate:     func(r *device.NumaRefitRequest) { r.DeviceType = "NoSuchVendor" },
			wantReason: "unknown device type",
		},
		{
			name:       "registered non-NVIDIA device type",
			mutate:     func(r *device.NumaRefitRequest) { r.DeviceType = enflame.EnflameVGCUDevice },
			wantReason: "not supported by the NUMA refit yet",
		},
		{
			name:       "empty allowed set",
			mutate:     func(r *device.NumaRefitRequest) { r.AllowedDeviceUUIDs = nil },
			wantReason: "empty allowed device set",
		},
		{
			name:       "container index out of range",
			mutate:     func(r *device.NumaRefitRequest) { r.ContainerIndex = 3 },
			wantReason: "outside the pod's containers",
		},
		{
			name:       "negative container index",
			mutate:     func(r *device.NumaRefitRequest) { r.ContainerIndex = -1 },
			wantReason: "outside the pod's containers",
		},
		{
			name:       "incomplete request",
			mutate:     func(r *device.NumaRefitRequest) { r.PodNamespace = "" },
			wantReason: "incomplete refit request",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, _ := refitFixture(t, 40000)
			_, calls := stubRefitPatch(t, nil)

			request := refitTestRequestFor("GPU-b")
			test.mutate(&request)
			response := s.RefitNumaAllocation(request)

			assert.Equal(t, response.Succeeded, false)
			assert.Assert(t, strings.Contains(response.FailureReason, test.wantReason),
				"reason %q does not contain %q", response.FailureReason, test.wantReason)
			assert.Equal(t, *calls, 0)
			assert.Equal(t, trackedUUID(t, s), "GPU-a")
		})
	}
}

func TestRefitNumaAllocationSeedsExistingAllocations(t *testing.T) {
	// Two GPU containers: container 0 already consumed its to-allocate
	// entry, container 1 is pending. The refit of container 1 must keep
	// container 0's entries intact, including the blank to-allocate slot.
	nodes := newNodeManager()
	nodes.addNode(refitNode, &device.NodeInfo{
		ID: refitNode, Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: refitNode}},
		Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: {
			{ID: "GPU-a", Count: 10, Devmem: 40000, Devcore: 100, Numa: 1, Type: nvidia.NvidiaGPUDevice, Health: true},
			{ID: "GPU-b", Count: 10, Devmem: 40000, Devcore: 100, Numa: 0, Type: nvidia.NvidiaGPUDevice, Health: true},
		}},
	})

	first := device.ContainerDevices{{UUID: "GPU-a", Type: nvidia.NvidiaGPUDevice, Usedmem: 5000, Usedcores: 10}}
	second := device.ContainerDevices{{UUID: "GPU-a", Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 30}}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID: refitPodUID, Name: refitPodName, Namespace: "default",
			Annotations: map[string]string{
				device.InRequestDevices[nvidia.NvidiaGPUDevice]: device.EncodePodSingleDevice(device.PodSingleDevice{{}, second}),
				device.SupportDevices[nvidia.NvidiaGPUDevice]:   device.EncodePodSingleDevice(device.PodSingleDevice{first, second}),
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "first"}, {Name: "second"}}},
	}
	pods := device.NewPodManager()
	pods.AddPod(pod, refitNode, device.PodDevices{nvidia.NvidiaGPUDevice: {first, second}})
	s := &Scheduler{nodeManager: nodes, podManager: pods, quotaManager: device.NewQuotaManager()}
	s.quotaManager.Quotas = map[string]*device.DeviceQuota{}
	captured, _ := stubRefitPatch(t, nil)

	request := refitTestRequestFor("GPU-b")
	request.ContainerIndex = 1
	response := s.RefitNumaAllocation(request)

	assert.Equal(t, response.Succeeded, true, "refit failed: %s", response.FailureReason)
	toAllocate := captured[device.InRequestDevices[nvidia.NvidiaGPUDevice]]
	allocatedAnno := captured[device.SupportDevices[nvidia.NvidiaGPUDevice]]
	// Container 0's consumed to-allocate slot stays blank; its allocated
	// record stays on GPU-a; only container 1 moves to GPU-b.
	assert.Assert(t, strings.HasPrefix(toAllocate, ";"), "to-allocate: %q", toAllocate)
	assert.Assert(t, strings.Contains(toAllocate, "GPU-b"), "to-allocate: %q", toAllocate)
	assert.Assert(t, strings.Contains(allocatedAnno, "GPU-a,NVIDIA,5000,10"), "allocated: %q", allocatedAnno)
	assert.Assert(t, strings.Contains(allocatedAnno, "GPU-b,NVIDIA,20000,30"), "allocated: %q", allocatedAnno)

	// In-memory accounting is rebuilt collapsed (one aggregate entry), the
	// same shape Filter and the informer store: GPU-a keeps container 0's
	// usage, GPU-b carries the refitted container 1.
	pi, _ := s.podManager.GetPod(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: refitPodUID}})
	collapsed := pi.Devices[nvidia.NvidiaGPUDevice]
	assert.Equal(t, len(collapsed), 1)
	byUUID := map[string]device.ContainerDevice{}
	for _, d := range collapsed[0] {
		byUUID[d.UUID] = d
	}
	assert.Equal(t, byUUID["GPU-a"].Usedmem, int32(5000))
	assert.Equal(t, byUUID["GPU-b"].Usedmem, int32(20000))
}

func TestRefitNumaAllocationCompetingRefits(t *testing.T) {
	// GPU-b only has room for one of the two reservations: after the first
	// refit moves onto it, the second must be refused on capacity.
	nodes := newNodeManager()
	nodes.addNode(refitNode, &device.NodeInfo{
		ID: refitNode, Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: refitNode}},
		Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: {
			{ID: "GPU-a", Count: 10, Devmem: 40000, Devcore: 100, Numa: 1, Type: nvidia.NvidiaGPUDevice, Health: true},
			{ID: "GPU-b", Count: 10, Devmem: 20000, Devcore: 100, Numa: 0, Type: nvidia.NvidiaGPUDevice, Health: true},
		}},
	})
	pods := device.NewPodManager()
	makePod := func(uid, name string) *corev1.Pod {
		reserved := device.PodSingleDevice{{{UUID: "GPU-a", Type: nvidia.NvidiaGPUDevice, Usedmem: 15000, Usedcores: 10}}}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				UID: k8stypes.UID(uid), Name: name, Namespace: "default",
				Annotations: map[string]string{
					device.InRequestDevices[nvidia.NvidiaGPUDevice]: device.EncodePodSingleDevice(reserved),
					device.SupportDevices[nvidia.NvidiaGPUDevice]:   device.EncodePodSingleDevice(reserved),
				},
			},
			Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
		}
		pods.AddPod(pod, refitNode, device.PodDevices{nvidia.NvidiaGPUDevice: reserved})
		return pod
	}
	makePod("pod-1-uid", "pod-1")
	makePod("pod-2-uid", "pod-2")
	s := &Scheduler{nodeManager: nodes, podManager: pods, quotaManager: device.NewQuotaManager()}
	s.quotaManager.Quotas = map[string]*device.DeviceQuota{}
	stubRefitPatch(t, nil)

	requestFor := func(uid, name string) device.NumaRefitRequest {
		return device.NumaRefitRequest{
			PodUID: uid, PodNamespace: "default", PodName: name, NodeName: refitNode,
			DeviceType: nvidia.NvidiaGPUDevice, AllowedDeviceUUIDs: []string{"GPU-b"},
		}
	}

	firstResponse := s.RefitNumaAllocation(requestFor("pod-1-uid", "pod-1"))
	assert.Equal(t, firstResponse.Succeeded, true, "first refit failed: %s", firstResponse.FailureReason)

	secondResponse := s.RefitNumaAllocation(requestFor("pod-2-uid", "pod-2"))
	assert.Equal(t, secondResponse.Succeeded, false)
	assert.Assert(t, strings.Contains(secondResponse.FailureReason, "no allowed device fits"),
		"reason: %s", secondResponse.FailureReason)

	// The loser keeps its original reservation.
	pi, _ := s.podManager.GetPod(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "pod-2-uid"}})
	assert.Equal(t, pi.Devices[nvidia.NvidiaGPUDevice][0][0].UUID, "GPU-a")
}

func TestRefitNumaAllocationContainerNameMismatch(t *testing.T) {
	s, _ := refitFixture(t, 40000)
	_, calls := stubRefitPatch(t, nil)

	request := refitTestRequestFor("GPU-b")
	request.ContainerName = "sidecar"
	response := s.RefitNumaAllocation(request)

	assert.Equal(t, response.Succeeded, false)
	assert.Assert(t, strings.Contains(response.FailureReason, `is "main", not "sidecar"`), "reason: %s", response.FailureReason)
	assert.Equal(t, *calls, 0)

	// The matching name is accepted.
	request.ContainerName = "main"
	response = s.RefitNumaAllocation(request)
	assert.Equal(t, response.Succeeded, true, "refit failed: %s", response.FailureReason)
}

func TestRefitNumaAllocationRejectsMigDevice(t *testing.T) {
	nodes := newNodeManager()
	nodes.addNode(refitNode, &device.NodeInfo{
		ID: refitNode, Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: refitNode}},
		Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: {
			{ID: "GPU-a", Count: 10, Devmem: 40000, Devcore: 100, Numa: 1, Type: nvidia.NvidiaGPUDevice, Health: true},
			{ID: "GPU-b", Count: 7, Devmem: 40000, Devcore: 100, Numa: 0, Type: nvidia.NvidiaGPUDevice, Mode: nvidia.MigMode, Health: true},
		}},
	})
	reserved := device.PodSingleDevice{{{UUID: "GPU-a", Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 30}}}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID: refitPodUID, Name: refitPodName, Namespace: "default",
			Annotations: map[string]string{
				device.InRequestDevices[nvidia.NvidiaGPUDevice]: device.EncodePodSingleDevice(reserved),
				device.SupportDevices[nvidia.NvidiaGPUDevice]:   device.EncodePodSingleDevice(reserved),
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
	}
	pods := device.NewPodManager()
	pods.AddPod(pod, refitNode, device.PodDevices{nvidia.NvidiaGPUDevice: reserved})
	s := &Scheduler{nodeManager: nodes, podManager: pods, quotaManager: device.NewQuotaManager()}
	s.quotaManager.Quotas = map[string]*device.DeviceQuota{}
	stubRefitPatch(t, nil)

	response := s.RefitNumaAllocation(refitTestRequestFor("GPU-b"))

	assert.Equal(t, response.Succeeded, false)
	assert.Assert(t, strings.Contains(response.FailureReason, "MIG"), "reason: %s", response.FailureReason)
}

func TestRefitNumaAllocationHeterogeneousReservation(t *testing.T) {
	// Two devices in one container with differing reserved amounts (possible
	// with percentage requests on mixed GPUs) cannot be re-fit faithfully.
	nodes := newNodeManager()
	nodes.addNode(refitNode, &device.NodeInfo{
		ID: refitNode, Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: refitNode}},
		Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: {
			{ID: "GPU-a", Count: 10, Devmem: 40000, Devcore: 100, Numa: 1, Type: nvidia.NvidiaGPUDevice, Health: true},
			{ID: "GPU-b", Count: 10, Devmem: 20000, Devcore: 100, Numa: 1, Type: nvidia.NvidiaGPUDevice, Health: true},
			{ID: "GPU-c", Count: 10, Devmem: 40000, Devcore: 100, Numa: 0, Type: nvidia.NvidiaGPUDevice, Health: true},
			{ID: "GPU-d", Count: 10, Devmem: 40000, Devcore: 100, Numa: 0, Type: nvidia.NvidiaGPUDevice, Health: true},
		}},
	})
	reserved := device.PodSingleDevice{{
		{UUID: "GPU-a", Type: nvidia.NvidiaGPUDevice, Usedmem: 40000, Usedcores: 30},
		{UUID: "GPU-b", Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 30},
	}}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID: refitPodUID, Name: refitPodName, Namespace: "default",
			Annotations: map[string]string{
				device.InRequestDevices[nvidia.NvidiaGPUDevice]: device.EncodePodSingleDevice(reserved),
				device.SupportDevices[nvidia.NvidiaGPUDevice]:   device.EncodePodSingleDevice(reserved),
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
	}
	pods := device.NewPodManager()
	pods.AddPod(pod, refitNode, device.PodDevices{nvidia.NvidiaGPUDevice: reserved})
	s := &Scheduler{nodeManager: nodes, podManager: pods, quotaManager: device.NewQuotaManager()}
	s.quotaManager.Quotas = map[string]*device.DeviceQuota{}
	_, calls := stubRefitPatch(t, nil)

	response := s.RefitNumaAllocation(refitTestRequestFor("GPU-c", "GPU-d"))

	assert.Equal(t, response.Succeeded, false)
	assert.Assert(t, strings.Contains(response.FailureReason, "heterogeneous"), "reason: %s", response.FailureReason)
	assert.Equal(t, *calls, 0)
}

func TestRefitNumaAllocationKeepsShrunkAccounting(t *testing.T) {
	// A pod whose init-container usage was already released must not have its
	// reservation re-inflated back to the init peak by a later refit.
	nodes := newNodeManager()
	nodes.addNode(refitNode, &device.NodeInfo{
		ID: refitNode, Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: refitNode}},
		Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: {
			{ID: "GPU-a", Count: 10, Devmem: 40000, Devcore: 100, Numa: 1, Type: nvidia.NvidiaGPUDevice, Health: true},
			{ID: "GPU-b", Count: 10, Devmem: 40000, Devcore: 100, Numa: 0, Type: nvidia.NvidiaGPUDevice, Health: true},
		}},
	})

	initDevices := device.ContainerDevices{{UUID: "GPU-a", Type: nvidia.NvidiaGPUDevice, Usedmem: 30000, Usedcores: 60}}
	appDevices := device.ContainerDevices{{UUID: "GPU-a", Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 30}}
	reserved := device.PodSingleDevice{initDevices, appDevices}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID: refitPodUID, Name: refitPodName, Namespace: "default",
			Annotations: map[string]string{
				device.InRequestDevices[nvidia.NvidiaGPUDevice]: device.EncodePodSingleDevice(reserved),
				device.SupportDevices[nvidia.NvidiaGPUDevice]:   device.EncodePodSingleDevice(reserved),
			},
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{Name: "init"}},
			Containers:     []corev1.Container{{Name: "main"}},
		},
	}
	pods := device.NewPodManager()
	pods.AddPod(pod, refitNode, device.PodDevices{nvidia.NvidiaGPUDevice: reserved})
	// Simulate the informer's post-init shrink, which arms the released flag.
	steady := device.SteadyStateDeviceUsage(pod, device.PodDevices{nvidia.NvidiaGPUDevice: reserved})
	pods.UpdatePodDevice(pod, steady)

	s := &Scheduler{nodeManager: nodes, podManager: pods, quotaManager: device.NewQuotaManager()}
	s.quotaManager.Quotas = map[string]*device.DeviceQuota{}
	stubRefitPatch(t, nil)

	request := refitTestRequestFor("GPU-b")
	request.ContainerIndex = 1
	request.ContainerName = "main"
	response := s.RefitNumaAllocation(request)
	assert.Equal(t, response.Succeeded, true, "refit failed: %s", response.FailureReason)

	pi, ok := s.podManager.GetPod(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: refitPodUID}})
	assert.Equal(t, ok, true)
	assert.Equal(t, pi.InitContainerResourceReleased, true)
	// Steady state is app-only: the refitted GPU-b at the app amount, with no
	// re-inflated init-container reservation on GPU-a.
	total := int32(0)
	for _, containerDevices := range pi.Devices[nvidia.NvidiaGPUDevice] {
		for _, d := range containerDevices {
			total += d.Usedmem
			assert.Equal(t, d.UUID, "GPU-b", "unexpected device %s after refit", d.UUID)
		}
	}
	assert.Equal(t, total, int32(20000))
}
