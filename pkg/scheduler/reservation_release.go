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
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

// The scheduler extender API has no Unreserve call. When kube-scheduler gives
// up a scheduling cycle after HAMi's Filter reserved devices, for example
// when a gang's Permit wait times out, HAMi is not told, and the reservation
// would stay counted until the pod is filtered again or deleted (#3484). The
// code here releases such a reservation when kube-scheduler reports the
// failure in the pod's PodScheduled condition, or, when
// config.UnboundReservationTTL is set, once it is older than that.

// releasedReservation identifies the Filter allocation that was released.
// The pod keeps the annotations of that allocation, so the marker stops an
// informer event from adding it back. A later Filter writes a new
// allocation, which no longer matches.
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

// reservationReleasable reports whether pod's reservation may be released:
// the pod is not bound and Bind has not started allocating it.
func reservationReleasable(pod *corev1.Pod) bool {
	return pod.Spec.NodeName == "" &&
		pod.Annotations[util.DeviceBindPhase] != util.DeviceBindAllocating
}

// releaseReservationLocked drops pod's Filter reservation and remembers the
// allocation so informer events do not add it back. Callers hold allocLock.
func (s *Scheduler) releaseReservationLocked(pod *corev1.Pod, reason string) bool {
	pi, ok := s.podManager.TakeAndDeletePod(pod)
	if !ok {
		return false
	}
	key := reservationKey(pod)
	if write, pending := s.filterWrites[pod.UID]; pending {
		// The informer has not shown this replica's Filter patch yet, so
		// pod may still carry an older allocation. Remember the one written.
		key = releasedReservation{
			node: write.annotations[util.AssignedNodeAnnotations],
			time: write.annotations[util.AssignedTimeAnnotations],
		}
	}
	s.quotaManager.RmUsage(pod, pi.Devices)
	delete(s.refitWrites, pod.UID)
	delete(s.filterWrites, pod.UID)
	s.markReservationReleased(pod.UID, key)
	klog.InfoS("Released device reservation of a pod that was not bound",
		"pod", klog.KObj(pod), "node", pi.NodeID, "reason", reason)
	return true
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

// reservationTime returns when pod's reservation was made. A Filter on this
// replica that the informer does not show yet is newer than the pod's
// annotations. Callers hold allocLock.
func (s *Scheduler) reservationTime(pod *corev1.Pod) (time.Time, bool) {
	if write, pending := s.filterWrites[pod.UID]; pending {
		return write.at, true
	}
	unix, err := strconv.ParseInt(pod.Annotations[util.AssignedTimeAnnotations], 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(unix, 0), true
}

// reservationSweepInterval bounds how often Filter scans the reservations
// for ones older than the TTL.
func reservationSweepInterval(ttl time.Duration) time.Duration {
	return max(ttl/10, time.Second)
}

// expireReservationsLocked releases reservations of unbound pods older than
// config.UnboundReservationTTL. It does nothing when the TTL is zero and scans
// at most once per reservationSweepInterval. Callers hold allocLock.
func (s *Scheduler) expireReservationsLocked(now time.Time) int {
	ttl := config.UnboundReservationTTL
	if ttl <= 0 || now.Sub(s.lastReservationSweep) < reservationSweepInterval(ttl) {
		return 0
	}
	s.lastReservationSweep = now
	released := 0
	for _, pod := range s.podManager.UnboundPods() {
		if !reservationReleasable(pod) {
			continue
		}
		reservedAt, ok := s.reservationTime(pod)
		if !ok || now.Sub(reservedAt) < ttl {
			continue
		}
		if s.releaseReservationLocked(pod, "UnboundReservationTTL") {
			released++
		}
	}
	return released
}
