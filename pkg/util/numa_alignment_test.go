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

func TestGetNumaAlignmentModeByPod(t *testing.T) {
	tests := []struct {
		name    string
		pod     *corev1.Pod
		want    NumaAlignmentMode
		wantErr bool
	}{
		{
			name: "nil pod defaults to none",
			pod:  nil,
			want: NumaAlignmentNone,
		},
		{
			name: "pod without annotations defaults to none",
			pod:  &corev1.Pod{},
			want: NumaAlignmentNone,
		},
		{
			name: "pod without the annotation defaults to none",
			pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{"other": "value"},
			}},
			want: NumaAlignmentNone,
		},
		{
			name: "best-effort",
			pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{NumaAlignmentAnnotationKey: "best-effort"},
			}},
			want: NumaAlignmentBestEffort,
		},
		{
			name: "surrounding whitespace is tolerated",
			pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{NumaAlignmentAnnotationKey: " best-effort "},
			}},
			want: NumaAlignmentBestEffort,
		},
		{
			name: "upper case best-effort is accepted",
			pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{NumaAlignmentAnnotationKey: "BEST-EFFORT"},
			}},
			want: NumaAlignmentBestEffort,
		},
		{
			name: "strict is rejected until the refit can enforce it",
			pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{NumaAlignmentAnnotationKey: "strict"},
			}},
			want:    NumaAlignmentNone,
			wantErr: true,
		},
		{
			name: "explicit empty value is rejected",
			pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{NumaAlignmentAnnotationKey: ""},
			}},
			want:    NumaAlignmentNone,
			wantErr: true,
		},
		{
			name: "unknown value is rejected",
			pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{NumaAlignmentAnnotationKey: "required"},
			}},
			want:    NumaAlignmentNone,
			wantErr: true,
		},
		{
			name: "boolean values from numa-bind are not valid modes",
			pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{NumaAlignmentAnnotationKey: "true"},
			}},
			want:    NumaAlignmentNone,
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := GetNumaAlignmentModeByPod(test.pod)
			if test.wantErr {
				assert.Assert(t, err != nil)
			} else {
				assert.NilError(t, err)
			}
			assert.Equal(t, got, test.want)
		})
	}
}

func TestParseNumaAlignmentModeErrorMentionsKeyAndValue(t *testing.T) {
	_, err := ParseNumaAlignmentMode("bogus")
	assert.Assert(t, err != nil)
	assert.ErrorContains(t, err, NumaAlignmentAnnotationKey)
	assert.ErrorContains(t, err, "bogus")
}
