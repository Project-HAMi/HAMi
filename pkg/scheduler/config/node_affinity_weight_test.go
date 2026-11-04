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

package config

import (
	"math"
	"testing"

	"gotest.tools/v3/assert"
)

func TestValidateNodeAffinityScoreWeight(t *testing.T) {
	valid := []float32{0, 0.5, 30, 100, 9999}
	for _, w := range valid {
		assert.NilError(t, ValidateNodeAffinityScoreWeight(w), "weight %v", w)
	}

	invalid := []float32{
		-1,
		-100,
		float32(math.NaN()),
		float32(math.Inf(1)),
		float32(math.Inf(-1)),
	}
	for _, w := range invalid {
		assert.ErrorContains(t, ValidateNodeAffinityScoreWeight(w), "--node-affinity-score-weight", "weight %v", w)
	}
}
