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
	"fmt"
	"maps"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/awsneuron"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

// usagePod returns a pod for the pod cache.
func usagePod(uid string) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: k8stypes.UID(uid), Name: uid, Namespace: "default"}}
}

// onlyDeviceWithout returns the device usage of node1 without the given pods.
func onlyDeviceWithout(t *testing.T, s *Scheduler, without ...k8stypes.UID) *device.DeviceUsage {
	t.Helper()
	gone := make(map[k8stypes.UID]struct{}, len(without))
	for _, uid := range without {
		gone[uid] = struct{}{}
	}
	nodeNames := []string{"node1"}
	usage, _, _, err := s.nodesUsageFrom(&nodeNames, nil, s.podManager.ListPodsInfo(), gone)
	assert.NilError(t, err)
	return (*usage)["node1"].Devices.DeviceLists[0].Device
}

// A pod that is left out stops counting, and the pod cache is not touched.
func Test_nodesUsageFrom_LeavesOutPods(t *testing.T) {
	nodes := newNodeManager()
	nodes.addNode("node1", testNodeInfo("node1", 1))
	pods := device.NewPodManager()
	pods.AddPod(usagePod("low"), "node1", device.PodDevices{nvidia.NvidiaGPUDevice: {{{
		UUID: "node1-gpu-0", Type: nvidia.NvidiaGPUDevice, Usedmem: 3000, Usedcores: 30,
	}}}})
	pods.AddPod(usagePod("other"), "node1", device.PodDevices{nvidia.NvidiaGPUDevice: {{{
		UUID: "node1-gpu-0", Type: nvidia.NvidiaGPUDevice, Usedmem: 2000, Usedcores: 20,
	}}}})
	s := Scheduler{nodeManager: nodes, podManager: pods}

	all := onlyDeviceWithout(t, &s)
	assert.Equal(t, all.Used, int64(2))
	assert.Equal(t, all.Usedmem, int64(5000))
	assert.Equal(t, all.Usedcores, int64(50))

	withoutLow := onlyDeviceWithout(t, &s, "low")
	assert.Equal(t, withoutLow.Used, int64(1))
	assert.Equal(t, withoutLow.Usedmem, int64(2000))
	assert.Equal(t, withoutLow.Usedcores, int64(20))

	withoutBoth := onlyDeviceWithout(t, &s, "low", "other")
	assert.Equal(t, withoutBoth.Used, int64(0))
	assert.Equal(t, withoutBoth.Usedmem, int64(0))
	assert.Equal(t, withoutBoth.Usedcores, int64(0))

	_, tracked := pods.GetPod(usagePod("low"))
	assert.Assert(t, tracked, "leaving a pod out of a snapshot must not drop it from the pod cache")
}

// The MIG slot of a pod that is left out is free again.
func Test_nodesUsageFrom_FreesMigPlacement(t *testing.T) {
	nodes := newNodeManager()
	nodes.addNode("node1", &device.NodeInfo{
		ID: "node1", Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
		Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: {{
			ID: "GPU-a", Count: 7, Devmem: 40960, Devcore: 100, Mode: nvidia.MigMode, Health: true,
			MIGProfiles: []device.MigProfile{{Name: "1g.5gb", MemoryMB: 5120, Core: 14, Placements: []device.MigPlacement{{Start: 5, Size: 1}, {Start: 6, Size: 1}}}},
		}}},
	})
	migPod := func(uid string, start int) *corev1.Pod {
		pod := usagePod(uid)
		pod.Annotations = map[string]string{nvidia.MigAllocationsAnnotation: fmt.Sprintf(
			`[{"containerIndex":0,"deviceIndex":0,"gpuUUID":"GPU-a","profile":"1g.5gb","placement":{"start":%d,"size":1}}]`, start)}
		return pod
	}
	slot := func() device.PodDevices {
		return device.PodDevices{nvidia.NvidiaGPUDevice: {{{UUID: "GPU-a", Usedmem: 5120, Usedcores: 14}}}}
	}
	pods := device.NewPodManager()
	pods.AddPod(migPod("low", 5), "node1", slot())
	pods.AddPod(migPod("other", 6), "node1", slot())
	s := Scheduler{nodeManager: nodes, podManager: pods}

	all := onlyDeviceWithout(t, &s)
	assert.Assert(t, all.Health)
	assert.Equal(t, len(all.MigAllocationsInUse), 2)

	withoutLow := onlyDeviceWithout(t, &s, "low")
	assert.Assert(t, withoutLow.Health)
	assert.Equal(t, len(withoutLow.MigAllocationsInUse), 1)
	assert.Equal(t, withoutLow.MigAllocationsInUse[0].Placement, device.MigPlacement{Start: 6, Size: 1})
}

// The core mask in use is rebuilt from the pods that stay.
func Test_nodesUsageFrom_RebuildsCoreMask(t *testing.T) {
	oldDevices, oldSupport := device.DevicesMap, maps.Clone(device.SupportDevices)
	t.Cleanup(func() { device.DevicesMap, device.SupportDevices = oldDevices, oldSupport })
	device.DevicesMap = map[string]device.Devices{
		awsneuron.AWSNeuronDevice: awsneuron.InitAWSNeuronDevice(awsneuron.AWSNeuronConfig{
			ResourceCountName: "aws.amazon.com/neuron", ResourceCoreName: "aws.amazon.com/neuroncore",
		}),
	}

	nodes := newNodeManager()
	nodes.addNode("node1", &device.NodeInfo{
		ID: "node1", Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
		Devices: map[string][]device.DeviceInfo{awsneuron.AWSNeuronDevice: {{
			ID: "neuron-0", Count: 4, Devcore: 4, Type: awsneuron.AWSNeuronDevice, Health: true,
		}}},
	})
	cores := func(mask int64) device.PodDevices {
		return device.PodDevices{awsneuron.AWSNeuronDevice: {{{
			UUID: "neuron-0", Type: awsneuron.AWSNeuronDevice, Usedcores: mask,
		}}}}
	}
	pods := device.NewPodManager()
	pods.AddPod(usagePod("low"), "node1", cores(0b0011))
	pods.AddPod(usagePod("other"), "node1", cores(0b0100))
	s := Scheduler{nodeManager: nodes, podManager: pods}

	assert.Equal(t, onlyDeviceWithout(t, &s).Usedcores, int64(0b0111))
	assert.Equal(t, onlyDeviceWithout(t, &s, "low").Usedcores, int64(0b0100))
	assert.Equal(t, onlyDeviceWithout(t, &s, "other").Usedcores, int64(0b0011))
}
