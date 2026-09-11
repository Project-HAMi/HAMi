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
	"errors"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

// stubFleet points the pool at a fixed set of nodes and pods for one test.
func stubFleet(t *testing.T, nodes []corev1.Node, pods []corev1.Pod, nodeErr error) {
	t.Helper()
	prevNodes, prevPods := listLupineNodes, listPods
	listLupineNodes = func(context.Context) ([]corev1.Node, error) { return nodes, nodeErr }
	listPods = func(context.Context) ([]corev1.Pod, error) { return pods, nil }
	// Servers report nothing unless a test says otherwise, so existing cases
	// keep exercising the local bookkeeping on its own.
	prevBusy := fetchBusyDevices
	fetchBusyDevices = func(context.Context, string) (map[string]struct{}, error) {
		return nil, errNoServerMetrics
	}
	t.Cleanup(func() {
		listLupineNodes, listPods, fetchBusyDevices = prevNodes, prevPods, prevBusy
	})
}

var errNoServerMetrics = errors.New("no metrics stubbed for this test")

// stubServerUsage makes the fleet report a client on the given GPU UUIDs.
func stubServerUsage(t *testing.T, byEndpoint map[string][]string) {
	t.Helper()
	prev := fetchBusyDevices
	fetchBusyDevices = func(_ context.Context, endpoint string) (map[string]struct{}, error) {
		uuids, ok := byEndpoint[endpoint]
		if !ok {
			return nil, errNoServerMetrics
		}
		out := map[string]struct{}{}
		for _, u := range uuids {
			out[u] = struct{}{}
		}
		return out, nil
	}
	t.Cleanup(func() { fetchBusyDevices = prev })
}

func lupineNode(name, ip, portLabel string, gpus []*device.DeviceInfo) corev1.Node {
	n := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Labels:      map[string]string{LupineServerLabel: portLabel},
			Annotations: map[string]string{},
		},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: ip}},
		},
	}
	if gpus != nil {
		n.Annotations[nvidiaRegisterAnnos] = device.MarshalNodeDevices(gpus)
	}
	return n
}

func gpu(uuid string, mem int32) *device.DeviceInfo {
	return &device.DeviceInfo{ID: uuid, Count: 10, Devmem: mem, Devcore: 100, Type: "NVIDIA", Health: true}
}

func TestPool_BuildsFleetFromNvidiaRegistration(t *testing.T) {
	stubFleet(t, []corev1.Node{
		lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000), gpu("GPU-2", 40000)}),
		lupineNode("gpu-b", "10.0.0.6", "24000", []*device.DeviceInfo{gpu("GPU-3", 80000)}),
	}, nil, nil)

	p := newPool(DefaultLupinePort)
	devices := p.snapshot(context.Background())
	assert.Equal(t, len(devices), 3)

	ids := map[string]bool{}
	for _, d := range devices {
		ids[d.ID] = true
		assert.Equal(t, d.DeviceVendor, RemoteGPUCommonWord)
		// Whole-card allocation: the NVIDIA plugin's split count is discarded.
		assert.Equal(t, d.Count, int32(1))
	}
	assert.Assert(t, ids["gpu-a/GPU-1"])
	assert.Assert(t, ids["gpu-b/GPU-3"])

	// The label value overrides the default port; an empty label keeps it.
	ep, ok := p.endpoint("gpu-a")
	assert.Assert(t, ok)
	assert.Equal(t, ep, "10.0.0.5:14833")
	ep, ok = p.endpoint("gpu-b")
	assert.Assert(t, ok)
	assert.Equal(t, ep, "10.0.0.6:24000")
}

func TestPool_SkipsUnusableNodes(t *testing.T) {
	noIP := lupineNode("gpu-noip", "", "", []*device.DeviceInfo{gpu("GPU-1", 40000)})
	noIP.Status.Addresses = nil
	stubFleet(t, []corev1.Node{
		noIP,
		lupineNode("gpu-unregistered", "10.0.0.7", "", nil),
		lupineNode("gpu-ok", "10.0.0.8", "", []*device.DeviceInfo{gpu("GPU-9", 40000)}),
	}, nil, nil)

	p := newPool(DefaultLupinePort)
	devices := p.snapshot(context.Background())
	assert.Equal(t, len(devices), 1)
	assert.Equal(t, devices[0].ID, "gpu-ok/GPU-9")
}

// An invalid port label must not drop the server; it falls back to the default.
func TestPool_InvalidPortLabelFallsBack(t *testing.T) {
	stubFleet(t, []corev1.Node{
		lupineNode("gpu-a", "10.0.0.5", "not-a-port", []*device.DeviceInfo{gpu("GPU-1", 40000)}),
	}, nil, nil)

	p := newPool(DefaultLupinePort)
	p.snapshot(context.Background())
	ep, ok := p.endpoint("gpu-a")
	assert.Assert(t, ok)
	assert.Equal(t, ep, "10.0.0.5:14833")
}

// A transient API error must keep the last good fleet rather than emptying it,
// which would make every remote-gpu pod unschedulable.
func TestPool_KeepsLastGoodSnapshotOnListError(t *testing.T) {
	nodes := []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)})}
	stubFleet(t, nodes, nil, nil)

	p := newPool(DefaultLupinePort)
	assert.Equal(t, len(p.snapshot(context.Background())), 1)

	stubFleet(t, nil, nil, errors.New("apiserver unavailable"))
	p.fetchedAt = p.fetchedAt.Add(-poolTTL) // force a refresh
	assert.Equal(t, len(p.snapshot(context.Background())), 1)
}

func TestPool_ReservationsFromPodAnnotations(t *testing.T) {
	held := device.EncodePodSingleDevice(device.PodSingleDevice{
		device.ContainerDevices{{UUID: "gpu-a/GPU-1", Type: RemoteGPUCommonWord, Usedmem: 40000, Usedcores: 100}},
	})
	pods := []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "live", Annotations: map[string]string{AllocatedAnnos: held}}},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "done", Annotations: map[string]string{AllocatedAnnos: device.EncodePodSingleDevice(device.PodSingleDevice{
				device.ContainerDevices{{UUID: "gpu-a/GPU-2", Type: RemoteGPUCommonWord}},
			})}},
			Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
		},
		{ObjectMeta: metav1.ObjectMeta{Name: "unrelated"}},
	}
	stubFleet(t, []corev1.Node{
		lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000), gpu("GPU-2", 40000)}),
	}, pods, nil)

	p := newPool(DefaultLupinePort)
	p.snapshot(context.Background())

	assert.Assert(t, p.reserved("gpu-a/GPU-1"), "live pod holds its card")
	assert.Assert(t, !p.reserved("gpu-a/GPU-2"), "a finished pod releases its card")
}

// reservations() only learns about an allocation once the pod carrying it turns
// up in a pod list, which is at most once per poolTTL. PatchAnnotations has to
// book the card itself, or two pods scheduled inside that window are both told
// it is free and get the same GPU.
func TestPatchAnnotations_BooksCardBeforePodListCatchesUp(t *testing.T) {
	stubFleet(t, []corev1.Node{
		lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)}),
	}, nil, nil)
	dev := InitRemoteGPUDevice(testConfig())
	dev.pool.snapshot(context.Background())
	assert.Assert(t, !dev.pool.reserved("gpu-a/GPU-1"), "nothing holds the card yet")

	annos := map[string]string{}
	dev.PatchAnnotations(nil, &annos, device.PodDevices{
		RemoteGPUCommonWord: device.PodSingleDevice{
			device.ContainerDevices{{UUID: "gpu-a/GPU-1", Type: RemoteGPUCommonWord, Usedmem: 40000, Usedcores: 100}},
		},
	})
	assert.Equal(t, annos[LupineServerAnno], "10.0.0.5:14833")
	assert.Assert(t, dev.pool.reserved("gpu-a/GPU-1"), "allocating the card books it right away")

	// The pod list still does not show the pod, so the booking has to survive.
	dev.pool.fetchedAt = dev.pool.fetchedAt.Add(-poolTTL)
	dev.pool.snapshot(context.Background())
	assert.Assert(t, dev.pool.reserved("gpu-a/GPU-1"), "booking outlives a refresh that has not seen the pod")

	// A pod that never appears must not hold the card forever.
	dev.pool.held["gpu-a/GPU-1"] = time.Now().Add(-heldTTL - time.Second)
	dev.pool.fetchedAt = dev.pool.fetchedAt.Add(-poolTTL)
	dev.pool.snapshot(context.Background())
	assert.Assert(t, !dev.pool.reserved("gpu-a/GPU-1"), "a booking whose pod never showed up expires")
}

func TestGetNodeDevices_ClientNodeSeesPoolLupineNodeDoesNot(t *testing.T) {
	stubFleet(t, []corev1.Node{
		lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)}),
	}, nil, nil)
	dev := InitRemoteGPUDevice(testConfig())

	client := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "cpu-1"}}
	devices, err := dev.GetNodeDevices(client)
	assert.NilError(t, err)
	assert.Equal(t, len(devices), 1)

	// Zero devices, not an error: an error would stop Scheduler.register from
	// pruning whatever this node had cached before it became a lupine server.
	devices, err = dev.GetNodeDevices(lupineNode("gpu-a", "10.0.0.5", "", nil))
	assert.NilError(t, err)
	assert.Equal(t, len(devices), 0)
}

// Losing the last lupine server must read as zero devices too, so every client
// node's cached pool entry is pruned instead of lingering with GPUs that are no
// longer reachable.
func TestGetNodeDevices_EmptyFleetReportsNoDevices(t *testing.T) {
	stubFleet(t, nil, nil, nil)
	dev := InitRemoteGPUDevice(testConfig())

	devices, err := dev.GetNodeDevices(corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "cpu-1"}})
	assert.NilError(t, err)
	assert.Equal(t, len(devices), 0)
}

func TestGetNodeDevices_DisabledWhenUnconfigured(t *testing.T) {
	stubFleet(t, []corev1.Node{
		lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)}),
	}, nil, nil)
	dev := InitRemoteGPUDevice(RemoteGPUConfig{})
	t.Cleanup(func() { InitRemoteGPUDevice(testConfig()) })

	_, err := dev.GetNodeDevices(corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "cpu-1"}})
	assert.Assert(t, errors.Is(err, errNoPool))
}

// The scheduler's own records only cover pods it placed. A card can also be
// busy with a client it did not place, an interactive session or one pointed
// at the server by hand, and only the server can see that.
func TestPool_HonoursUsageReportedByTheServer(t *testing.T) {
	nodes := []corev1.Node{
		lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000), gpu("GPU-2", 40000)}),
	}
	stubFleet(t, nodes, nil, nil)
	stubServerUsage(t, map[string][]string{"10.0.0.5:14833": {"GPU-2"}})

	p := newPool(DefaultLupinePort)
	p.snapshot(context.Background())

	assert.Assert(t, !p.reserved("gpu-a/GPU-1"), "a card nobody is on stays available")
	assert.Assert(t, p.reserved("gpu-a/GPU-2"), "a card the server reports a client on is taken")
}

// A monitoring endpoint going quiet says nothing about the cards behind it, so
// it must not hand them out; the local records still stand on their own.
func TestPool_UnreachableServerDoesNotFreeItsCards(t *testing.T) {
	held := device.EncodePodSingleDevice(device.PodSingleDevice{
		device.ContainerDevices{{UUID: "gpu-a/GPU-1", Type: RemoteGPUCommonWord, Usedmem: 40000, Usedcores: 100}},
	})
	pods := []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{
		Name: "live", Annotations: map[string]string{AllocatedAnnos: held},
	}}}
	stubFleet(t, []corev1.Node{
		lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000), gpu("GPU-2", 40000)}),
	}, pods, nil)
	// fetchBusyDevices keeps failing, the default from stubFleet.

	p := newPool(DefaultLupinePort)
	p.snapshot(context.Background())

	assert.Assert(t, p.reserved("gpu-a/GPU-1"), "the pod's own booking survives")
	assert.Assert(t, !p.reserved("gpu-a/GPU-2"), "an unreachable server does not make everything busy")
}

func TestParseBusyDevices(t *testing.T) {
	body := `# HELP lupine_host_gpu_memory_total_bytes Total GPU memory in bytes.
# TYPE lupine_host_gpu_memory_total_bytes gauge
lupine_host_gpu_memory_total_bytes{device_uuid="GPU-1",device_index="0"} 85520809984
lupine_monitor_nvml_up 1
# TYPE lupine_client_device_memory_used_bytes gauge
lupine_client_device_memory_used_bytes{client_id="10.0.0.9:pod-a:4026532:7",client_address="10.0.0.9",client_hostname="pod-a",client_name="python3",device_uuid="GPU-1",device_index="0"} 83886080
lupine_client_device_utilization_percent{client_id="10.0.0.9:pod-a:4026532:7",client_address="10.0.0.9",client_hostname="pod-a",client_name="python3",device_uuid="GPU-9",device_index="1"} 12
`
	busy := parseBusyDevices(strings.NewReader(body))
	assert.Equal(t, len(busy), 1, "only the client memory metric names a busy card")
	_, ok := busy["GPU-1"]
	assert.Assert(t, ok)
}
