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
	"sync"
	"testing"
	"testing/iotest"
	"time"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
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
	return &device.DeviceInfo{ID: uuid, Count: 10, Devmem: mem, Devcore: 100, Type: "NVIDIA", Health: true, Mode: nvidia.RemoteMode}
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
		{
			ObjectMeta: metav1.ObjectMeta{Name: "live", Annotations: map[string]string{AllocatedAnnos: held}},
			Spec:       corev1.PodSpec{NodeName: "cpu-1"},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "done", Annotations: map[string]string{AllocatedAnnos: device.EncodePodSingleDevice(device.PodSingleDevice{
				device.ContainerDevices{{UUID: "gpu-a/GPU-2", Type: RemoteGPUCommonWord}},
			})}},
			Spec:   corev1.PodSpec{NodeName: "cpu-1"},
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

// Filter writes the allocation annotation before Bind, and nothing clears it
// when Bind fails. Counting an unplaced pod would leave it reserving its own
// cards against its next attempt, which on a one-server fleet never ends.
func TestPool_UnplacedPodDoesNotReserveAgainstItself(t *testing.T) {
	held := device.EncodePodSingleDevice(device.PodSingleDevice{
		device.ContainerDevices{{UUID: "gpu-a/GPU-1", Type: RemoteGPUCommonWord, Usedmem: 40000, Usedcores: 100}},
	})
	stubFleet(t, []corev1.Node{
		lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)}),
	}, []corev1.Pod{{
		ObjectMeta: metav1.ObjectMeta{Name: "never-bound", Annotations: map[string]string{AllocatedAnnos: held}},
		Status:     corev1.PodStatus{Phase: corev1.PodPending},
	}}, nil)

	p := newPool(DefaultLupinePort)
	p.snapshot(context.Background())
	assert.Assert(t, !p.reserved("gpu-a/GPU-1"), "a pod that never landed holds nothing")
}

// A scheduling attempt that falls through gives its cards back at once rather
// than leaving a whole server booked for heldTTL.
func TestPool_ReleaseGivesBackAPodsBooking(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", UID: "uid-p"}}
	other := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "q", UID: "uid-q"}}

	dev.pool.hold(pod.UID, "gpu-a/GPU-1")
	dev.pool.hold(other.UID, "gpu-a/GPU-2")
	assert.Assert(t, dev.pool.reserved("gpu-a/GPU-1"))

	assert.NilError(t, dev.ReleaseNodeLock(nil, pod))
	assert.Assert(t, !dev.pool.reserved("gpu-a/GPU-1"), "the failed attempt's card is free again")
	assert.Assert(t, dev.pool.reserved("gpu-a/GPU-2"), "and nobody else's booking moved")
}

// Every client node is handed the same pool, so re-registering it for each of
// them on every tick is a deep copy per node for a fleet that has not moved.
func TestCheckHealth_OnlyAsksForAnUpdateWhenTheFleetMoves(t *testing.T) {
	nodes := []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)})}
	stubFleet(t, nodes, nil, nil)
	dev := InitRemoteGPUDevice(testConfig())
	client := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "cpu-1"}}

	_, update := dev.CheckHealth(RemoteGPUCommonWord, client)
	assert.Assert(t, update, "a node that has never taken a copy needs one")
	dev.pool.snapshot(context.Background())
	_, update = dev.CheckHealth(RemoteGPUCommonWord, client)
	assert.Assert(t, update, "the first fleet it sees is a change")
	_, update = dev.CheckHealth(RemoteGPUCommonWord, client)
	assert.Assert(t, !update, "and an unchanged fleet is not")

	// A card turns up.
	stubFleet(t, []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "",
		[]*device.DeviceInfo{gpu("GPU-1", 40000), gpu("GPU-2", 40000)})}, nil, nil)
	dev.pool.fetchedAt = dev.pool.fetchedAt.Add(-poolTTL)
	dev.pool.snapshot(context.Background())
	_, update = dev.CheckHealth(RemoteGPUCommonWord, client)
	assert.Assert(t, update, "a fleet that gained a card is a change")

	assert.NilError(t, dev.NodeCleanUp("cpu-1"))
	_, update = dev.CheckHealth(RemoteGPUCommonWord, client)
	assert.Assert(t, update, "a node that came back has taken no copy")
}

// A pod list that failed has seen nothing, so ageing a booking out on its word
// would free the card of a pod that did bind and let a second pod onto it.
func TestPool_FailedPodListDoesNotExpireABooking(t *testing.T) {
	stubFleet(t, []corev1.Node{
		lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)}),
	}, nil, nil)
	p := newPool(DefaultLupinePort)
	p.snapshot(context.Background())
	p.hold("uid-p", "gpu-a/GPU-1")

	// The pod never turns up, and the list keeps failing for longer than a
	// booking would normally survive.
	prev := listPods
	listPods = func(context.Context) ([]corev1.Pod, error) { return nil, errors.New("apiserver unavailable") }
	t.Cleanup(func() { listPods = prev })
	p.mu.Lock()
	p.held["gpu-a/GPU-1"] = booking{at: time.Now().Add(-heldTTL - time.Second), pod: "uid-p"}
	p.mu.Unlock()
	p.fetchedAt = p.fetchedAt.Add(-poolTTL)
	p.snapshot(context.Background())

	assert.Assert(t, p.reserved("gpu-a/GPU-1"), "an unread pod list is no reason to free the card")

	// Once the list works again and still does not name the pod, it expires.
	listPods = prev
	p.fetchedAt = p.fetchedAt.Add(-poolTTL)
	p.snapshot(context.Background())
	assert.Assert(t, !p.reserved("gpu-a/GPU-1"), "a list that ran and saw nothing does expire it")
}

// A card going unhealthy keeps its ID and its memory, so comparing only those
// would report nothing to do while the scheduler kept allocating from the
// healthy copy it had cached.
func TestCheckHealth_NoticesACardGoingUnhealthy(t *testing.T) {
	healthy := gpu("GPU-1", 40000)
	stubFleet(t, []corev1.Node{
		lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{healthy}),
	}, nil, nil)
	dev := InitRemoteGPUDevice(testConfig())
	client := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "cpu-1"}}
	dev.pool.snapshot(context.Background())
	dev.CheckHealth(RemoteGPUCommonWord, client)
	_, update := dev.CheckHealth(RemoteGPUCommonWord, client)
	assert.Assert(t, !update, "nothing has moved yet")

	sick := gpu("GPU-1", 40000)
	sick.Health = false
	stubFleet(t, []corev1.Node{
		lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{sick}),
	}, nil, nil)
	dev.pool.fetchedAt = dev.pool.fetchedAt.Add(-poolTTL)
	dev.pool.snapshot(context.Background())

	_, update = dev.CheckHealth(RemoteGPUCommonWord, client)
	assert.Assert(t, update, "the same card, now unhealthy, is a change")
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
	dev.pool.held["gpu-a/GPU-1"] = booking{at: time.Now().Add(-heldTTL - time.Second)}
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
	pods := []corev1.Pod{{
		ObjectMeta: metav1.ObjectMeta{Name: "live", Annotations: map[string]string{AllocatedAnnos: held}},
		Spec:       corev1.PodSpec{NodeName: "cpu-1"},
	}}
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
	busy, err := parseBusyDevices(strings.NewReader(body))
	assert.NilError(t, err)
	assert.Equal(t, len(busy), 1, "only the client memory metric names a busy card")
	_, ok := busy["GPU-1"]
	assert.Assert(t, ok)
}

// A body that breaks part way through is not a shorter list of busy cards: the
// lines that never arrived are the ones this would otherwise call free.
func TestParseBusyDevicesReportsATruncatedBody(t *testing.T) {
	_, err := parseBusyDevices(iotest.TimeoutReader(strings.NewReader(
		"lupine_client_device_memory_used_bytes{device_uuid=\"GPU-1\"} 1\nlupine_client_device_memory_used_bytes{device_uuid=\"GPU-2\"} 1\n")))
	assert.ErrorContains(t, err, "timeout")
}

// A refresh whose metrics call fails must keep what the server last said.
// Forgetting it would free a card an outside client is still holding and let
// the scheduler put a pod on top of it.
func TestPool_FailedMetricsRefreshKeepsTheLastReportedUsage(t *testing.T) {
	stubFleet(t, []corev1.Node{
		lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000), gpu("GPU-2", 40000)}),
	}, nil, nil)
	stubServerUsage(t, map[string][]string{"10.0.0.5:14833": {"GPU-2"}})

	p := newPool(DefaultLupinePort)
	p.snapshot(context.Background())
	assert.Assert(t, p.reserved("gpu-a/GPU-2"), "the server reported a client on it")

	// The server goes quiet on the next round.
	prev := fetchBusyDevices
	fetchBusyDevices = func(context.Context, string) (map[string]struct{}, error) {
		return nil, errNoServerMetrics
	}
	t.Cleanup(func() { fetchBusyDevices = prev })
	p.refreshNow(context.Background())

	assert.Assert(t, p.reserved("gpu-a/GPU-2"), "a quiet server does not release the card it reported")
	assert.Assert(t, !p.reserved("gpu-a/GPU-1"), "and says nothing new about the others")
}

// The plugin publishes one registration annotation whatever mode it runs in.
// A card still in a local mode belongs to its node's kubelet, so the pool must
// leave it there rather than offer the same GPU twice.
func TestPool_TakesOnlyTheCardsServedRemotely(t *testing.T) {
	local := gpu("GPU-2", 40000)
	local.Mode = "hami-core"
	stubFleet(t, []corev1.Node{
		lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000), local}),
	}, nil, nil)

	p := newPool(DefaultLupinePort)
	devices := p.snapshot(context.Background())
	assert.Equal(t, len(devices), 1)
	assert.Equal(t, devices[0].ID, "gpu-a/GPU-1")
}

// refresh() must not hold the pool lock across the network fetch: every other
// server has to pay its own metricsTimeout, not everyone else's turn in line.
// reserved(), hold() and endpoint() are what Fit and PatchAnnotations call on
// the hot path, so a slow or unreachable server must not stall them.
func TestPool_RefreshDoesNotBlockReservedDuringAServerFetch(t *testing.T) {
	stubFleet(t, []corev1.Node{
		lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)}),
	}, nil, nil)

	release := make(chan struct{})
	entered := make(chan struct{})
	var once sync.Once
	prevBusy := fetchBusyDevices
	fetchBusyDevices = func(context.Context, string) (map[string]struct{}, error) {
		once.Do(func() { close(entered) })
		<-release
		return nil, errNoServerMetrics
	}
	t.Cleanup(func() { fetchBusyDevices = prevBusy })

	p := newPool(DefaultLupinePort)
	refreshDone := make(chan struct{})
	go func() {
		p.refresh(context.Background())
		close(refreshDone)
	}()

	// Wait until refresh() is provably inside the blocking server call before
	// checking whether reserved() can still proceed.
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		<-refreshDone
		t.Fatal("fetchBusyDevices was never called; the test setup is broken")
	}

	done := make(chan struct{})
	go func() {
		p.reserved("gpu-a/GPU-1")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		close(release)
		<-refreshDone
		t.Fatal("reserved() blocked while a lupine server fetch was still in flight")
	}
	close(release)
	<-refreshDone
}
