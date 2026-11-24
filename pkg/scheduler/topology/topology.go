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

// Package topology models a vendor-neutral interconnect hierarchy from
// operator-provided node labels so later scheduler PRs can keep members of a
// distributed job inside the same network domain.
//
// Tier 0 is the node itself. Configured tiers are numbered from 1 upward in the
// order given on --topology-tier-labels (smallest domain first). The lowest
// common ancestor of two nodes is their distance: the smaller its tier, the
// closer they sit on the fabric.
//
// The tree lives in scheduler memory and is rebuilt from the nodes the
// scheduler already lists. Enabling it adds no CRD and no extra controller.
// Canonical node keys shared with superpod-aware scheduling are
// util.SuperPodIDLabel and util.RackIDLabel (hami.io/superpodid,
// hami.io/rackid); only node labels are accepted.
package topology

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

const (
	// NodeTier is the tier of a node in its own right: the deepest level of the
	// hierarchy. The lowest common ancestor of a node with itself sits here.
	NodeTier = 0

	// domainIDSeparator joins the parts of a domain identity. It cannot appear
	// in a label key, so identities stay unambiguous.
	domainIDSeparator = "\x00"
)

// Tier is one level of the network hierarchy, read from a node label.
type Tier struct {
	// Level is 1 for the smallest domain above the node, and grows with each
	// level up.
	Level int
	// Key is the node label that holds this tier's value.
	Key string
}

// String renders the tier the way it is written in the tier labels flag.
func (t Tier) String() string {
	return t.Key
}

// ParseTiers reads the tier specification from the comma-separated list of
// label keys given on the command line. Entries are numbered from 1 in the
// order given: the most local tier first, the whole fabric last. An entry may
// also carry an explicit level, as in "hami.io/rackid=1". Annotation keys
// (prefixed with "@") are rejected. Duplicate keys and duplicate levels are
// rejected instead of being collapsed, so a mistyped level cannot silently
// leave a gap in the hierarchy. An empty specification disables the feature.
func ParseTiers(spec string) ([]Tier, error) {
	type entry struct {
		tier  Tier
		level int
	}

	entries := make([]entry, 0, 4)
	seenKey := make(map[string]bool)
	seenLevel := make(map[int]bool)
	for _, raw := range strings.Split(spec, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if strings.HasPrefix(raw, "@") {
			return nil, fmt.Errorf("topology tier %q uses an annotation key; only node labels are supported", raw)
		}

		key := raw
		level := 0
		if idx := strings.LastIndex(key, "="); idx >= 0 {
			parsed, err := strconv.Atoi(strings.TrimSpace(key[idx+1:]))
			if err != nil || parsed < 1 {
				return nil, fmt.Errorf("invalid topology tier level in %q: want a positive integer", raw)
			}
			level = parsed
			key = strings.TrimSpace(key[:idx])
		}
		if key == "" {
			return nil, fmt.Errorf("empty topology tier key in %q", raw)
		}

		if seenKey[key] {
			return nil, fmt.Errorf("duplicate topology tier key %q", key)
		}
		seenKey[key] = true
		if level != 0 {
			if seenLevel[level] {
				return nil, fmt.Errorf("duplicate topology tier level %d", level)
			}
			seenLevel[level] = true
		}
		entries = append(entries, entry{tier: Tier{Key: key}, level: level})
	}
	if len(entries) == 0 {
		return nil, nil
	}

	tiers := make([]Tier, 0, len(entries))
	next := 1
	for _, e := range entries {
		if e.level != 0 {
			e.tier.Level = e.level
			tiers = append(tiers, e.tier)
			continue
		}
		for seenLevel[next] {
			next++
		}
		e.tier.Level = next
		seenLevel[next] = true
		tiers = append(tiers, e.tier)
	}
	return tiers, nil
}

// HyperNode is one network domain: every node carrying the same value for the
// domain's tier belongs to it. A tier 0 domain is a single node.
type HyperNode struct {
	// Level is the tier this domain represents.
	Level int
	// Key identifies the domain within its tier, as "<tier key>/<value>".
	Key string
	// Nodes are the nodes inside this domain, sorted by name.
	Nodes []string
}

// Tier is the level this domain represents.
func (h *HyperNode) Tier() int { return h.Level }

// String renders the domain identity for logs.
func (h *HyperNode) String() string {
	if h == nil {
		return "<none>"
	}
	return fmt.Sprintf("tier%d/%s", h.Level, h.Key)
}

// Tree is the network hierarchy of a set of nodes.
type Tree struct {
	tiers   []Tier
	chains  map[string][]*HyperNode
	fabric  []*HyperNode
	maxTier int
}

// Build constructs the hierarchy of nodes for tiers. Nodes that carry no tier
// value at all still get a chain holding only themselves, so that a pod pair on
// one such node is still recognized as sharing a node; they belong to no domain
// above it, and pairs involving them are reported at UnknownTier. A node that
// carries some tier values but not all of them belongs to the levels below the
// first missing value: the chain is truncated, never guessed.
//
// Domains are registered by the value they are built from, so two nodes share a
// domain exactly when they carry the same value for its tier, whatever order the
// nodes are visited in.
func Build(nodes []*corev1.Node, tiers []Tier) *Tree {
	tree := &Tree{
		tiers:  append([]Tier(nil), tiers...),
		chains: make(map[string][]*HyperNode, len(nodes)),
	}
	sort.SliceStable(tree.tiers, func(i, j int) bool { return tree.tiers[i].Level < tree.tiers[j].Level })
	for _, tier := range tree.tiers {
		tree.maxTier = max(tree.maxTier, tier.Level)
	}

	registry := make(map[string]*HyperNode, len(nodes))
	for _, node := range nodes {
		if node == nil || node.Name == "" {
			continue
		}

		chain := make([]*HyperNode, tree.maxTier+1)
		chain[NodeTier] = &HyperNode{Level: NodeTier, Key: node.Name, Nodes: []string{node.Name}}
		for _, tier := range tree.tiers {
			value := tier.Value(node)
			if value == "" {
				break
			}
			chain[tier.Level] = tree.domain(registry, node.Name, tier, value)
		}
		tree.chains[node.Name] = chain
	}
	for _, domain := range tree.fabric {
		domain.Nodes = dedupeSorted(domain.Nodes)
	}
	return tree
}

// domain returns the canonical domain of a tier value, creating it the first
// time some node carries that value and recording nodeName as a member.
func (t *Tree) domain(registry map[string]*HyperNode, nodeName string, tier Tier, value string) *HyperNode {
	id := strconv.Itoa(tier.Level) + domainIDSeparator + tier.Key + domainIDSeparator + value
	domain, ok := registry[id]
	if !ok {
		domain = &HyperNode{Level: tier.Level, Key: tier.Key + "/" + value}
		registry[id] = domain
		t.fabric = append(t.fabric, domain)
	}
	domain.Nodes = append(domain.Nodes, nodeName)
	return domain
}

// Value reads the label this tier takes on a node, or "" when the node carries
// none.
func (t Tier) Value(node *corev1.Node) string {
	if node == nil {
		return ""
	}
	return node.Labels[t.Key]
}

// dedupeSorted sorts values in place and removes duplicate entries.
func dedupeSorted(values []string) []string {
	sort.Strings(values)
	unique := values[:0]
	for i, value := range values {
		if i == 0 || value != values[i-1] {
			unique = append(unique, value)
		}
	}
	return unique
}

// Tiers returns the levels of the tree, most local first.
func (t *Tree) Tiers() []Tier {
	if t == nil {
		return nil
	}
	return append([]Tier(nil), t.tiers...)
}

// MaxTier is the highest level configured, or 0 when the tree carries no tiers.
func (t *Tree) MaxTier() int {
	if t == nil {
		return 0
	}
	return t.maxTier
}

// UnknownTier is the tier reported for a pair of nodes that share no configured
// domain. It sits above every real tier, so a later hard tier limit rejects such
// a pair.
func (t *Tree) UnknownTier() int {
	return t.MaxTier() + 1
}

// HyperNodes returns every domain above the node level, in the order the nodes
// were visited.
func (t *Tree) HyperNodes() []*HyperNode {
	if t == nil {
		return nil
	}
	return append([]*HyperNode(nil), t.fabric...)
}

// Chain returns the domains a node belongs to, starting with the node itself at
// NodeTier. Entries for levels the node carries no value for are nil.
func (t *Tree) Chain(node string) []*HyperNode {
	if t == nil {
		return nil
	}
	return append([]*HyperNode(nil), t.chains[node]...)
}

// InModel reports whether node carries at least one tier value.
func (t *Tree) InModel(node string) bool {
	if t == nil {
		return false
	}
	chain, ok := t.chains[node]
	if !ok {
		return false
	}
	for level := 1; level < len(chain); level++ {
		if chain[level] != nil {
			return true
		}
	}
	return false
}

// LCA returns the lowest common ancestor of a and b: the deepest tier both
// nodes belong to, including NodeTier when they are the same node. It is nil
// when either node is unknown to the tree or the two share no domain at all.
func (t *Tree) LCA(a, b string) *HyperNode {
	if t == nil {
		return nil
	}
	left, ok := t.chains[a]
	if !ok {
		return nil
	}
	right, ok := t.chains[b]
	if !ok {
		return nil
	}
	for level := 0; level < min(len(left), len(right)); level++ {
		if left[level] == nil || right[level] == nil {
			continue
		}
		if left[level] == right[level] {
			return left[level]
		}
	}
	return nil
}

// TierOf returns the tier of the lowest common ancestor of a and b, which is
// UnknownTier when they share no configured domain.
func (t *Tree) TierOf(a, b string) int {
	lca := t.LCA(a, b)
	if lca == nil {
		return t.UnknownTier()
	}
	return lca.Level
}

// Violation is a placement that would stretch a group of pods over more of the
// fabric than it allows. Later PRs use it for hard filtering; this package only
// computes it.
type Violation struct {
	// Tier is the tier of the lowest common ancestor of the candidate and Peer.
	Tier int
	// Peer is the placed node the candidate is too far away from.
	Peer string
	// Unknown is set when Tier is UnknownTier.
	Unknown bool
}

// String renders a rejection for scheduler logs and failed-node messages.
func (v *Violation) String() string {
	if v == nil {
		return ""
	}
	if v.Unknown {
		return fmt.Sprintf("no common topology domain with node %s", v.Peer)
	}
	return fmt.Sprintf("lowest common topology tier with node %s is %d", v.Peer, v.Tier)
}

// CheckCandidate reports why candidate cannot host another member of a group
// whose other members sit on placed nodes, or nil when it can. A pair is
// rejected when its lowest common ancestor is deeper in the hierarchy than
// highestTierAllowed, which includes pairs that share no configured domain.
func (t *Tree) CheckCandidate(candidate string, placed []string, highestTierAllowed int) *Violation {
	if t == nil {
		return nil
	}

	worst := (*Violation)(nil)
	for _, peer := range placed {
		if peer == candidate {
			continue
		}
		tier := t.TierOf(candidate, peer)
		if tier <= highestTierAllowed {
			continue
		}
		if worst == nil || tier > worst.Tier {
			worst = &Violation{Tier: tier, Peer: peer, Unknown: tier == t.UnknownTier()}
		}
	}
	return worst
}

// Locality reports how close candidate is wired to the placed nodes, as a mean
// over them: 1 when candidate is one of them, and 0 when it is outside the
// described fabric or shares no domain with any of them. Later PRs use it for
// soft scoring; this package only computes it.
func (t *Tree) Locality(candidate string, placed []string) float64 {
	if t == nil || len(placed) == 0 {
		return 0
	}

	unknown := float64(t.UnknownTier())
	total, peers := 0.0, 0
	for _, peer := range placed {
		peers++
		total += (unknown - float64(t.TierOf(candidate, peer))) / unknown
	}
	if peers == 0 {
		return 0
	}
	return total / float64(peers)
}
