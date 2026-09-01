/*
Copyright 2025 The HAMi Authors.

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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
)

func cordonNode(uuids string) *device.NodeInfo {
	return &device.NodeInfo{Node: &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{device.DeviceCordonAnnotation: uuids}},
	}}
}

func TestFit_DeviceCordon(t *testing.T) {
	dev := InitAWSNeuronDevice(AWSNeuronConfig{
		ResourceCountName: "aws.amazon.com/neuron",
		ResourceCoreName:  "aws.amazon.com/neuroncore",
	})

	newDevices := func() []*device.DeviceUsage {
		return []*device.DeviceUsage{
			makeAWSDeviceUsage("dev-0", 0, 0, 2, 3, 0, "trn", true),
			makeAWSDeviceUsage("dev-1", 1, 0, 12, 3, 0, "trn", true),
		}
	}
	request := device.ContainerDeviceRequest{Nums: 1, Coresreq: 2, Type: AWSNeuronDevice}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}

	t.Run("cordoned device is skipped, healthy sibling still fits", func(t *testing.T) {
		fit, result, reason := dev.Fit(newDevices(), request, pod, cordonNode("dev-1, "), &device.PodDevices{})
		if !fit {
			t.Fatalf("expected fit, got reason: %s", reason)
		}
		if got := result[AWSNeuronDevice][0].UUID; got != "dev-0" {
			t.Errorf("expected dev-0 (dev-1 is cordoned), got %s", got)
		}
	})

	t.Run("all devices cordoned fails with CardCordoned reason", func(t *testing.T) {
		fit, _, reason := dev.Fit(newDevices(), request, pod, cordonNode("dev-0,dev-1"), &device.PodDevices{})
		if fit {
			t.Fatal("expected no fit, all devices are cordoned")
		}
		if want := "2/2 " + common.CardCordoned; reason != want {
			t.Errorf("expected reason %q, got %q", want, reason)
		}
	})

	t.Run("no annotation or no node info means nothing cordoned", func(t *testing.T) {
		for name, nodeInfo := range map[string]*device.NodeInfo{
			"empty NodeInfo":        {},
			"node without annos":    {Node: &corev1.Node{}},
			"annotation empty":      cordonNode(""),
			"annotation whitespace": cordonNode("   "),
		} {
			fit, result, reason := dev.Fit(newDevices(), request, pod, nodeInfo, &device.PodDevices{})
			if !fit {
				t.Fatalf("%s: expected fit, got reason: %s", name, reason)
			}
			if got := result[AWSNeuronDevice][0].UUID; got != "dev-1" {
				t.Errorf("%s: expected dev-1, got %s", name, got)
			}
		}
	})
}

// newRingDevices builds the 16-device ring graphSelect walks by slice position.
// Only the first device carries the node type, matching how the node reports it.
func newRingDevices() []*device.DeviceUsage {
	devices := make([]*device.DeviceUsage, 16)
	for i := range devices {
		du := &device.DeviceUsage{
			ID:     fmt.Sprintf("dev-%d", i),
			Index:  uint(i),
			Type:   AWSNeuronDevice,
			Health: true,
		}
		if i == 0 {
			du.CustomInfo = map[string]any{AWSNodeType: "inf2"}
		}
		devices[i] = du
	}
	return devices
}

func TestFit_DeviceCordonMultiCardTopology(t *testing.T) {
	dev := InitAWSNeuronDevice(AWSNeuronConfig{
		ResourceCountName: "aws.amazon.com/neuron",
		ResourceCoreName:  "aws.amazon.com/neuroncore",
	})
	request := device.ContainerDeviceRequest{Nums: 4, Type: AWSNeuronDevice}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}

	t.Run("a run stepping over a cordoned device is rejected, the next run wins", func(t *testing.T) {
		fit, result, reason := dev.Fit(newRingDevices(), request, pod, cordonNode("dev-0"), &device.PodDevices{})
		if !fit {
			t.Fatalf("expected fit on a run clear of the cordoned device, got reason: %s", reason)
		}
		allocated := result[AWSNeuronDevice]
		if len(allocated) != 4 {
			t.Fatalf("expected 4 allocated devices, got %d", len(allocated))
		}
		// The contiguous run must start after the cordoned device, keeping the
		// ring positions intact rather than compacting the slice.
		for i, d := range allocated {
			if want := fmt.Sprintf("dev-%d", i+1); d.UUID != want {
				t.Errorf("expected %s at position %d, got %s", want, i, d.UUID)
			}
			if d.UUID == "dev-0" {
				t.Errorf("cordoned dev-0 must not be allocated")
			}
		}
	})

	t.Run("all devices cordoned fails with CardCordoned rather than NumaNotFit", func(t *testing.T) {
		all := make([]string, 16)
		for i := range all {
			all[i] = fmt.Sprintf("dev-%d", i)
		}
		fit, _, reason := dev.Fit(newRingDevices(), request, pod, cordonNode(strings.Join(all, ",")), &device.PodDevices{})
		if fit {
			t.Fatal("expected no fit, all devices are cordoned")
		}
		if want := "16/16 " + common.CardCordoned; reason != want {
			t.Errorf("expected reason %q, got %q", want, reason)
		}
	})

	t.Run("nothing cordoned still reports NumaNotFit when no run exists", func(t *testing.T) {
		devices := newRingDevices()
		// Break every possible run of 4 without cordoning anything.
		for i := 3; i < len(devices); i += 4 {
			devices[i].Used = 1
		}
		fit, _, reason := dev.Fit(devices, request, pod, &device.NodeInfo{}, &device.PodDevices{})
		if fit {
			t.Fatal("expected no fit, no contiguous run of 4 is available")
		}
		if want := "1/16 " + common.NumaNotFit; reason != want {
			t.Errorf("expected reason %q, got %q", want, reason)
		}
	})
}
