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

package enflame

import (
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
	dev := InitEnflameDevice(EnflameConfig{ResourceNameDRSGCU: "enflame.com/drs-gcu"})

	newDevices := func() []*device.DeviceUsage {
		profiles := map[string]string{"1g.6gb": "0", "3g.20gb": "1", "6g.40gb": "2"}
		return []*device.DeviceUsage{
			{
				ID: "drs-0", Index: 0, Count: 6, Totalmem: 40960, Type: EnflameVGCUDevice, Health: true,
				CustomInfo: map[string]any{"minor": "0", "index": "0", "profiles": profiles},
			},
			{
				ID: "drs-1", Index: 1, Count: 6, Totalmem: 40960, Type: EnflameVGCUDevice, Health: true,
				CustomInfo: map[string]any{"minor": "1", "index": "1", "profiles": profiles},
			},
		}
	}
	request := device.ContainerDeviceRequest{Nums: 1, Type: EnflameVGCUDevice, Memreq: 3}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}

	t.Run("cordoned device is skipped, healthy sibling still fits", func(t *testing.T) {
		fit, result, reason := dev.Fit(newDevices(), request, pod, cordonNode("drs-1, "), &device.PodDevices{})
		if !fit {
			t.Fatalf("expected fit, got reason: %s", reason)
		}
		if got := result[EnflameVGCUDevice][0].UUID; got != "drs-0" {
			t.Errorf("expected drs-0 (drs-1 is cordoned), got %s", got)
		}
	})

	t.Run("all devices cordoned fails with CardCordoned reason", func(t *testing.T) {
		fit, _, reason := dev.Fit(newDevices(), request, pod, cordonNode("drs-0,drs-1"), &device.PodDevices{})
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
			if got := result[EnflameVGCUDevice][0].UUID; got != "drs-1" {
				t.Errorf("%s: expected drs-1, got %s", name, got)
			}
		}
	})
}

func TestGCUFit_DeviceCordon(t *testing.T) {
	dev := InitGCUDevice(EnflameConfig{ResourceNameGCU: "enflame.com/gcu"})

	newDevices := func() []*device.DeviceUsage {
		return []*device.DeviceUsage{
			{ID: "dev-0", Index: 0, Count: 1, Totalmem: 100, Totalcore: 100, Type: EnflameGCUDevice, Health: true},
			{ID: "dev-1", Index: 1, Count: 1, Totalmem: 100, Totalcore: 100, Type: EnflameGCUDevice, Health: true},
		}
	}
	request := device.ContainerDeviceRequest{Nums: 1, Memreq: 100, MemPercentagereq: 100, Coresreq: 100, Type: EnflameGCUDevice}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}

	t.Run("cordoned device is skipped, healthy sibling still fits", func(t *testing.T) {
		fit, result, reason := dev.Fit(newDevices(), request, pod, cordonNode("dev-1, "), &device.PodDevices{})
		if !fit {
			t.Fatalf("expected fit, got reason: %s", reason)
		}
		if got := result[EnflameGCUDevice][0].UUID; got != "dev-0" {
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
			if got := result[EnflameGCUDevice][0].UUID; got != "dev-1" {
				t.Errorf("%s: expected dev-1, got %s", name, got)
			}
		}
	})
}
