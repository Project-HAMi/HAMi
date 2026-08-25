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

package device

import (
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestReplacePodDevicesPreservesInitFlag(t *testing.T) {
	m := NewPodManager()
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "pod-uid", Name: "pod", Namespace: "ns"}}
	m.AddPod(pod, "node-1", PodDevices{"NVIDIA": {{{UUID: "GPU-a", Usedmem: 100}}}})

	old, ok := m.ReplacePodDevices(pod, PodDevices{"NVIDIA": {{{UUID: "GPU-b", Usedmem: 100}}}})
	assert.Equal(t, ok, true)
	assert.Equal(t, old["NVIDIA"][0][0].UUID, "GPU-a")

	pi, found := m.GetPod(pod)
	assert.Equal(t, found, true)
	assert.Equal(t, pi.Devices["NVIDIA"][0][0].UUID, "GPU-b")
	// Unlike UpdatePodDevice, the init-container shrink must stay armed.
	assert.Equal(t, pi.InitContainerResourceReleased, false)
}

func TestReplacePodDevicesUnknownPod(t *testing.T) {
	m := NewPodManager()
	_, ok := m.ReplacePodDevices(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "missing"}}, nil)
	assert.Equal(t, ok, false)
}
