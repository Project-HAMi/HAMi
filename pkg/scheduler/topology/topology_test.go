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

package topology

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func labeledNode(name string, labels map[string]string) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
}

func annotatedNode(name string, annotations map[string]string) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Annotations: annotations}}
}

// cceFabric is the eight node, three level fabric of the CCE multi-level
// topology example: two nodes per tier 1 domain, two tier 1 domains per tier 2
// domain, and both tier 2 domains under a single tier 3 domain.
func cceFabric() []*corev1.Node {
	return []*corev1.Node{
		labeledNode("node0", map[string]string{"test-tier-1": "HyperNode0", "test-tier-2": "HyperNode4", "test-tier-3": "HyperNode6"}),
		labeledNode("node1", map[string]string{"test-tier-1": "HyperNode0", "test-tier-2": "HyperNode4", "test-tier-3": "HyperNode6"}),
		labeledNode("node2", map[string]string{"test-tier-1": "HyperNode1", "test-tier-2": "HyperNode4", "test-tier-3": "HyperNode6"}),
		labeledNode("node3", map[string]string{"test-tier-1": "HyperNode1", "test-tier-2": "HyperNode4", "test-tier-3": "HyperNode6"}),
		labeledNode("node4", map[string]string{"test-tier-1": "HyperNode2", "test-tier-2": "HyperNode5", "test-tier-3": "HyperNode6"}),
		labeledNode("node5", map[string]string{"test-tier-1": "HyperNode2", "test-tier-2": "HyperNode5", "test-tier-3": "HyperNode6"}),
		labeledNode("node6", map[string]string{"test-tier-1": "HyperNode3", "test-tier-2": "HyperNode5", "test-tier-3": "HyperNode6"}),
		labeledNode("node7", map[string]string{"test-tier-1": "HyperNode3", "test-tier-2": "HyperNode5", "test-tier-3": "HyperNode6"}),
	}
}

func cceTree(t *testing.T) *Tree {
	t.Helper()
	tiers, err := ParseTiers("test-tier-1,test-tier-2,test-tier-3")
	require.NoError(t, err)
	return Build(cceFabric(), tiers)
}

// disjointFabric holds two nodes whose domains have nothing in common: they sit
// in different tier 1, tier 2 and tier 3 domains, which is what a pair of nodes
// wired to separate spine switches looks like.
func disjointFabric() []*corev1.Node {
	return []*corev1.Node{
		labeledNode("node0", map[string]string{"test-tier-1": "HyperNode0", "test-tier-2": "HyperNode4", "test-tier-3": "HyperNode6"}),
		labeledNode("node8", map[string]string{"test-tier-1": "HyperNode8", "test-tier-2": "HyperNode9", "test-tier-3": "HyperNode10"}),
	}
}

func TestParseTiers(t *testing.T) {
	tests := []struct {
		name string
		spec string
		want []Tier
	}{
		{
			name: "empty specification disables the feature",
			spec: "",
			want: nil,
		},
		{
			name: "keys are numbered from the most local tier",
			spec: " rack-id , tor-id ",
			want: []Tier{
				{Level: 1, Key: "rack-id"},
				{Level: 2, Key: "tor-id"},
			},
		},
		{
			name: "an at sign selects a node annotation",
			spec: "@hami.io/npu-super-pod-id",
			want: []Tier{{Level: 1, Key: "hami.io/npu-super-pod-id", Source: FromAnnotation}},
		},
		{
			name: "explicit levels are honoured",
			spec: "tor-id=3,rack-id=1",
			want: []Tier{
				{Level: 3, Key: "tor-id"},
				{Level: 1, Key: "rack-id"},
			},
		},
		{
			name: "implicit levels skip the ones taken explicitly",
			spec: "tor-id=1,rack-id",
			want: []Tier{
				{Level: 1, Key: "tor-id"},
				{Level: 2, Key: "rack-id"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseTiers(tc.spec)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestParseTiersRejectsAmbiguousSpecs(t *testing.T) {
	tests := []struct {
		name string
		spec string
	}{
		{name: "duplicate key", spec: "rack-id,rack-id"},
		{name: "duplicate explicit level", spec: "rack-id=1,tor-id=1"},
		{name: "empty key", spec: "rack-id,=2"},
		{name: "non numeric level", spec: "rack-id=two"},
		{name: "zero level", spec: "rack-id=0"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseTiers(tc.spec)
			require.Error(t, err)
		})
	}
}

func TestBuildTreeShape(t *testing.T) {
	tree := cceTree(t)

	assert.Equal(t, 3, tree.MaxTier())
	assert.Equal(t, 4, tree.UnknownTier())
	assert.Equal(t, []Tier{
		{Level: 1, Key: "test-tier-1"},
		{Level: 2, Key: "test-tier-2"},
		{Level: 3, Key: "test-tier-3"},
	}, tree.Tiers())

	perLevel := map[int]int{}
	for _, domain := range tree.HyperNodes() {
		perLevel[domain.Level]++
	}
	assert.Equal(t, map[int]int{1: 4, 2: 2, 3: 1}, perLevel)
	assert.Len(t, tree.HyperNodes()[0].Nodes, 2)
}

func TestTierOf(t *testing.T) {
	tree := cceTree(t)

	tests := []struct {
		name  string
		left  string
		right string
		want  int
	}{
		{name: "same node", left: "node0", right: "node0", want: 0},
		{name: "same tier 1 domain", left: "node0", right: "node1", want: 1},
		{name: "same tier 2 domain", left: "node1", right: "node2", want: 2},
		{name: "only the whole fabric in common", left: "node3", right: "node4", want: 3},
		{name: "outermost pair", left: "node0", right: "node7", want: 3},
		{name: "a node the tree does not know", left: "node0", right: "absent", want: 4},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tree.TierOf(tc.left, tc.right))
			assert.Equal(t, tc.want, tree.TierOf(tc.right, tc.left))
		})
	}
}

func TestBuildTruncatesMissingLevels(t *testing.T) {
	nodes := append(cceFabric(),
		labeledNode("leaf-only", map[string]string{"test-tier-1": "HyperNode2"}),
		labeledNode("unlabeled", nil),
	)
	tiers, err := ParseTiers("test-tier-1,test-tier-2,test-tier-3")
	require.NoError(t, err)
	tree := Build(nodes, tiers)

	assert.True(t, tree.InModel("leaf-only"))
	assert.False(t, tree.InModel("unlabeled"))
	assert.Equal(t, 1, tree.TierOf("leaf-only", "node4"))
	assert.Equal(t, tree.UnknownTier(), tree.TierOf("leaf-only", "node0"))
	assert.Equal(t, tree.UnknownTier(), tree.TierOf("node0", "unlabeled"))
	assert.Equal(t, 0, tree.TierOf("unlabeled", "unlabeled"))
}

func TestBuildReadsTierValuesFromAnnotations(t *testing.T) {
	nodes := []*corev1.Node{
		annotatedNode("node0", map[string]string{"hami.io/npu-super-pod-id": "pod-a"}),
		annotatedNode("node1", map[string]string{"hami.io/npu-super-pod-id": "pod-a"}),
		annotatedNode("node2", map[string]string{"hami.io/npu-super-pod-id": "pod-b"}),
	}
	tiers, err := ParseTiers("@hami.io/npu-super-pod-id")
	require.NoError(t, err)
	tree := Build(nodes, tiers)

	assert.Equal(t, 1, tree.MaxTier())
	assert.Equal(t, 1, tree.TierOf("node0", "node1"))
	assert.Equal(t, 0, tree.TierOf("node0", "node0"))
	assert.Nil(t, tree.LCA("node0", "node2"))
	assert.Equal(t, 2, tree.TierOf("node0", "node2"))
}

func TestLCAUsesTheDeepestSharedDomain(t *testing.T) {
	// A tier 1 domain whose members are labeled with two different tier 2
	// domains is an ill-formed wiring, but the tree must still answer with the
	// deepest domain the two nodes do share instead of inventing a hierarchy.
	nodes := []*corev1.Node{
		labeledNode("node0", map[string]string{"test-tier-1": "HyperNode0", "test-tier-2": "HyperNode4"}),
		labeledNode("node1", map[string]string{"test-tier-1": "HyperNode0", "test-tier-2": "HyperNode5"}),
	}
	tiers, err := ParseTiers("test-tier-1,test-tier-2")
	require.NoError(t, err)
	tree := Build(nodes, tiers)

	assert.Equal(t, 1, tree.TierOf("node0", "node1"))
	assert.Equal(t, "tier1/test-tier-1/HyperNode0", tree.LCA("node0", "node1").String())
	assert.Equal(t, 0, tree.TierOf("node0", "node0"))
	assert.Equal(t, []string{"node0", "node1"}, tree.HyperNodes()[0].Nodes)
}

func TestCheckCandidate(t *testing.T) {
	tree := cceTree(t)

	tests := []struct {
		name      string
		candidate string
		placed    []string
		highest   int
		want      *Violation
	}{
		{
			name:      "no placed member leaves the first pod free",
			candidate: "node4",
			placed:    nil,
			highest:   1,
		},
		{
			name:      "same tier 1 domain is accepted at tier 1",
			candidate: "node1",
			placed:    []string{"node0"},
			highest:   1,
		},
		{
			name:      "one tier too far",
			candidate: "node2",
			placed:    []string{"node1"},
			highest:   1,
			want:      &Violation{Tier: 2, Peer: "node1"},
		},
		{
			name:      "the same pair is accepted once the limit is raised",
			candidate: "node2",
			placed:    []string{"node1"},
			highest:   2,
		},
		{
			name:      "sharing only the whole fabric is one tier too far",
			candidate: "node4",
			placed:    []string{"node0"},
			highest:   2,
			want:      &Violation{Tier: 3, Peer: "node0"},
		},
		{
			name:      "the worst placed member is reported",
			candidate: "node1",
			placed:    []string{"node0", "node2"},
			highest:   1,
			want:      &Violation{Tier: 2, Peer: "node2"},
		},
		{
			name:      "a candidate on a node the group already uses is accepted",
			candidate: "node0",
			placed:    []string{"node0"},
			highest:   0,
		},
		{
			name:      "a group already spread too far rejects every node",
			candidate: "node0",
			placed:    []string{"node0", "node4"},
			highest:   0,
			want:      &Violation{Tier: 3, Peer: "node4"},
		},
		{
			name:      "a candidate outside the described fabric is not constrained",
			candidate: "unlabeled",
			placed:    []string{"node0"},
			highest:   0,
		},
		{
			name:      "a placed member outside the described fabric does not constrain",
			candidate: "node1",
			placed:    []string{"unlabeled"},
			highest:   0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tree.CheckCandidate(tc.candidate, tc.placed, tc.highest))
		})
	}
}

func TestCheckCandidateWithoutCommonDomain(t *testing.T) {
	tiers, err := ParseTiers("test-tier-1,test-tier-2,test-tier-3")
	require.NoError(t, err)
	tree := Build(disjointFabric(), tiers)

	assert.True(t, tree.InModel("node0"))
	assert.True(t, tree.InModel("node8"))
	assert.Equal(t, tree.UnknownTier(), tree.TierOf("node0", "node8"))
	assert.Equal(t, &Violation{Tier: 4, Peer: "node8", Unknown: true},
		tree.CheckCandidate("node0", []string{"node8"}, 3))
	assert.Nil(t, tree.CheckCandidate("node0", []string{"node8"}, tree.UnknownTier()))
	assert.Zero(t, tree.Locality("node0", []string{"node8"}))
}

func TestLocality(t *testing.T) {
	tree := cceTree(t)

	tests := []struct {
		name      string
		candidate string
		placed    []string
		want      float64
	}{
		{name: "no placed member scores nothing", candidate: "node1", placed: nil, want: 0},
		{name: "same node", candidate: "node1", placed: []string{"node1"}, want: 1},
		{name: "same tier 1 domain", candidate: "node1", placed: []string{"node0"}, want: 0.75},
		{name: "same tier 2 domain", candidate: "node1", placed: []string{"node2"}, want: 0.5},
		{name: "whole fabric in common", candidate: "node1", placed: []string{"node4"}, want: 0.25},
		{name: "outside the described fabric", candidate: "unlabeled", placed: []string{"node0"}, want: 0},
		{
			name:      "members outside the fabric are not counted",
			candidate: "node1",
			placed:    []string{"node0", "unlabeled"},
			want:      0.75,
		},
		{
			name:      "the mean rewards the domain holding most of the group",
			candidate: "node1",
			placed:    []string{"node0", "node1"},
			want:      0.875,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.InDelta(t, tc.want, tree.Locality(tc.candidate, tc.placed), 0.0001)
		})
	}
}

func TestNilTreeIsInert(t *testing.T) {
	var tree *Tree

	assert.Equal(t, 0, tree.MaxTier())
	assert.Equal(t, 1, tree.UnknownTier())
	assert.False(t, tree.InModel("node0"))
	assert.Nil(t, tree.LCA("node0", "node1"))
	assert.Zero(t, tree.Locality("node0", []string{"node1"}))
	assert.Nil(t, tree.CheckCandidate("node0", []string{"node1"}, 0))
}
