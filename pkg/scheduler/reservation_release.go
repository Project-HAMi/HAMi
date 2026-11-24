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
	"encoding/json"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

// The scheduler extender API has no Unreserve call. When kube-scheduler gives
// up a scheduling cycle after HAMi's Filter reserved devices, for example
// when a gang's Permit wait times out, HAMi is not told, and the reservation
// would stay counted until the pod is filtered again or deleted (#3484). The
// code here releases such a reservation when kube-scheduler reports the
// failure in the pod's PodScheduled condition.

// releasedReservation identifies the Filter allocation that was released.
// Until the allocation is cleared from the pod, the marker stops an informer
// event from adding it back. A later Filter writes a new allocation, which no
// longer matches.
type releasedReservation struct {
	node string
	time string
}

// reservationKey returns the allocation pod's annotations describe.
func reservationKey(pod *corev1.Pod) releasedReservation {
	return releasedReservation{
		node: pod.Annotations[util.AssignedNodeAnnotations],
		time: pod.Annotations[util.AssignedTimeAnnotations],
	}
}

// podScheduledCondition returns pod's PodScheduled condition, or nil.
func podScheduledCondition(pod *corev1.Pod) *corev1.PodCondition {
	for i := range pod.Status.Conditions {
		if pod.Status.Conditions[i].Type == corev1.PodScheduled {
			return &pod.Status.Conditions[i]
		}
	}
	return nil
}

// schedulingFailureReported reports whether an update is kube-scheduler
// recording a failed scheduling cycle for an unbound pod: a PodScheduled=False
// condition that is new or changed. A resync, where nothing changed, is not
// a new failure.
func schedulingFailureReported(oldPod, newPod *corev1.Pod) bool {
	if oldPod == nil || newPod.Spec.NodeName != "" {
		return false
	}
	newCond := podScheduledCondition(newPod)
	if newCond == nil || newCond.Status != corev1.ConditionFalse {
		return false
	}
	oldCond := podScheduledCondition(oldPod)
	if oldCond == nil {
		return true
	}
	return oldCond.Status != newCond.Status ||
		oldCond.Reason != newCond.Reason ||
		oldCond.Message != newCond.Message ||
		!oldCond.LastTransitionTime.Equal(&newCond.LastTransitionTime) ||
		!oldCond.LastProbeTime.Equal(&newCond.LastProbeTime)
}

// releaseOnSchedulingFailureLocked releases pod's reservation when an update
// reports that kube-scheduler gave up the cycle it was made for. It reports
// whether it handled the update. Callers hold allocLock.
func (s *Scheduler) releaseOnSchedulingFailureLocked(oldPod, newPod *corev1.Pod) bool {
	if !schedulingFailureReported(oldPod, newPod) ||
		newPod.Annotations[util.DeviceBindPhase] == util.DeviceBindAllocating {
		return false
	}
	if write, pending := s.filterWrites[newPod.UID]; pending && !write.shownBy(newPod) {
		// The update does not show this replica's newer Filter yet, so the
		// failure it reports is from an earlier cycle.
		return false
	}
	if pi, ok := s.podManager.TakeAndDeletePod(newPod); ok {
		s.quotaManager.RmUsage(newPod, pi.Devices)
		delete(s.refitWrites, newPod.UID)
		delete(s.filterWrites, newPod.UID)
		klog.InfoS("Released device reservation of a pod that was not bound",
			"pod", klog.KObj(newPod), "node", pi.NodeID, "reason", "SchedulingFailed")
	}
	// Also when not cached, as on a replica that started after the Filter:
	// the allocation is just as dead, so do not add it.
	s.markReservationReleased(newPod.UID, reservationKey(newPod))
	s.clearReleasedAllocation(newPod)
	return true
}

const (
	// releasedAllocationPatchTimeout bounds the API calls that clear a
	// released allocation from its pod.
	releasedAllocationPatchTimeout = 30 * time.Second
	// releasedAllocationPatchAttempts bounds how often the patch is tried
	// again after the pod changed for another reason.
	releasedAllocationPatchAttempts = 3
)

// clearReleasedAllocation removes the released allocation's annotations from
// pod in the background, so that a restarted scheduler or a new leader, which
// do not have the in-memory marker, do not count it again. The patch applies
// only while the pod is unchanged, so it never clears an allocation that a
// later Filter wrote. When the pod did change, for example a label update, it
// is read again and patched again while it still carries the released
// allocation. Only the leader patches.
func (s *Scheduler) clearReleasedAllocation(pod *corev1.Pod) {
	if s.kubeClient == nil || (s.leaderManager != nil && !s.leaderManager.IsLeader()) {
		return
	}
	patch, err := releasedAllocationPatch(pod)
	if err != nil {
		klog.ErrorS(err, "Failed to build the patch clearing a released allocation", "pod", klog.KObj(pod))
		return
	}
	if patch == nil {
		return
	}
	namespace, name := pod.Namespace, pod.Name
	s.allocationCleanups.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), releasedAllocationPatchTimeout)
		defer cancel()
		pods := s.kubeClient.CoreV1().Pods(namespace)
		for attempt := 1; ; attempt++ {
			_, err := pods.Patch(ctx, name, k8stypes.JSONPatchType, patch, metav1.PatchOptions{})
			if err == nil {
				klog.V(4).InfoS("Cleared a released allocation from the pod", "pod", klog.KRef(namespace, name))
				return
			}
			if apierrors.IsNotFound(err) {
				return
			}
			if attempt == releasedAllocationPatchAttempts {
				// The in-memory marker still keeps the allocation out until
				// this process exits.
				klog.InfoS("Did not clear a released allocation from the pod", "pod", klog.KRef(namespace, name), "err", err)
				return
			}
			live, err := pods.Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				if !apierrors.IsNotFound(err) {
					klog.InfoS("Did not clear a released allocation from the pod", "pod", klog.KRef(namespace, name), "err", err)
				}
				return
			}
			if !s.allocationStillReleased(live) {
				// A later Filter wrote a new allocation, which must stay.
				return
			}
			if patch, err = releasedAllocationPatch(live); err != nil || patch == nil {
				return
			}
		}
	})
}

// allocationStillReleased reports whether pod still carries the allocation
// that was released, and no Filter has reserved devices for it since: Filter
// adds the pod back to the cache and forgets the marker.
func (s *Scheduler) allocationStillReleased(pod *corev1.Pod) bool {
	s.allocLock.Lock()
	defer s.allocLock.Unlock()
	if _, reserved := s.podManager.GetPod(pod); reserved {
		return false
	}
	return s.reservationReleased(pod)
}

// releasedAllocationPatch returns a JSON patch that removes pod's allocation
// annotations if, and only if, the pod still has the resourceVersion and the
// allocation it had when the failure was reported. It returns nil when pod
// carries no allocation.
func releasedAllocationPatch(pod *corev1.Pod) ([]byte, error) {
	node, at := pod.Annotations[util.AssignedNodeAnnotations], pod.Annotations[util.AssignedTimeAnnotations]
	if node == "" || at == "" || pod.ResourceVersion == "" {
		return nil, nil
	}
	type operation struct {
		Op    string `json:"op"`
		Path  string `json:"path"`
		Value string `json:"value,omitempty"`
	}
	annotation := func(key string) string {
		return "/metadata/annotations/" + strings.NewReplacer("~", "~0", "/", "~1").Replace(key)
	}
	ops := []operation{
		{Op: "test", Path: "/metadata/resourceVersion", Value: pod.ResourceVersion},
		{Op: "test", Path: annotation(util.AssignedNodeAnnotations), Value: node},
		{Op: "test", Path: annotation(util.AssignedTimeAnnotations), Value: at},
		{Op: "remove", Path: annotation(util.AssignedNodeAnnotations)},
		{Op: "remove", Path: annotation(util.AssignedTimeAnnotations)},
	}
	for _, key := range device.SupportDevices {
		if _, ok := pod.Annotations[key]; ok {
			ops = append(ops, operation{Op: "remove", Path: annotation(key)})
		}
	}
	return json.Marshal(ops)
}

func (s *Scheduler) markReservationReleased(uid k8stypes.UID, key releasedReservation) {
	s.releasedMu.Lock()
	defer s.releasedMu.Unlock()
	if s.releasedReservations == nil {
		s.releasedReservations = make(map[k8stypes.UID]releasedReservation)
	}
	s.releasedReservations[uid] = key
}

func (s *Scheduler) forgetReleasedReservation(uid k8stypes.UID) {
	s.releasedMu.Lock()
	defer s.releasedMu.Unlock()
	delete(s.releasedReservations, uid)
}

// reservationReleased reports whether pod still describes an allocation that
// was released. Only an unbound pod can: once bound, the pod uses the
// devices and must be counted, whatever happened before.
func (s *Scheduler) reservationReleased(pod *corev1.Pod) bool {
	if pod.Spec.NodeName != "" {
		return false
	}
	s.releasedMu.Lock()
	defer s.releasedMu.Unlock()
	released, ok := s.releasedReservations[pod.UID]
	return ok && released == reservationKey(pod)
}
