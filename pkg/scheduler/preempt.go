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
	"errors"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

// preemptionPlanTimeout is how long a plan lasts after Preempt made it. It
// covers the time the victims take to terminate and the next scheduling
// attempt of the pod. A pod that is still nominated after that holds nothing
// back any more, so a preemption that never completes cannot keep devices from
// other pods for good. A later Preempt for the pod makes a new plan.
const preemptionPlanTimeout = 5 * time.Minute

// preemptionPlan is what Preempt worked out for one pod: what the pod gets on
// each node of the answer once the victims are gone, and when that was.
type preemptionPlan struct {
	pod   *corev1.Pod
	nodes map[string]plannedDevices
	at    time.Time
}

// plannedDevices are the devices of a preemptionPlan on one node, as the pod
// cache would hold them, and the MIG placements among them, as the pod would
// carry them in its annotation.
type plannedDevices struct {
	devices       device.PodDevices
	migPlacements string
}

// Preempt answers the preempt verb of the scheduler extender. kube-scheduler
// calls it while it looks for pods to evict for a pod that fits no node, with
// the candidate nodes it found and the victims it proposes on each. It takes
// the victims of every node from the answer, and stops considering a node the
// answer leaves out.
//
// kube-scheduler does not see the devices HAMi shares, so its victims free
// what it counts itself and nothing more. On every candidate node Preempt adds
// the pods that have to go for the devices of the pod as well, and leaves out
// a node those devices do not fit on even then. The victims kube-scheduler
// proposed stay in the answer as they came.
//
// kube-scheduler picks one node of the answer, evicts the victims on it and
// nominates the pod to it. Preempt keeps the devices it planned on every node
// of the answer, so that the ones on the nominated node can be held back until
// the pod gets there. See nominatedReservations.
func (s *Scheduler) Preempt(args extenderv1.ExtenderPreemptionArgs) (*extenderv1.ExtenderPreemptionResult, error) {
	if args.Pod == nil {
		return nil, errors.New("extender preemption args missing pod")
	}
	// The pod in the request is input, as it is for Filter: its priority and
	// its requests are taken from the live object.
	pod, err := s.authoritativePod(args.Pod)
	if err != nil {
		klog.ErrorS(err, "Rejecting preemption request", "pod", klog.KObj(args.Pod))
		return nil, err
	}
	resourceReqs, err := device.Resourcereqs(pod)
	if err != nil {
		// Filter rejects this pod on every node, so evicting for it frees
		// nothing it can use.
		return nil, fmt.Errorf("invalid device request for pod %s: %w", klog.KObj(pod), err)
	}

	candidates := preemptionCandidates(args)
	if !hasDeviceRequest(resourceReqs) {
		klog.V(4).InfoS("Returning preemption candidates unchanged, pod requests no HAMi resource",
			"pod", klog.KObj(pod), "uid", pod.UID, "candidateNodes", len(candidates))
		return &extenderv1.ExtenderPreemptionResult{NodeNameToMetaVictims: candidates}, nil
	}

	s.allocLock.Lock()
	defer s.allocLock.Unlock()

	podsByNode := make(map[string][]*device.PodInfo)
	for _, pi := range s.podManager.ListPodsInfo() {
		podsByNode[pi.NodeID] = append(podsByNode[pi.NodeID], pi)
	}
	// What is held back for another nominated pod is not this pod's to plan
	// with either, unless this pod has the higher priority.
	for _, reservation := range s.nominatedReservations(pod) {
		podsByNode[reservation.NodeID] = append(podsByNode[reservation.NodeID], reservation)
	}
	plan := &preemptionPlan{pod: pod, nodes: make(map[string]plannedDevices), at: time.Now()}
	for nodeName, proposed := range candidates {
		victims, planned, fits := s.selectVictims(pod, resourceReqs, nodeName, podsByNode[nodeName], proposed)
		if !fits {
			klog.V(4).InfoS("Leaving out preemption candidate, devices do not fit even without lower priority pods",
				"pod", klog.KObj(pod), "node", nodeName)
			delete(candidates, nodeName)
			continue
		}
		for _, victim := range victims {
			proposed.Pods = append(proposed.Pods, &extenderv1.MetaPod{UID: string(victim.UID)})
		}
		// kube-scheduler drops a node nobody names a victim on, so the pod is
		// never nominated to one.
		if len(proposed.Pods) > 0 {
			migPlacements, _ := nvidia.EncodeMigAllocations(planned[nvidia.NvidiaGPUDevice])
			plan.nodes[nodeName] = plannedDevices{
				devices:       device.CollapseInitContainerUsage(pod, planned),
				migPlacements: migPlacements,
			}
		}
		klog.V(4).InfoS("Selected preemption victims for devices",
			"pod", klog.KObj(pod), "node", nodeName, "added", len(victims), "victims", len(proposed.Pods))
	}
	// This answer replaces what an earlier one planned for the pod.
	delete(s.preemptionPlans, pod.UID)
	if len(plan.nodes) > 0 {
		if s.preemptionPlans == nil {
			s.preemptionPlans = make(map[k8stypes.UID]*preemptionPlan)
		}
		s.preemptionPlans[pod.UID] = plan
	}
	return &extenderv1.ExtenderPreemptionResult{NodeNameToMetaVictims: candidates}, nil
}

// hasDeviceRequest reports whether a container of the pod asks for a device
// HAMi schedules.
func hasDeviceRequest(resourceReqs device.PodDeviceRequests) bool {
	for _, containerReqs := range resourceReqs {
		if len(containerReqs) > 0 {
			return true
		}
	}
	return false
}

// selectVictims returns the pods that have to leave nodeName for the devices
// of pod to fit there, on top of the victims kube-scheduler proposed, and the
// devices pod gets once they are all gone. It reports false if the devices do
// not fit even with every candidate gone.
//
// It picks them the way kube-scheduler picks its own: all candidates are taken
// away, and then put back one at a time, most important first. A candidate
// that pod no longer fits with is a victim. Callers hold allocLock.
func (s *Scheduler) selectVictims(pod *corev1.Pod, resourceReqs device.PodDeviceRequests, nodeName string, nodePods []*device.PodInfo, proposed *extenderv1.MetaVictims) ([]*device.PodInfo, device.PodDevices, bool) {
	// Filter drops what the pod holds from an earlier attempt before it looks
	// for a fit, so that does not count here either.
	gone := map[k8stypes.UID]struct{}{pod.UID: {}}
	taken := make(map[k8stypes.UID]struct{}, len(proposed.Pods))
	for _, victim := range proposed.Pods {
		taken[k8stypes.UID(victim.UID)] = struct{}{}
		gone[k8stypes.UID(victim.UID)] = struct{}{}
	}
	candidates := victimCandidates(nodePods, nodeName, pod, taken)
	for _, candidate := range candidates {
		gone[candidate.UID] = struct{}{}
	}
	planned, fits := s.fitsWithout(pod, resourceReqs, nodeName, nodePods, gone)
	if !fits {
		return nil, nil, false
	}

	var victims []*device.PodInfo
	for _, candidate := range candidates {
		delete(gone, candidate.UID)
		if devices, stillFits := s.fitsWithout(pod, resourceReqs, nodeName, nodePods, gone); stillFits {
			planned = devices
			continue
		}
		gone[candidate.UID] = struct{}{}
		victims = append(victims, candidate)
	}
	return victims, planned, true
}

// reservationFor returns what the pod cache would hold for pod if a Filter had
// given it the planned devices on nodeName. Usage is built from entries of
// this form, so adding one to the pods of a node holds those devices back.
func reservationFor(pod *corev1.Pod, nodeName string, planned plannedDevices) *device.PodInfo {
	holder := pod.DeepCopy()
	// Usage reads the MIG placements of a pod from this annotation. What the
	// pod carries there from an earlier attempt is not what is planned now.
	delete(holder.Annotations, nvidia.MigAllocationsAnnotation)
	if planned.migPlacements != "" {
		if holder.Annotations == nil {
			holder.Annotations = make(map[string]string)
		}
		holder.Annotations[nvidia.MigAllocationsAnnotation] = planned.migPlacements
	}
	return &device.PodInfo{Pod: holder, NodeID: nodeName, Devices: planned.devices}
}

// nominatedReservations returns the devices that are held back for the pods
// kube-scheduler has nominated to a node, as entries of the pod cache.
//
// While the victims of a preemption terminate, kube-scheduler counts what the
// pod it nominated asks for as taken on that node, for every pod of the same
// or a lower priority. It cannot do that for the devices HAMi shares, so a pod
// of a lower priority could take them the moment a victim is gone. Filter and
// Preempt add these entries to the pods they build usage from, which holds the
// devices back from task unless task has a higher priority than their pod.
//
// A plan is held back only once the nomination shows in the pod lister, and
// only on the node it names. A plan goes when its pod gets devices from a
// Filter, when a later Preempt answers for the pod again, and here, once the
// pod is no longer waiting for a node or the plan is older than
// preemptionPlanTimeout. Callers hold allocLock.
func (s *Scheduler) nominatedReservations(task *corev1.Pod) []*device.PodInfo {
	if s.podLister == nil {
		return nil
	}
	var held []*device.PodInfo
	for uid, plan := range s.preemptionPlans {
		if time.Since(plan.at) > preemptionPlanTimeout {
			delete(s.preemptionPlans, uid)
			continue
		}
		live, err := s.podLister.Pods(plan.pod.Namespace).Get(plan.pod.Name)
		if err != nil || live.UID != uid || live.Spec.NodeName != "" || util.IsPodTerminating(live) || util.IsPodInTerminatedState(live) {
			delete(s.preemptionPlans, uid)
			continue
		}
		// Without a nomination kube-scheduler has not picked a node, or its
		// pick does not show yet. A nomination to a node that is not in the
		// plan is one from before this plan, which the lister still shows.
		nominated := live.Status.NominatedNodeName
		planned, ok := plan.nodes[nominated]
		if !ok || uid == task.UID || podPriority(live) < podPriority(task) {
			continue
		}
		klog.V(4).InfoS("Holding back devices planned for a nominated pod",
			"nominatedPod", klog.KObj(live), "node", nominated, "from", klog.KObj(task))
		held = append(held, reservationFor(live, nominated, planned))
	}
	return held
}

// preemptionCandidates returns the candidate nodes of a preemption request,
// each with the victims proposed on it, in the form the answer carries.
// kube-scheduler sends NodeNameToMetaVictims to an extender that is
// nodeCacheCapable and NodeNameToVictims to one that is not, and reads
// NodeNameToMetaVictims back from both. Should a request carry both, the meta
// victims win.
func preemptionCandidates(args extenderv1.ExtenderPreemptionArgs) map[string]*extenderv1.MetaVictims {
	candidates := make(map[string]*extenderv1.MetaVictims, len(args.NodeNameToVictims)+len(args.NodeNameToMetaVictims))
	for nodeName, victims := range args.NodeNameToVictims {
		meta := &extenderv1.MetaVictims{Pods: []*extenderv1.MetaPod{}}
		if victims != nil {
			meta.NumPDBViolations = victims.NumPDBViolations
			for _, pod := range victims.Pods {
				if pod != nil {
					meta.Pods = append(meta.Pods, &extenderv1.MetaPod{UID: string(pod.UID)})
				}
			}
		}
		candidates[nodeName] = meta
	}
	for nodeName, victims := range args.NodeNameToMetaVictims {
		meta := &extenderv1.MetaVictims{Pods: []*extenderv1.MetaPod{}}
		if victims != nil {
			meta.NumPDBViolations = victims.NumPDBViolations
			for _, pod := range victims.Pods {
				if pod != nil {
					meta.Pods = append(meta.Pods, &extenderv1.MetaPod{UID: pod.UID})
				}
			}
		}
		candidates[nodeName] = meta
	}
	return candidates
}

// podPriority returns the priority admission resolved from the pod's
// PriorityClass, or 0 for a pod that has none.
func podPriority(pod *corev1.Pod) int32 {
	if pod.Spec.Priority != nil {
		return *pod.Spec.Priority
	}
	return 0
}

// moreImportantPod orders pods the way kube-scheduler does when it decides
// which victims to spare first: the higher priority wins, then the pod that
// started earlier. A pod that has not started yet is the newest, and the UID
// settles what is left, so the order is the same on every call.
func moreImportantPod(a, b *corev1.Pod) bool {
	if pa, pb := podPriority(a), podPriority(b); pa != pb {
		return pa > pb
	}
	sa, sb := a.Status.StartTime, b.Status.StartTime
	switch {
	case sa != nil && sb != nil && !sa.Equal(sb):
		return sa.Before(sb)
	case (sa == nil) != (sb == nil):
		return sa != nil
	}
	return a.UID < b.UID
}

// victimCandidates returns the pods among nodePods that preemptor may evict
// from nodeName, most important first: pods that hold devices there, are bound
// to the node, are not terminating, have a lower priority than preemptor, and
// are not in taken.
//
// kube-scheduler looks every victim it gets back up among the pods it has on
// that node, and fails the whole preemption on a miss. A pod Filter has
// assigned but that is not bound yet is one it does not have there.
func victimCandidates(nodePods []*device.PodInfo, nodeName string, preemptor *corev1.Pod, taken map[k8stypes.UID]struct{}) []*device.PodInfo {
	var candidates []*device.PodInfo
	for _, pi := range nodePods {
		if pi == nil || pi.Pod == nil || pi.NodeID != nodeName || len(pi.Devices) == 0 {
			continue
		}
		if _, ok := taken[pi.UID]; ok {
			continue
		}
		if pi.Spec.NodeName != nodeName || util.IsPodTerminating(pi.Pod) || podPriority(pi.Pod) >= podPriority(preemptor) {
			continue
		}
		candidates = append(candidates, pi)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return moreImportantPod(candidates[i].Pod, candidates[j].Pod)
	})
	return candidates
}

// fitsWithout reports whether pod fits nodeName once the pods in gone hold no
// devices there, and which devices it would get. nodePods are the pods the pod
// cache has on that node. It takes the path filterSimulation takes, which
// scores the node and writes nothing. Callers hold allocLock.
func (s *Scheduler) fitsWithout(pod *corev1.Pod, resourceReqs device.PodDeviceRequests, nodeName string, nodePods []*device.PodInfo, gone map[k8stypes.UID]struct{}) (device.PodDevices, bool) {
	nodeNames := []string{nodeName}
	usage, _, failedNodes, err := s.nodesUsageFrom(&nodeNames, pod, nodePods, gone)
	if err != nil {
		klog.ErrorS(err, "Failed to build node usage for preemption", "pod", klog.KObj(pod), "node", nodeName)
		return nil, false
	}
	scores, err := s.calcScoreWithOptions(usage, resourceReqs, pod, failedNodes, false, true)
	if err != nil {
		klog.ErrorS(err, "Failed to score node for preemption", "pod", klog.KObj(pod), "node", nodeName)
		return nil, false
	}
	if len(scores.NodeList) == 0 {
		return nil, false
	}
	return scores.NodeList[0].Devices, true
}
