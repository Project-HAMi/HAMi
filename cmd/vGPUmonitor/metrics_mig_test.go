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

package main

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestMigAllocationContainerName(t *testing.T) {
	pod := &corev1.Pod{Spec: corev1.PodSpec{
		InitContainers: []corev1.Container{{Name: "init"}},
		Containers:     []corev1.Container{{Name: "main-a"}, {Name: "main-b"}},
	}}
	tests := []struct {
		index int
		name  string
		ok    bool
	}{
		{index: 0, name: "init", ok: true},
		{index: 1, name: "main-a", ok: true},
		{index: 2, name: "main-b", ok: true},
		{index: 3, ok: false},
		{index: -1, ok: false},
	}
	for _, test := range tests {
		name, ok := migAllocationContainerName(pod, test.index)
		if name != test.name || ok != test.ok {
			t.Fatalf("index %d: got (%q, %t), want (%q, %t)", test.index, name, ok, test.name, test.ok)
		}
	}
}
