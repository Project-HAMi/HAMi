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

package metax

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
)

func nodeWithCordon(uuids string) *device.NodeInfo {
	return &device.NodeInfo{Node: &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{device.DeviceCordonAnnotation: uuids}},
	}}
}

func TestFit_DeviceCordon(t *testing.T) {
	dev := InitMetaxDevice(MetaxConfig{ResourceCountName: "metax-tech.com/gpu"})

	newDevices := func() []*device.DeviceUsage {
		return []*device.DeviceUsage{
			{ID: "dev-0", Index: 0, Count: 100, Totalmem: 128, Totalcore: 100, Type: MetaxGPUDevice, Health: true},
			{ID: "dev-1", Index: 1, Count: 100, Totalmem: 128, Totalcore: 100, Type: MetaxGPUDevice, Health: true},
		}
	}
	request := device.ContainerDeviceRequest{Nums: 1, Memreq: 64, Coresreq: 50, Type: MetaxGPUDevice}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}

	t.Run("cordoned device is skipped, healthy sibling still fits", func(t *testing.T) {
		fit, result, reason := dev.Fit(newDevices(), request, pod, nodeWithCordon("dev-1, "), &device.PodDevices{})
		if !fit {
			t.Fatalf("expected fit, got reason: %s", reason)
		}
		if got := result[MetaxGPUDevice][0].UUID; got != "dev-0" {
			t.Errorf("expected dev-0 (dev-1 is cordoned), got %s", got)
		}
	})

	t.Run("all devices cordoned fails with CardCordoned reason", func(t *testing.T) {
		fit, _, reason := dev.Fit(newDevices(), request, pod, nodeWithCordon("dev-0,dev-1"), &device.PodDevices{})
		if fit {
			t.Fatal("expected no fit, all devices are cordoned")
		}
		if want := "2/2 " + common.CardCordoned; reason != want {
			t.Errorf("expected reason %q, got %q", want, reason)
		}
	})

	t.Run("running pods on a cordoned device are unaffected", func(t *testing.T) {
		devices := newDevices()
		devices[1].Used = 1
		devices[1].Usedcores = 50
		devices[1].Usedmem = 64
		fit, result, reason := dev.Fit(devices, request, pod, nodeWithCordon("dev-1"), &device.PodDevices{})
		if !fit {
			t.Fatalf("expected fit onto the non-cordoned device, got reason: %s", reason)
		}
		if got := result[MetaxGPUDevice][0].UUID; got != "dev-0" {
			t.Errorf("expected dev-0, got %s", got)
		}
		if devices[1].Used != 1 {
			t.Errorf("cordon must not touch existing usage on dev-1, got Used=%d", devices[1].Used)
		}
	})

	t.Run("no annotation or no node info means nothing cordoned", func(t *testing.T) {
		for name, nodeInfo := range map[string]*device.NodeInfo{
			"empty NodeInfo":        {},
			"node without annos":    {Node: &corev1.Node{}},
			"annotation empty":      nodeWithCordon(""),
			"annotation whitespace": nodeWithCordon("   "),
		} {
			fit, result, reason := dev.Fit(newDevices(), request, pod, nodeInfo, &device.PodDevices{})
			if !fit {
				t.Fatalf("%s: expected fit, got reason: %s", name, reason)
			}
			if got := result[MetaxGPUDevice][0].UUID; got != "dev-1" {
				t.Errorf("%s: expected dev-1, got %s", name, got)
			}
		}
	})
}

func TestSDeviceFit_DeviceCordon(t *testing.T) {
	dev := InitMetaxSDevice(MetaxConfig{
		ResourceVCountName:  "metax-tech.com/sgpu",
		ResourceVCoreName:   "metax-tech.com/vcore",
		ResourceVMemoryName: "metax-tech.com/vmemory",
	})

	newDevices := func() []*device.DeviceUsage {
		return []*device.DeviceUsage{
			{ID: "dev-0", Index: 0, Count: 100, Totalmem: 128, Totalcore: 100, Type: MetaxSGPUDevice, Health: true},
			{ID: "dev-1", Index: 1, Count: 100, Totalmem: 128, Totalcore: 100, Type: MetaxSGPUDevice, Health: true},
		}
	}
	request := device.ContainerDeviceRequest{Nums: 1, Memreq: 64, Coresreq: 50, Type: MetaxSGPUDevice}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}

	t.Run("cordoned device is skipped, healthy sibling still fits", func(t *testing.T) {
		fit, result, reason := dev.Fit(newDevices(), request, pod, nodeWithCordon("dev-1, "), &device.PodDevices{})
		if !fit {
			t.Fatalf("expected fit, got reason: %s", reason)
		}
		if got := result[MetaxSGPUDevice][0].UUID; got != "dev-0" {
			t.Errorf("expected dev-0 (dev-1 is cordoned), got %s", got)
		}
	})

	t.Run("all devices cordoned fails with CardCordoned reason", func(t *testing.T) {
		fit, _, reason := dev.Fit(newDevices(), request, pod, nodeWithCordon("dev-0,dev-1"), &device.PodDevices{})
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
			"annotation empty":      nodeWithCordon(""),
			"annotation whitespace": nodeWithCordon("   "),
		} {
			fit, result, reason := dev.Fit(newDevices(), request, pod, nodeInfo, &device.PodDevices{})
			if !fit {
				t.Fatalf("%s: expected fit, got reason: %s", name, reason)
			}
			if got := result[MetaxSGPUDevice][0].UUID; got != "dev-1" {
				t.Errorf("%s: expected dev-1, got %s", name, got)
			}
		}
	})
}
