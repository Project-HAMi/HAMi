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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

func topologyLabeledNode(name string, labels map[string]string) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
}

func TestRebuildTopologyFromTierLabels(t *testing.T) {
	original := config.TopologyTierLabels
	t.Cleanup(func() { config.TopologyTierLabels = original })

	scheduler := NewScheduler()

	tests := []struct {
		name       string
		tierLabels string
		wantNil    bool
		wantTiers  int
	}{
		{name: "empty disables the feature", tierLabels: "", wantNil: true},
		{name: "invalid disables the feature", tierLabels: "@hami.io/superpodid", wantNil: true},
		{
			name:       "canonical labels build a tree",
			tierLabels: util.RackIDLabel + "," + util.SuperPodIDLabel,
			wantTiers:  2,
		},
	}

	nodes := []*corev1.Node{
		topologyLabeledNode("n0", map[string]string{util.RackIDLabel: "r1", util.SuperPodIDLabel: "s1"}),
		topologyLabeledNode("n1", map[string]string{util.RackIDLabel: "r1", util.SuperPodIDLabel: "s1"}),
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config.TopologyTierLabels = tt.tierLabels
			scheduler.rebuildTopology(nodes)
			view := scheduler.topologySnapshot()
			if tt.wantNil {
				assert.Nil(t, view)
				return
			}
			require.NotNil(t, view)
			assert.Equal(t, tt.wantTiers, view.tree.MaxTier())
			assert.Equal(t, 1, view.tree.TierOf("n0", "n1"))
		})
	}
}

func TestOnUpdateNodeNotifiesTopologyChange(t *testing.T) {
	original := config.TopologyTierLabels
	t.Cleanup(func() { config.TopologyTierLabels = original })
	config.TopologyTierLabels = util.RackIDLabel + "," + util.SuperPodIDLabel

	scheduler := NewScheduler()
	oldNode := topologyLabeledNode("n0", map[string]string{util.RackIDLabel: "r1", util.SuperPodIDLabel: "s1"})
	scheduler.rebuildTopology([]*corev1.Node{oldNode})

	tests := []struct {
		name       string
		newNode    *corev1.Node
		wantNotify bool
	}{
		{
			name: "unchanged labels do not notify",
			newNode: topologyLabeledNode("n0", map[string]string{
				util.RackIDLabel: "r1", util.SuperPodIDLabel: "s1",
			}),
		},
		{
			name: "tier label change notifies",
			newNode: topologyLabeledNode("n0", map[string]string{
				util.RackIDLabel: "r2", util.SuperPodIDLabel: "s1",
			}),
			wantNotify: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			select {
			case <-scheduler.nodeNotify:
			default:
			}
			scheduler.onUpdateNode(oldNode, tt.newNode)
			notified := false
			select {
			case <-scheduler.nodeNotify:
				notified = true
			default:
			}
			assert.Equal(t, tt.wantNotify, notified)
		})
	}
}
