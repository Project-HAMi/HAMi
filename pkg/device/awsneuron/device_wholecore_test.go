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
	"github.com/Project-HAMi/HAMi/pkg/util"
)

// A whole-device request must reserve every core exposed by the selected
// device, so no core request can share it afterwards.

func Test_GenerateResourceRequests_WholeDeviceDefersCoreCountToFit(t *testing.T) {
	dev := &AWSNeuronDevices{
		resourceCountName: "aws.amazon.com/neuron",
		resourceCoreName:  "aws.amazon.com/neuroncore",
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
	assert.Equal(t, req.Coresreq, int32(0))
}

func Test_GetNodeDevices_RegistersFourCoreInferentia1Mask(t *testing.T) {
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
	assert.Equal(t, devices[0].Devcore, int32(15))
}

func Test_Fit_WholeDeviceNotShared(t *testing.T) {
	dev := &AWSNeuronDevices{
		resourceCountName: "aws.amazon.com/neuron",
		resourceCoreName:  "aws.amazon.com/neuroncore",
	}
	du := &device.DeviceUsage{
		ID:        "node-AWSNeuron-0",
		Index:     0,
		Count:     4,
		Totalcore: 15,
		Type:      AWSNeuronDevice,
		Health:    true,
		CustomInfo: map[string]any{
			AWSNodeType:             "inf1.6xlarge",
			AWSCoresPerNeuronDevice: int32(4),
		},
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

func Test_Fit_FourCoreRequestRequiresInferentia1(t *testing.T) {
	dev := InitAWSNeuronDevice(AWSNeuronConfig{
		ResourceCountName: "aws.amazon.com/neuron",
		ResourceCoreName:  "aws.amazon.com/neuroncore",
	})
	request := dev.GenerateResourceRequests(&corev1.Container{
		Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
			"aws.amazon.com/neuroncore": resource.MustParse("4"),
		}},
	})
	assert.Equal(t, request.Coresreq, int32(4))

	inf1Devices, err := dev.GetNodeDevices(newNeuronNode("inf1", "inf1.6xlarge", 1, 4))
	assert.NilError(t, err)
	inf1Usage := &device.DeviceUsage{
		ID:         inf1Devices[0].ID,
		Index:      inf1Devices[0].Index,
		Count:      inf1Devices[0].Count,
		Totalcore:  inf1Devices[0].Devcore,
		Type:       inf1Devices[0].Type,
		Health:     true,
		CustomInfo: inf1Devices[0].CustomInfo,
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "four-core", Namespace: "default", Annotations: map[string]string{}}}
	fit, allocations, reason := dev.Fit([]*device.DeviceUsage{inf1Usage}, request, pod, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, true, reason)
	assert.Equal(t, allocations[AWSNeuronDevice][0].Usedcores, int32(15))

	inf2Devices, err := dev.GetNodeDevices(newNeuronNode("inf2", "inf2.8xlarge", 1, 2))
	assert.NilError(t, err)
	inf2Usage := &device.DeviceUsage{
		ID:         inf2Devices[0].ID,
		Index:      inf2Devices[0].Index,
		Count:      inf2Devices[0].Count,
		Totalcore:  inf2Devices[0].Devcore,
		Type:       inf2Devices[0].Type,
		Health:     true,
		CustomInfo: inf2Devices[0].CustomInfo,
	}
	fit, _, _ = dev.Fit([]*device.DeviceUsage{inf2Usage}, request, pod, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, false)
}

func Test_PatchAnnotations_EmitsAllInferentia1CoreIndexes(t *testing.T) {
	dev := InitAWSNeuronDevice(AWSNeuronConfig{
		ResourceCountName: "aws.amazon.com/neuron",
		ResourceCoreName:  "aws.amazon.com/neuroncore",
	})
	pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{
		Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
			"aws.amazon.com/neuroncore": resource.MustParse("4"),
		}},
	}}}}
	annotations := map[string]string{util.AssignedNodeAnnotations: "inf1"}
	dev.PatchAnnotations(pod, &annotations, device.PodDevices{
		AWSNeuronDevice: device.PodSingleDevice{device.ContainerDevices{{
			Idx:       1,
			Usedcores: 15,
			CustomInfo: map[string]any{
				AWSCoresPerNeuronDevice: int32(4),
			},
		}}},
	})
	assert.Equal(t, annotations[AWSNeuronAssignedIndex], "4,5,6,7")
}
