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
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
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

// An informer delete notification is only an identity signal. The final Pod
// object may no longer carry the scheduler annotations that were present when
// its allocation was cached, so cleanup must use the UID-keyed PodManager
// entry instead of depending on the delete object's annotations.
func Test_onDelPod_CleansCachedAllocationWithoutAnnotations(t *testing.T) {
	initReplayDevices(t)
	s := NewScheduler()
	pod := newTerminatingAllocatedPod(
		"annotationless-delete-uid",
		"annotationless-delete",
		"annotationless-delete-ns",
	)
	t.Cleanup(func() { s.onDelPod(pod) })

	s.onAddPod(pod)
	deletedPod := pod.DeepCopy()
	deletedPod.Annotations = nil

	s.onDelPod(deletedPod)

	_, cached := s.podManager.GetPod(pod)
	assert.Equal(
		t,
		cached,
		false,
		"delete must evict the allocation by UID even without annotations",
	)
	assert.Equal(t, replayQuotaUsage(s, pod.Namespace, "hami.io/gpumem"), int64(0))
	assert.Equal(t, replayQuotaUsage(s, pod.Namespace, "hami.io/gpucores"), int64(0))
}

func Test_onDelPod_CleansAnnotationlessTombstoneAndIsIdempotent(t *testing.T) {
	initReplayDevices(t)
	s := NewScheduler()
	pod := newTerminatingAllocatedPod(
		"annotationless-tombstone-uid",
		"annotationless-tombstone",
		"annotationless-tombstone-ns",
	)
	t.Cleanup(func() { s.onDelPod(pod) })

	s.onAddPod(pod)
	deletedPod := pod.DeepCopy()
	deletedPod.Annotations = nil
	tombstone := cache.DeletedFinalStateUnknown{
		Key: deletedPod.Namespace + "/" + deletedPod.Name,
		Obj: deletedPod,
	}

	s.onDelPod(tombstone)
	s.onDelPod(tombstone)

	_, cached := s.podManager.GetPod(pod)
	assert.Equal(t, cached, false)
	assert.Equal(t, replayQuotaUsage(s, pod.Namespace, "hami.io/gpumem"), int64(0))
	assert.Equal(t, replayQuotaUsage(s, pod.Namespace, "hami.io/gpucores"), int64(0))
}

func Test_onDelPod_AnnotationlessOldUIDDoesNotDeleteReplacement(t *testing.T) {
	initReplayDevices(t)
	s := NewScheduler()
	oldPod := newTerminatingAllocatedPod(
		"old-uid",
		"replaced-pod",
		"replacement-delete-ns",
	)
	newPod := newTerminatingAllocatedPod("new-uid", oldPod.Name, oldPod.Namespace)
	t.Cleanup(func() {
		s.onDelPod(oldPod)
		s.onDelPod(newPod)
	})

	s.onAddPod(oldPod)
	s.onAddPod(newPod)
	deletedOldPod := oldPod.DeepCopy()
	deletedOldPod.Annotations = nil

	s.onDelPod(deletedOldPod)

	_, oldCached := s.podManager.GetPod(oldPod)
	_, newCached := s.podManager.GetPod(newPod)
	assert.Equal(t, oldCached, false)
	assert.Equal(
		t,
		newCached,
		true,
		"a stale delete must not evict the same-name replacement UID",
	)
	assert.Equal(t, replayQuotaUsage(s, oldPod.Namespace, "hami.io/gpumem"), int64(20000))
	assert.Equal(t, replayQuotaUsage(s, oldPod.Namespace, "hami.io/gpucores"), int64(100))
}

func Test_onAddPod_ReportsUndecodableBoundAllocation(t *testing.T) {
	initReplayDevices(t)
	recorder := record.NewFakeRecorder(1)
	s := NewScheduler()
	s.eventRecorder = recorder
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bad-allocation",
			Namespace: "default",
			Annotations: map[string]string{
				util.AssignedNodeAnnotations:                  "node1",
				device.SupportDevices[nvidia.NvidiaGPUDevice]: "GPU0,NVIDIA,20000:;",
			},
		},
		Spec: corev1.PodSpec{NodeName: "node1"},
	}

	s.onAddPod(pod)

	select {
	case event := <-recorder.Events:
		assert.Assert(t, strings.Contains(event, EventReasonAllocationDecodeFailed), "event: %q", event)
		assert.Assert(t, strings.Contains(event, "node1"), "event: %q", event)
	case <-time.After(time.Second):
		t.Fatal("expected allocation decode failure event")
	}
}

func TestStartReportsUndecodableAllocationDuringInitialReplay(t *testing.T) {
	initReplayDevices(t)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "startup-decode-failure",
			Name:      "startup-decode-failure",
			Namespace: "default",
			Annotations: map[string]string{
				util.AssignedNodeAnnotations:                  "node1",
				device.SupportDevices[nvidia.NvidiaGPUDevice]: "GPU0,NVIDIA,20000:;",
			},
		},
		Spec: corev1.PodSpec{NodeName: "node1"},
	}
	fakeClient := fake.NewSimpleClientset(pod)
	previousClient := client.KubeClient
	client.KubeClient = fakeClient
	t.Cleanup(func() { client.KubeClient = previousClient })
	previousLeaderElect := config.LeaderElect
	config.LeaderElect = false
	t.Cleanup(func() { config.LeaderElect = previousLeaderElect })

	s := NewScheduler()
	require.NoError(t, s.Start())
	t.Cleanup(s.Stop)

	require.Eventually(t, func() bool {
		events, err := fakeClient.CoreV1().Events(pod.Namespace).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			return false
		}
		for i := range events.Items {
			if events.Items[i].Reason == EventReasonAllocationDecodeFailed {
				return true
			}
		}
		return false
	}, time.Second, 10*time.Millisecond)
}

func Test_onAddPod_ReportsUndecodableAllocationWithMismatchedAssignment(t *testing.T) {
	initReplayDevices(t)
	recorder := record.NewFakeRecorder(1)
	s := NewScheduler()
	s.eventRecorder = recorder
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mismatched-allocation", Namespace: "default",
			Annotations: map[string]string{
				util.AssignedNodeAnnotations:                  "annotated-node",
				device.SupportDevices[nvidia.NvidiaGPUDevice]: "GPU0,NVIDIA,20000:;",
			},
		},
		Spec: corev1.PodSpec{NodeName: "bound-node"},
	}

	s.onAddPod(pod)
	select {
	case event := <-recorder.Events:
		assert.Assert(t, strings.Contains(event, EventReasonAllocationDecodeFailed), "event: %q", event)
		assert.Assert(t, strings.Contains(event, "bound-node"), "event: %q", event)
	case <-time.After(time.Second):
		t.Fatal("expected allocation decode failure event")
	}
}

func Test_onUpdatePod_ReportsUndecodableInitShrink(t *testing.T) {
	initReplayDevices(t)
	recorder := record.NewFakeRecorder(1)
	s := NewScheduler()
	s.eventRecorder = recorder
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID: "shrink-event-uid", Name: "shrink-event", Namespace: "default",
			Annotations: map[string]string{util.AssignedNodeAnnotations: "node1"},
		},
		Spec: corev1.PodSpec{
			NodeName:       "node1",
			InitContainers: []corev1.Container{{Name: "init"}},
			Containers:     []corev1.Container{{Name: "app"}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	validDevices := device.PodDevices{nvidia.NvidiaGPUDevice: device.PodSingleDevice{
		{{UUID: "GPU0", Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 10}},
		{{UUID: "GPU0", Type: nvidia.NvidiaGPUDevice, Usedmem: 10000, Usedcores: 5}},
	}}
	maps.Copy(pod.Annotations, device.EncodePodDevices(device.SupportDevices, validDevices))
	t.Cleanup(func() { s.onDelPod(pod) })
	s.onAddPod(pod)
	before, ok := s.podManager.GetPod(pod)
	require.True(t, ok)

	updated := pod.DeepCopy()
	updated.Status.InitContainerStatuses = []corev1.ContainerStatus{{
		Name: "init", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}},
	}}
	updated.Annotations[device.SupportDevices[nvidia.NvidiaGPUDevice]] = "GPU0,NVIDIA,20000:;"
	s.onUpdatePod(pod, updated)

	select {
	case event := <-recorder.Events:
		assert.Assert(t, strings.Contains(event, EventReasonAllocationDecodeFailed), "event: %q", event)
		assert.Assert(t, strings.Contains(event, "node1"), "event: %q", event)
	case <-time.After(time.Second):
		t.Fatal("expected allocation decode failure event")
	}
	after, ok := s.podManager.GetPod(pod)
	require.True(t, ok)
	require.False(t, after.InitContainerResourceReleased)
	require.Equal(t, before.Devices, after.Devices)
	require.Equal(t, int64(20000), replayQuotaUsage(s, pod.Namespace, "hami.io/gpumem"))

	// A later valid event must still be able to perform the one-time release.
	repaired := updated.DeepCopy()
	maps.Copy(repaired.Annotations, device.EncodePodDevices(device.SupportDevices, validDevices))
	s.onUpdatePod(updated, repaired)
	after, ok = s.podManager.GetPod(pod)
	require.True(t, ok)
	require.True(t, after.InitContainerResourceReleased)
	require.Equal(t, device.SteadyStateDeviceUsage(repaired, validDevices), after.Devices)
	require.Equal(t, int64(10000), replayQuotaUsage(s, pod.Namespace, "hami.io/gpumem"))
}

// Unrelated Pod events must not wait for allocation or NUMA refit operations.
func TestPodWithoutAssignmentDoesNotWaitForAllocLock(t *testing.T) {
	initReplayDevices(t)
	for _, event := range []string{"add", "update"} {
		for _, state := range []string{"pending", "running", "terminating"} {
			t.Run(event+"/"+state, func(t *testing.T) {
				s := NewScheduler()
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{UID: k8stypes.UID(t.Name()), Name: "ordinary", Namespace: "default"},
					Status:     corev1.PodStatus{Phase: corev1.PodPending},
				}
				if state != "pending" {
					pod.Status.Phase = corev1.PodRunning
					pod.Annotations = map[string]string{"unrelated": "annotation"}
				}
				if state == "terminating" {
					now := metav1.Now()
					pod.DeletionTimestamp = &now
				}
				s.allocLock.Lock()
				done := make(chan struct{})
				t.Cleanup(func() {
					s.allocLock.Unlock()
					select {
					case <-done:
					case <-time.After(time.Second):
						t.Error("Pod handler did not finish after allocLock was released")
					}
				})
				go func() {
					if event == "add" {
						s.onAddPod(pod)
					} else {
						s.onUpdatePod(pod.DeepCopy(), pod)
					}
					close(done)
				}()
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Fatal("Pod without a HAMi assignment waited for allocLock")
				}
				_, cached := s.podManager.GetPod(pod)
				require.False(t, cached)
			})
		}
	}
}

// Terminal events must remove cached reservations even if annotations are gone.
func TestPodTerminatedWithoutAssignmentRemovesCachedUsage(t *testing.T) {
	initReplayDevices(t)
	for _, event := range []string{"add", "update"} {
		for _, phase := range []corev1.PodPhase{corev1.PodSucceeded, corev1.PodFailed} {
			t.Run(event+"/"+string(phase), func(t *testing.T) {
				s := NewScheduler()
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						UID: "terminal-without-assignment", Name: "terminal", Namespace: "default",
						Annotations: map[string]string{util.AssignedNodeAnnotations: "node1"},
					},
					Spec: corev1.PodSpec{NodeName: "node1"},
				}
				devices := device.PodDevices{nvidia.NvidiaGPUDevice: device.PodSingleDevice{
					{{UUID: "GPU0", Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 100}},
				}}
				maps.Copy(pod.Annotations, device.EncodePodDevices(device.SupportDevices, devices))
				s.onAddPod(pod)
				t.Cleanup(func() { s.onDelPod(pod) })
				require.Equal(t, int64(20000), replayQuotaUsage(s, pod.Namespace, "hami.io/gpumem"))

				terminated := pod.DeepCopy()
				terminated.Status.Phase = phase
				terminated.Annotations = nil
				for range 2 {
					if event == "add" {
						s.onAddPod(terminated)
					} else {
						s.onUpdatePod(pod, terminated)
					}

					_, cached := s.podManager.GetPod(terminated)
					assert.Assert(t, !cached)
					assert.Equal(t, replayQuotaUsage(s, pod.Namespace, "hami.io/gpumem"), int64(0))
				}
			})
		}
	}
}

func TestPodReplayRestoresInitContainerUsage(t *testing.T) {
	initReplayDevices(t)
	always := corev1.ContainerRestartPolicyAlways
	allocation := func(uuid string, memory, cores int64) device.ContainerDevices {
		return device.ContainerDevices{{UUID: uuid, Type: nvidia.NvidiaGPUDevice, Usedmem: memory, Usedcores: cores}}
	}
	succeeded := func(name string) corev1.ContainerStatus {
		return corev1.ContainerStatus{Name: name, State: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{ExitCode: 0},
		}}
	}
	type usage struct{ memory, cores, slots int64 }
	for _, tc := range []struct {
		name     string
		init     []corev1.Container
		statuses []corev1.ContainerStatus
		rows     device.PodSingleDevice
		want     map[string]usage
		released bool
	}{
		{
			name: "completed init on app GPU", init: []corev1.Container{{Name: "init"}},
			statuses: []corev1.ContainerStatus{succeeded("init")},
			rows:     device.PodSingleDevice{allocation("GPU0", 20000, 60), allocation("GPU0", 4000, 10)},
			want:     map[string]usage{"GPU0": {4000, 10, 1}}, released: true,
		},
		{
			name: "completed init on another GPU", init: []corev1.Container{{Name: "init"}},
			statuses: []corev1.ContainerStatus{succeeded("init")},
			rows:     device.PodSingleDevice{allocation("GPU0", 20000, 60), allocation("GPU1", 4000, 10)},
			want:     map[string]usage{"GPU1": {4000, 10, 1}}, released: true,
		},
		{
			name:     "completed init with running sidecar",
			init:     []corev1.Container{{Name: "sidecar", RestartPolicy: &always}, {Name: "init"}},
			statuses: []corev1.ContainerStatus{succeeded("init")},
			rows: device.PodSingleDevice{
				allocation("GPU0", 2000, 5), allocation("GPU0", 20000, 60), allocation("GPU0", 4000, 10),
			},
			want: map[string]usage{"GPU0": {6000, 15, 2}}, released: true,
		},
		{
			name: "completed init is the only GPU consumer", init: []corev1.Container{{Name: "init"}},
			statuses: []corev1.ContainerStatus{succeeded("init")},
			rows:     device.PodSingleDevice{allocation("GPU0", 20000, 60), {}},
			want:     map[string]usage{}, released: true,
		},
		{
			name: "init status missing", init: []corev1.Container{{Name: "init"}},
			rows: device.PodSingleDevice{allocation("GPU0", 20000, 60), allocation("GPU0", 4000, 10)},
			want: map[string]usage{"GPU0": {20000, 60, 1}},
		},
		{
			name: "init failed", init: []corev1.Container{{Name: "init"}},
			statuses: []corev1.ContainerStatus{{Name: "init", State: corev1.ContainerState{
				Terminated: &corev1.ContainerStateTerminated{ExitCode: 1},
			}}},
			rows: device.PodSingleDevice{allocation("GPU0", 20000, 60), allocation("GPU0", 4000, 10)},
			want: map[string]usage{"GPU0": {20000, 60, 1}},
		},
		{
			name: "later init status missing", init: []corev1.Container{{Name: "init"}, {Name: "later"}},
			statuses: []corev1.ContainerStatus{succeeded("init")},
			rows: device.PodSingleDevice{
				allocation("GPU0", 20000, 60), allocation("GPU0", 8000, 20), allocation("GPU0", 4000, 10),
			},
			want: map[string]usage{"GPU0": {20000, 60, 1}},
		},
	} {
		for _, event := range []struct {
			name        string
			update      bool
			cached      bool
			terminating bool
		}{
			{name: "add"}, {name: "uncached update", update: true},
			{name: "cached update", update: true, cached: true},
			{name: "terminating add", terminating: true},
			{name: "terminating uncached update", update: true, terminating: true},
		} {
			t.Run(tc.name+"/"+event.name, func(t *testing.T) {
				s := NewScheduler()
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						UID: k8stypes.UID(t.Name()), Name: "replayed", Namespace: "init-replay",
						Annotations: map[string]string{util.AssignedNodeAnnotations: "node1"},
					},
					Spec: corev1.PodSpec{
						NodeName: "node1", InitContainers: tc.init, Containers: []corev1.Container{{Name: "app"}},
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, InitContainerStatuses: tc.statuses},
				}
				if event.terminating {
					now := metav1.Now()
					pod.DeletionTimestamp = &now
				}
				maps.Copy(pod.Annotations, device.EncodePodDevices(device.SupportDevices,
					device.PodDevices{nvidia.NvidiaGPUDevice: tc.rows}))
				quota := &corev1.ResourceQuota{
					ObjectMeta: metav1.ObjectMeta{Name: "gpu-memory", Namespace: pod.Namespace},
					Spec: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{
						"limits.hami.io/gpumem": resource.MustParse("25000"),
					}},
				}
				s.quotaManager.AddQuota(quota)
				t.Cleanup(func() { s.onDelPod(pod); s.quotaManager.DelQuota(quota) })
				if event.cached {
					initial := pod.DeepCopy()
					initial.Status.InitContainerStatuses = nil
					s.onAddPod(initial)
				}
				s.addNode("node1", &device.NodeInfo{
					ID: "node1", Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
					Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: {
						{ID: "GPU0", Type: nvidia.NvidiaGPUDevice, DeviceVendor: nvidia.NvidiaGPUDevice, Count: 10, Devmem: 40000, Devcore: 100, Health: true},
						{ID: "GPU1", Type: nvidia.NvidiaGPUDevice, DeviceVendor: nvidia.NvidiaGPUDevice, Count: 10, Devmem: 40000, Devcore: 100, Health: true},
					}},
				})
				// Replaying the same object must also leave quota accounting stable.
				for range 2 {
					if event.update {
						s.onUpdatePod(pod.DeepCopy(), pod)
					} else {
						s.onAddPod(pod)
					}
					pi, ok := s.podManager.GetPod(pod)
					require.True(t, ok)
					if pi.InitContainerResourceReleased != tc.released {
						t.Errorf("init resources released = %t, want %t", pi.InitContainerResourceReleased, tc.released)
					}
					_, nodes, _, err := s.getNodesUsage(nil, nil)
					require.NoError(t, err)
					for _, dl := range (*nodes)["node1"].Devices.DeviceLists {
						got := usage{dl.Device.Usedmem, dl.Device.Usedcores, dl.Device.Used}
						if got != tc.want[dl.Device.ID] {
							t.Errorf("%s usage = %+v, want %+v", dl.Device.ID, got, tc.want[dl.Device.ID])
						}
					}
					var wantMemory, wantCores int64
					for _, want := range tc.want {
						wantMemory += want.memory
						wantCores += want.cores
					}
					require.Equal(t, wantMemory, replayQuotaUsage(s, pod.Namespace, "hami.io/gpumem"))
					require.Equal(t, wantCores, replayQuotaUsage(s, pod.Namespace, "hami.io/gpucores"))
					if fit := s.quotaManager.FitQuota(pod.Namespace, 10000, 1, 0, nvidia.NvidiaGPUDevice); fit != (wantMemory+10000 <= 25000) {
						t.Errorf("quota fit = %t with restored usage %d", fit, wantMemory)
					}
				}
				if tc.released {
					// A re-add of an older annotation must preserve an already
					// released reservation, including a subsequent NUMA refit.
					refitted := device.PodDevices{nvidia.NvidiaGPUDevice: {
						{{UUID: "GPU1", Type: nvidia.NvidiaGPUDevice, Usedmem: 8000, Usedcores: 20, Slots: 1}},
					}}
					oldDevices, ok := s.podManager.ReplacePodDevices(pod, refitted)
					require.True(t, ok)
					s.quotaManager.ReplaceUsage(pod, oldDevices, refitted)
					s.onAddPod(pod)
					s.onUpdatePod(pod.DeepCopy(), pod)
					pi, ok := s.podManager.GetPod(pod)
					require.True(t, ok)
					require.Equal(t, refitted, pi.Devices)
					require.Equal(t, int64(8000), replayQuotaUsage(s, pod.Namespace, "hami.io/gpumem"))
				}
			})
		}
	}
}

func TestInformerReplayRestoresSchedulingCapacity(t *testing.T) {
	initReplayDevices(t)
	s := NewScheduler()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID: "informer-init-replay", Name: "existing", Namespace: "informer-init-replay",
			Annotations: map[string]string{util.AssignedNodeAnnotations: "node1"},
		},
		Spec: corev1.PodSpec{
			NodeName: "node1",
			InitContainers: []corev1.Container{{Name: "init", Image: "example/init",
				Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
					"hami.io/gpu": resource.MustParse("1"), "hami.io/gpumem": resource.MustParse("20000"),
					"hami.io/gpucores": resource.MustParse("60"),
				}},
			}},
			Containers: []corev1.Container{{Name: "app", Image: "example/app",
				Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
					"hami.io/gpu": resource.MustParse("1"), "hami.io/gpumem": resource.MustParse("4000"),
					"hami.io/gpucores": resource.MustParse("10"),
				}},
			}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			InitContainerStatuses: []corev1.ContainerStatus{{Name: "init", State: corev1.ContainerState{
				Terminated: &corev1.ContainerStateTerminated{ExitCode: 0},
			}}},
		},
	}
	maps.Copy(pod.Annotations, device.EncodePodDevices(device.SupportDevices,
		device.PodDevices{nvidia.NvidiaGPUDevice: {
			{{UUID: "GPU0", Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 60}},
			{{UUID: "GPU0", Type: nvidia.NvidiaGPUDevice, Usedmem: 4000, Usedcores: 10}},
		}}))
	s.addNode("node1", &device.NodeInfo{
		ID: "node1", Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
		Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: {
			{ID: "GPU0", Type: nvidia.NvidiaGPUDevice, DeviceVendor: nvidia.NvidiaGPUDevice,
				Count: 10, Devmem: 24000, Devcore: 100, Health: true},
		}},
	})
	// An existing running pod is delivered by the initial LIST, without any
	// later status update to trigger the normal init-container release path.
	s.kubeClient = fake.NewClientset(pod)
	previousClient := client.KubeClient
	client.KubeClient = s.kubeClient
	t.Cleanup(func() { client.KubeClient = previousClient })
	factory := informers.NewSharedInformerFactory(s.kubeClient, time.Hour)
	s.podLister = factory.Core().V1().Pods().Lister()
	informer := factory.Core().V1().Pods().Informer()
	handler, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{AddFunc: s.onAddPod})
	require.NoError(t, err)
	stop := make(chan struct{})
	t.Cleanup(func() {
		close(stop)
		factory.Shutdown()
		s.onDelPod(pod)
	})
	factory.Start(stop)
	require.True(t, cache.WaitForCacheSync(stop, informer.HasSynced, handler.HasSynced))

	pending := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{UID: "pending-init-replay", Name: "pending", Namespace: pod.Namespace},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "example/app",
			Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
				"hami.io/gpu": resource.MustParse("1"), "hami.io/gpumem": resource.MustParse("10000"),
				"hami.io/gpucores": resource.MustParse("20"),
			}},
		}}},
	}
	_, err = s.kubeClient.CoreV1().Pods(pending.Namespace).Create(context.Background(), pending, metav1.CreateOptions{})
	require.NoError(t, err)
	t.Cleanup(func() { s.onDelPod(pending) })
	result, err := s.Filter(extenderv1.ExtenderArgs{Pod: pending, NodeNames: &[]string{"node1"}})
	require.NoError(t, err)
	require.NotNil(t, result.NodeNames, "completed init usage must not block a 10000 MiB request: %v", result.FailedNodes)
	require.Equal(t, []string{"node1"}, *result.NodeNames)
	stored, err := s.kubeClient.CoreV1().Pods(pending.Namespace).Get(context.Background(), pending.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "node1", stored.Annotations[util.AssignedNodeAnnotations])
	allocated, err := device.DecodePodDevices(device.SupportDevices, stored.Annotations)
	require.NoError(t, err)
	require.NotEmpty(t, allocated[nvidia.NvidiaGPUDevice])
	require.Len(t, allocated[nvidia.NvidiaGPUDevice][0], 1)
	require.Equal(t, "GPU0", allocated[nvidia.NvidiaGPUDevice][0][0].UUID)
	require.Equal(t, int64(10000), allocated[nvidia.NvidiaGPUDevice][0][0].Usedmem)
	require.Equal(t, int64(14000), replayQuotaUsage(s, pod.Namespace, "hami.io/gpumem"))
}
