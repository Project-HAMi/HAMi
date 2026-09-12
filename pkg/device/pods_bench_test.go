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
	"fmt"
	"io"
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

// newBenchmarkCachedPod includes nested fields to exercise Pod copying costs.
func newBenchmarkCachedPod(index int) *corev1.Pod {
	envs := make([]corev1.EnvVar, 0, 20)
	for i := range 20 {
		envs = append(envs, corev1.EnvVar{Name: fmt.Sprintf("ENV_%d", i), Value: fmt.Sprintf("value-%d-%d", index, i)})
	}
	mounts := make([]corev1.VolumeMount, 0, 6)
	for i := range 6 {
		mounts = append(mounts, corev1.VolumeMount{Name: fmt.Sprintf("vol-%d", i), MountPath: fmt.Sprintf("/mnt/%d", i)})
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("cached-pod-%d", index),
			Namespace: "default",
			UID:       k8stypes.UID(fmt.Sprintf("cached-uid-%d", index)),
			Labels:    map[string]string{"app": "train", "release": "v1", "team": "ml"},
			Annotations: map[string]string{
				"hami.io/vgpu-devices-allocated": "GPU-0,NVIDIA,1024,10:;",
			},
		},
		Spec: corev1.PodSpec{
			NodeName:   "node-0",
			Containers: []corev1.Container{{Name: "main", Image: "example.com/train:v1", Env: envs, VolumeMounts: mounts}},
			Volumes:    []corev1.Volume{{Name: "vol-0"}, {Name: "vol-1"}},
		},
		Status: corev1.PodStatus{
			Phase:             corev1.PodRunning,
			Conditions:        []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			ContainerStatuses: []corev1.ContainerStatus{{Name: "main", Image: "example.com/train:v1", Ready: true}},
		},
	}
}

// newBenchmarkPodManager returns a manager holding podCount cached pods, each
// with a single-device allocation.
func newBenchmarkPodManager(podCount int) *PodManager {
	manager := NewPodManager()
	for i := range podCount {
		manager.AddPod(newBenchmarkCachedPod(i), "node-0", PodDevices{
			"NVIDIA": PodSingleDevice{
				ContainerDevices{{UUID: "GPU-0", Type: "NVIDIA", Usedmem: 1024, Usedcores: 10}},
			},
		})
	}
	return manager
}

// BenchmarkListPodsInfo measures the cache snapshot taken once per Filter
// across different cached Pod counts. AddPod logs at Info level, so klog is
// silenced for the whole benchmark. SetOutput alone is not enough: klog
// defaults to logtostderr, which writes to stderr directly and never consults
// the configured output.
func BenchmarkListPodsInfo(b *testing.B) {
	klog.LogToStderr(false)
	klog.SetOutput(io.Discard)
	b.Cleanup(func() {
		klog.SetOutput(os.Stderr)
		klog.LogToStderr(true)
	})

	for _, podCount := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("pods=%d", podCount), func(b *testing.B) {
			manager := newBenchmarkPodManager(podCount)

			b.ReportAllocs()
			for b.Loop() {
				if got := len(manager.ListPodsInfo()); got != podCount {
					b.Fatalf("ListPodsInfo returned %d pods, want %d", got, podCount)
				}
			}
		})
	}
}
