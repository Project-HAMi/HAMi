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

package plugin

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

// Kubelet's DevicesIds order is unrelated to the annotation order. Each
// device must keep its own memory and core limits, otherwise the per device
// CUDA memory limit lands on the wrong GPU on heterogeneous nodes.
func TestAlignContainerDevicesWithAllocatedIDsMatchesByUUID(t *testing.T) {
	plugin := &NvidiaDevicePlugin{}
	devreq := device.ContainerDevices{
		{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67a", Type: nvidia.NvidiaGPUDevice, Usedmem: 40000, Usedcores: 50},
		{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67b", Type: nvidia.NvidiaGPUDevice, Usedmem: 12000, Usedcores: 30},
	}

	// Kubelet returns the second annotated device first.
	aligned, err := plugin.alignContainerDevicesWithAllocatedIDs(devreq, []string{
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67b-1",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67a-0",
	})
	require.NoError(t, err)
	require.Equal(t, "GPU-03f69c50-207a-2038-9b45-23cac89cb67b", aligned[0].UUID)
	require.Equal(t, int32(12000), aligned[0].Usedmem)
	require.Equal(t, int32(30), aligned[0].Usedcores)
	require.Equal(t, "GPU-03f69c50-207a-2038-9b45-23cac89cb67a", aligned[1].UUID)
	require.Equal(t, int32(40000), aligned[1].Usedmem)
	require.Equal(t, int32(50), aligned[1].Usedcores)
}

// When kubelet picks devices the annotation does not mention, the previous
// behavior is preserved: entries adopt the kubelet devices in annotation order.
func TestAlignContainerDevicesWithAllocatedIDsFallsBackPositionally(t *testing.T) {
	plugin := &NvidiaDevicePlugin{}
	devreq := device.ContainerDevices{
		{UUID: "GPU-annotated-a", Type: nvidia.NvidiaGPUDevice, Usedmem: 40000, Usedcores: 50},
		{UUID: "GPU-annotated-b", Type: nvidia.NvidiaGPUDevice, Usedmem: 12000, Usedcores: 30},
	}

	aligned, err := plugin.alignContainerDevicesWithAllocatedIDs(devreq, []string{
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67c-0",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67d-1",
	})
	require.NoError(t, err)
	require.Equal(t, "GPU-03f69c50-207a-2038-9b45-23cac89cb67c", aligned[0].UUID)
	require.Equal(t, int32(40000), aligned[0].Usedmem)
	require.Equal(t, "GPU-03f69c50-207a-2038-9b45-23cac89cb67d", aligned[1].UUID)
	require.Equal(t, int32(12000), aligned[1].Usedmem)
}

// A mix of matched and unmatched devices keeps matched limits on their own
// GPU and hands the leftovers out in annotation order.
func TestAlignContainerDevicesWithAllocatedIDsPartialMatch(t *testing.T) {
	plugin := &NvidiaDevicePlugin{}
	devreq := device.ContainerDevices{
		{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67a", Type: nvidia.NvidiaGPUDevice, Usedmem: 40000, Usedcores: 50},
		{UUID: "GPU-annotated-b", Type: nvidia.NvidiaGPUDevice, Usedmem: 12000, Usedcores: 30},
	}

	aligned, err := plugin.alignContainerDevicesWithAllocatedIDs(devreq, []string{
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67e-0",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67a-1",
	})
	require.NoError(t, err)
	require.Equal(t, "GPU-03f69c50-207a-2038-9b45-23cac89cb67e", aligned[0].UUID)
	require.Equal(t, int32(12000), aligned[0].Usedmem)
	require.Equal(t, "GPU-03f69c50-207a-2038-9b45-23cac89cb67a", aligned[1].UUID)
	require.Equal(t, int32(40000), aligned[1].Usedmem)
}

// Annotated UUIDs may carry a replica suffix themselves; matching must
// compare physical IDs on both sides.
func TestAlignContainerDevicesWithAllocatedIDsMatchesAnnotatedReplicaUUIDs(t *testing.T) {
	plugin := &NvidiaDevicePlugin{}
	devreq := device.ContainerDevices{
		{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67a-2", Type: nvidia.NvidiaGPUDevice, Usedmem: 40000, Usedcores: 50},
		{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67b-4", Type: nvidia.NvidiaGPUDevice, Usedmem: 12000, Usedcores: 30},
	}

	aligned, err := plugin.alignContainerDevicesWithAllocatedIDs(devreq, []string{
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67b-1",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67a-0",
	})
	require.NoError(t, err)
	require.Equal(t, "GPU-03f69c50-207a-2038-9b45-23cac89cb67b", aligned[0].UUID)
	require.Equal(t, int32(12000), aligned[0].Usedmem)
	require.Equal(t, "GPU-03f69c50-207a-2038-9b45-23cac89cb67a", aligned[1].UUID)
	require.Equal(t, int32(40000), aligned[1].Usedmem)
}
