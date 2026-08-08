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
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// DeviceScoringWeights controls the relative influence of each resource
// dimension on device-level binpack and spread scores.
type DeviceScoringWeights struct {
	Slot   int64
	Core   int64
	Memory int64
}

// DefaultDeviceScoringWeights preserves HAMi's existing equal-weight score.
func DefaultDeviceScoringWeights() DeviceScoringWeights {
	return DeviceScoringWeights{Slot: 1, Core: 1, Memory: 1}
}

// GetDeviceScoringWeightsByPod returns the default weights unless the Pod has
// a device-scoring annotation. The annotation must specify each dimension in
// the form "slot=1,core=1,memory=3".
func GetDeviceScoringWeightsByPod(pod *corev1.Pod) (DeviceScoringWeights, error) {
	if pod == nil || pod.Annotations == nil {
		return DefaultDeviceScoringWeights(), nil
	}

	value, ok := pod.Annotations[DeviceScoringWeightsAnnotationKey]
	if !ok {
		return DefaultDeviceScoringWeights(), nil
	}

	return ParseDeviceScoringWeights(value)
}

// ParseDeviceScoringWeights parses and validates a device-scoring annotation.
func ParseDeviceScoringWeights(value string) (DeviceScoringWeights, error) {
	weights := DeviceScoringWeights{}
	seen := make(map[string]struct{}, 3)
	parts := strings.Split(value, ",")
	if len(parts) != 3 {
		return weights, fmt.Errorf("invalid %s annotation %q: expected slot, core, and memory weights", DeviceScoringWeightsAnnotationKey, value)
	}

	for _, part := range parts {
		keyValue := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(keyValue) != 2 {
			return weights, fmt.Errorf("invalid %s annotation %q: expected key=value entries", DeviceScoringWeightsAnnotationKey, value)
		}

		key := strings.TrimSpace(keyValue[0])
		if _, duplicate := seen[key]; duplicate {
			return weights, fmt.Errorf("invalid %s annotation %q: duplicate %q weight", DeviceScoringWeightsAnnotationKey, value, key)
		}

		weight, err := strconv.ParseInt(strings.TrimSpace(keyValue[1]), 10, 64)
		if err != nil {
			return weights, fmt.Errorf("invalid %s annotation %q: %q weight must be an integer", DeviceScoringWeightsAnnotationKey, value, key)
		}
		if weight < 0 {
			return weights, fmt.Errorf("invalid %s annotation %q: %q weight must not be negative", DeviceScoringWeightsAnnotationKey, value, key)
		}

		switch key {
		case "slot":
			weights.Slot = weight
		case "core":
			weights.Core = weight
		case "memory":
			weights.Memory = weight
		default:
			return weights, fmt.Errorf("invalid %s annotation %q: unknown weight %q", DeviceScoringWeightsAnnotationKey, value, key)
		}
		seen[key] = struct{}{}
	}

	for _, required := range []string{"slot", "core", "memory"} {
		if _, ok := seen[required]; !ok {
			return weights, fmt.Errorf("invalid %s annotation %q: missing %q weight", DeviceScoringWeightsAnnotationKey, value, required)
		}
	}
	if weights.Slot == 0 && weights.Core == 0 && weights.Memory == 0 {
		return weights, fmt.Errorf("invalid %s annotation %q: at least one weight must be positive", DeviceScoringWeightsAnnotationKey, value)
	}

	return weights, nil
}
