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

func TestGetDeviceScoringWeightsByPod(t *testing.T) {
	tests := []struct {
		name    string
		pod     *corev1.Pod
		want    DeviceScoringWeights
		wantErr bool
	}{
		{name: "nil Pod uses defaults", want: DefaultDeviceScoringWeights()},
		{name: "missing annotation uses defaults", pod: &corev1.Pod{}, want: DefaultDeviceScoringWeights()},
		{
			name: "valid annotation",
			pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				DeviceScoringWeightsAnnotationKey: "slot=1,core=2,memory=3",
			}}},
			want: DeviceScoringWeights{Slot: 1, Core: 2, Memory: 3},
		},
		{
			name: "invalid annotation",
			pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				DeviceScoringWeightsAnnotationKey: "slot=1,core=-1,memory=3",
			}}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetDeviceScoringWeightsByPod(tt.pod)
			if tt.wantErr {
				assert.Assert(t, err != nil)
				return
			}
			assert.NilError(t, err)
			assert.DeepEqual(t, got, tt.want)
		})
	}
}

func TestParseDeviceScoringWeights(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    DeviceScoringWeights
		wantErr string
	}{
		{
			name:  "valid ordered weights",
			value: "slot=1,core=2,memory=3",
			want:  DeviceScoringWeights{Slot: 1, Core: 2, Memory: 3},
		},
		{
			name:  "valid reordered weights and whitespace",
			value: " memory = 3, slot = 1, core = 0 ",
			want:  DeviceScoringWeights{Slot: 1, Core: 0, Memory: 3},
		},
		{name: "empty annotation", value: "", wantErr: "expected slot, core, and memory weights"},
		{name: "missing dimension", value: "slot=1,memory=3", wantErr: "expected slot, core, and memory weights"},
		{name: "malformed entry", value: "slot=1,core,memory=3", wantErr: "expected key=value entries"},
		{name: "non-integer", value: "slot=1,core=high,memory=3", wantErr: `"core" weight must be an integer`},
		{name: "negative", value: "slot=1,core=-1,memory=3", wantErr: `"core" weight must not be negative`},
		{name: "unknown key", value: "slot=1,bandwidth=2,memory=3", wantErr: `unknown weight "bandwidth"`},
		{name: "duplicate key", value: "slot=1,slot=2,memory=3", wantErr: `duplicate "slot" weight`},
		{name: "all zero", value: "slot=0,core=0,memory=0", wantErr: "at least one weight must be positive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDeviceScoringWeights(tt.value)
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			assert.NilError(t, err)
			assert.DeepEqual(t, got, tt.want)
		})
	}
}
