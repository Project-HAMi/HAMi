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

package util

import (
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Benchmark_OverwriteEnvDecision_NoAnnotations measures the baseline cost when
// no opt-out annotations are set (the common case): the function returns Unset
// after two map lookups + one ParseBool("") that fails fast.
func Benchmark_OverwriteEnvDecision_NoAnnotations(b *testing.B) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: nil}}
	ctr := &corev1.Container{Name: "main"}
	b.ResetTimer()
	for range b.N {
		OverwriteEnvDecision(pod, ctr)
	}
}

// Benchmark_OverwriteEnvDecision_PodLevelOnly measures the cost when only the
// pod-level single-value annotation is set (no JSON to parse).
func Benchmark_OverwriteEnvDecision_PodLevelOnly(b *testing.B) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
		OverwriteEnvAnnotationKey: "true",
	}}}
	ctr := &corev1.Container{Name: "main"}
	b.ResetTimer()
	for range b.N {
		OverwriteEnvDecision(pod, ctr)
	}
}

// Benchmark_OverwriteEnvDecision_ContainerJSON measures a single call with the
// container-level JSON annotation set (the case that pays the json.Unmarshal).
// The JSON has 3 containers, a realistic pod size.
func Benchmark_OverwriteEnvDecision_ContainerJSON(b *testing.B) {
	entries := map[string]string{"main": "true", "sidecar": "false", "worker": "0"}
	raw, _ := json.Marshal(entries)
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
		OverwriteEnvAnnotationKey:           "false",
		OverwriteEnvContainersAnnotationKey: string(raw),
	}}}
	ctr := &corev1.Container{Name: "main"}
	b.ResetTimer()
	for range b.N {
		OverwriteEnvDecision(pod, ctr)
	}
}

// Benchmark_OverwriteEnvDecision_WebhookLoop simulates the real webhook path:
// the webhook iterates device.GetDevices() and calls MutateAdmission (→
// OverwriteEnvDecision) once per registered ascend chip (7 on the arm231
// cluster). This is the number to compare against a cached implementation.
func Benchmark_OverwriteEnvDecision_WebhookLoop(b *testing.B) {
	entries := map[string]string{"main": "true", "sidecar": "false", "worker": "0"}
	raw, _ := json.Marshal(entries)
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
		OverwriteEnvAnnotationKey:           "false",
		OverwriteEnvContainersAnnotationKey: string(raw),
	}}}
	ctr := &corev1.Container{Name: "main"}
	const numChips = 7
	b.ResetTimer()
	for range b.N {
		for range numChips {
			OverwriteEnvDecision(pod, ctr)
		}
	}
}

// Benchmark_OverwriteEnvDecision_WebhookLoop_Cached simulates the cached path
// the Ascend backend uses: the container-level JSON is decoded once (first chip)
// and served from the LRU cache for the remaining 6 chips. It measures the
// util-level resolve (ParsePodOverwriteEnv + DecodeContainerOverwriteEnvJSON once
// + ResolveOverwriteEnv 7×) to show the ceiling the ascend cache achieves; the
// actual ascend cache helper is exercised by the ascend package tests.
func Benchmark_OverwriteEnvDecision_WebhookLoop_Cached(b *testing.B) {
	entries := map[string]string{"main": "true", "sidecar": "false", "worker": "0"}
	raw, _ := json.Marshal(entries)
	rawJSON := string(raw)
	podMode, _ := ParsePodOverwriteEnv("false")
	ctr := &corev1.Container{Name: "main"}
	const numChips = 7
	b.ResetTimer()
	for range b.N {
		cachedEntries, _ := DecodeContainerOverwriteEnvJSON(rawJSON)
		for range numChips {
			ResolveOverwriteEnv(podMode, cachedEntries, ctr)
		}
	}
}
