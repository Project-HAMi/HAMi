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
	"testing"
	"time"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

// asking returns a pod that waits for a node and asks for mem MiB of one GPU.
func asking(name string, priority int32, mem int64) *corev1.Pod {
	pod := movePod()
	pod.Name, pod.UID = name, k8stypes.UID(name)
	pod.Spec.Priority = &priority
	pod.Spec.Containers[0].Resources.Limits[moveMem] = *resource.NewQuantity(mem, resource.BinarySI)
	return pod
}

// nominationScheduler returns a scheduler whose lister holds the given pods,
// so a test nominates one by setting its status. node-x has one 8192 MiB GPU.
func nominationScheduler(t *testing.T, pods ...*corev1.Pod) *Scheduler {
	t.Helper()
	initMoveDevices(t)
	s := schedulerWithPods(t, pods...)
	s.quotaManager = &device.QuotaManager{Quotas: map[string]*device.DeviceQuota{}}
	oldClient := client.KubeClient
	client.KubeClient = s.kubeClient
	t.Cleanup(func() { client.KubeClient = oldClient })
	s.addNode(moveNodeX, testNodeInfo(moveNodeX, 1))
	return s
}

// preemptOn runs a preemption for pod on node and checks who has to go.
func preemptOn(t *testing.T, s *Scheduler, pod *corev1.Pod, node string, victims ...string) {
	t.Helper()
	want := &extenderv1.MetaVictims{Pods: []*extenderv1.MetaPod{}}
	for _, uid := range victims {
		want.Pods = append(want.Pods, &extenderv1.MetaPod{UID: uid})
	}
	res, err := s.Preempt(extenderv1.ExtenderPreemptionArgs{
		Pod:                   pod,
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{node: {}},
	})
	assert.NilError(t, err)
	assert.DeepEqual(t, res.NodeNameToMetaVictims, map[string]*extenderv1.MetaVictims{node: want})
}

// filterOn runs Filter for pod on node and reports whether the pod got it.
func filterOn(t *testing.T, s *Scheduler, pod *corev1.Pod, node string) bool {
	t.Helper()
	res, err := s.Filter(extenderv1.ExtenderArgs{Pod: pod, NodeNames: &[]string{node}})
	assert.NilError(t, err)
	return res.NodeNames != nil && len(*res.NodeNames) == 1 && (*res.NodeNames)[0] == node
}

// Until a nominated pod gets its node, its planned devices are held back from
// pods of the same or a lower priority.
func Test_Preempt_HoldsDevicesForNominatedPod(t *testing.T) {
	// high asks for 4500 of 8192 MiB, and low is evicted for it. 3692 MiB stay
	// free next to what is held back.
	evicted := func(t *testing.T, others ...*corev1.Pod) (*Scheduler, *corev1.Pod) {
		t.Helper()
		high := asking("high", 1000, 4500)
		s := nominationScheduler(t, append([]*corev1.Pod{high}, others...)...)
		low := holding(trackedOn(moveNodeX, "low", 10), 8000)
		track(t, s, low)
		preemptOn(t, s, high, moveNodeX, "low")
		s.podManager.DelPod(low.Pod)
		return s, high
	}

	t.Run("nothing is held back before the pod is nominated", func(t *testing.T) {
		other := asking("other", 10, 5000)
		s, _ := evicted(t, other)
		assert.Assert(t, filterOn(t, s, other, moveNodeX))
		assert.Equal(t, len(s.preemptionPlans), 1, "the plan has to wait for the nomination")
	})

	t.Run("a pod of a lower priority does not get what is held back", func(t *testing.T) {
		other := asking("other", 10, 5000)
		s, high := evicted(t, other)
		high.Status.NominatedNodeName = moveNodeX
		assert.Assert(t, !filterOn(t, s, other, moveNodeX))
	})

	t.Run("nor does a pod of the same priority", func(t *testing.T) {
		other := asking("other", 1000, 5000)
		s, high := evicted(t, other)
		high.Status.NominatedNodeName = moveNodeX
		assert.Assert(t, !filterOn(t, s, other, moveNodeX))
	})

	t.Run("a pod that fits next to it is not turned away", func(t *testing.T) {
		other := asking("other", 10, 3500)
		s, high := evicted(t, other)
		high.Status.NominatedNodeName = moveNodeX
		assert.Assert(t, filterOn(t, s, other, moveNodeX))
	})

	t.Run("a pod of a higher priority takes what it needs", func(t *testing.T) {
		other := asking("other", 2000, 5000)
		s, high := evicted(t, other)
		high.Status.NominatedNodeName = moveNodeX
		assert.Assert(t, filterOn(t, s, other, moveNodeX))
	})

	t.Run("the pod itself gets its devices, and nothing is held back after", func(t *testing.T) {
		other := asking("other", 10, 3500)
		s, high := evicted(t, other)
		high.Status.NominatedNodeName = moveNodeX
		assert.Assert(t, filterOn(t, s, high, moveNodeX), "what is held back for the pod is not in its own way")
		assert.Equal(t, len(s.preemptionPlans), 0)
		assert.Assert(t, filterOn(t, s, other, moveNodeX), "the 4500 MiB are in use now, and counted once")
	})

	t.Run("nothing is held back on a node the pod is not nominated to", func(t *testing.T) {
		other := asking("other", 10, 5000)
		s, high := evicted(t, other)
		high.Status.NominatedNodeName = moveNodeY
		assert.Assert(t, filterOn(t, s, other, moveNodeX))
	})

	for name, stopWaiting := range map[string]func(pod *corev1.Pod){
		"is bound": func(pod *corev1.Pod) { pod.Spec.NodeName = moveNodeY },
		"is terminating": func(pod *corev1.Pod) {
			now := metav1.Now()
			pod.DeletionTimestamp = &now
		},
		"has failed":   func(pod *corev1.Pod) { pod.Status.Phase = corev1.PodFailed },
		"was replaced": func(pod *corev1.Pod) { pod.UID = "another-uid" },
	} {
		t.Run("a pod that "+name+" is no longer waiting, and its plan goes", func(t *testing.T) {
			other := asking("other", 10, 5000)
			s, high := evicted(t, other)
			high.Status.NominatedNodeName = moveNodeX
			stopWaiting(high)
			assert.Assert(t, filterOn(t, s, other, moveNodeX))
			assert.Equal(t, len(s.preemptionPlans), 0)
		})
	}

	t.Run("a plan that has timed out holds nothing back, and goes", func(t *testing.T) {
		other := asking("other", 10, 5000)
		s, high := evicted(t, other)
		high.Status.NominatedNodeName = moveNodeX
		plan := s.preemptionPlans[high.UID]
		plan.at = time.Now().Add(-preemptionPlanTimeout + time.Minute)
		assert.Assert(t, !filterOn(t, s, other, moveNodeX), "a plan that is not that old yet is still held back")
		plan.at = time.Now().Add(-preemptionPlanTimeout - time.Second)
		assert.Assert(t, filterOn(t, s, other, moveNodeX))
		assert.Equal(t, len(s.preemptionPlans), 0)
	})

	t.Run("an answer that names no victim leaves no plan behind", func(t *testing.T) {
		s, high := evicted(t)
		// The victim is gone, so the pod fits and nobody has to go.
		preemptOn(t, s, high, moveNodeX)
		assert.Equal(t, len(s.preemptionPlans), 0)
	})

	t.Run("a preemption of a lower priority plans around what is held back", func(t *testing.T) {
		mid := asking("mid", 500, 3500)
		s, high := evicted(t, mid)
		high.Status.NominatedNodeName = moveNodeX
		// 2692 MiB are free next to small, and mid asks for 3500.
		track(t, s, holding(trackedOn(moveNodeX, "small", 10), 1000))
		preemptOn(t, s, mid, moveNodeX, "small")
	})

	t.Run("a preemption of a higher priority does not", func(t *testing.T) {
		urgent := asking("urgent", 2000, 3500)
		s, high := evicted(t, urgent)
		high.Status.NominatedNodeName = moveNodeX
		track(t, s, holding(trackedOn(moveNodeX, "small", 10), 1000))
		preemptOn(t, s, urgent, moveNodeX)
	})
}

// The entry holds the planned devices. A MIG placement the pod carries from an
// earlier attempt is not part of it.
func Test_reservationFor(t *testing.T) {
	pod := movePod()
	pod.Annotations = map[string]string{
		nvidia.MigAllocationsAnnotation: `[{"containerIndex":0,"deviceIndex":0,"gpuUUID":"GPU-mig","profile":"1g.6gb","placement":{"start":0,"size":1}}]`,
		"example.com/kept":              "yes",
	}
	planned := plannedDevices{devices: device.PodDevices{nvidia.NvidiaGPUDevice: {{{
		UUID: moveNodeX + "-gpu-0", Type: nvidia.NvidiaGPUDevice, Usedmem: 1000,
	}}}}}

	held := reservationFor(pod, moveNodeX, planned)

	assert.Equal(t, held.UID, pod.UID)
	assert.Equal(t, held.NodeID, moveNodeX)
	assert.Equal(t, held.Devices[nvidia.NvidiaGPUDevice][0][0].UUID, moveNodeX+"-gpu-0")
	assert.Equal(t, held.Devices[nvidia.NvidiaGPUDevice][0][0].Usedmem, int64(1000))
	_, stale := held.Annotations[nvidia.MigAllocationsAnnotation]
	assert.Assert(t, !stale, "the placement of an earlier attempt must not be held back")
	assert.Equal(t, held.Annotations["example.com/kept"], "yes")
	_, kept := pod.Annotations[nvidia.MigAllocationsAnnotation]
	assert.Assert(t, kept, "the pod itself must not be changed")

	planned.migPlacements = `[{"containerIndex":0,"deviceIndex":0,"gpuUUID":"GPU-mig","profile":"2g.12gb","placement":{"start":2,"size":2}}]`
	held = reservationFor(pod, moveNodeX, planned)
	assert.Equal(t, held.Annotations[nvidia.MigAllocationsAnnotation], planned.migPlacements)
}
