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

package scheduler

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/topology"
)

// topologyView is one immutable snapshot of the cluster interconnect hierarchy
// built from node labels. Placement decisions that consume this view land in
// later PRs; this file only keeps the cache fresh.
type topologyView struct {
	tree  *topology.Tree
	tiers []topology.Tier
}

// rebuildTopology replaces the cached hierarchy with one built from nodes. It
// is called from register, which already holds the scheduler lock and has
// already listed the nodes, so a relabeled fabric is picked up on the next
// register tick without any additional watch.
func (s *Scheduler) rebuildTopology(nodes []*corev1.Node) {
	tiers, err := topology.ParseTiers(config.TopologyTierLabels)
	if err != nil {
		klog.ErrorS(err, "Ignoring invalid topology tier labels", "tierLabels", config.TopologyTierLabels)
		s.topology.Store(nil)
		return
	}
	if len(tiers) == 0 {
		s.topology.Store(nil)
		return
	}
	s.topology.Store(&topologyView{tree: topology.Build(nodes, tiers), tiers: tiers})
}

// topologySnapshot returns the cached hierarchy, nil when the feature is off.
func (s *Scheduler) topologySnapshot() *topologyView {
	return s.topology.Load()
}

// notifyIfTopologyChanged asks register to rebuild the hierarchy when a node's
// configured tier labels change. Other node updates still wait for the ticker.
//
// The configured tiers are consulted even before the first topology view is
// published, so a label update that races the first register still triggers a
// refresh instead of waiting for the next ticker.
func (s *Scheduler) notifyIfTopologyChanged(oldNode, newNode *corev1.Node) {
	tiers := s.configuredTopologyTiers()
	if len(tiers) == 0 {
		return
	}
	for _, tier := range tiers {
		if tier.Value(oldNode) == tier.Value(newNode) {
			continue
		}
		klog.InfoS("Node topology labels changed, refreshing the hierarchy",
			"node", newNode.Name, "tier", tier.String(),
			"from", tier.Value(oldNode), "to", tier.Value(newNode))
		s.doNodeNotify()
		return
	}
}

// configuredTopologyTiers returns the tiers named by --topology-tier-labels,
// preferring the cached view when it is already built. An empty or invalid
// configuration disables notifications.
func (s *Scheduler) configuredTopologyTiers() []topology.Tier {
	if view := s.topologySnapshot(); view != nil {
		return view.tiers
	}
	tiers, err := topology.ParseTiers(config.TopologyTierLabels)
	if err != nil || len(tiers) == 0 {
		return nil
	}
	return tiers
}
