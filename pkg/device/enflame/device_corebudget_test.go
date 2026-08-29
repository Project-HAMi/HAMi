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

package enflame

import (
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

func drsBudgetDevice() *device.DeviceUsage {
	return &device.DeviceUsage{
		ID:        "node-a-enflame-drs-0",
		Index:     0,
		Count:     6,
		Totalmem:  40960,
		Totalcore: 100,
		Type:      EnflameVGCUDevice,
		Health:    true,
		CustomInfo: map[string]any{
			"minor": "0",
			"index": "0",
			"profiles": map[string]string{
				"1g.6gb":  "0",
				"3g.20gb": "1",
				"6g.40gb": "2",
			},
		},
	}
}

// A device advertising six DRS slices must accept six 1g.6gb workloads. The
// ceil based core cost priced each slice at 17, so the sixth slice was
// rejected with CardInsufficientCore while slices and memory were still free.
func TestFit_AllAdvertisedSlicesSchedulable(t *testing.T) {
	dev := InitEnflameDevice(EnflameConfig{ResourceNameDRSGCU: "enflame.com/drs-gcu"})
	du := drsBudgetDevice()
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}
	req := device.ContainerDeviceRequest{Nums: 1, Type: EnflameVGCUDevice, Memreq: 1}

	for i := range 6 {
		fit, result, reason := dev.Fit([]*device.DeviceUsage{du}, req, pod, &device.NodeInfo{}, &device.PodDevices{})
		assert.Equal(t, fit, true, "slice %d of 6 rejected: %s", i+1, reason)
		cd := result[EnflameVGCUDevice][0]
		assert.NilError(t, dev.AddResourceUsage(pod, du, &cd))
	}
	assert.Equal(t, du.Used, int32(6))

	// The device is now genuinely full: a seventh slice must be rejected.
	fit, _, _ := dev.Fit([]*device.DeviceUsage{du}, req, pod, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, false)
}

// Mixed profiles that exactly fill the slice capacity must also fit: one
// 3g.20gb plus three 1g.6gb slices occupy all six slices.
func TestFit_MixedProfilesFillDevice(t *testing.T) {
	dev := InitEnflameDevice(EnflameConfig{ResourceNameDRSGCU: "enflame.com/drs-gcu"})
	du := drsBudgetDevice()
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}

	requests := []device.ContainerDeviceRequest{
		{Nums: 1, Type: EnflameVGCUDevice, Memreq: 3},
		{Nums: 1, Type: EnflameVGCUDevice, Memreq: 1},
		{Nums: 1, Type: EnflameVGCUDevice, Memreq: 1},
		{Nums: 1, Type: EnflameVGCUDevice, Memreq: 1},
	}
	for i, req := range requests {
		fit, result, reason := dev.Fit([]*device.DeviceUsage{du}, req, pod, &device.NodeInfo{}, &device.PodDevices{})
		assert.Equal(t, fit, true, "request %d rejected: %s", i, reason)
		cd := result[EnflameVGCUDevice][0]
		assert.NilError(t, dev.AddResourceUsage(pod, du, &cd))
	}
	assert.Equal(t, du.Used, int32(6))
}
