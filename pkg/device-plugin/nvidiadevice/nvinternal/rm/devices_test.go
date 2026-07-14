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

package rm

import (
	"testing"

	"github.com/stretchr/testify/require"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

func TestGetPluginDevicesTopology(t *testing.T) {
	numaTopology := &kubeletdevicepluginv1beta1.TopologyInfo{
		Nodes: []*kubeletdevicepluginv1beta1.NUMANode{{ID: 3}},
	}

	testCases := []struct {
		description          string
		devices              Devices
		count                uint
		numaTopology         bool
		expectedIDs          []string
		expectedTopologyNil  bool
		expectedTopologyNode int64
	}{
		{
			description:  "empty device list returns empty result",
			devices:      Devices{},
			count:        2,
			numaTopology: true,
			expectedIDs:  nil,
		},
		{
			description: "non-MIG replicas do not get topology when disabled",
			devices: Devices{
				"GPU-uuid-1": &Device{
					Device: kubeletdevicepluginv1beta1.Device{
						ID:       "GPU-uuid-1",
						Health:   kubeletdevicepluginv1beta1.Healthy,
						Topology: numaTopology,
					},
				},
			},
			count:               2,
			numaTopology:        false,
			expectedIDs:         []string{"GPU-uuid-1-0", "GPU-uuid-1-1"},
			expectedTopologyNil: true,
		},
		{
			description: "non-MIG replicas inherit topology when enabled",
			devices: Devices{
				"GPU-uuid-1": &Device{
					Device: kubeletdevicepluginv1beta1.Device{
						ID:       "GPU-uuid-1",
						Health:   kubeletdevicepluginv1beta1.Healthy,
						Topology: numaTopology,
					},
				},
			},
			count:                2,
			numaTopology:         true,
			expectedIDs:          []string{"GPU-uuid-1-0", "GPU-uuid-1-1"},
			expectedTopologyNil:  false,
			expectedTopologyNode: 3,
		},
		{
			description: "MIG devices always keep their topology",
			devices: Devices{
				"MIG-GPU-uuid-1[0]": &Device{
					Device: kubeletdevicepluginv1beta1.Device{
						ID:       "MIG-GPU-uuid-1[0]",
						Health:   kubeletdevicepluginv1beta1.Healthy,
						Topology: numaTopology,
					},
				},
			},
			count:                1,
			numaTopology:         false,
			expectedIDs:          []string{"MIG-GPU-uuid-1[0]"},
			expectedTopologyNil:  false,
			expectedTopologyNode: 3,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			result := tc.devices.GetPluginDevices(tc.count, tc.numaTopology)
			require.Len(t, result, len(tc.expectedIDs))

			var ids []string
			for _, d := range result {
				ids = append(ids, d.ID)
				if tc.expectedTopologyNil {
					require.Nil(t, d.Topology)
				} else {
					require.NotNil(t, d.Topology)
					require.Len(t, d.Topology.Nodes, 1)
					require.Equal(t, tc.expectedTopologyNode, d.Topology.Nodes[0].ID)
				}
			}
			require.Equal(t, tc.expectedIDs, ids)
		})
	}
}
