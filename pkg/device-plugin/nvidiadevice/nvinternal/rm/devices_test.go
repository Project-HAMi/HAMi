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
	"fmt"
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

// makeGPUs builds a Devices map with n non-MIG entries, each with the given
// health status.  The IDs are "GPU-uuid-<i>" so they do NOT contain "MIG".
func makeGPUs(n int, health string) Devices {
	ds := make(Devices, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("GPU-uuid-%d", i)
		ds[id] = &Device{
			Device: kubeletdevicepluginv1beta1.Device{
				ID:     id,
				Health: health,
			},
		}
	}
	return ds
}

func TestGetPluginDevices_EntryCountLogging(t *testing.T) {
	// kubeletListAndWatchMaxEntries = 60 000.
	// Each non-MIG GPU generates `count` entries.
	// With 1 GPU and splitCount=60001 we exceed the limit.
	// With 1 GPU and splitCount=60000 we are exactly at the limit (still OK).

	tests := []struct {
		name          string
		gpuCount      int
		splitCount    uint
		expectEntries int
		expectOverMax bool
	}{
		{
			name:          "below limit: 1 GPU × 100 splits = 100 entries",
			gpuCount:      1,
			splitCount:    100,
			expectEntries: 100,
			expectOverMax: false,
		},
		{
			name:          "at limit: 1 GPU × 60000 splits = 60000 entries",
			gpuCount:      1,
			splitCount:    60_000,
			expectEntries: 60_000,
			expectOverMax: false,
		},
		{
			name:          "over limit: 1 GPU × 60001 splits = 60001 entries",
			gpuCount:      1,
			splitCount:    60_001,
			expectEntries: 60_001,
			expectOverMax: true,
		},
		{
			name:          "over limit: 8 GPUs × 10000 splits = 80000 entries",
			gpuCount:      8,
			splitCount:    10_000,
			expectEntries: 80_000,
			expectOverMax: true,
		},
		{
			name:          "empty device list returns nothing",
			gpuCount:      0,
			splitCount:    100,
			expectEntries: 0,
			expectOverMax: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ds := makeGPUs(tc.gpuCount, kubeletdevicepluginv1beta1.Healthy)
			result := ds.GetPluginDevices(tc.splitCount, false)

			require.Len(t, result, tc.expectEntries,
				"expected %d entries, got %d", tc.expectEntries, len(result))

			// Verify that entries beyond the limit are still returned (we warn but
			// do not truncate — truncation would hide the problem from the caller).
			if tc.expectOverMax {
				require.Greater(t, len(result), kubeletListAndWatchMaxEntries,
					"expected entry count to exceed kubeletListAndWatchMaxEntries")
			}
		})
	}
}

// TestGetPluginDevices_UniqueIDs verifies that every generated entry has a
// unique ID of the form "<uuid>-<index>", which is what the gRPC ListAndWatch
// protocol requires.
func TestGetPluginDevices_UniqueIDs(t *testing.T) {
	ds := makeGPUs(3, kubeletdevicepluginv1beta1.Healthy)
	result := ds.GetPluginDevices(4, false)

	require.Len(t, result, 12) // 3 GPUs × 4 splits

	seen := make(map[string]struct{}, len(result))
	for _, d := range result {
		_, dup := seen[d.ID]
		require.False(t, dup, "duplicate device ID %q", d.ID)
		seen[d.ID] = struct{}{}
	}
}
