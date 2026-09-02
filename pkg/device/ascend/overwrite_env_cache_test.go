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

package ascend

import (
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"

	"github.com/Project-HAMi/HAMi/pkg/util"
)

// Test_cachedPodOverwriteEnv covers hit/miss, invalid-value caching, and empty
// short-circuit. The cache is package-global, so each case uses a distinct
// input to avoid cross-case interference.
func Test_cachedPodOverwriteEnv(t *testing.T) {
	// miss → parse + cache
	got := cachedPodOverwriteEnv("true")
	assert.Equal(t, got, util.OverwriteEnvOn)
	// hit (same value) → returns cached without re-parse
	got = cachedPodOverwriteEnv("true")
	assert.Equal(t, got, util.OverwriteEnvOn)

	// invalid value caches Unset so repeated calls don't re-warn
	got = cachedPodOverwriteEnv("not-a-bool")
	assert.Equal(t, got, util.OverwriteEnvUnset)
	got = cachedPodOverwriteEnv("not-a-bool") // hit
	assert.Equal(t, got, util.OverwriteEnvUnset)

	// empty short-circuits (no cache entry)
	got = cachedPodOverwriteEnv("")
	assert.Equal(t, got, util.OverwriteEnvUnset)
}

// Test_cachedContainerOverwriteEnv covers hit/miss, malformed-JSON nil caching,
// and empty short-circuit. Distinct JSON per case avoids cross-case sharing.
func Test_cachedContainerOverwriteEnv(t *testing.T) {
	// miss → decode + cache
	got := cachedContainerOverwriteEnv(`{"main":"true"}`)
	assert.Equal(t, len(got), 1)
	assert.Equal(t, got["main"], util.OverwriteEnvOn)
	// hit (same JSON) → returns cached map
	got = cachedContainerOverwriteEnv(`{"main":"true"}`)
	assert.Equal(t, got["main"], util.OverwriteEnvOn)

	// malformed JSON caches nil so repeated calls don't re-decode/re-warn
	got = cachedContainerOverwriteEnv("not-json")
	assert.Assert(t, got == nil, "malformed JSON must return nil")
	got = cachedContainerOverwriteEnv("not-json") // hit, no re-warn
	assert.Assert(t, got == nil)

	// empty short-circuits (no cache entry)
	got = cachedContainerOverwriteEnv("")
	assert.Assert(t, got == nil)

	// multi-container JSON
	got = cachedContainerOverwriteEnv(`{"main":"true","sidecar":"false"}`)
	assert.Equal(t, len(got), 2)
	assert.Equal(t, got["main"], util.OverwriteEnvOn)
	assert.Equal(t, got["sidecar"], util.OverwriteEnvOff)
}

// Test_cachedOverwriteEnv_DecisionEquivalence confirms the cached ascend path
// (cachedPodOverwriteEnv + cachedContainerOverwriteEnv + ResolveOverwriteEnv)
// produces the same decision as the uncached util.OverwriteEnvDecision for
// representative inputs.
func Test_cachedOverwriteEnv_DecisionEquivalence(t *testing.T) {
	cases := []struct {
		name    string
		podVal  string
		rawJSON string
		ctrName string
	}{
		{"pod-only-true", "true", "", "main"},
		{"pod-only-false", "false", "", "main"},
		{"container-overrides-pod", "false", `{"main":"true"}`, "main"},
		{"unlisted-container-falls-back", "true", `{"other":"false"}`, "main"},
		{"malformed-json-falls-back", "true", "not-json", "main"},
		{"no-annotations", "", "", "main"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pod := &corev1.Pod{}
			pod.Annotations = map[string]string{}
			if c.podVal != "" {
				pod.Annotations[util.OverwriteEnvAnnotationKey] = c.podVal
			}
			if c.rawJSON != "" {
				pod.Annotations[util.OverwriteEnvContainersAnnotationKey] = c.rawJSON
			}
			ctr := &corev1.Container{Name: c.ctrName}
			want := util.OverwriteEnvDecision(pod, ctr)
			podMode := cachedPodOverwriteEnv(c.podVal)
			entries := cachedContainerOverwriteEnv(c.rawJSON)
			got := util.ResolveOverwriteEnv(podMode, entries, ctr)
			assert.Equal(t, got, want, "cached path must match uncached decision")
		})
	}
}
