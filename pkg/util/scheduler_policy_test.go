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
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIsValidGPUSchedulerPolicy(t *testing.T) {
	tests := []struct {
		policy string
		want   bool
	}{
		{"binpack", true},
		{"spread", true},
		{"numa", true},
		{"mutex", true},
		{"topology-aware", true},
		{"binpack,numa", true},
		{"binpack, numa", true},
		{"mutex,topology-aware", true},
		{"", false},
		{"   ", false},
		{"binpakc", false},
		{"Binpack", false},
		{"binpack,nmua", false},
		{"binpack,", false},
	}
	for _, tt := range tests {
		t.Run(tt.policy, func(t *testing.T) {
			assert.Equal(t, tt.want, IsValidGPUSchedulerPolicy(tt.policy))
		})
	}
}

func TestIsValidNodeSchedulerPolicy(t *testing.T) {
	tests := []struct {
		policy string
		want   bool
	}{
		{"binpack", true},
		{"spread", true},
		{"", false},
		{"binpakc", false},
		{"Spread", false},
		{"binpack,spread", false},
		{"numa", false},
	}
	for _, tt := range tests {
		t.Run(tt.policy, func(t *testing.T) {
			assert.Equal(t, tt.want, IsValidNodeSchedulerPolicy(tt.policy))
		})
	}
}

func TestGetGPUSchedulerPolicyByPodUnrecognizedValue(t *testing.T) {
	podWith := func(value string) *corev1.Pod {
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Name:        "policy-pod",
			Namespace:   "default",
			Annotations: map[string]string{GPUSchedulerPolicyAnnotationKey: value},
		}}
	}

	tests := []struct {
		name          string
		defaultPolicy string
		annotation    string
		want          string
	}{
		{"valid value overrides the configured policy", "binpack", "spread", "spread"},
		{"chain form is honoured", "spread", "binpack,numa", "binpack,numa"},
		{"filter-only form is honoured", "spread", "topology-aware", "topology-aware"},
		{"unrecognized value keeps the configured policy", "binpack", "binpakc", "binpack"},
		{"empty value keeps the configured policy", "binpack", "", "binpack"},
		{"chain with an unknown entry keeps the configured policy", "binpack", "binpack,nmua", "binpack"},
		// The same cases against a spread default, so "keeps the configured
		// policy" cannot pass by coincidence against a fixed fallback.
		{"unrecognized value keeps a spread default", "spread", "binpakc", "spread"},
		{"empty value keeps a spread default", "spread", "", "spread"},
		{"chain with an unknown entry keeps a spread default", "spread", "binpack,nmua", "spread"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, GetGPUSchedulerPolicyByPod(tt.defaultPolicy, podWith(tt.annotation)))
		})
	}
}
