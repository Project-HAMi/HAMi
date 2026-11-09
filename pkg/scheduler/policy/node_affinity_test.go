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

package policy

import (
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/util"
)

func preferTerm(weight int32, key, value string) corev1.PreferredSchedulingTerm {
	return corev1.PreferredSchedulingTerm{
		Weight: weight,
		Preference: corev1.NodeSelectorTerm{
			MatchExpressions: []corev1.NodeSelectorRequirement{{
				Key: key, Operator: corev1.NodeSelectorOpIn, Values: []string{value},
			}},
		},
	}
}

// assertScoreNear compares float32 scores with a tolerance: 100 * (30/100)
// evaluates to 30.000002 in float32, which is not a bug worth failing on.
func assertScoreNear(t *testing.T, got, want float32) {
	t.Helper()
	if diff := got - want; diff > 1e-3 || diff < -1e-3 {
		t.Fatalf("score = %f, want %f", got, want)
	}
}

func labelledNode(labels map[string]string) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n", Labels: labels}}
}

func TestNewPreferredNodeAffinity(t *testing.T) {
	t.Run("sums every term weight", func(t *testing.T) {
		pref, err := NewPreferredNodeAffinity([]corev1.PreferredSchedulingTerm{
			preferTerm(70, "rack", "r1"),
			preferTerm(30, "disk", "nvme"),
		})
		assert.NilError(t, err)
		assert.Equal(t, pref.totalWeight, int64(100))
		assert.Assert(t, pref.terms != nil)
	})

	t.Run("no terms give nil", func(t *testing.T) {
		pref, err := NewPreferredNodeAffinity(nil)
		assert.NilError(t, err)
		assert.Assert(t, pref == nil)
	})

	t.Run("invalid operator is reported", func(t *testing.T) {
		bad := preferTerm(10, "rack", "r1")
		bad.Preference.MatchExpressions[0].Operator = "Bogus"
		pref, err := NewPreferredNodeAffinity([]corev1.PreferredSchedulingTerm{bad})
		assert.ErrorContains(t, err, "Bogus")
		assert.Assert(t, pref == nil)
	})

	t.Run("empty and zero-weight terms are left out of the denominator", func(t *testing.T) {
		pref, err := NewPreferredNodeAffinity([]corev1.PreferredSchedulingTerm{
			{Weight: 100}, // empty preference: component-helpers skips it
			preferTerm(0, "disk", "nvme"),
			preferTerm(100, "rack", "r1"),
		})
		assert.NilError(t, err)
		assert.Equal(t, pref.totalWeight, int64(100))
	})

	t.Run("only empty terms give nil", func(t *testing.T) {
		pref, err := NewPreferredNodeAffinity([]corev1.PreferredSchedulingTerm{{Weight: 100}})
		assert.NilError(t, err)
		assert.Assert(t, pref == nil)
	})

	t.Run("a malformed term does not drop the valid ones", func(t *testing.T) {
		// Gt with a non-integer value passes API validation but not the matcher.
		bad := preferTerm(50, "gpu-count", "abc")
		bad.Preference.MatchExpressions[0].Operator = corev1.NodeSelectorOpGt
		pref, err := NewPreferredNodeAffinity([]corev1.PreferredSchedulingTerm{
			bad,
			preferTerm(70, "rack", "r1"),
		})
		assert.ErrorContains(t, err, "preferred term 0")
		assert.Assert(t, pref != nil)
		assert.Equal(t, pref.totalWeight, int64(70))
	})
}

func TestAddNodeAffinityScore(t *testing.T) {
	twoTerms := []corev1.PreferredSchedulingTerm{
		preferTerm(70, "rack", "r1"),
		preferTerm(30, "disk", "nvme"),
	}
	mustParse := func(t *testing.T, terms []corev1.PreferredSchedulingTerm) *PreferredNodeAffinity {
		t.Helper()
		pref, err := NewPreferredNodeAffinity(terms)
		assert.NilError(t, err)
		return pref
	}

	tests := []struct {
		name   string
		pref   *PreferredNodeAffinity
		node   *corev1.Node
		policy string
		weight float32
		base   float32
		want   float32
	}{
		{
			name:   "nil pref leaves the score untouched",
			pref:   nil,
			node:   labelledNode(map[string]string{"rack": "r1"}),
			policy: util.NodeSchedulerPolicyBinpack.String(),
			weight: 100, base: 9, want: 9,
		},
		{
			name:   "nil node leaves the score untouched",
			pref:   mustParse(t, twoTerms),
			node:   nil,
			policy: util.NodeSchedulerPolicyBinpack.String(),
			weight: 100, base: 9, want: 9,
		},
		{
			name:   "no usable terms leave the score untouched",
			pref:   mustParse(t, nil),
			node:   labelledNode(map[string]string{"rack": "r1"}),
			policy: util.NodeSchedulerPolicyBinpack.String(),
			weight: 100, base: 9, want: 9,
		},
		{
			name:   "weight 0 disables the bonus",
			pref:   mustParse(t, twoTerms),
			node:   labelledNode(map[string]string{"rack": "r1", "disk": "nvme"}),
			policy: util.NodeSchedulerPolicyBinpack.String(),
			weight: 0, base: 9, want: 9,
		},
		{
			name:   "binpack full match adds the whole weight",
			pref:   mustParse(t, twoTerms),
			node:   labelledNode(map[string]string{"rack": "r1", "disk": "nvme"}),
			policy: util.NodeSchedulerPolicyBinpack.String(),
			weight: 100, base: 9, want: 109,
		},
		{
			name:   "spread full match subtracts the whole weight",
			pref:   mustParse(t, twoTerms),
			node:   labelledNode(map[string]string{"rack": "r1", "disk": "nvme"}),
			policy: util.NodeSchedulerPolicySpread.String(),
			weight: 100, base: 9, want: -91,
		},
		{
			name:   "partial match follows the weight ratio (70/100)",
			pref:   mustParse(t, twoTerms),
			node:   labelledNode(map[string]string{"rack": "r1"}),
			policy: util.NodeSchedulerPolicyBinpack.String(),
			weight: 100, base: 0, want: 70,
		},
		{
			name:   "partial match follows the weight ratio (30/100)",
			pref:   mustParse(t, twoTerms),
			node:   labelledNode(map[string]string{"disk": "nvme"}),
			policy: util.NodeSchedulerPolicyBinpack.String(),
			weight: 100, base: 0, want: 30,
		},
		{
			name:   "ratio is scaled by the configured weight",
			pref:   mustParse(t, twoTerms),
			node:   labelledNode(map[string]string{"rack": "r1"}),
			policy: util.NodeSchedulerPolicyBinpack.String(),
			weight: 10, base: 0, want: 7,
		},
		{
			name:   "no match leaves the score untouched",
			pref:   mustParse(t, twoTerms),
			node:   labelledNode(map[string]string{"rack": "r2"}),
			policy: util.NodeSchedulerPolicyBinpack.String(),
			weight: 100, base: 9, want: 9,
		},
		{
			name:   "node without labels matches nothing",
			pref:   mustParse(t, twoTerms),
			node:   labelledNode(nil),
			policy: util.NodeSchedulerPolicySpread.String(),
			weight: 100, base: 9, want: 9,
		},
	}
	t.Run("empty term does not halve the bonus", func(t *testing.T) {
		pref := mustParse(t, []corev1.PreferredSchedulingTerm{
			{Weight: 100},
			preferTerm(100, "rack", "r1"),
		})
		ns := &NodeScore{NodeID: "n", Node: labelledNode(map[string]string{"rack": "r1"})}
		ns.AddNodeAffinityScore(pref, util.NodeSchedulerPolicyBinpack.String(), 100)
		assertScoreNear(t, ns.Score, 100) // was 50 when the empty term counted
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ns := &NodeScore{NodeID: "n", Node: tt.node, Score: tt.base}
			ns.AddNodeAffinityScore(tt.pref, tt.policy, tt.weight)
			assertScoreNear(t, ns.Score, tt.want)
		})
	}
}
