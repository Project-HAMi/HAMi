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

package kunlun

import (
	"fmt"
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

// newXPUs builds the 8-card topology graghSelect reads by slice position, so a
// cordoned card must be rejected in place rather than filtered out of the slice.
func newXPUs(count int32) []*device.DeviceUsage {
	devices := make([]*device.DeviceUsage, 8)
	for i := range devices {
		devices[i] = &device.DeviceUsage{
			Index:     uint(i),
			ID:        fmt.Sprintf("xpu-%d", i),
			Count:     count,
			Totalmem:  KunlunMaxMemory,
			Totalcore: 100,
			Health:    true,
		}
	}
	return devices
}

func allXPUs() string {
	all := ""
	for i := range 8 {
		if i > 0 {
			all += ","
		}
		all += fmt.Sprintf("xpu-%d", i)
	}
	return all
}

func TestKunlunDevices_Fit_DeviceCordon(t *testing.T) {
	dev := &KunlunDevices{}
	request := device.ContainerDeviceRequest{Nums: 1, Type: KunlunGPUDevice}
	pod := &corev1.Pod{}

	t.Run("a cordoned card is not allocated, positions are preserved", func(t *testing.T) {
		// Take whatever the uncordoned topology picks, then cordon exactly that
		// card: the fit must survive and land somewhere else.
		fit, result, reason := dev.Fit(newXPUs(1), request, pod, &device.NodeInfo{}, &device.PodDevices{})
		if !fit {
			t.Fatalf("expected fit with nothing cordoned, got reason: %s", reason)
		}
		picked := result[KunlunGPUDevice][0].UUID

		fit, result, reason = dev.Fit(newXPUs(1), request, pod, cordonNode(picked+", "), &device.PodDevices{})
		if !fit {
			t.Fatalf("expected fit onto another card, got reason: %s", reason)
		}
		if got := result[KunlunGPUDevice][0].UUID; got == picked {
			t.Errorf("expected a card other than the cordoned %s, got %s", picked, got)
		}
	})

	t.Run("all cards cordoned fails with CardCordoned reason", func(t *testing.T) {
		fit, _, reason := dev.Fit(newXPUs(1), request, pod, cordonNode(allXPUs()), &device.PodDevices{})
		if fit {
			t.Fatal("expected no fit, all cards are cordoned")
		}
		if want := "8/8 " + common.CardCordoned; reason != want {
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
			fit, _, reason := dev.Fit(newXPUs(1), request, pod, nodeInfo, &device.PodDevices{})
			if !fit {
				t.Fatalf("%s: expected fit, got reason: %s", name, reason)
			}
		}
	})
}

func TestKunlunVDevices_Fit_DeviceCordon(t *testing.T) {
	dev := &KunlunVDevices{}
	request := device.ContainerDeviceRequest{Nums: 1, Type: XPUDevice, Memreq: KunlunMaxMemory}
	pod := &corev1.Pod{}

	t.Run("a cordoned card is not allocated, positions are preserved", func(t *testing.T) {
		fit, result, reason := dev.Fit(newXPUs(10), request, pod, &device.NodeInfo{}, &device.PodDevices{})
		if !fit {
			t.Fatalf("expected fit with nothing cordoned, got reason: %s", reason)
		}
		picked := result[XPUDevice][0].UUID

		fit, result, reason = dev.Fit(newXPUs(10), request, pod, cordonNode(picked+", "), &device.PodDevices{})
		if !fit {
			t.Fatalf("expected fit onto another card, got reason: %s", reason)
		}
		if got := result[XPUDevice][0].UUID; got == picked {
			t.Errorf("expected a card other than the cordoned %s, got %s", picked, got)
		}
	})

	t.Run("all cards cordoned fails with CardCordoned reason", func(t *testing.T) {
		fit, _, reason := dev.Fit(newXPUs(10), request, pod, cordonNode(allXPUs()), &device.PodDevices{})
		if fit {
			t.Fatal("expected no fit, all cards are cordoned")
		}
		if want := "8/8 " + common.CardCordoned; reason != want {
			t.Errorf("expected reason %q, got %q", want, reason)
		}
	})

	t.Run("a cordoned card is not double-counted under the mutex policy", func(t *testing.T) {
		// Every card is in use, so without the cordon each would be reported as
		// an ExclusiveDeviceAllocateConflict. Cordoned cards must be reported
		// only as CardCordoned.
		devices := newXPUs(10)
		for _, d := range devices {
			d.Used = 1
		}
		mutexPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{"hami.io/gpu-scheduler-policy": "mutex"},
		}}
		fit, _, reason := dev.Fit(devices, request, mutexPod, cordonNode(allXPUs()), &device.PodDevices{})
		if fit {
			t.Fatal("expected no fit, all cards are cordoned and in use")
		}
		if want := "8/8 " + common.CardCordoned; reason != want {
			t.Errorf("expected only the cordon reason %q, got %q", want, reason)
		}
	})

	t.Run("no annotation or no node info means nothing cordoned", func(t *testing.T) {
		for name, nodeInfo := range map[string]*device.NodeInfo{
			"empty NodeInfo":        {},
			"node without annos":    {Node: &corev1.Node{}},
			"annotation empty":      cordonNode(""),
			"annotation whitespace": cordonNode("   "),
		} {
			fit, _, reason := dev.Fit(newXPUs(10), request, pod, nodeInfo, &device.PodDevices{})
			if !fit {
				t.Fatalf("%s: expected fit, got reason: %s", name, reason)
			}
		}
	})
}
