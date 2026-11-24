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
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/remotegpu"
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

// releaseOnSchedulingFailureLocked releases pod's reservation when an update
// reports that kube-scheduler gave up the cycle it was made for. It reports
// whether it handled the update. Callers hold allocLock.
func (s *Scheduler) releaseOnSchedulingFailureLocked(oldPod, newPod *corev1.Pod) bool {
	if !schedulingFailureReported(oldPod, newPod) || !reservationReleasable(newPod) {
		return false
	}
	if _, binding := s.bindClaims[newPod.UID]; binding {
		// Bind is committing this reservation, so the failure is from an
		// earlier cycle.
		return false
	}
	if write, pending := s.filterWrites[newPod.UID]; pending && !write.shownBy(newPod) {
		// The update does not show this replica's newer Filter yet, so the
		// failure it reports is from an earlier cycle.
		return false
	}
	if !s.releaseReservationLocked(newPod, "SchedulingFailed") {
		// Not cached, as on a replica that started after the Filter: the
		// allocation is just as dead, so do not add it.
		s.markReservationReleased(newPod.UID, reservationKey(newPod))
		s.clearReleasedAllocation(newPod, newPod.Annotations)
	}
	return true
}

// releaseReservationLocked drops pod's Filter reservation and remembers the
// allocation so informer events do not add it back. A reservation that Bind
// has claimed is kept. Callers hold allocLock.
func (s *Scheduler) releaseReservationLocked(pod *corev1.Pod, reason string) bool {
	if _, binding := s.bindClaims[pod.UID]; binding {
		return false
	}
	pi, ok := s.podManager.TakeAndDeletePod(pod)
	if !ok {
		return false
	}
	allocation := pod.Annotations
	if write, pending := s.filterWrites[pod.UID]; pending {
		// The informer has not shown this replica's Filter patch yet, so
		// pod may still carry an older allocation. Release the one written.
		allocation = write.annotations
	}
	key := releasedReservation{
		node: allocation[util.AssignedNodeAnnotations],
		time: allocation[util.AssignedTimeAnnotations],
	}
	s.quotaManager.RmUsage(pod, pi.Devices)
	// Give back RemoteGPU cards booked for the attempt now, as a Filter whose
	// patch failed does, instead of after the booking's TTL.
	if remote, ok := device.GetDevices()[remotegpu.RemoteGPUDevice].(*remotegpu.RemoteGPUDevices); ok {
		_ = remote.ReleaseNodeLock(nil, pod)
	}
	delete(s.refitWrites, pod.UID)
	delete(s.filterWrites, pod.UID)
	s.markReservationReleased(pod.UID, key)
	s.clearReleasedAllocation(pod, allocation)
	klog.InfoS("Released device reservation of a pod that was not bound",
		"pod", klog.KObj(pod), "node", pi.NodeID, "reason", reason)
	return true
}

// releasedAllocationPatchTimeout bounds the API call that clears a released
// allocation from its pod.
const releasedAllocationPatchTimeout = 30 * time.Second

// clearReleasedAllocation removes a released allocation's annotations from the
// pod in the background, so that the allocation is not counted again after a
// scheduler restart or by a new leader, which do not have the in-memory
// marker. The patch only applies while the pod still carries this allocation,
// so it never clears one that a later Filter wrote. Only the leader patches;
// every replica keeps its own marker. Callers hold allocLock.
func (s *Scheduler) clearReleasedAllocation(pod *corev1.Pod, allocation map[string]string) {
	if s.kubeClient == nil || (s.leaderManager != nil && !s.leaderManager.IsLeader()) {
		return
	}
	node, at := allocation[util.AssignedNodeAnnotations], allocation[util.AssignedTimeAnnotations]
	if node == "" || at == "" {
		return
	}
	keys := []string{util.AssignedNodeAnnotations, util.AssignedTimeAnnotations}
	for _, key := range device.SupportDevices {
		if _, ok := allocation[key]; ok {
			keys = append(keys, key)
		}
	}
	patch, err := releasedAllocationPatch(node, at, keys)
	if err != nil {
		klog.ErrorS(err, "Failed to build the patch clearing a released allocation", "pod", klog.KObj(pod))
		return
	}
	namespace, name := pod.Namespace, pod.Name
	s.allocationCleanups.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), releasedAllocationPatchTimeout)
		defer cancel()
		_, err := s.kubeClient.CoreV1().Pods(namespace).Patch(ctx, name, k8stypes.JSONPatchType, patch, metav1.PatchOptions{})
		switch {
		case err == nil:
			klog.V(4).InfoS("Cleared a released allocation from the pod", "pod", klog.KRef(namespace, name))
		case apierrors.IsNotFound(err):
		default:
			// Most often a later Filter has written a new allocation, which
			// the test operations protect. The in-memory marker still
			// keeps the released one from being added back here.
			klog.InfoS("Did not clear a released allocation from the pod", "pod", klog.KRef(namespace, name), "err", err)
		}
	})
}

// releasedAllocationPatch returns a JSON patch that removes keys from a pod's
// annotations if, and only if, the pod still carries the allocation of node
// made at time at.
func releasedAllocationPatch(node, at string, keys []string) ([]byte, error) {
	type operation struct {
		Op    string `json:"op"`
		Path  string `json:"path"`
		Value string `json:"value,omitempty"`
	}
	path := func(key string) string {
		return "/metadata/annotations/" + strings.NewReplacer("~", "~0", "/", "~1").Replace(key)
	}
	ops := []operation{
		{Op: "test", Path: path(util.AssignedNodeAnnotations), Value: node},
		{Op: "test", Path: path(util.AssignedTimeAnnotations), Value: at},
	}
	for _, key := range keys {
		ops = append(ops, operation{Op: "remove", Path: path(key)})
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

// claimBind is called by Bind before it acts on pod's reservation. A reserved
// pod is claimed, and no release touches a claimed reservation until the
// informer shows the pod bound or Bind drops the claim on failure. An
// unreserved, unbound pod whose reservation was released is refused: its
// devices may belong to another pod by now. The check and the claim happen
// under allocLock, like every release. It reports whether pod was claimed.
func (s *Scheduler) claimBind(pod *corev1.Pod) (bool, error) {
	s.allocLock.Lock()
	defer s.allocLock.Unlock()
	if _, reserved := s.podManager.GetPod(pod); reserved {
		if s.bindClaims == nil {
			s.bindClaims = make(map[k8stypes.UID]struct{})
		}
		s.bindClaims[pod.UID] = struct{}{}
		return true, nil
	}
	if pod.Spec.NodeName == "" && s.hasReleasedReservation(pod.UID) {
		return false, fmt.Errorf("device reservation of pod %s/%s was released after a failed scheduling cycle",
			pod.Namespace, pod.Name)
	}
	return false, nil
}

// dropBindClaim ends a claim of a Bind that failed.
func (s *Scheduler) dropBindClaim(uid k8stypes.UID) {
	s.allocLock.Lock()
	defer s.allocLock.Unlock()
	delete(s.bindClaims, uid)
}

// hasReleasedReservation reports whether any reservation of the pod was
// released since its last Filter.
func (s *Scheduler) hasReleasedReservation(uid k8stypes.UID) bool {
	s.releasedMu.Lock()
	defer s.releasedMu.Unlock()
	_, ok := s.releasedReservations[uid]
	return ok
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
