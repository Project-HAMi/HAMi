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
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/util"
)

// labeledNode returns a node carrying the given labels.
func labeledNode(name string, labels map[string]string) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
}

// exampleFabric is an eight-node, three-level fabric: two nodes per tier 1
// domain, two tier 1 domains per tier 2 domain, and both tier 2 domains under a
// single tier 3 domain.
func exampleFabric() []*corev1.Node {
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

func exampleTree(t *testing.T) *Tree {
	t.Helper()
	tiers, err := ParseTiers("test-tier-1,test-tier-2,test-tier-3")
	require.NoError(t, err)
	return Build(exampleFabric(), tiers)
}

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
			name: "canonical superpod labels",
			spec: util.RackIDLabel + "," + util.SuperPodIDLabel,
			want: []Tier{
				{Level: 1, Key: util.RackIDLabel},
				{Level: 2, Key: util.SuperPodIDLabel},
			},
		},
		{
			name: "explicit levels are honoured",
			spec: "tor-id=3,rack-id=1",
			want: []Tier{
				{Level: 3, Key: "tor-id"},
				{Level: 1, Key: "rack-id"},
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
		{name: "annotation key", spec: "@hami.io/superpodid"},
		{name: "annotation key among labels", spec: util.RackIDLabel + ",@hami.io/superpodid"},
		{name: "key with spaces", spec: "rack id," + util.SuperPodIDLabel},
		{name: "level above the maximum", spec: "rack-id=" + strconv.Itoa(MaxTierLevel+1)},
		{name: "max int64 level", spec: "rack-id=9223372036854775807"},
		{name: "mix explicit and plain keys", spec: "rack-id=2," + util.SuperPodIDLabel},
		{name: "mix plain then explicit", spec: util.RackIDLabel + ",tor-id=2"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseTiers(tc.spec)
			require.Error(t, err)
		})
	}
}

func TestParseTiersAcceptsBoundaryLevels(t *testing.T) {
	got, err := ParseTiers("tor-id=3,rack-id=1")
	require.NoError(t, err)
	assert.Equal(t, []Tier{
		{Level: 3, Key: "tor-id"},
		{Level: 1, Key: "rack-id"},
	}, got)

	got, err = ParseTiers("fabric-id=" + strconv.Itoa(MaxTierLevel))
	require.NoError(t, err)
	assert.Equal(t, []Tier{{Level: MaxTierLevel, Key: "fabric-id"}}, got)
}

func TestBuildTreeShape(t *testing.T) {
	tree := exampleTree(t)

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
	tree := exampleTree(t)

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

func TestBuildSkipsMissingLevels(t *testing.T) {
	nodes := append(exampleFabric(),
		labeledNode("leaf-a", map[string]string{"test-tier-1": "HyperNode2"}),
		labeledNode("leaf-b", map[string]string{"test-tier-1": "HyperNode2"}),
		labeledNode("mid-missing", map[string]string{"test-tier-1": "HyperNode0", "test-tier-3": "HyperNode6"}),
		labeledNode("fabric-only", map[string]string{"test-tier-3": "HyperNode6"}),
		labeledNode("unlabeled", nil),
	)
	tiers, err := ParseTiers("test-tier-1,test-tier-2,test-tier-3")
	require.NoError(t, err)
	tree := Build(nodes, tiers)

	assert.True(t, tree.InModel("leaf-a"))
	assert.False(t, tree.InModel("unlabeled"))
	// Same local label with the same (empty) parent path still shares a domain.
	assert.Equal(t, 1, tree.TierOf("leaf-a", "leaf-b"))
	// A local-only node does not merge with a fully labeled node that carries
	// different higher-tier parents, even when the local label matches.
	assert.Equal(t, tree.UnknownTier(), tree.TierOf("leaf-a", "node4"))
	assert.Equal(t, tree.UnknownTier(), tree.TierOf("node0", "unlabeled"))
	assert.Equal(t, 0, tree.TierOf("unlabeled", "unlabeled"))

	// A missing mid-tier is skipped, so the node still joins the fabric above.
	assert.True(t, tree.InModel("mid-missing"))
	assert.Nil(t, tree.Chain("mid-missing")[2])
	assert.Equal(t, 3, tree.TierOf("mid-missing", "node4"))
	assert.Equal(t, 3, tree.TierOf("fabric-only", "node0"))
}

func TestBuildReadsCanonicalSuperpodLabels(t *testing.T) {
	nodes := []*corev1.Node{
		labeledNode("node0", map[string]string{util.RackIDLabel: "rack-a", util.SuperPodIDLabel: "sp-1"}),
		labeledNode("node1", map[string]string{util.RackIDLabel: "rack-a", util.SuperPodIDLabel: "sp-1"}),
		labeledNode("node2", map[string]string{util.RackIDLabel: "rack-b", util.SuperPodIDLabel: "sp-1"}),
		labeledNode("node3", map[string]string{util.RackIDLabel: "rack-c", util.SuperPodIDLabel: "sp-2"}),
	}
	tiers, err := ParseTiers(util.RackIDLabel + "," + util.SuperPodIDLabel)
	require.NoError(t, err)
	tree := Build(nodes, tiers)

	assert.Equal(t, 1, tree.TierOf("node0", "node1"))
	assert.Equal(t, 2, tree.TierOf("node0", "node2"))
	assert.Equal(t, tree.UnknownTier(), tree.TierOf("node0", "node3"))
}

func TestBuildKeysLocalDomainsByParent(t *testing.T) {
	nodes := []*corev1.Node{
		labeledNode("node0", map[string]string{util.RackIDLabel: "rack-a", util.SuperPodIDLabel: "sp-1"}),
		labeledNode("node1", map[string]string{util.RackIDLabel: "rack-a", util.SuperPodIDLabel: "sp-2"}),
		labeledNode("node2", map[string]string{util.SuperPodIDLabel: "sp-1"}),
	}
	tiers, err := ParseTiers(util.RackIDLabel + "," + util.SuperPodIDLabel)
	require.NoError(t, err)
	tree := Build(nodes, tiers)

	// Same rack label under different superpods must not merge.
	assert.Equal(t, tree.UnknownTier(), tree.TierOf("node0", "node1"))
	assert.Equal(t, 2, tree.TierOf("node0", "node2"))
	assert.NotEqual(t, tree.Chain("node0")[1], tree.Chain("node1")[1])
	assert.Equal(t, util.SuperPodIDLabel+"/sp-1/"+util.RackIDLabel+"/rack-a", tree.Chain("node0")[1].Key)
	assert.Equal(t, util.SuperPodIDLabel+"/sp-2/"+util.RackIDLabel+"/rack-a", tree.Chain("node1")[1].Key)
}

func TestLCAUsesTheDeepestSharedDomain(t *testing.T) {
	nodes := []*corev1.Node{
		labeledNode("node0", map[string]string{"test-tier-1": "HyperNode0", "test-tier-2": "HyperNode4"}),
		labeledNode("node1", map[string]string{"test-tier-1": "HyperNode0", "test-tier-2": "HyperNode4"}),
		labeledNode("node2", map[string]string{"test-tier-1": "HyperNode0", "test-tier-2": "HyperNode5"}),
	}
	tiers, err := ParseTiers("test-tier-1,test-tier-2")
	require.NoError(t, err)
	tree := Build(nodes, tiers)

	assert.Equal(t, 1, tree.TierOf("node0", "node1"))
	assert.Equal(t, "tier1/test-tier-2/HyperNode4/test-tier-1/HyperNode0", tree.LCA("node0", "node1").String())
	assert.Equal(t, []string{"node0", "node1"}, tree.LCA("node0", "node1").Nodes)
	assert.Equal(t, 0, tree.TierOf("node0", "node0"))

	// Parent-keyed racks: the same local label under a different parent does not merge.
	assert.Equal(t, tree.UnknownTier(), tree.TierOf("node0", "node2"))
	assert.Nil(t, tree.LCA("node0", "node2"))
	assert.NotEqual(t, tree.Chain("node0")[1].Key, tree.Chain("node2")[1].Key)
}

func TestDisjointDomainsHaveNoLCA(t *testing.T) {
	tiers, err := ParseTiers("test-tier-1,test-tier-2,test-tier-3")
	require.NoError(t, err)
	tree := Build(disjointFabric(), tiers)

	assert.True(t, tree.InModel("node0"))
	assert.True(t, tree.InModel("node8"))
	assert.Equal(t, tree.UnknownTier(), tree.TierOf("node0", "node8"))
	assert.Nil(t, tree.LCA("node0", "node8"))
}

func TestNilTreeIsInert(t *testing.T) {
	var tree *Tree

	assert.Equal(t, 0, tree.MaxTier())
	assert.Equal(t, 1, tree.UnknownTier())
	assert.False(t, tree.InModel("node0"))
	assert.Nil(t, tree.LCA("node0", "node1"))
	assert.Equal(t, 1, tree.TierOf("node0", "node1"))
	assert.Zero(t, tree.Locality("node0", []string{"node1"}))
	assert.Nil(t, tree.CheckCandidate("node0", []string{"node1"}, 0))
}

func TestCheckCandidateAndLocality(t *testing.T) {
	tree := exampleTree(t)

	assert.Nil(t, tree.CheckCandidate("node1", []string{"node0"}, 1))
	assert.Equal(t, &Violation{Tier: 2, Peer: "node1"}, tree.CheckCandidate("node2", []string{"node1"}, 1))
	assert.Equal(t, &Violation{Tier: 4, Peer: "node0", Unknown: true},
		tree.CheckCandidate("absent", []string{"node0"}, 0))
	assert.Equal(t, &Violation{Tier: 4, Peer: "node0", Unknown: true},
		tree.CheckCandidate("absent", []string{"node0"}, tree.UnknownTier()),
		"unknown domains stay rejected even when the numeric cap is UnknownTier or higher")
	assert.Equal(t, &Violation{Tier: 4, Peer: "node0", Unknown: true},
		tree.CheckCandidate("absent", []string{"node0"}, 100))

	assert.InDelta(t, 1.0, tree.Locality("node1", []string{"node1"}), 0.0001)
	assert.InDelta(t, 0.75, tree.Locality("node1", []string{"node0"}), 0.0001)
	assert.InDelta(t, 0.5, tree.Locality("node1", []string{"node2"}), 0.0001)
	assert.Zero(t, tree.Locality("unlabeled", []string{"node0"}))
}
