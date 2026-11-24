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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/awsneuron"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

const migNode = "mig-node"

// addMigCard gives s a node with one MIG card of four slices. A 1g instance
// takes one slice, a 2g instance takes two neighboring slices.
func addMigCard(s *Scheduler) {
	s.addNode(migNode, &device.NodeInfo{
		ID: migNode, Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: migNode}},
		Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: {{
			ID: "GPU-mig", Count: 4, Devmem: 24576, Devcore: 100, Type: nvidia.NvidiaGPUDevice,
			Mode: nvidia.MigMode, Health: true, DeviceVendor: nvidia.NvidiaGPUDevice,
			MIGProfiles: []device.MigProfile{
				{Name: "1g.6gb", MemoryMB: 6144, Core: 25, Placements: []device.MigPlacement{
					{Start: 0, Size: 1}, {Start: 1, Size: 1}, {Start: 2, Size: 1}, {Start: 3, Size: 1},
				}},
				{Name: "2g.12gb", MemoryMB: 12288, Core: 50, Placements: []device.MigPlacement{
					{Start: 0, Size: 2}, {Start: 2, Size: 2},
				}},
			},
		}}},
	})
}

// onMigSlice returns a pod bound to the MIG node with a 1g instance at start.
func onMigSlice(uid string, priority int32, start int) *device.PodInfo {
	pi := trackedOn(migNode, uid, priority)
	pi.Annotations = map[string]string{nvidia.MigAllocationsAnnotation: fmt.Sprintf(
		`[{"containerIndex":0,"deviceIndex":0,"gpuUUID":"GPU-mig","profile":"1g.6gb","placement":{"start":%d,"size":1}}]`, start)}
	pi.Devices = device.PodDevices{nvidia.NvidiaGPUDevice: {{{
		UUID: "GPU-mig", Type: nvidia.NvidiaGPUDevice, Usedmem: 6144, Usedcores: 25,
	}}}}
	return pi
}

// Slices 1 and 3 are free, but a 2g instance needs two neighboring slices.
// The less important pod is the victim.
func Test_Preempt_FreesMigPlacement(t *testing.T) {
	pod := movePod()
	high := int32(1000)
	pod.Spec.Priority = &high
	pod.Spec.Containers[0].Resources.Limits[moveMem] = *resource.NewQuantity(12000, resource.BinarySI)
	s, _ := filterLeader(t, pod, 1)
	addMigCard(s)
	track(t, s, onMigSlice("mid", 500, 0), onMigSlice("low", 10, 2))

	res, err := s.Preempt(extenderv1.ExtenderPreemptionArgs{
		Pod:                   pod,
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{migNode: {}},
	})
	assert.NilError(t, err)
	assert.DeepEqual(t, res.NodeNameToMetaVictims, map[string]*extenderv1.MetaVictims{
		migNode: {Pods: []*extenderv1.MetaPod{{UID: "low"}}},
	})
}

// The planned placement, slices 0 and 1, is held back for the nominated pod.
// A second 2g instance does not fit, and a 1g instance still fits on slice 3.
func Test_Preempt_HoldsMigPlacementForNominatedPod(t *testing.T) {
	high := asking("high", 1000, 12000)
	twoSlices := asking("two-slices", 10, 12000)
	oneSlice := asking("one-slice", 10, 6000)
	s := nominationScheduler(t, high, twoSlices, oneSlice)
	addMigCard(s)
	low := onMigSlice("low", 10, 0)
	track(t, s, onMigSlice("mid", 500, 2), low)

	preemptOn(t, s, high, migNode, "low")
	planned, err := nvidia.DecodeMigAllocations(s.preemptionPlans[high.UID].nodes[migNode].migPlacements)
	assert.NilError(t, err)
	assert.Equal(t, len(planned), 1)
	assert.Equal(t, planned[0].Placement, device.MigPlacement{Start: 0, Size: 2})

	s.podManager.DelPod(low.Pod)
	high.Status.NominatedNodeName = migNode
	assert.Assert(t, !filterOn(t, s, twoSlices, migNode), "slices 0 and 1 are held back, and slice 2 is taken")
	assert.Assert(t, filterOn(t, s, oneSlice, migNode), "slice 3 is free, and the card is still in use")
}

// Cores 1 and 3 are free, but a pod gets neighboring cores. The pod on core 2
// is the victim.
func Test_Preempt_FreesCoreMask(t *testing.T) {
	oldDevices, oldSupport := device.DevicesMap, maps.Clone(device.SupportDevices)
	t.Cleanup(func() { device.DevicesMap, device.SupportDevices = oldDevices, oldSupport })
	neuron := awsneuron.InitAWSNeuronDevice(awsneuron.AWSNeuronConfig{
		ResourceCountName: "aws.amazon.com/neuron", ResourceCoreName: "aws.amazon.com/neuroncore",
	})
	device.DevicesMap = map[string]device.Devices{awsneuron.AWSNeuronDevice: neuron}

	high := int32(1000)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "high", Namespace: "neuron-preemption", UID: "high"},
		Spec: corev1.PodSpec{
			Priority: &high,
			Containers: []corev1.Container{{Name: "app", Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{"aws.amazon.com/neuroncore": resource.MustParse("2")},
			}}},
		},
	}
	s := schedulerWithPods(t, pod)

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "neuron-node", Labels: map[string]string{"node.kubernetes.io/instance-type": "inf1.xlarge"}},
		Status: corev1.NodeStatus{Capacity: corev1.ResourceList{
			"aws.amazon.com/neuron": resource.MustParse("1"), "aws.amazon.com/neuroncore": resource.MustParse("4"),
		}},
	}
	inventory, err := neuron.GetNodeDevices(*node)
	assert.NilError(t, err)
	s.addNode(node.Name, &device.NodeInfo{ID: node.Name, Node: node,
		Devices: map[string][]device.DeviceInfo{awsneuron.AWSNeuronDevice: {*inventory[0]}},
	})
	// onCores returns a pod that has the cores of mask on the device.
	onCores := func(uid string, priority int32, mask int64) *device.PodInfo {
		pi := trackedOn(node.Name, uid, priority)
		pi.Devices = device.PodDevices{awsneuron.AWSNeuronDevice: {{{
			UUID: inventory[0].ID, Type: awsneuron.AWSNeuronDevice, Usedcores: mask,
		}}}}
		return pi
	}
	track(t, s, onCores("higher", 2000, 0b0001), onCores("low", 10, 0b0100))

	res, err := s.Preempt(extenderv1.ExtenderPreemptionArgs{
		Pod:                   pod,
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{node.Name: {}},
	})
	assert.NilError(t, err)
	assert.DeepEqual(t, res.NodeNameToMetaVictims, map[string]*extenderv1.MetaVictims{
		node.Name: {Pods: []*extenderv1.MetaPod{{UID: "low"}}},
	})
}
