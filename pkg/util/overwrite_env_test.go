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

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func podWithAnnos(annos map[string]string) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: annos}}
}

func ctrNamed(name string) *corev1.Container {
	return &corev1.Container{Name: name}
}

// containerJSON builds the JSON value for hami.io/overwrite-env-containers.
func containerJSON(t *testing.T, m map[string]string) string {
	t.Helper()
	b, err := json.Marshal(m)
	assert.NilError(t, err)
	return string(b)
}

func Test_OverwriteEnvDecision_BothUnset(t *testing.T) {
	got := OverwriteEnvDecision(podWithAnnos(nil), ctrNamed("main"))
	assert.Equal(t, got, OverwriteEnvUnset)
}

func Test_OverwriteEnvDecision_PodLevel(t *testing.T) {
	tests := []struct {
		name string
		val  string
		want OverwriteEnvMode
	}{
		{"true", "true", OverwriteEnvOn},
		{"True", "True", OverwriteEnvOn},
		{"1", "1", OverwriteEnvOn},
		{"t", "t", OverwriteEnvOn},
		{"false", "false", OverwriteEnvOff},
		{"0", "0", OverwriteEnvOff},
		{"f", "f", OverwriteEnvOff},
		{"invalid-yes", "yes", OverwriteEnvUnset},
		{"invalid-maybe", "maybe", OverwriteEnvUnset},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := OverwriteEnvDecision(
				podWithAnnos(map[string]string{OverwriteEnvAnnotationKey: tc.val}),
				ctrNamed("main"))
			assert.Equal(t, got, tc.want)
		})
	}
}

func Test_OverwriteEnvDecision_ContainerOverridesPod(t *testing.T) {
	tests := []struct {
		name   string
		podVal string
		ctrVal string
		want   OverwriteEnvMode
	}{
		{"reverse-pod-false-ctr-true", "false", "true", OverwriteEnvOn},
		{"reverse-pod-true-ctr-false", "true", "false", OverwriteEnvOff},
		{"same-pod-true-ctr-true", "true", "true", OverwriteEnvOn},
		{"same-pod-false-ctr-false", "false", "false", OverwriteEnvOff},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			annos := map[string]string{
				OverwriteEnvAnnotationKey:           tc.podVal,
				OverwriteEnvContainersAnnotationKey: containerJSON(t, map[string]string{"main": tc.ctrVal}),
			}
			got := OverwriteEnvDecision(podWithAnnos(annos), ctrNamed("main"))
			assert.Equal(t, got, tc.want)
		})
	}
}

func Test_OverwriteEnvDecision_ContainerOnly(t *testing.T) {
	annos := map[string]string{
		OverwriteEnvContainersAnnotationKey: containerJSON(t, map[string]string{"main": "true"}),
	}
	got := OverwriteEnvDecision(podWithAnnos(annos), ctrNamed("main"))
	assert.Equal(t, got, OverwriteEnvOn)
}

func Test_OverwriteEnvDecision_NilSafety(t *testing.T) {
	tests := []struct {
		name string
		pod  *corev1.Pod
		ctr  *corev1.Container
	}{
		{"nil-pod", nil, ctrNamed("main")},
		{"nil-ctr", podWithAnnos(nil), nil},
		{"nil-annotations", &corev1.Pod{}, ctrNamed("main")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := OverwriteEnvDecision(tc.pod, tc.ctr)
			assert.Equal(t, got, OverwriteEnvUnset)
		})
	}
}

func Test_OverwriteEnvDecision_InvalidContainerValueFallsBackToPod(t *testing.T) {
	annos := map[string]string{
		OverwriteEnvAnnotationKey:           "false",
		OverwriteEnvContainersAnnotationKey: containerJSON(t, map[string]string{"main": "yes"}),
	}
	got := OverwriteEnvDecision(podWithAnnos(annos), ctrNamed("main"))
	assert.Equal(t, got, OverwriteEnvOff)
}

func Test_OverwriteEnvDecision_MalformedJSONFallsBackToPod(t *testing.T) {
	annos := map[string]string{
		OverwriteEnvAnnotationKey:           "true",
		OverwriteEnvContainersAnnotationKey: "not-json",
	}
	got := OverwriteEnvDecision(podWithAnnos(annos), ctrNamed("main"))
	assert.Equal(t, got, OverwriteEnvOn)
}

func Test_OverwriteEnvDecision_UnlistedContainerFallsBackToPod(t *testing.T) {
	annos := map[string]string{
		OverwriteEnvAnnotationKey:           "false",
		OverwriteEnvContainersAnnotationKey: containerJSON(t, map[string]string{"other": "true"}),
	}
	got := OverwriteEnvDecision(podWithAnnos(annos), ctrNamed("main"))
	assert.Equal(t, got, OverwriteEnvOff)
}

func Test_OverwriteEnvDecision_MultiContainerJSON(t *testing.T) {
	annos := map[string]string{
		OverwriteEnvContainersAnnotationKey: containerJSON(t, map[string]string{
			"main":    "true",
			"sidecar": "false",
		}),
	}
	assert.Equal(t, OverwriteEnvDecision(podWithAnnos(annos), ctrNamed("main")), OverwriteEnvOn)
	assert.Equal(t, OverwriteEnvDecision(podWithAnnos(annos), ctrNamed("sidecar")), OverwriteEnvOff)
}

// Test_OverwriteEnvDecision_NoWildcard confirms "*" is a literal container name,
// not a wildcard — a container named "main" does not inherit a "*" entry.
func Test_OverwriteEnvDecision_NoWildcard(t *testing.T) {
	annos := map[string]string{
		OverwriteEnvContainersAnnotationKey: containerJSON(t, map[string]string{"*": "true"}),
	}
	got := OverwriteEnvDecision(podWithAnnos(annos), ctrNamed("main"))
	assert.Equal(t, got, OverwriteEnvUnset)
}
