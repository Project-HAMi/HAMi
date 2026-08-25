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

import "strings"

// ValidNodeSchedulerPolicy reports whether value names a recognized node
// scheduler policy.
func ValidNodeSchedulerPolicy(value string) bool {
	switch SchedulerPolicyName(strings.TrimSpace(value)) {
	case NodeSchedulerPolicyBinpack, NodeSchedulerPolicySpread:
		return true
	}
	return false
}

// ValidGPUSchedulerPolicy reports whether value names a recognized GPU
// scheduler policy, or a comma-separated chain of them. binpack, spread, and
// numa order the device sort; mutex and topology-aware are filters consumed
// by the device backends.
func ValidGPUSchedulerPolicy(value string) bool {
	for part := range strings.SplitSeq(value, ",") {
		switch SchedulerPolicyName(strings.TrimSpace(part)) {
		case GPUSchedulerPolicyBinpack, GPUSchedulerPolicySpread, GPUSchedulerPolicyNuma,
			GPUSchedulerPolicyMutex, GPUSchedulerPolicyTopology:
		default:
			return false
		}
	}
	return true
}
