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

package nvidia

import (
	"strconv"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

func makeBenchmarkDevices(n int) []*device.DeviceUsage {
	devices := make([]*device.DeviceUsage, n)
	for i := range n {
		devices[i] = &device.DeviceUsage{
			ID:        "dev-" + strconv.Itoa(i),
			Index:     uint(i),
			Used:      0,
			Count:     10,
			Usedmem:   0,
			Totalmem:  32768,
			Usedcores: 0,
			Totalcore: 100,
			Type:      NvidiaGPUDevice,
			Health:    true,
		}
	}
	return devices
}

// registerBenchmarkDevice installs nv into the global DevicesMap so that the
// fitQuota path takes its real branch (rather than the early-return when the
// device is unregistered). This makes benchmark numbers reflect production.
func registerBenchmarkDevice(nv *NvidiaGPUDevices) {
	device.DevicesMap = map[string]device.Devices{NvidiaGPUDevice: nv}
}

func BenchmarkFit_SingleCardFromEight(b *testing.B) {
	nv := InitNvidiaDevice(NvidiaConfig{
		ResourceCountName:  "nvidia.com/gpu",
		ResourceMemoryName: "nvidia.com/gpumem",
		ResourceCoreName:   "nvidia.com/gpucores",
	})
	registerBenchmarkDevice(nv)
	devices := makeBenchmarkDevices(8)
	req := device.ContainerDeviceRequest{Nums: 1, Memreq: 1024, Coresreq: 10, Type: NvidiaGPUDevice}
	pod := &corev1.Pod{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = nv.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
	}
}

func BenchmarkFit_FourCardsFromEight(b *testing.B) {
	nv := InitNvidiaDevice(NvidiaConfig{
		ResourceCountName:  "nvidia.com/gpu",
		ResourceMemoryName: "nvidia.com/gpumem",
		ResourceCoreName:   "nvidia.com/gpucores",
	})
	registerBenchmarkDevice(nv)
	devices := makeBenchmarkDevices(8)
	req := device.ContainerDeviceRequest{Nums: 4, Memreq: 1024, Coresreq: 10, Type: NvidiaGPUDevice}
	pod := &corev1.Pod{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = nv.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
	}
}

func BenchmarkFit_AllCardsFail(b *testing.B) {
	nv := InitNvidiaDevice(NvidiaConfig{
		ResourceCountName:  "nvidia.com/gpu",
		ResourceMemoryName: "nvidia.com/gpumem",
		ResourceCoreName:   "nvidia.com/gpucores",
	})
	registerBenchmarkDevice(nv)
	devices := makeBenchmarkDevices(8)
	for _, d := range devices {
		d.Totalmem = 100
		d.Usedmem = 100
	}
	req := device.ContainerDeviceRequest{Nums: 1, Memreq: 1024, Coresreq: 10, Type: NvidiaGPUDevice}
	pod := &corev1.Pod{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = nv.Fit(devices, req, pod, &device.NodeInfo{}, &device.PodDevices{})
	}
}

func BenchmarkRunCardChecks_AllPass(b *testing.B) {
	nv := InitNvidiaDevice(NvidiaConfig{
		ResourceCountName:  "nvidia.com/gpu",
		ResourceMemoryName: "nvidia.com/gpumem",
		ResourceCoreName:   "nvidia.com/gpucores",
	})
	registerBenchmarkDevice(nv)
	dev := &device.DeviceUsage{
		ID:        "dev-0",
		Health:    true,
		Count:     10,
		Used:      0,
		Totalmem:  32768,
		Usedmem:   0,
		Totalcore: 100,
		Usedcores: 0,
		Type:      NvidiaGPUDevice,
	}
	req := device.ContainerDeviceRequest{Type: NvidiaGPUDevice, Memreq: 1024, Coresreq: 10}
	tmpDevs := make(map[string]device.ContainerDevices)
	ctx := &cardCheckCtx{
		request:    &req,
		pod:        &corev1.Pod{},
		tmpDevsMap: tmpDevs,
		deviceType: NvidiaGPUDevice,
		commonWord: nv.CommonWord(),
		nv:         nv,
		memreq:     1024,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = runCardChecks(dev, ctx)
	}
}
