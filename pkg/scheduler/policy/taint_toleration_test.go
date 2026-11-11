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

func taintedNode(taints ...corev1.Taint) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "n"},
		Spec:       corev1.NodeSpec{Taints: taints},
	}
}

func preferNoSchedule(key, value string) corev1.Taint {
	return corev1.Taint{Key: key, Value: value, Effect: corev1.TaintEffectPreferNoSchedule}
}

func TestPreferNoScheduleTolerations(t *testing.T) {
	in := []corev1.Toleration{
		{Key: "a", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule},
		{Key: "b", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectPreferNoSchedule},
		{Key: "c", Operator: corev1.TolerationOpExists},
		{Key: "d", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute},
	}
	got := PreferNoScheduleTolerations(in)
	assert.DeepEqual(t, got, []corev1.Toleration{in[1], in[2]})
	assert.Assert(t, PreferNoScheduleTolerations(nil) == nil)
}

func TestCountIntolerablePreferNoScheduleTaints(t *testing.T) {
	training := preferNoSchedule("dedicated", "training")
	tests := []struct {
		name        string
		node        *corev1.Node
		tolerations []corev1.Toleration
		want        int
	}{
		{name: "nil node", node: nil, want: 0},
		{name: "no taints", node: taintedNode(), want: 0},
		{name: "untolerated PreferNoSchedule", node: taintedNode(training), want: 1},
		{
			name: "NoSchedule and NoExecute are not counted",
			node: taintedNode(
				corev1.Taint{Key: "x", Effect: corev1.TaintEffectNoSchedule},
				corev1.Taint{Key: "y", Effect: corev1.TaintEffectNoExecute},
			),
			want: 0,
		},
		{
			name: "every untolerated taint counts",
			node: taintedNode(training, preferNoSchedule("zone", "slow"), preferNoSchedule("team", "a")),
			want: 3,
		},
		{
			name:        "Equal toleration with matching value",
			node:        taintedNode(training),
			tolerations: []corev1.Toleration{{Key: "dedicated", Operator: corev1.TolerationOpEqual, Value: "training", Effect: corev1.TaintEffectPreferNoSchedule}},
			want:        0,
		},
		{
			name:        "Equal toleration with another value",
			node:        taintedNode(training),
			tolerations: []corev1.Toleration{{Key: "dedicated", Operator: corev1.TolerationOpEqual, Value: "inference"}},
			want:        1,
		},
		{
			name:        "Exists toleration on the key",
			node:        taintedNode(training, preferNoSchedule("zone", "slow")),
			tolerations: []corev1.Toleration{{Key: "dedicated", Operator: corev1.TolerationOpExists}},
			want:        1,
		},
		{
			name:        "empty key with Exists tolerates everything",
			node:        taintedNode(training, preferNoSchedule("zone", "slow")),
			tolerations: []corev1.Toleration{{Operator: corev1.TolerationOpExists}},
			want:        0,
		},
		{
			name:        "Gt toleration is evaluated",
			node:        taintedNode(preferNoSchedule("tier", "5")),
			tolerations: []corev1.Toleration{{Key: "tier", Operator: corev1.TolerationOpGt, Value: "3"}},
			want:        0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, CountIntolerablePreferNoScheduleTaints(tc.node, tc.tolerations), tc.want)
		})
	}
}

func TestAddTaintTolerationScore(t *testing.T) {
	binpack := util.NodeSchedulerPolicyBinpack.String()
	spread := util.NodeSchedulerPolicySpread.String()
	tests := []struct {
		name            string
		count, maxCount int
		policy          string
		weight          float32
		want            float32
	}{
		{name: "no taint is unchanged", count: 0, maxCount: 2, policy: binpack, weight: 50, want: 10},
		{name: "no tainted node at all", count: 0, maxCount: 0, policy: binpack, weight: 50, want: 10},
		{name: "binpack subtracts the full weight at max", count: 2, maxCount: 2, policy: binpack, weight: 50, want: -40},
		{name: "binpack scales with count", count: 1, maxCount: 2, policy: binpack, weight: 50, want: -15},
		{name: "spread adds the full weight at max", count: 2, maxCount: 2, policy: spread, weight: 50, want: 60},
		{name: "spread scales with count", count: 1, maxCount: 4, policy: spread, weight: 40, want: 20},
		{name: "unknown policy behaves as binpack", count: 1, maxCount: 1, policy: "", weight: 5, want: 5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ns := &NodeScore{NodeID: "n", Score: 10}
			ns.AddTaintTolerationScore(tc.count, tc.maxCount, tc.policy, tc.weight)
			assertScoreNear(t, ns.Score, tc.want)
		})
	}
}
