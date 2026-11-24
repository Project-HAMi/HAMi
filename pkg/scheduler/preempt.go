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

// preemptionPlanTimeout is how long a plan holds devices back, so that a
// preemption that never completes cannot hold them for good.
const preemptionPlanTimeout = 5 * time.Minute

// preemptionPlan holds the devices Preempt planned for one pod, by node.
type preemptionPlan struct {
	pod   *corev1.Pod
	nodes map[string]plannedDevices
	at    time.Time
}

// plannedDevices are the planned devices and MIG placements on one node.
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
// On each candidate node Preempt adds the victims that the devices of the pod
// need, and leaves out a node where the devices cannot fit. The devices it
// planned are kept for nominatedReservations.
func (s *Scheduler) Preempt(args extenderv1.ExtenderPreemptionArgs) (*extenderv1.ExtenderPreemptionResult, error) {
	if args.Pod == nil {
		return nil, errors.New("extender preemption args missing pod")
	}
	// Like Filter, trust the live pod, not the pod in the request.
	pod, err := s.authoritativePod(args.Pod)
	if err != nil {
		klog.ErrorS(err, "Rejecting preemption request", "pod", klog.KObj(args.Pod))
		return nil, err
	}
	resourceReqs, err := device.Resourcereqs(pod)
	if err != nil {
		// Filter rejects this pod anyway, so evicting for it helps nothing.
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
	// Devices held back for other nominated pods count as taken here too.
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
		// kube-scheduler drops a node without victims, so it needs no plan.
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
	// A new answer replaces the earlier plan of the pod.
	delete(s.preemptionPlans, pod.UID)
	if len(plan.nodes) > 0 {
		if s.preemptionPlans == nil {
			s.preemptionPlans = make(map[k8stypes.UID]*preemptionPlan)
		}
		s.preemptionPlans[pod.UID] = plan
	}
	return &extenderv1.ExtenderPreemptionResult{NodeNameToMetaVictims: candidates}, nil
}

// hasDeviceRequest reports whether the pod asks for a HAMi device.
func hasDeviceRequest(resourceReqs device.PodDeviceRequests) bool {
	for _, containerReqs := range resourceReqs {
		if len(containerReqs) > 0 {
			return true
		}
	}
	return false
}

// selectVictims returns the extra pods that must leave nodeName so that pod
// fits, and the devices pod gets then. It reports false if pod cannot fit.
// Like kube-scheduler, it removes all candidates and puts them back one by
// one. Callers hold allocLock.
func (s *Scheduler) selectVictims(pod *corev1.Pod, resourceReqs device.PodDeviceRequests, nodeName string, nodePods []*device.PodInfo, proposed *extenderv1.MetaVictims) ([]*device.PodInfo, device.PodDevices, bool) {
	// Like Filter, ignore what the pod holds from an earlier attempt.
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

// reservationFor returns a pod cache entry that holds the planned devices for
// pod on nodeName.
func reservationFor(pod *corev1.Pod, nodeName string, planned plannedDevices) *device.PodInfo {
	holder := pod.DeepCopy()
	// Usage reads MIG placements from this annotation.
	delete(holder.Annotations, nvidia.MigAllocationsAnnotation)
	if planned.migPlacements != "" {
		if holder.Annotations == nil {
			holder.Annotations = make(map[string]string)
		}
		holder.Annotations[nvidia.MigAllocationsAnnotation] = planned.migPlacements
	}
	return &device.PodInfo{Pod: holder, NodeID: nodeName, Devices: planned.devices}
}

// nominatedReservations returns pod cache entries that hold the planned
// devices of nominated pods back from task, unless task has a higher priority.
// It drops a plan whose pod no longer waits for a node, and a plan older than
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
		// Held back only once the pod is nominated to a node of the plan.
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

// podPriority returns the priority of the pod, or 0 if it has none.
func podPriority(pod *corev1.Pod) int32 {
	if pod.Spec.Priority != nil {
		return *pod.Spec.Priority
	}
	return 0
}

// moreImportantPod orders pods like kube-scheduler does for victims: higher
// priority first, then earlier start time, then UID.
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

// victimCandidates returns the pods preemptor may evict from nodeName, most important first.
func victimCandidates(nodePods []*device.PodInfo, nodeName string, preemptor *corev1.Pod, taken map[k8stypes.UID]struct{}) []*device.PodInfo {
	var candidates []*device.PodInfo
	for _, pi := range nodePods {
		if pi == nil || pi.Pod == nil || pi.NodeID != nodeName || len(pi.Devices) == 0 {
			continue
		}
		if _, ok := taken[pi.UID]; ok {
			continue
		}
		if pi.Spec.NodeName != nodeName || podPriority(pi.Pod) >= podPriority(preemptor) {
			continue
		}
		candidates = append(candidates, pi)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return moreImportantPod(candidates[i].Pod, candidates[j].Pod)
	})
	return candidates
}

// fitsWithout reports whether pod fits nodeName without the pods in gone, and
// the devices it would get. It writes nothing. Callers hold allocLock.
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
