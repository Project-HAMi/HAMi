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
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
	"github.com/Project-HAMi/HAMi/pkg/util/leaderelection"
)

const gangNode = "gang-node"

// gangPod returns a PodGroup member that needs a whole GPU (all 8192 MiB of
// a testNodeInfo device), so one GPU fits exactly one member.
func gangPod(group string, i int) *corev1.Pod {
	name := fmt.Sprintf("%s-%d", group, i)
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       k8stypes.UID(name + "-uid"),
			Name:      name,
			Namespace: "default",
			Labels:    map[string]string{util.PodGroupLabel: group},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "main",
			Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
				"hami.io/gpu": *resource.NewQuantity(1, resource.BinarySI),
				moveMem:       *resource.NewQuantity(8192, resource.BinarySI),
			}},
		}}},
	}
}

// gangScheduler returns a scheduler with one node of gpus whole GPUs and the
// given pods stored in a fake API server.
func gangScheduler(t *testing.T, gpus int, pods ...*corev1.Pod) (*Scheduler, *fake.Clientset) {
	t.Helper()
	initMoveDevices(t)
	s := NewScheduler()
	s.quotaManager = &device.QuotaManager{Quotas: map[string]*device.DeviceQuota{}}
	kubeClient := fake.NewClientset()
	oldClient := client.KubeClient
	client.KubeClient = kubeClient
	t.Cleanup(func() { client.KubeClient = oldClient })
	s.kubeClient = kubeClient
	s.addNode(gangNode, testNodeInfo(gangNode, gpus))
	seedPods(t, s, pods...)
	return s, kubeClient
}

// gangFilter runs Filter for pod and reports whether it reserved a GPU.
func gangFilter(t *testing.T, s *Scheduler, pod *corev1.Pod) bool {
	t.Helper()
	res, err := s.Filter(extenderv1.ExtenderArgs{Pod: pod, NodeNames: &[]string{gangNode}})
	require.NoError(t, err)
	return res.NodeNames != nil && len(*res.NodeNames) == 1
}

// livePod returns the pod as the fake API server holds it.
func livePod(t *testing.T, kubeClient *fake.Clientset, pod *corev1.Pod) *corev1.Pod {
	t.Helper()
	live, err := kubeClient.CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
	require.NoError(t, err)
	return live
}

// storeVersion stores pod in the fake API server with resourceVersion rv,
// which the fake does not set itself, and returns it.
func storeVersion(t *testing.T, kubeClient *fake.Clientset, pod *corev1.Pod, rv string) *corev1.Pod {
	t.Helper()
	pod = pod.DeepCopy()
	pod.ResourceVersion = rv
	stored, err := kubeClient.CoreV1().Pods(pod.Namespace).Update(context.Background(), pod, metav1.UpdateOptions{})
	require.NoError(t, err)
	return stored
}

// withSchedulingFailure returns pod as kube-scheduler leaves it after giving
// up a scheduling cycle, for example when a gang's Permit wait times out.
func withSchedulingFailure(pod *corev1.Pod, message string) *corev1.Pod {
	failed := pod.DeepCopy()
	failed.Status.Conditions = []corev1.PodCondition{{
		Type:               corev1.PodScheduled,
		Status:             corev1.ConditionFalse,
		Reason:             corev1.PodReasonUnschedulable,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}}
	return failed
}

// permitTimeout delivers the informer updates for pod: first HAMi's own
// Filter patch, then kube-scheduler's PodScheduled=False status.
func permitTimeout(t *testing.T, s *Scheduler, kubeClient *fake.Clientset, pod *corev1.Pod) {
	t.Helper()
	patched := livePod(t, kubeClient, pod)
	s.onUpdatePod(pod, patched)
	s.onUpdatePod(patched, withSchedulingFailure(patched, "rejected due to timeout after waiting 1m0s at plugin Coscheduling"))
}

// TestPodGroupDeadlockReleasesReservationsOnPermitTimeout is the regression
// test for #3484. Two gangs of four split a 4-GPU node two and two. When
// kube-scheduler gives up gang B at Permit, HAMi must release B's GPUs so
// gang A can finish; before the fix they stayed reserved until each B pod
// was filtered again or deleted.
func TestPodGroupDeadlockReleasesReservationsOnPermitTimeout(t *testing.T) {
	var a, b []*corev1.Pod
	for i := 1; i <= 4; i++ {
		a = append(a, gangPod("job-a", i))
		b = append(b, gangPod("job-b", i))
	}
	s, kubeClient := gangScheduler(t, 4, append(append([]*corev1.Pod{}, a...), b...)...)

	// Queue order A1, B1, A2, B2: each gang holds half the node.
	for i := range 2 {
		require.True(t, gangFilter(t, s, a[i]), "a%d", i+1)
		require.True(t, gangFilter(t, s, b[i]), "b%d", i+1)
	}
	require.False(t, gangFilter(t, s, a[2]), "the node is full, a3 must not fit yet")

	// Gang B times out at Permit and is sent back to the queue.
	for i := range 2 {
		permitTimeout(t, s, kubeClient, b[i])
	}

	for i := 2; i < 4; i++ {
		require.True(t, gangFilter(t, s, a[i]), "a%d must fit once gang B released its GPUs", i+1)
	}
	for i := range 2 {
		_, held := s.podManager.GetPod(b[i])
		require.False(t, held, "b%d must not hold a reservation", i+1)
	}
}

// TestReleasedReservationIsNotAddedBack checks that later informer events for
// a released pod, which still carries the old annotations, do not count its
// GPU again.
func TestReleasedReservationIsNotAddedBack(t *testing.T) {
	pod := gangPod("job", 1)
	s, kubeClient := gangScheduler(t, 1, pod)
	require.True(t, gangFilter(t, s, pod))
	permitTimeout(t, s, kubeClient, pod)

	failed := withSchedulingFailure(livePod(t, kubeClient, pod), "timeout")
	relabeled := failed.DeepCopy()
	relabeled.Labels["touched"] = "true"
	s.onUpdatePod(failed, relabeled)
	s.onAddPod(relabeled) // as a restarted informer replays it

	_, held := s.podManager.GetPod(pod)
	require.False(t, held)
	other := gangPod("other", 1)
	seedPods(t, s, other)
	require.True(t, gangFilter(t, s, other), "the released GPU must be free")
}

// TestResyncDoesNotReleaseReservation checks that an update where the
// PodScheduled condition did not change, as on a resync, keeps the
// reservation of a pod waiting at Permit.
func TestResyncDoesNotReleaseReservation(t *testing.T) {
	pod := gangPod("job", 1)
	s, kubeClient := gangScheduler(t, 1, pod)
	// An earlier cycle failed before HAMi reserved anything.
	earlier := withSchedulingFailure(pod, "waiting for the gang")
	require.True(t, gangFilter(t, s, pod))
	patched := livePod(t, kubeClient, pod)
	patched.Status = earlier.Status
	s.onUpdatePod(patched, patched.DeepCopy())

	_, held := s.podManager.GetPod(pod)
	require.True(t, held, "an unchanged condition is not a new failure")
}

// TestBoundOrAllocatingPodIsNotReleased checks that a reservation is kept
// once the pod is bound or Bind has started allocating it.
func TestBoundOrAllocatingPodIsNotReleased(t *testing.T) {
	for name, mutate := range map[string]func(*corev1.Pod){
		"bound":      func(p *corev1.Pod) { p.Spec.NodeName = gangNode },
		"allocating": func(p *corev1.Pod) { p.Annotations[util.DeviceBindPhase] = util.DeviceBindAllocating },
	} {
		t.Run(name, func(t *testing.T) {
			pod := gangPod("job", 1)
			s, kubeClient := gangScheduler(t, 1, pod)
			require.True(t, gangFilter(t, s, pod))
			patched := livePod(t, kubeClient, pod)
			mutate(patched)
			s.onUpdatePod(pod, patched)
			s.onUpdatePod(patched, withSchedulingFailure(patched, "bind failed"))

			_, held := s.podManager.GetPod(pod)
			require.True(t, held)
		})
	}
}

// TestRefilterAfterReleaseReservesAgain checks that the next Filter of a
// released pod reserves and counts its GPU again.
func TestRefilterAfterReleaseReservesAgain(t *testing.T) {
	pod := gangPod("job", 1)
	s, kubeClient := gangScheduler(t, 1, pod)
	require.True(t, gangFilter(t, s, pod))
	permitTimeout(t, s, kubeClient, pod)

	// Assigned time has a one-second resolution; a new Filter in the same
	// second must still win over the released allocation.
	require.True(t, gangFilter(t, s, pod))
	_, held := s.podManager.GetPod(pod)
	require.True(t, held)
	refiltered := livePod(t, kubeClient, pod)
	resync := withSchedulingFailure(refiltered, "timeout")
	s.onUpdatePod(resync, resync.DeepCopy())
	_, held = s.podManager.GetPod(pod)
	require.True(t, held, "a resync after the new Filter must keep it")
	require.False(t, s.reservationReleased(refiltered))

	other := gangPod("other", 1)
	seedPods(t, s, other)
	require.False(t, gangFilter(t, s, other), "the GPU is reserved again")
}

// TestDeletedPodForgetsRelease checks that the release marker does not
// outlive the pod.
func TestDeletedPodForgetsRelease(t *testing.T) {
	pod := gangPod("job", 1)
	s, kubeClient := gangScheduler(t, 1, pod)
	require.True(t, gangFilter(t, s, pod))
	permitTimeout(t, s, kubeClient, pod)
	require.Len(t, s.releasedReservations, 1)

	s.onDelPod(livePod(t, kubeClient, pod))
	require.Empty(t, s.releasedReservations)
}

// TestReleaseOnUncachedReplica checks that a replica which never cached the
// reservation, like one started after the Filter, does not add it from the
// annotations once a failure is reported.
func TestReleaseOnUncachedReplica(t *testing.T) {
	pod := gangPod("job", 1)
	leader, kubeClient := gangScheduler(t, 1, pod)
	require.True(t, gangFilter(t, leader, pod))
	patched := livePod(t, kubeClient, pod)

	standby := moveReplica(t)
	standby.addNode(gangNode, testNodeInfo(gangNode, 1))
	standby.onUpdatePod(patched, withSchedulingFailure(patched, "timeout"))
	_, held := standby.podManager.GetPod(pod)
	require.False(t, held)
	standby.onAddPod(withSchedulingFailure(patched, "timeout"))
	_, held = standby.podManager.GetPod(pod)
	require.False(t, held)
}

// TestFailureBeforeNewerFilterKeepsReservation checks that a failure report
// for an earlier cycle, delivered after a newer Filter on this replica that
// the informer does not show yet, does not release the newer reservation.
func TestFailureBeforeNewerFilterKeepsReservation(t *testing.T) {
	pod := gangPod("job", 1)
	s, kubeClient := gangScheduler(t, 1, pod)

	// Cycle A: Filter, and the informer shows its patch.
	require.True(t, gangFilter(t, s, pod))
	cycleA := livePod(t, kubeClient, pod)
	cycleA.Annotations[util.AssignedTimeAnnotations] = strconv.FormatInt(time.Now().Add(-time.Minute).Unix(), 10)
	_, err := kubeClient.CoreV1().Pods(cycleA.Namespace).Update(context.Background(), cycleA, metav1.UpdateOptions{})
	require.NoError(t, err)
	s.onUpdatePod(pod, cycleA)

	// Cycle B: a newer Filter whose patch the informer has not shown yet.
	require.True(t, gangFilter(t, s, pod))

	// The informer now delivers cycle A's failure.
	s.onUpdatePod(cycleA, withSchedulingFailure(cycleA, "cycle A failed"))
	_, held := s.podManager.GetPod(pod)
	require.True(t, held, "cycle B's reservation must survive cycle A's failure")
	require.Empty(t, s.releasedReservations)

	// Cycle B's own failure, which shows its patch, still releases it.
	cycleB := livePod(t, kubeClient, pod)
	s.onUpdatePod(cycleA, cycleB)
	s.onUpdatePod(cycleB, withSchedulingFailure(cycleB, "cycle B failed"))
	_, held = s.podManager.GetPod(pod)
	require.False(t, held)
}

// TestReleaseClearsAllocationForRestart checks that a release removes the
// allocation from the pod, so that a restarted scheduler, which has no
// in-memory marker, does not count the released GPU again.
func TestReleaseClearsAllocationForRestart(t *testing.T) {
	pod := gangPod("job", 1)
	s, kubeClient := gangScheduler(t, 1, pod)
	require.True(t, gangFilter(t, s, pod))
	storeVersion(t, kubeClient, livePod(t, kubeClient, pod), "2")
	permitTimeout(t, s, kubeClient, pod)
	s.allocationCleanups.Wait()

	live := livePod(t, kubeClient, pod)
	require.NotContains(t, live.Annotations, util.AssignedNodeAnnotations)
	require.NotContains(t, live.Annotations, util.AssignedTimeAnnotations)
	for _, key := range device.SupportDevices {
		require.NotContains(t, live.Annotations, key)
	}

	restarted := NewScheduler()
	restarted.quotaManager = &device.QuotaManager{Quotas: map[string]*device.DeviceQuota{}}
	restarted.kubeClient = kubeClient
	restarted.addNode(gangNode, testNodeInfo(gangNode, 1))
	restarted.onAddPod(live) // the informer's initial sync
	_, held := restarted.podManager.GetPod(pod)
	require.False(t, held)
	other := gangPod("other", 1)
	seedPods(t, restarted, other)
	require.True(t, gangFilter(t, restarted, other), "the released GPU must be free after a restart")
}

// TestClearReleasedAllocationAfterUnrelatedUpdate checks that an update of the
// pod between the failure report and the cleanup, such as a label change, does
// not leave the released allocation on the pod for a restart to count again.
func TestClearReleasedAllocationAfterUnrelatedUpdate(t *testing.T) {
	pod := gangPod("job", 1)
	s, kubeClient := gangScheduler(t, 1, pod)
	require.True(t, gangFilter(t, s, pod))
	patched := storeVersion(t, kubeClient, livePod(t, kubeClient, pod), "2")
	s.onUpdatePod(pod, patched)

	relabeled := patched.DeepCopy()
	relabeled.Labels["touched"] = "true"
	storeVersion(t, kubeClient, relabeled, "3")
	// The informer delivers the failure before it shows the label update.
	s.onUpdatePod(patched, withSchedulingFailure(patched, "timeout"))
	s.allocationCleanups.Wait()

	live := livePod(t, kubeClient, pod)
	require.NotContains(t, live.Annotations, util.AssignedNodeAnnotations)
	require.NotContains(t, live.Annotations, util.AssignedTimeAnnotations)
	require.Equal(t, "true", live.Labels["touched"])
}

// TestClearReleasedAllocationKeepsNewerAllocation checks that the cleanup of a
// released allocation does not remove one that a later Filter wrote.
func TestClearReleasedAllocationKeepsNewerAllocation(t *testing.T) {
	pod := gangPod("job", 1)
	s, kubeClient := gangScheduler(t, 1, pod)
	require.True(t, gangFilter(t, s, pod))
	failed := withSchedulingFailure(storeVersion(t, kubeClient, livePod(t, kubeClient, pod), "2"), "timeout")

	// A later Filter in the same second writes the same node and time, so
	// only the resourceVersion tells the allocations apart.
	newer := storeVersion(t, kubeClient, livePod(t, kubeClient, pod), "3")

	s.clearReleasedAllocation(failed)
	s.allocationCleanups.Wait()
	require.Equal(t, newer.Annotations, livePod(t, kubeClient, pod).Annotations)
}

// TestStandbyDoesNotClearReleasedAllocation checks that only the leader writes
// the cleanup; a standby releases its own copy and keeps the marker.
func TestStandbyDoesNotClearReleasedAllocation(t *testing.T) {
	pod := gangPod("job", 1)
	s, kubeClient := gangScheduler(t, 1, pod)
	require.True(t, gangFilter(t, s, pod))
	s.leaderManager = leaderelection.NewDummyLeaderManager(false)
	permitTimeout(t, s, kubeClient, pod)
	s.allocationCleanups.Wait()

	_, held := s.podManager.GetPod(pod)
	require.False(t, held)
	require.Contains(t, livePod(t, kubeClient, pod).Annotations, util.AssignedNodeAnnotations)
}

func TestReleasedAllocationPatch(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{ResourceVersion: "7", Annotations: map[string]string{
		util.AssignedNodeAnnotations: "node-1",
		util.AssignedTimeAnnotations: "100",
	}}}
	patch, err := releasedAllocationPatch(pod)
	require.NoError(t, err)
	require.JSONEq(t, `[
		{"op":"test","path":"/metadata/resourceVersion","value":"7"},
		{"op":"test","path":"/metadata/annotations/hami.io~1vgpu-node","value":"node-1"},
		{"op":"test","path":"/metadata/annotations/hami.io~1vgpu-time","value":"100"},
		{"op":"remove","path":"/metadata/annotations/hami.io~1vgpu-node"},
		{"op":"remove","path":"/metadata/annotations/hami.io~1vgpu-time"}
	]`, string(patch))

	patch, err = releasedAllocationPatch(&corev1.Pod{})
	require.NoError(t, err)
	require.Nil(t, patch, "nothing to clear without an allocation")
}
