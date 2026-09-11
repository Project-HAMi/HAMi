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

package ascend

import (
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"

	"github.com/Project-HAMi/HAMi/pkg/util"
)

// Test_cachedContainerOverwriteEnv covers hit/miss, malformed-JSON nil caching,
// and empty short-circuit. Distinct JSON per case avoids cross-case sharing.
func Test_cachedContainerOverwriteEnv(t *testing.T) {
	// miss → decode + cache, lookup by container name
	mode, listed := cachedContainerOverwriteEnv(`{"main":"true"}`, "main")
	assert.Equal(t, mode, util.OverwriteEnvOn)
	assert.Assert(t, listed)
	// hit (same JSON) → same answer, no re-decode
	mode, listed = cachedContainerOverwriteEnv(`{"main":"true"}`, "main")
	assert.Equal(t, mode, util.OverwriteEnvOn)
	assert.Assert(t, listed)
	// unlisted container
	mode, listed = cachedContainerOverwriteEnv(`{"main":"true"}`, "sidecar")
	assert.Equal(t, mode, util.OverwriteEnvUnset)
	assert.Assert(t, !listed)

	// malformed JSON caches nil so repeated calls don't re-decode/re-warn
	mode, listed = cachedContainerOverwriteEnv("not-json", "main")
	assert.Equal(t, mode, util.OverwriteEnvUnset)
	assert.Assert(t, !listed)
	_, listed = cachedContainerOverwriteEnv("not-json", "main") // hit, no re-warn
	assert.Assert(t, !listed)

	// empty short-circuits (no cache entry)
	_, listed = cachedContainerOverwriteEnv("", "main")
	assert.Assert(t, !listed)
}

// Test_cachedOverwriteEnv_DecisionEquivalence confirms the cached ascend path
// (util.ParsePodOverwriteEnv + cachedContainerOverwriteEnv) with the inline
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
			podMode, _ := util.ParsePodOverwriteEnv(c.podVal)
			if ctrMode, listed := cachedContainerOverwriteEnv(c.rawJSON, c.ctrName); listed {
				podMode = ctrMode
			}
			assert.Equal(t, podMode, want, "cached path must match uncached decision")
		})
	}
}
