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
	assert.Equal(t, byID["gpu-a/GPU-1"].Device.Devmem, int64(40000))
	assert.Equal(t, byID["gpu-a/GPU-1"].Device.Type, RemoteGPUCommonWord)
}

// A server whose /metrics stops answering reads as down, and keeps the time of
// its last good read so an alert can tell how stale its reported usage is.
func TestInspectServers_TracksScrapeOutcome(t *testing.T) {
	stubFleet(t, []corev1.Node{
		lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)}),
		lupineNode("gpu-b", "10.0.0.6", "", []*device.DeviceInfo{gpu("GPU-2", 40000)}),
	}, nil, nil)
	stubServerUsage(t, map[string][]string{"10.0.0.5:14833": nil})

	p := newPool(DefaultLupinePort)
	assert.Equal(t, len(p.inspectServers()), 0, "nothing read yet")
	p.snapshot(context.Background())

	got := p.inspectServers()
	assert.Equal(t, len(got), 2)
	assert.Equal(t, got[0].Server, "gpu-a")
	assert.Assert(t, got[0].Up)
	firstSuccess := got[0].LastSuccess
	assert.Assert(t, !firstSuccess.IsZero())
	assert.Equal(t, got[1].Server, "gpu-b")
	assert.Assert(t, !got[1].Up)
	assert.Assert(t, got[1].LastSuccess.IsZero(), "never read")

	stubServerUsage(t, map[string][]string{})
	p.refreshNow(context.Background())
	got = p.inspectServers()
	assert.Assert(t, !got[0].Up)
	assert.Equal(t, got[0].LastSuccess, firstSuccess, "a failed read keeps the last success")
}

// A server that is NotReady for one refresh is not asked, and must not lose
// the time of its last good read while it is out.
func TestInspectServers_KeepsLastSuccessAcrossNotReady(t *testing.T) {
	nodes := []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)})}
	stubFleet(t, nodes, nil, nil)
	stubServerUsage(t, map[string][]string{"10.0.0.5:14833": nil})

	p := newPool(DefaultLupinePort)
	p.snapshot(context.Background())
	firstSuccess := p.inspectServers()[0].LastSuccess
	assert.Assert(t, !firstSuccess.IsZero())

	setNodeReady(&nodes[0], corev1.ConditionFalse)
	p.fetchedAt = p.fetchedAt.Add(-poolTTL)
	p.snapshot(context.Background())
	assert.Equal(t, len(p.inspectServers()), 0, "a NotReady server is not listed")

	setNodeReady(&nodes[0], corev1.ConditionTrue)
	stubServerUsage(t, map[string][]string{})
	p.fetchedAt = p.fetchedAt.Add(-poolTTL)
	p.snapshot(context.Background())
	got := p.inspectServers()
	assert.Equal(t, len(got), 1)
	assert.Assert(t, !got[0].Up)
	assert.Equal(t, got[0].LastSuccess, firstSuccess)
}
