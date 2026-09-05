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
	"maps"
	"strconv"
	"sync"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

func initReplayDevices(t *testing.T) {
	t.Helper()
	sConfig := &config.Config{
		NvidiaConfig: nvidia.NvidiaConfig{
			ResourceCountName:            "hami.io/gpu",
			ResourceMemoryName:           "hami.io/gpumem",
			ResourceMemoryPercentageName: "hami.io/gpumem-percentage",
			ResourceCoreName:             "hami.io/gpucores",
			DefaultGPUNum:                1,
		},
	}
	assert.NilError(t, config.InitDevicesWithConfig(sConfig))
}

func replayQuotaUsage(s *Scheduler, namespace, resourceName string) int64 {
	dq, ok := s.quotaManager.GetResourceQuota()[namespace]
	if !ok {
		return 0
	}
	quota, ok := (*dq)[resourceName]
	if !ok {
		return 0
	}
	return quota.Used
}

func newTerminatingAllocatedPod(uid, name, namespace string) *corev1.Pod {
	podDevices := device.PodDevices{
		nvidia.NvidiaGPUDevice: device.PodSingleDevice{
			[]device.ContainerDevice{{Idx: 0, UUID: "GPU0", Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 100}},
		},
	}
	now := metav1.Now()
	grace := int64(3600)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:               k8stypes.UID(uid),
			Name:              name,
			Namespace:         namespace,
			Annotations:       map[string]string{util.AssignedNodeAnnotations: "node1"},
			DeletionTimestamp: &now,
		},
		Spec:   corev1.PodSpec{TerminationGracePeriodSeconds: &grace},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	maps.Copy(pod.Annotations, device.EncodePodDevices(device.SupportDevices, podDevices))
	return pod
}

func addReplayNode(s *Scheduler, nodeName string) {
	s.addNode(nodeName, &device.NodeInfo{
		ID:   nodeName,
		Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}},
		Devices: map[string][]device.DeviceInfo{
			nvidia.NvidiaGPUDevice: {{
				ID:      "GPU0",
				Index:   0,
				Count:   10,
				Devmem:  40000,
				Devcore: 100,
				Mode:    "hami",
				Health:  true,
			}},
		},
	})
}

func newMalformedAllocatedPod(uid, name, namespace, nodeName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       k8stypes.UID(uid),
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				util.AssignedNodeAnnotations:                  nodeName,
				device.SupportDevices[nvidia.NvidiaGPUDevice]: "GPU0,NVIDIA,20000:;",
			},
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
			Containers: []corev1.Container{{
				Name: "app",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{"hami.io/gpu": resource.MustParse("1")},
				},
			}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{{
				Type:   corev1.PodScheduled,
				Status: corev1.ConditionTrue,
			}},
		},
	}
}

func validAllocatedAnnotations() map[string]string {
	return device.EncodePodDevices(device.SupportDevices, device.PodDevices{
		nvidia.NvidiaGPUDevice: device.PodSingleDevice{
			device.ContainerDevices{{
				UUID:      "GPU0",
				Type:      nvidia.NvidiaGPUDevice,
				Usedmem:   20000,
				Usedcores: 50,
			}},
		},
	})
}

func TestPodAllocationDecodeFailuresConcurrentAccess(t *testing.T) {
	var failures podAllocationDecodeFailures
	var wg sync.WaitGroup
	for i := range 100 {
		uid := k8stypes.UID("pod-" + strconv.Itoa(i))
		wg.Add(2)
		go func() {
			defer wg.Done()
			failures.record(uid, "node1")
			failures.clearPod(uid)
		}()
		go func() {
			defer wg.Done()
			_ = failures.nodes()
		}()
	}
	wg.Wait()
	assert.Equal(t, len(failures.nodes()), 0)
}

// After a scheduler restart the informer's initial sync replays every pod as
// an add. A pod that is terminating with a long grace period still runs and
// holds its devices, so it must land in the cache; dropping it made its GPU
// look free and allowed overcommit.
func Test_onAddPod_TerminatingPodReplayedIntoEmptyCache(t *testing.T) {
	initReplayDevices(t)
	s := NewScheduler()
	pod := newTerminatingAllocatedPod("replay-add-uid", "long-graceful-add", "replay-add-ns")
	// The quota manager is a process wide singleton; release the usage so
	// later tests start from a clean slate.
	t.Cleanup(func() { s.onDelPod(pod) })

	s.onAddPod(pod)

	pi, ok := s.podManager.GetPod(pod)
	assert.Equal(t, ok, true, "terminating pod replayed on initial sync must be cached")
	assert.Equal(t, pi.Devices[nvidia.NvidiaGPUDevice][0][0].UUID, "GPU0")
	assert.Equal(t, replayQuotaUsage(s, pod.Namespace, "hami.io/gpumem"), int64(20000))
	assert.Equal(t, replayQuotaUsage(s, pod.Namespace, "hami.io/gpucores"), int64(100))
}

// The periodic resync delivers the same situation as an update.
func Test_onUpdatePod_TerminatingPodMissingFromCache(t *testing.T) {
	initReplayDevices(t)
	s := NewScheduler()
	pod := newTerminatingAllocatedPod("replay-update-uid", "long-graceful-update", "replay-update-ns")
	t.Cleanup(func() { s.onDelPod(pod) })

	s.onUpdatePod(pod.DeepCopy(), pod)

	pi, ok := s.podManager.GetPod(pod)
	assert.Equal(t, ok, true, "terminating pod resynced into an empty cache must be cached")
	assert.Equal(t, pi.Devices[nvidia.NvidiaGPUDevice][0][0].UUID, "GPU0")
	assert.Equal(t, replayQuotaUsage(s, pod.Namespace, "hami.io/gpumem"), int64(20000))
	assert.Equal(t, replayQuotaUsage(s, pod.Namespace, "hami.io/gpucores"), int64(100))
}

func Test_onUpdatePod_ValidAllocationClearsDecodeFailure(t *testing.T) {
	initReplayDevices(t)
	s := NewScheduler()
	addReplayNode(s, "node1")
	pod := newMalformedAllocatedPod("decode-recovery-uid", "decode-recovery", "decode-recovery-ns", "node1")

	s.onAddPod(pod)

	nodes := []string{"node1"}
	candidates, _, failedNodes, err := s.getNodesUsage(&nodes, nil)
	assert.NilError(t, err)
	assert.Equal(t, len(*candidates), 0)
	assert.Equal(t, failedNodes["node1"], unaccountedPodAllocationReason)

	updated := pod.DeepCopy()
	maps.Copy(updated.Annotations, validAllocatedAnnotations())
	s.onUpdatePod(pod, updated)
	t.Cleanup(func() { s.onDelPod(updated) })

	candidates, _, failedNodes, err = s.getNodesUsage(&nodes, nil)
	assert.NilError(t, err)
	assert.Equal(t, len(failedNodes), 0)
	usage, ok := (*candidates)["node1"]
	assert.Equal(t, ok, true)
	assert.Equal(t, usage.Devices.DeviceLists[0].Device.Usedmem, int32(20000))
	assert.Equal(t, usage.Devices.DeviceLists[0].Device.Usedcores, int32(50))
}

func Test_onDelPod_ClearsOnlyDeletedPodDecodeFailure(t *testing.T) {
	initReplayDevices(t)
	s := NewScheduler()
	addReplayNode(s, "node1")
	first := newMalformedAllocatedPod("decode-delete-first", "decode-delete-first", "decode-delete-ns", "node1")
	second := newMalformedAllocatedPod("decode-delete-second", "decode-delete-second", "decode-delete-ns", "node1")
	s.onAddPod(first)
	s.onAddPod(second)
	nodes := []string{"node1"}

	deletedFirst := first.DeepCopy()
	deletedFirst.Annotations = nil
	s.onDelPod(deletedFirst)
	candidates, _, failedNodes, err := s.getNodesUsage(&nodes, nil)
	assert.NilError(t, err)
	assert.Equal(t, len(*candidates), 0)
	assert.Equal(t, failedNodes["node1"], unaccountedPodAllocationReason)

	deletedSecond := second.DeepCopy()
	deletedSecond.Annotations = nil
	s.onDelPod(deletedSecond)
	candidates, _, failedNodes, err = s.getNodesUsage(&nodes, nil)
	assert.NilError(t, err)
	assert.Equal(t, len(failedNodes), 0)
	_, ok := (*candidates)["node1"]
	assert.Equal(t, ok, true)
}

func Test_onAddPod_DecodeFailureDoesNotBlockFromAnnotationAlone(t *testing.T) {
	initReplayDevices(t)
	s := NewScheduler()
	addReplayNode(s, "node1")
	pod := newMalformedAllocatedPod("decode-unbound-uid", "decode-unbound", "decode-unbound-ns", "node1")
	pod.Spec.NodeName = ""

	s.onAddPod(pod)

	nodes := []string{"node1"}
	candidates, _, failedNodes, err := s.getNodesUsage(&nodes, nil)
	assert.NilError(t, err)
	assert.Equal(t, len(failedNodes), 0)
	_, ok := (*candidates)["node1"]
	assert.Equal(t, ok, true)
}

func Test_onAddPod_DecodeFailureRequiresScheduledHAMiPod(t *testing.T) {
	initReplayDevices(t)
	tests := []struct {
		name   string
		mutate func(*corev1.Pod)
	}{
		{
			name: "manually bound pod",
			mutate: func(pod *corev1.Pod) {
				pod.Status.Conditions = nil
			},
		},
		{
			name: "scheduled pod without HAMi resource",
			mutate: func(pod *corev1.Pod) {
				pod.Spec.Containers[0].Resources = corev1.ResourceRequirements{}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := NewScheduler()
			addReplayNode(s, "node1")
			pod := newMalformedAllocatedPod("decode-untrusted-uid", "decode-untrusted", "decode-untrusted-ns", "node1")
			test.mutate(pod)

			s.onAddPod(pod)

			nodes := []string{"node1"}
			candidates, _, failedNodes, err := s.getNodesUsage(&nodes, nil)
			assert.NilError(t, err)
			assert.Equal(t, len(failedNodes), 0)
			_, ok := (*candidates)["node1"]
			assert.Equal(t, ok, true)
		})
	}
}

func Test_onUpdatePod_RemovedAssignmentClearsDecodeFailure(t *testing.T) {
	initReplayDevices(t)
	s := NewScheduler()
	addReplayNode(s, "node1")
	pod := newMalformedAllocatedPod("decode-unassigned-uid", "decode-unassigned", "decode-unassigned-ns", "node1")
	s.onAddPod(pod)

	updated := pod.DeepCopy()
	delete(updated.Annotations, util.AssignedNodeAnnotations)
	s.onUpdatePod(pod, updated)

	nodes := []string{"node1"}
	candidates, _, failedNodes, err := s.getNodesUsage(&nodes, nil)
	assert.NilError(t, err)
	assert.Equal(t, len(failedNodes), 0)
	_, ok := (*candidates)["node1"]
	assert.Equal(t, ok, true)
}

func Test_onAddPod_BadResyncKeepsCachedAllocation(t *testing.T) {
	initReplayDevices(t)
	s := NewScheduler()
	addReplayNode(s, "node1")
	pod := newMalformedAllocatedPod("decode-cached-uid", "decode-cached", "decode-cached-ns", "node1")
	maps.Copy(pod.Annotations, validAllocatedAnnotations())
	s.onAddPod(pod)
	t.Cleanup(func() { s.onDelPod(pod) })

	malformed := pod.DeepCopy()
	malformed.Annotations[device.SupportDevices[nvidia.NvidiaGPUDevice]] = "GPU0,NVIDIA,20000:;"
	s.onAddPod(malformed)

	nodes := []string{"node1"}
	candidates, _, failedNodes, err := s.getNodesUsage(&nodes, nil)
	assert.NilError(t, err)
	assert.Equal(t, len(failedNodes), 0)
	usage, ok := (*candidates)["node1"]
	assert.Equal(t, ok, true)
	assert.Equal(t, usage.Devices.DeviceLists[0].Device.Usedmem, int32(20000))
}

func Test_onDelNode_ClearsAllocationDecodeFailures(t *testing.T) {
	initReplayDevices(t)
	s := NewScheduler()
	addReplayNode(s, "node1")
	pod := newMalformedAllocatedPod("decode-node-delete-uid", "decode-node-delete", "decode-node-delete-ns", "node1")
	s.onAddPod(pod)

	s.onDelNode(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}})
	_, blocked := s.allocationDecodeFailures.nodes()["node1"]
	assert.Equal(t, blocked, false)
}
