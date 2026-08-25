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

package util

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// NumaAlignmentMode selects how HAMi handles a mismatch between the
// scheduler-selected GPU and the NUMA-aligned replicas kubelet's Topology
// Manager restricts an allocation to. It is distinct from the
// nvidia.com/numa-bind annotation, which requests GPU-to-GPU co-location on
// one NUMA node at scheduling time.
type NumaAlignmentMode string

const (
	// NumaAlignmentNone means the Pod does not opt into NUMA alignment
	// handling and mismatches are treated exactly as before.
	NumaAlignmentNone NumaAlignmentMode = ""
	// NumaAlignmentBestEffort asks for the NUMA refit when it is available
	// but never fails the allocation: on a refit failure or with the refit
	// disabled, the mismatch is only surfaced and kubelet's own selection
	// stands.
	NumaAlignmentBestEffort NumaAlignmentMode = "best-effort"
	// NumaAlignmentStrict fails the allocation when the NUMA refit is
	// enabled on the cluster and cannot reconcile the mismatch. With the
	// refit disabled, strict only logs the mismatch at error severity.
	NumaAlignmentStrict NumaAlignmentMode = "strict"
)

// GetNumaAlignmentModeByPod returns the Pod's NUMA alignment mode, or
// NumaAlignmentNone when the Pod does not carry the annotation.
func GetNumaAlignmentModeByPod(pod *corev1.Pod) (NumaAlignmentMode, error) {
	if pod == nil || pod.Annotations == nil {
		return NumaAlignmentNone, nil
	}

	value, ok := pod.Annotations[NumaAlignmentAnnotationKey]
	if !ok {
		return NumaAlignmentNone, nil
	}

	return ParseNumaAlignmentMode(value)
}

// ParseNumaAlignmentMode parses and validates a numa-alignment annotation
// value. Values are case-insensitive and surrounding whitespace is ignored.
func ParseNumaAlignmentMode(value string) (NumaAlignmentMode, error) {
	switch mode := NumaAlignmentMode(strings.ToLower(strings.TrimSpace(value))); mode {
	case NumaAlignmentBestEffort, NumaAlignmentStrict:
		return mode, nil
	default:
		return NumaAlignmentNone, fmt.Errorf("invalid %s annotation %q: expected %q or %q",
			NumaAlignmentAnnotationKey, value, NumaAlignmentBestEffort, NumaAlignmentStrict)
	}
}
