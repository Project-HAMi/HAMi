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

	"k8s.io/klog/v2"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"
)

// Preempt answers the preempt verb of the scheduler extender. kube-scheduler
// calls it while it looks for pods to evict for a pod that fits no node, with
// the candidate nodes it found and the victims it proposes on each. It takes
// the victims of every node from the answer, and stops considering a node the
// answer leaves out.
func (s *Scheduler) Preempt(args extenderv1.ExtenderPreemptionArgs) (*extenderv1.ExtenderPreemptionResult, error) {
	if args.Pod == nil {
		return nil, errors.New("extender preemption args missing pod")
	}
	candidates := preemptionCandidates(args)
	klog.V(4).InfoS("Returning preemption candidates unchanged",
		"pod", klog.KObj(args.Pod), "uid", args.Pod.UID, "candidateNodes", len(candidates))
	return &extenderv1.ExtenderPreemptionResult{NodeNameToMetaVictims: candidates}, nil
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
