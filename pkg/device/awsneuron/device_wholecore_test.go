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
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

// On inf1 hardware a NeuronDevice exposes four cores, and addCoreUsage /
// PatchAnnotations track all of them (not just the first two, as older
// versions of this backend assumed). A whole-device request must therefore
// reserve the real per-device core count, and the registered core mask must
// span exactly that many bits, or a second pod could be packed onto a device
// that is already fully owned.

func Test_GenerateResourceRequests_WholeDeviceUsesObservedCores(t *testing.T) {
	dev := &AWSNeuronDevices{
		resourceCountName: "aws.amazon.com/neuron",
		resourceCoreName:  "aws.amazon.com/neuroncore",
		coresPerAWSNeuron: 4,
	}
	ctr := &corev1.Container{
		Name: "ctr",
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				"aws.amazon.com/neuron": resource.MustParse("1"),
			},
		},
	}
	req := dev.GenerateResourceRequests(ctr)
	assert.Equal(t, req.Coresreq, int32(4))
}

func Test_GenerateResourceRequests_WholeDeviceTwoCoreHardwareUnaffected(t *testing.T) {
	dev := &AWSNeuronDevices{
		resourceCountName: "aws.amazon.com/neuron",
		resourceCoreName:  "aws.amazon.com/neuroncore",
		coresPerAWSNeuron: 2,
	}
	ctr := &corev1.Container{
		Name: "ctr",
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				"aws.amazon.com/neuron": resource.MustParse("1"),
			},
		},
	}
	req := dev.GenerateResourceRequests(ctr)
	assert.Equal(t, req.Coresreq, int32(2))
}

func Test_GetNodeDevices_CoreMaskMatchesFourCoreHardware(t *testing.T) {
	dev := InitAWSNeuronDevice(AWSNeuronConfig{
		ResourceCountName: "aws.amazon.com/neuron",
		ResourceCoreName:  "aws.amazon.com/neuroncore",
	})
	node := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "inf1-node",
			Labels: map[string]string{"node.kubernetes.io/instance-type": "inf1.6xlarge"},
		},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				"aws.amazon.com/neuron":     resource.MustParse("4"),
				"aws.amazon.com/neuroncore": resource.MustParse("16"),
			},
		},
	}
	devices, err := dev.GetNodeDevices(node)
	assert.NilError(t, err)
	assert.Equal(t, len(devices), 4)
	// Four addressable cores yield the four-bit mask 15, covering all of Inf1's
	// real per-chip capacity.
	assert.Equal(t, devices[0].Devcore, int32(15))
}

func Test_GetNodeDevices_CoreMaskUnaffectedOnTwoCoreHardware(t *testing.T) {
	dev := InitAWSNeuronDevice(AWSNeuronConfig{
		ResourceCountName: "aws.amazon.com/neuron",
		ResourceCoreName:  "aws.amazon.com/neuroncore",
	})
	node := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "trn1-node",
			Labels: map[string]string{"node.kubernetes.io/instance-type": "trn1.2xlarge"},
		},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				"aws.amazon.com/neuron":     resource.MustParse("1"),
				"aws.amazon.com/neuroncore": resource.MustParse("2"),
			},
		},
	}
	devices, err := dev.GetNodeDevices(node)
	assert.NilError(t, err)
	assert.Equal(t, len(devices), 1)
	// Trn1/Inf2 behavior is unchanged: two addressable cores, two-bit mask.
	assert.Equal(t, devices[0].Devcore, int32(3))
}

func Test_addCoreUsage_FourCoreDevice(t *testing.T) {
	tests := []struct {
		name     string
		prev     map[string]any
		require  int
		maxCores int
		want     int
	}{
		{"empty device, request 1 of 4", map[string]any{}, 1, 4, 0b0001},
		{"empty device, request 3 of 4", map[string]any{}, 3, 4, 0b0111},
		{"empty device, request 4 of 4", map[string]any{}, 4, 4, 0b1111},
		// addCoreUsage returns only the newly assigned bits, not the union
		// with prev: bit 2 is the one new bit picked here, not 0b0111.
		{"2 already used, request 1 more of 4", map[string]any{AWSUsageInfo: 0b0011}, 1, 4, 0b0100},
		{"2-core device unaffected", map[string]any{}, 2, 2, 0b11},
		{"2-core device single-core unaffected", map[string]any{}, 1, 2, 0b01},
		{"device full, request 1 more returns no new bits", map[string]any{AWSUsageInfo: 0b1111}, 1, 4, 0b0000},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := addCoreUsage(test.prev, test.require, test.maxCores)
			gotVal, ok := got[AWSUsageInfo].(int)
			assert.Assert(t, ok, "expected AWSUsageInfo to be an int")
			assert.Equal(t, gotVal, test.want)
		})
	}
}

func Test_PatchAnnotations_Inf1FourCore(t *testing.T) {
	config := AWSNeuronConfig{
		ResourceCountName: "aws.amazon.com/neuron",
		ResourceCoreName:  "aws.amazon.com/neuroncore",
	}
	dev := InitAWSNeuronDevice(config)
	dev.coresPerAWSNeuron = 4

	pod := corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"aws.amazon.com/neuroncore": resource.MustParse("3"),
						},
					},
				},
			},
		},
	}
	annoinput := map[string]string{}
	pd := device.PodDevices{
		AWSNeuronDevice: device.PodSingleDevice{
			device.ContainerDevices{
				{
					Idx:       0,
					UUID:      "test1",
					Type:      AWSNeuronDevice,
					Usedmem:   int32(0),
					Usedcores: int32(0b0111), // cores 0,1,2 of 4 used
					CustomInfo: map[string]any{
						AWSUsageInfo: 0b0111,
					},
				},
			},
		},
	}
	result := dev.PatchAnnotations(&pod, &annoinput, pd)
	// All three used cores (0,1,2) must be reported, not just the first two.
	assert.Equal(t, result[AWSNeuronAssignedIndex], "0,1,2")
	assert.Equal(t, result[AWSNeuronResourceType], "aws.amazon.com/neuroncore")
}

func Test_Fit_WholeDeviceNotShared(t *testing.T) {
	dev := &AWSNeuronDevices{
		resourceCountName: "aws.amazon.com/neuron",
		resourceCoreName:  "aws.amazon.com/neuroncore",
		coresPerAWSNeuron: 4,
	}
	du := &device.DeviceUsage{
		ID:         "node-AWSNeuron-0",
		Index:      0,
		Count:      4,
		Totalcore:  15,
		Type:       AWSNeuronDevice,
		Health:     true,
		CustomInfo: map[string]any{AWSNodeType: "inf1.6xlarge"},
	}
	podA := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "default", Annotations: map[string]string{}}}
	wholeDevice := dev.GenerateResourceRequests(&corev1.Container{
		Name: "ctr",
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{"aws.amazon.com/neuron": resource.MustParse("1")},
		},
	})
	fit, tmp, reason := dev.Fit([]*device.DeviceUsage{du}, wholeDevice, podA, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, true, reason)
	cd := tmp[AWSNeuronDevice][0]
	assert.NilError(t, dev.AddResourceUsage(podA, du, &cd))

	podB := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "default", Annotations: map[string]string{}}}
	coreReq := device.ContainerDeviceRequest{Nums: 1, Type: AWSNeuronDevice, Coresreq: 2}
	fitB, _, _ := dev.Fit([]*device.DeviceUsage{du}, coreReq, podB, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fitB, false)
}
