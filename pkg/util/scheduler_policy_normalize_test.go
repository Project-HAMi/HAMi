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

// The validator trims each comma token, so padded values are accepted; the
// returned policy must be canonical because DeviceUsageList.Less compares the
// whole string and would silently fall back to the spread branch otherwise.
func TestGetGPUSchedulerPolicyByPodNormalizesAcceptedValues(t *testing.T) {
	podWith := func(value string) *corev1.Pod {
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Name: "p", Namespace: "default",
			Annotations: map[string]string{GPUSchedulerPolicyAnnotationKey: value},
		}}
	}

	tests := []struct {
		value string
		want  string
	}{
		{"binpack", "binpack"},
		{" binpack ", "binpack"},
		{"binpack, numa", "binpack,numa"},
		{" spread ,  mutex ", "spread,mutex"},
		{"binpakc", "spread"},
		{"", "spread"},
	}
	for _, tt := range tests {
		got := GetGPUSchedulerPolicyByPod("spread", podWith(tt.value))
		assert.Equal(t, got, tt.want, "value %q", tt.value)
	}

	// --gpu-scheduler-policy is unvalidated, so a padded cluster default must
	// be normalized on the way out as well.
	assert.Equal(t, GetGPUSchedulerPolicyByPod(" binpack ", nil), "binpack")
	assert.Equal(t, GetGPUSchedulerPolicyByPod("binpack, numa", nil), "binpack,numa")
}
