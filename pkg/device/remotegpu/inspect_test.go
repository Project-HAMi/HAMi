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

package remotegpu

import (
	"context"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

// Inspect is the metrics view of the fleet: one entry per card, keyed by the
// server that owns it, reserved when a pod anywhere holds it or this scheduler
// booked it, and never a refresh of its own.
func TestInspect_ReportsEachCardOnceByServer(t *testing.T) {
	held := device.EncodePodSingleDevice(device.PodSingleDevice{
		device.ContainerDevices{{UUID: "gpu-a/GPU-1", Type: RemoteGPUCommonWord}},
	})
	stubFleet(t, []corev1.Node{
		lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000), gpu("GPU-2", 40000)}),
		lupineNode("gpu-b", "10.0.0.6", "15000", []*device.DeviceInfo{gpu("GPU-3", 80000)}),
	}, []corev1.Pod{{
		ObjectMeta: metav1.ObjectMeta{Name: "live", Annotations: map[string]string{AllocatedAnnos: held}},
		Spec:       corev1.PodSpec{NodeName: "cpu-1"},
	}}, nil)

	p := newPool(DefaultLupinePort)
	assert.Equal(t, len(p.inspect()), 0, "nothing read yet, and inspect does not read")

	p.snapshot(context.Background())
	p.hold("pod-2", "gpu-b/GPU-3")

	got := p.inspect()
	assert.Equal(t, len(got), 3)
	byID := map[string]PoolDevice{}
	for _, d := range got {
		byID[d.Device.ID] = d
	}
	assert.DeepEqual(t, byID["gpu-a/GPU-1"], PoolDevice{Server: "gpu-a", Endpoint: "10.0.0.5:14833", Device: byID["gpu-a/GPU-1"].Device, Reserved: true})
	assert.Equal(t, byID["gpu-a/GPU-2"].Reserved, false)
	assert.Equal(t, byID["gpu-b/GPU-3"].Endpoint, "10.0.0.6:15000")
	assert.Assert(t, byID["gpu-b/GPU-3"].Reserved, "a booking counts until the pod list catches up")
	assert.Equal(t, byID["gpu-a/GPU-1"].Device.Devmem, int32(40000))
	assert.Equal(t, byID["gpu-a/GPU-1"].Device.Type, RemoteGPUCommonWord)
}
