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

package awsneuron

import (
	"fmt"
	"maps"
	"math"
	"sync"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

func newNeuronBackend() *AWSNeuronDevices {
	return InitAWSNeuronDevice(AWSNeuronConfig{
		ResourceCountName: "aws.amazon.com/neuron",
		ResourceCoreName:  "aws.amazon.com/neuroncore",
	})
}

func newNeuronNode(name, instanceType string, deviceCount, coreCount int64) corev1.Node {
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"node.kubernetes.io/instance-type": instanceType},
		},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				"aws.amazon.com/neuron":     *resource.NewQuantity(deviceCount, resource.DecimalSI),
				"aws.amazon.com/neuroncore": *resource.NewQuantity(coreCount, resource.DecimalSI),
			},
		},
	}
}

func assertNeuronGeometry(t *testing.T, devices []*device.DeviceInfo, wantCount, wantMask, wantCoresPerDevice int32) {
	t.Helper()
	assert.Assert(t, len(devices) > 0)
	for _, dev := range devices {
		assert.Equal(t, dev.Count, wantCount)
		assert.Equal(t, dev.Devcore, wantMask)
		assert.Equal(t, coresPerNeuronDevice(dev.CustomInfo), wantCoresPerDevice)
	}
}

func TestGetNodeDevicesKeepsGeometryPerNode(t *testing.T) {
	fourCoreNode := newNeuronNode("inf1-node", "inf1.6xlarge", 2, 8)
	twoCoreNode := newNeuronNode("inf2-node", "inf2.8xlarge", 2, 4)

	tests := []struct {
		name        string
		first       corev1.Node
		second      corev1.Node
		firstCores  int32
		secondCores int32
	}{
		{
			name:        "four-core node followed by two-core node",
			first:       fourCoreNode,
			second:      twoCoreNode,
			firstCores:  4,
			secondCores: 2,
		},
		{
			name:        "two-core node followed by four-core node",
			first:       twoCoreNode,
			second:      fourCoreNode,
			firstCores:  2,
			secondCores: 4,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := newNeuronBackend()
			firstDevices, err := dev.GetNodeDevices(test.first)
			assert.NilError(t, err)
			secondDevices, err := dev.GetNodeDevices(test.second)
			assert.NilError(t, err)

			assertNeuronGeometry(t, firstDevices, test.firstCores, (1<<test.firstCores)-1, test.firstCores)
			assertNeuronGeometry(t, secondDevices, test.secondCores, (1<<test.secondCores)-1, test.secondCores)
		})
	}
}

func TestGenerateResourceRequestsDoesNotDependOnRegisteredNode(t *testing.T) {
	dev := newNeuronBackend()
	container := &corev1.Container{
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				"aws.amazon.com/neuron": resource.MustParse("1"),
			},
		},
	}

	want, err := dev.GenerateResourceRequests(container)
	assert.NilError(t, err)
	assert.Equal(t, want.Coresreq, int32(0))

	_, err = dev.GetNodeDevices(newNeuronNode("inf1-node", "inf1.xlarge", 1, 4))
	assert.NilError(t, err)
	got, err := dev.GenerateResourceRequests(container)
	assert.NilError(t, err)
	assert.DeepEqual(t, got, want)

	_, err = dev.GetNodeDevices(newNeuronNode("inf2-node", "inf2.xlarge", 1, 2))
	assert.NilError(t, err)
	got, err = dev.GenerateResourceRequests(container)
	assert.NilError(t, err)
	assert.DeepEqual(t, got, want)
}

func TestMixedNodeGeometryAllocationWorkflow(t *testing.T) {
	fourCoreNode := newNeuronNode("inf1-node", "inf1.6xlarge", 2, 8)
	twoCoreNode := newNeuronNode("inf2-node", "inf2.8xlarge", 2, 4)

	tests := []struct {
		name            string
		first           corev1.Node
		target          corev1.Node
		wantCoreIndexes string
	}{
		{
			name:            "two-core target keeps two-core index stride",
			first:           fourCoreNode,
			target:          twoCoreNode,
			wantCoreIndexes: "2,3",
		},
		{
			name:            "four-core target keeps four-core index stride",
			first:           twoCoreNode,
			target:          fourCoreNode,
			wantCoreIndexes: "4,5",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := newNeuronBackend()
			_, err := dev.GetNodeDevices(test.first)
			assert.NilError(t, err)
			targetDevices, err := dev.GetNodeDevices(test.target)
			assert.NilError(t, err)
			assert.Equal(t, len(targetDevices), 2)

			selected := targetDevices[1]
			usage := &device.DeviceUsage{
				ID:         selected.ID,
				Index:      selected.Index,
				Count:      selected.Count,
				Totalcore:  selected.Devcore,
				Type:       selected.Type,
				Health:     selected.Health,
				CustomInfo: maps.Clone(selected.CustomInfo),
			}
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "neuron-workload",
					Namespace:   "default",
					Annotations: map[string]string{},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "inference",
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								"aws.amazon.com/neuroncore": resource.MustParse("2"),
							},
						},
					}},
				},
			}

			request, err := dev.GenerateResourceRequests(&pod.Spec.Containers[0])
			assert.NilError(t, err)
			fit, allocations, reason := dev.Fit(
				[]*device.DeviceUsage{usage},
				request,
				pod,
				&device.NodeInfo{ID: test.target.Name, Node: test.target.DeepCopy()},
				&device.PodDevices{},
			)
			assert.Equal(t, fit, true, reason)
			assert.Equal(t, len(allocations[AWSNeuronDevice]), 1)

			annotations := map[string]string{util.AssignedNodeAnnotations: test.target.Name}
			dev.PatchAnnotations(pod, &annotations, device.PodDevices{
				AWSNeuronDevice: device.PodSingleDevice{allocations[AWSNeuronDevice]},
			})
			assert.Equal(t, annotations[AWSNeuronAssignedIndex], test.wantCoreIndexes)
		})
	}
}

func TestGetNodeDevicesConcurrentMixedGeometry(t *testing.T) {
	dev := newNeuronBackend()
	nodes := []corev1.Node{
		newNeuronNode("inf1-node", "inf1.6xlarge", 2, 8),
		newNeuronNode("inf2-node", "inf2.8xlarge", 2, 4),
	}
	wantCores := []int32{4, 2}

	const iterations = 100
	errCh := make(chan error, iterations*len(nodes))
	var wg sync.WaitGroup
	for range iterations {
		for nodeIndex := range nodes {
			wg.Add(1)
			go func(node corev1.Node, want int32) {
				defer wg.Done()
				devices, err := dev.GetNodeDevices(node)
				if err != nil {
					errCh <- err
					return
				}
				if len(devices) != 2 || devices[0].Count != want || coresPerNeuronDevice(devices[0].CustomInfo) != want {
					errCh <- fmt.Errorf("node %s returned mixed geometry: %+v", node.Name, devices)
				}
			}(nodes[nodeIndex], wantCores[nodeIndex])
		}
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}
}

func TestGetNodeDevicesRejectsInvalidGeometry(t *testing.T) {
	dev := newNeuronBackend()
	_, err := dev.GetNodeDevices(newNeuronNode("invalid-node", "inf1.xlarge", 2, 3))
	assert.ErrorContains(t, err, "is not divisible")
}

func TestPatchAnnotationsKeepsLargeCoreIndexesPositive(t *testing.T) {
	dev := newNeuronBackend()
	maxCoresPerDevice := int64(math.MaxInt32)
	nodeDevices, err := dev.GetNodeDevices(newNeuronNode(
		"large-node", "test.neuron", 2, maxCoresPerDevice*2))
	assert.NilError(t, err)
	assert.Equal(t, len(nodeDevices), 2)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "neuron-workload", Namespace: "default"},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "inference",
			Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
				"aws.amazon.com/neuroncore": resource.MustParse("2"),
			}},
		}}},
	}
	annotations := map[string]string{util.AssignedNodeAnnotations: "large-node"}
	dev.PatchAnnotations(pod, &annotations, device.PodDevices{
		AWSNeuronDevice: device.PodSingleDevice{device.ContainerDevices{{
			Idx:        1,
			Usedcores:  3,
			CustomInfo: maps.Clone(nodeDevices[1].CustomInfo),
		}}},
	})

	assert.Equal(t, annotations[AWSNeuronAssignedIndex], "2147483647,2147483648")
}
