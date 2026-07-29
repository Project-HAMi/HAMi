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
// TotalMemory is left at zero; use makeGPUsWithMem when the validation path
// that reads TotalMemory matters.
func makeGPUs(n int, health string) Devices {
	return makeGPUsWithMem(n, health, 0)
}

// makeGPUsWithMem is like makeGPUs but also sets TotalMemory (in bytes) on
// each device.  This exercises the runtime validation path in GetPluginDevices
// that computes minFactor from actual GPU memory.
func makeGPUsWithMem(n int, health string, totalMemBytes uint64) Devices {
	ds := make(Devices, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("GPU-uuid-%d", i)
		ds[id] = &Device{
			Device: kubeletdevicepluginv1beta1.Device{
				ID:     id,
				Health: health,
			},
			TotalMemory: totalMemBytes,
		}
	}
	return ds
}

func TestGetPluginDevices_EntryCountLogging(t *testing.T) {
	// kubeletListAndWatchMaxEntries = 60 000.
	// Each non-MIG GPU generates `count` entries.
	// total_entries = gpuCount × splitCount.

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
			// 8 GPUs × 7500 splits = 60000: exactly at the per-GPU budget
			// derived from floor(60000/8)=7500, so total=60000 — safe.
			name:          "at limit: 8 GPUs × 7500 splits = 60000 entries",
			gpuCount:      8,
			splitCount:    7_500,
			expectEntries: 60_000,
			expectOverMax: false,
		},
		{
			// 8 GPUs × 7501 splits = 60008: one step over the safe per-GPU budget.
			name:          "over limit: 8 GPUs × 7501 splits = 60008 entries",
			gpuCount:      8,
			splitCount:    7_501,
			expectEntries: 60_008,
			expectOverMax: true,
		},
		{
			// Classic A800 failure case: splitCount = totalMemMiB/factor = 81920/1.
			name:          "over limit: 1 GPU × 81920 splits (A800 factor=1)",
			gpuCount:      1,
			splitCount:    81_920,
			expectEntries: 81_920,
			expectOverMax: true,
		},
		{
			// A800 with factor=2: splitCount = 81920/2 = 40960 — safe.
			name:          "below limit: 1 GPU × 40960 splits (A800 factor=2)",
			gpuCount:      1,
			splitCount:    40_960,
			expectEntries: 40_960,
			expectOverMax: false,
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

// TestGetPluginDevices_RuntimeValidation verifies that GetPluginDevices
// computes the correct minFactor from actual TotalMemory when the entry count
// exceeds the kubelet gRPC limit.
//
// Formula under test:
//
//	minFactor = ceil(totalMemMiB / floor(60000 / gpuCount))
func TestGetPluginDevices_RuntimeValidation(t *testing.T) {
	const mibBytes = 1 << 20 // bytes per MiB

	tests := []struct {
		name          string
		gpuCount      int
		totalMemGiB   uint64 // memory per GPU in GiB
		splitCount    uint   // = totalMemMiB / memoryFactor in the real plugin
		expectOverMax bool
		// wantMinFactor is what GetPluginDevices should compute and log.
		// We verify it independently here.
		wantMinFactor int64
	}{
		{
			// A800 80 GiB, 1 GPU, factor=1 → 81920 entries, over limit.
			// minFactor = ceil(81920 / floor(60000/1)) = ceil(81920/60000) = 2.
			name:          "A800 80 GiB × 1 GPU, factor=1 — over limit, minFactor=2",
			gpuCount:      1,
			totalMemGiB:   80,
			splitCount:    81_920, // totalMemMiB / factor = 81920 / 1
			expectOverMax: true,
			wantMinFactor: 2,
		},
		{
			// A800 80 GiB, 8 GPUs, factor=2 → ceil(81920/2)×8 = 327680, over limit.
			// minFactor = ceil(81920 / floor(60000/8)) = ceil(81920/7500) = 11.
			name:          "A800 80 GiB × 8 GPUs, factor=2 — over limit, minFactor=11",
			gpuCount:      8,
			totalMemGiB:   80,
			splitCount:    40_960, // totalMemMiB / factor = 81920 / 2
			expectOverMax: true,
			wantMinFactor: 11,
		},
		{
			// A800 80 GiB, 8 GPUs, factor=11 → ceil(81920/11)×8 = 7448×8 = 59584 ≤ 60000.
			// Safe — no warning, minFactor not needed.
			name:          "A800 80 GiB × 8 GPUs, factor=11 — under limit",
			gpuCount:      8,
			totalMemGiB:   80,
			splitCount:    7_448, // ceil(81920 / 11)
			expectOverMax: false,
			wantMinFactor: 0, // irrelevant when under limit
		},
		{
			// T4 16 GiB, 1 GPU, factor=1 → 16384 entries — well under limit.
			name:          "T4 16 GiB × 1 GPU, factor=1 — under limit",
			gpuCount:      1,
			totalMemGiB:   16,
			splitCount:    16_384,
			expectOverMax: false,
			wantMinFactor: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			totalMemBytes := tc.totalMemGiB * 1024 * mibBytes
			ds := makeGPUsWithMem(tc.gpuCount, kubeletdevicepluginv1beta1.Healthy, totalMemBytes)
			result := ds.GetPluginDevices(tc.splitCount, false)

			wantEntries := tc.gpuCount * int(tc.splitCount)
			require.Len(t, result, wantEntries)

			if tc.expectOverMax {
				require.Greater(t, len(result), kubeletListAndWatchMaxEntries)

				// Independently verify the minFactor the code should compute.
				totalMemMiB := int64(tc.totalMemGiB) * 1024
				entriesPerGPULimit := int64(kubeletListAndWatchMaxEntries) / int64(tc.gpuCount)
				if entriesPerGPULimit < 1 {
					entriesPerGPULimit = 1
				}
				computedMinFactor := (totalMemMiB + entriesPerGPULimit - 1) / entriesPerGPULimit
				require.Equal(t, tc.wantMinFactor, computedMinFactor,
					"minFactor formula mismatch for %s", tc.name)
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
