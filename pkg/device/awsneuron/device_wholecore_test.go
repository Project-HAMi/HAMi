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

// On inf1 hardware a NeuronDevice exposes four cores, but addCoreUsage tracks
// a two-bit mask and PatchAnnotations emits only the first two core indexes.
// A whole-device request must therefore reserve the capped core count, and the
// registered core mask must stay within the same cap, or a second pod can be
// packed onto a device that is already fully owned.

func Test_GenerateResourceRequests_WholeDeviceCapsCores(t *testing.T) {
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
	assert.Equal(t, req.Coresreq, int32(maxCoresPerNeuronDevice))
}

func Test_GetNodeDevices_CoreMaskCappedOnFourCoreHardware(t *testing.T) {
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
	// Two addressable cores yield the two-bit mask 3, not the four-bit mask 15.
	assert.Equal(t, devices[0].Devcore, int32(3))
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
		Totalcore:  3,
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
