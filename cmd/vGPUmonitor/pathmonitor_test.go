/*
Copyright 2024 The HAMi Authors.

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

package main

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIsVaildPod(t *testing.T) {
	pods := &corev1.PodList{
		Items: []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{
					UID: "123",
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					UID: "456",
				},
			},
		},
	}

	cases := []struct {
		name     string
		expected bool
	}{
		{
			name:     "123",
			expected: true,
		},
		{
			name:     "789",
			expected: false,
		},
	}

	for _, c := range cases {
		if got := isVaildPod(c.name, pods); got != c.expected {
			t.Errorf("isVaildPod(%q) == %v, want %v", c.name, got, c.expected)
		}
	}
}
