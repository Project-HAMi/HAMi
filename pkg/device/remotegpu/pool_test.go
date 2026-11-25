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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
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
	fetchBusyDevices = func(context.Context, string) (map[string][]string, error) {
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
	fetchBusyDevices = func(_ context.Context, endpoint string) (map[string][]string, error) {
		uuids, ok := byEndpoint[endpoint]
		if !ok {
			return nil, errNoServerMetrics
		}
		out := map[string][]string{}
		for _, u := range uuids {
			// An anonymous client, as a session or a hand-run client reports.
			out[u] = append(out[u], "10.0.0.9:host:4026531836:7")
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
			Conditions: []corev1.NodeCondition{{
				Type:   corev1.NodeReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}
	if gpus != nil {
		n.Annotations[nvidiaRegisterAnnos] = device.MarshalNodeDevices(gpus)
	}
	return n
}

func setNodeReady(n *corev1.Node, status corev1.ConditionStatus) {
	n.Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeReady, Status: status}}
}

func gpu(uuid string, mem int64) *device.DeviceInfo {
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
		// The NVIDIA plugin's split count bounds how many pods share a card.
		assert.Equal(t, d.Count, int64(10))
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

func TestPool_RefreshUpdatesServerLabels(t *testing.T) {
	nodes := []corev1.Node{
		lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-a", 40000)}),
		lupineNode("gpu-b", "10.0.0.6", "", []*device.DeviceInfo{gpu("GPU-b", 40000)}),
	}
	nodes[0].Labels["gpu-pool"] = "training"
	nodes[1].Labels["gpu-pool"] = "inference"
	stubFleet(t, nodes, nil, nil)

	p := newPool(DefaultLupinePort)
	p.snapshot(context.Background())
	selector := labels.SelectorFromSet(labels.Set{"gpu-pool": "inference"})
	assert.DeepEqual(t, p.matchingServers(selector, []string{"gpu-a", "gpu-b"}), []string{"gpu-b"})

	nodes[0].Labels["gpu-pool"] = "inference"
	nodes[1].Labels["gpu-pool"] = "training"
	p.fetchedAt = p.fetchedAt.Add(-poolTTL)
	p.snapshot(context.Background())
	assert.DeepEqual(t, p.matchingServers(selector, []string{"gpu-a", "gpu-b"}), []string{"gpu-a"})
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

func TestPool_ExcludesNotReadyAndUnknownServersFromNewAllocations(t *testing.T) {
	ready := lupineNode("gpu-ready", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)})
	notReady := lupineNode("gpu-not-ready", "10.0.0.6", "", []*device.DeviceInfo{gpu("GPU-2", 40000)})
	unknown := lupineNode("gpu-unknown", "10.0.0.7", "", []*device.DeviceInfo{gpu("GPU-3", 40000)})
	setNodeReady(&notReady, corev1.ConditionFalse)
	setNodeReady(&unknown, corev1.ConditionUnknown)
	stubFleet(t, []corev1.Node{ready, notReady, unknown}, nil, nil)

	p := newPool(DefaultLupinePort)
	devices := p.snapshot(context.Background())
	assert.Equal(t, len(devices), 1)
	assert.Equal(t, devices[0].ID, "gpu-ready/GPU-1")
	_, ok := p.endpoint("gpu-not-ready")
	assert.Assert(t, !ok, "a NotReady server must not receive new allocations")
	_, ok = p.endpoint("gpu-unknown")
	assert.Assert(t, !ok, "an Unknown server must not receive new allocations")
}

func TestPool_RecoversAReadyServerWithoutReleasingItsAllocation(t *testing.T) {
	allocation := device.EncodePodSingleDevice(device.PodSingleDevice{
		device.ContainerDevices{{UUID: "gpu-a/GPU-1", Type: RemoteGPUCommonWord, Usedmem: 40000, Usedcores: 100}},
	})
	pods := []corev1.Pod{{
		ObjectMeta: metav1.ObjectMeta{Name: "live", Annotations: map[string]string{AllocatedAnnos: allocation}},
		Spec:       corev1.PodSpec{NodeName: "cpu-1"},
	}}
	notReady := lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)})
	setNodeReady(&notReady, corev1.ConditionFalse)
	stubFleet(t, []corev1.Node{notReady}, pods, nil)

	p := newPool(DefaultLupinePort)
	assert.Equal(t, len(p.snapshot(context.Background())), 0)
	assert.Assert(t, p.reserved("gpu-a/GPU-1"), "a NotReady server must retain its live allocation")

	recovered := lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)})
	stubFleet(t, []corev1.Node{recovered}, pods, nil)
	p.fetchedAt = p.fetchedAt.Add(-poolTTL)
	assert.Equal(t, len(p.snapshot(context.Background())), 1)
	assert.Assert(t, p.reserved("gpu-a/GPU-1"), "recovery must not free a live allocation")
}

func TestPool_KeepsServerReportedUsageAcrossNotReadyRecovery(t *testing.T) {
	nodes := []corev1.Node{
		lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)}),
	}
	phase := "reported-busy"
	prevNodes, prevPods, prevBusy := listLupineNodes, listPods, fetchBusyDevices
	listLupineNodes = func(context.Context) ([]corev1.Node, error) { return nodes, nil }
	listPods = func(context.Context) ([]corev1.Pod, error) { return nil, nil }
	fetchBusyDevices = func(_ context.Context, _ string) (map[string][]string, error) {
		switch phase {
		case "reported-busy":
			return map[string][]string{"GPU-1": {"10.0.0.9:host:4026531836:7"}}, nil
		case "metrics-error":
			return nil, errNoServerMetrics
		default:
			return map[string][]string{}, nil
		}
	}
	t.Cleanup(func() { listLupineNodes, listPods, fetchBusyDevices = prevNodes, prevPods, prevBusy })

	p := newPool(DefaultLupinePort)
	p.snapshot(context.Background())
	assert.Assert(t, p.reserved("gpu-a/GPU-1"), "initial server report must reserve the GPU")

	setNodeReady(&nodes[0], corev1.ConditionFalse)
	p.fetchedAt = p.fetchedAt.Add(-poolTTL)
	assert.Equal(t, len(p.snapshot(context.Background())), 0)
	assert.Assert(t, p.reserved("gpu-a/GPU-1"), "NotReady must retain the last server-reported usage")

	setNodeReady(&nodes[0], corev1.ConditionTrue)
	phase = "metrics-error"
	p.fetchedAt = p.fetchedAt.Add(-poolTTL)
	p.snapshot(context.Background())
	assert.Assert(t, p.reserved("gpu-a/GPU-1"), "a metrics failure after recovery must retain usage")

	phase = "reported-free"
	p.fetchedAt = p.fetchedAt.Add(-poolTTL)
	p.snapshot(context.Background())
	assert.Assert(t, !p.reserved("gpu-a/GPU-1"), "a successful empty report must release stale usage")
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

// A successful reservation baseline and subsequent bookings protect known
// servers even when later Pod lists fail.
func TestPool_FailedPodListKeepsKnownFreeServer(t *testing.T) {
	nodes := []corev1.Node{
		lupineNode("gpu-free", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000), gpu("GPU-2", 40000)}),
		lupineNode("gpu-booked", "10.0.0.6", "", []*device.DeviceInfo{gpu("GPU-3", 40000), gpu("GPU-4", 40000)}),
		lupineNode("gpu-owned", "10.0.0.7", "", []*device.DeviceInfo{gpu("GPU-5", 40000)}),
	}
	owner := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{AllocatedAnnos: device.EncodePodSingleDevice(device.PodSingleDevice{{{
			UUID: "gpu-owned/GPU-5", Type: RemoteGPUCommonWord, Usedmem: 40000, Usedcores: 100,
		}}})}},
		Spec: corev1.PodSpec{NodeName: "cpu-1"},
	}
	stubFleet(t, nodes, []corev1.Pod{owner}, nil)
	dev := InitRemoteGPUDevice(testConfig())
	clientNode := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "cpu-2"}}
	_, err := dev.GetNodeDevices(clientNode)
	assert.NilError(t, err)

	// Filter books every card before Bind. This Pod binds after the last
	// successful list, but never connects to CUDA, so metrics cannot protect it.
	bound := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "new-owner"}}
	allocation := device.PodDevices{RemoteGPUCommonWord: device.PodSingleDevice{{
		{UUID: "gpu-booked/GPU-3", Type: RemoteGPUCommonWord, Usedmem: 40000, Usedcores: 100},
		{UUID: "gpu-booked/GPU-4", Type: RemoteGPUCommonWord, Usedmem: 40000, Usedcores: 100},
	}}}
	annos := map[string]string{}
	dev.PatchAnnotations(bound, &annos, allocation)
	for _, pods := range dev.pool.held {
		for pod, b := range pods {
			b.at = time.Now().Add(-heldTTL - time.Second)
			pods[pod] = b
		}
	}
	nodes = append(nodes, lupineNode("gpu-new", "10.0.0.8", "", []*device.DeviceInfo{gpu("GPU-6", 40000)}))
	listLupineNodes = func(context.Context) ([]corev1.Node, error) { return nodes, nil }
	listPods = func(context.Context) ([]corev1.Pod, error) { return nil, errors.New("pod list unavailable") }
	stubServerUsage(t, map[string][]string{
		"10.0.0.5:14833": {}, "10.0.0.6:14833": {}, "10.0.0.7:14833": {}, "10.0.0.8:14833": {},
	})
	for range 3 {
		dev.pool.fetchedAt = time.Time{}
		reported, err := dev.GetNodeDevices(clientNode)
		assert.NilError(t, err)
		for _, server := range []string{"gpu-free", "gpu-booked", "gpu-owned", "gpu-new"} {
			var usage []*device.DeviceUsage
			for _, d := range reported {
				if serverOf(d.ID) == server {
					// Exercise stale healthy usage as well as the current pool.
					usage = append(usage, card(server, strings.TrimPrefix(d.ID, server+"/"), d.Devmem))
				}
			}
			for range 2 {
				fit, allocated, reason := dev.Fit(usage, request(1, 1000), &corev1.Pod{}, &device.NodeInfo{ID: "cpu-2"}, nil)
				assert.Check(t, fit == (server == "gpu-free"), "server %s: fit=%v, reason=%s", server, fit, reason)
				if fit {
					assert.Equal(t, len(allocated[RemoteGPUCommonWord]), 1, "the free server hands out the card asked for")
				}
			}
		}
		assert.Check(t, dev.pool.reserved("gpu-owned/GPU-5"), "the last successful Pod list still protects its owner")
		assert.Check(t, dev.pool.reserved("gpu-booked/GPU-3"), "a post-snapshot booking must not expire on a failed list")
		assert.Check(t, dev.pool.reserved("gpu-booked/GPU-4"), "the booking protects the entire server across client nodes")
	}

	// A real successful list is allowed to release old reservations and bookings.
	listPods = func(context.Context) ([]corev1.Pod, error) { return nil, nil }
	dev.pool.fetchedAt = time.Time{}
	reported, err := dev.GetNodeDevices(clientNode)
	assert.NilError(t, err)
	for _, d := range reported {
		assert.Check(t, d.Health, "recovery restores %s", d.ID)
		assert.Check(t, !dev.pool.reserved(d.ID), "recovery releases %s", d.ID)
	}
}

// A partial fleet update cannot inherit an unchanged server's ownership proof.
func TestPool_FailedPodListRequiresUnchangedServer(t *testing.T) {
	for _, change := range []string{"added", "removed", "replaced", "unhealthy", "NotReady", "endpoint"} {
		t.Run(change, func(t *testing.T) {
			original := lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000), gpu("GPU-2", 40000)})
			nodes := []corev1.Node{original}
			stubFleet(t, nodes, nil, nil)
			dev := InitRemoteGPUDevice(testConfig())
			dev.pool.snapshot(context.Background())
			cached := []*device.DeviceUsage{card("gpu-a", "GPU-1", 40000), card("gpu-a", "GPU-2", 40000)}
			cards := []*device.DeviceInfo{gpu("GPU-1", 40000), gpu("GPU-2", 40000)}
			switch change {
			case "added":
				cards = append(cards, gpu("GPU-3", 40000))
			case "removed":
				cards = cards[:1]
			case "replaced":
				cards[1] = gpu("GPU-3", 40000)
			case "unhealthy":
				cards[1].Health = false
			}
			nodes[0] = lupineNode("gpu-a", "10.0.0.5", "", cards)
			if change == "NotReady" {
				setNodeReady(&nodes[0], corev1.ConditionFalse)
			}
			if change == "endpoint" {
				nodes[0].Status.Addresses[0].Address = "10.0.0.6"
			}
			listPods = func(context.Context) ([]corev1.Pod, error) { return nil, errors.New("pod list unavailable") }
			for range 2 {
				dev.pool.fetchedAt = time.Time{}
				reported := dev.pool.snapshot(context.Background())
				for _, d := range reported {
					assert.Check(t, !d.Health, "a changed server's card %s must remain unavailable", d.ID)
				}
				fit, _, _ := dev.Fit(cached, request(1, 0), &corev1.Pod{}, nil, nil)
				assert.Check(t, !fit, "stale healthy usage must not bypass the current pool")
				// Reappearance of the old hardware or readiness is not a new
				// reservation baseline, even when metrics respond successfully.
				nodes[0] = original
			}
			listPods = func(context.Context) ([]corev1.Pod, error) { return nil, nil }
			dev.pool.fetchedAt = time.Time{}
			dev.pool.snapshot(context.Background())
			fit, _, reason := dev.Fit(cached, request(1, 0), &corev1.Pod{}, nil, nil)
			assert.Check(t, fit, "a successful Pod list restores the unchanged healthy server: %s", reason)
		})
	}
}

func TestPool_EmptyNodeListDoesNotEstablishReservationBaseline(t *testing.T) {
	stubFleet(t, nil, nil, nil)
	dev := InitRemoteGPUDevice(testConfig())
	dev.pool.snapshot(context.Background())
	assert.Check(t, !dev.pool.podsListed, "the Pod list was skipped")
	stubFleet(t, []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)})}, nil, nil)
	listPods = func(context.Context) ([]corev1.Pod, error) { return nil, errors.New("pod list unavailable") }
	stubServerUsage(t, map[string][]string{"10.0.0.5:14833": {}})
	dev.pool.fetchedAt = time.Time{}
	reported := dev.pool.snapshot(context.Background())
	assert.Equal(t, len(reported), 1)
	assert.Check(t, !reported[0].Health, "successful Node and metrics reads do not establish reservation ownership")
}

func TestPool_FailedPodListBlocksAllocation(t *testing.T) {
	for _, warm := range []bool{false, true} {
		name := "cold start"
		if warm {
			name = "new server after a successful refresh"
		}
		t.Run(name, func(t *testing.T) {
			known := lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)})
			newServer := lupineNode("gpu-b", "10.0.0.6", "", []*device.DeviceInfo{gpu("GPU-2", 80000)})
			stubFleet(t, []corev1.Node{known}, nil, nil)
			// A bound Pod can still be starting its containers, with no CUDA
			// connection for the server's metrics endpoint to report yet.
			stubServerUsage(t, map[string][]string{"10.0.0.5:14833": {}, "10.0.0.6:14833": {}})
			dev := InitRemoteGPUDevice(testConfig())
			clientNode := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "cpu-2"}}
			if warm {
				_, err := dev.GetNodeDevices(clientNode)
				assert.NilError(t, err)
			}
			listLupineNodes = func(context.Context) ([]corev1.Node, error) {
				return []corev1.Node{known, newServer}, nil
			}
			allocation := device.EncodePodSingleDevice(device.PodSingleDevice{{{
				UUID: "gpu-b/GPU-2", Type: RemoteGPUCommonWord, Usedmem: 80000, Usedcores: 100,
			}}})
			owners := []corev1.Pod{{
				ObjectMeta: metav1.ObjectMeta{Name: "already-bound", Namespace: "workloads", Annotations: map[string]string{AllocatedAnnos: allocation}},
				Spec:       corev1.PodSpec{NodeName: "cpu-1"},
				Status:     corev1.PodStatus{Phase: corev1.PodPending},
			}}
			podListCalls := 0
			podListErr := errors.New("pod list unavailable")
			listPods = func(context.Context) ([]corev1.Pod, error) {
				podListCalls++
				if podListErr != nil {
					return nil, podListErr
				}
				return owners, nil
			}
			dev.pool.fetchedAt = time.Time{}

			fitReported := func(mem int64) bool {
				t.Helper()
				reported, err := dev.GetNodeDevices(clientNode)
				assert.NilError(t, err)
				var devices []*device.DeviceUsage
				for _, d := range reported {
					devices = append(devices, &device.DeviceUsage{
						ID: d.ID, Index: d.Index, Type: d.Type, Count: d.Count,
						Totalmem: d.Devmem, Totalcore: d.Devcore, Health: d.Health,
					})
				}
				fit, _, _ := dev.Fit(devices, request(1, mem), &corev1.Pod{}, nil, nil)
				return fit
			}

			assert.Check(t, !fitReported(50000), "a failed Pod list must not expose a new server whose reservations are unknown")
			assert.Check(t, !fitReported(50000), "the incomplete snapshot must not become eligible on a cached read")
			assert.Equal(t, podListCalls, 1, "a failed refresh must still respect poolTTL")
			assert.Equal(t, fitReported(1000), warm, "only the server covered by the successful Pod list remains usable")
			setNodeReady(&known, corev1.ConditionFalse)
			dev.pool.fetchedAt = time.Time{}
			reported, err := dev.GetNodeDevices(clientNode)
			assert.NilError(t, err)
			for _, d := range reported {
				assert.Check(t, d.ID != "gpu-a/GPU-1", "a failed Pod list must not preserve a server that became NotReady")
			}
			setNodeReady(&known, corev1.ConditionTrue)

			podListErr = nil
			dev.pool.fetchedAt = time.Time{}
			assert.Check(t, !fitReported(50000), "recovery must restore the existing reservation on the other client node")
			assert.Check(t, fitReported(1000), "a successful refresh must restore healthy, unreserved servers")

			owners = nil
			dev.pool.fetchedAt = time.Time{}
			assert.Check(t, fitReported(50000), "a complete refresh after the owner leaves must make the server available")
		})
	}
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
	p.held["gpu-a/GPU-1"]["uid-p"] = booking{at: time.Now().Add(-heldTTL - time.Second), share: share{users: 1, mem: -1}}
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
	assert.Equal(t, annos[LupineServerAnno], "10.0.0.5:14834", "the pod's own server listens above the node's port")
	assert.Assert(t, dev.pool.reserved("gpu-a/GPU-1"), "allocating the card books it right away")

	// The pod list still does not show the pod, so the booking has to survive.
	dev.pool.fetchedAt = dev.pool.fetchedAt.Add(-poolTTL)
	dev.pool.snapshot(context.Background())
	assert.Assert(t, dev.pool.reserved("gpu-a/GPU-1"), "booking outlives a refresh that has not seen the pod")

	// A pod that never appears must not hold the card forever.
	dev.pool.held["gpu-a/GPU-1"][""] = booking{at: time.Now().Add(-heldTTL - time.Second), share: share{users: 1, mem: 40000}}
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
	assert.DeepEqual(t, busy["GPU-1"], []string{"10.0.0.9:pod-a:4026532:7"})
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
	fetchBusyDevices = func(context.Context, string) (map[string][]string, error) {
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
	fetchBusyDevices = func(context.Context, string) (map[string][]string, error) {
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

// A pod Fit refused must be requeued once the fleet changes, because
// kube-scheduler does not requeue it on that change by itself.
func TestPool_NudgesRefusedPodWhenFleetChanges(t *testing.T) {
	nodes := []corev1.Node{}
	stubFleet(t, nodes, nil, nil)
	nudged := make(chan []waiter, 1)
	prev := nudgePods
	nudgePods = func(pods []waiter, _ uint64) []waiter { nudged <- pods; return nil }
	t.Cleanup(func() { nudgePods = prev })

	dev := InitRemoteGPUDevice(testConfig())
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "p", UID: "uid-1"}}
	dev.WaitForFleet(pod, dev.FleetRevision())

	// Nothing changed: the refused pod stays untouched.
	dev.pool.snapshot(context.Background())
	select {
	case <-nudged:
		t.Fatal("pod nudged without a fleet change")
	default:
	}

	nodes = append(nodes, lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)}))
	stubFleet(t, nodes, nil, nil)
	dev.pool.mu.Lock()
	dev.pool.fetchedAt = time.Time{}
	dev.pool.mu.Unlock()
	dev.pool.snapshot(context.Background())
	select {
	case got := <-nudged:
		assert.Equal(t, len(got), 1)
		assert.Equal(t, got[0].namespace+"/"+got[0].name, "ns/p")
	case <-time.After(time.Second):
		t.Fatal("refused pod was not nudged after the fleet changed")
	}
}

// A nudge that could not reach the pod must not forget it.
func TestPool_KeepsPodsTheNudgeFailedFor(t *testing.T) {
	p := newPool(DefaultLupinePort)
	p.wait(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "p", UID: "uid-1"}}, 0)
	p.wait(nil, 0)
	p.wait(&corev1.Pod{}, 0)
	p.mu.Lock()
	taken := p.takeWaiting()
	p.mu.Unlock()
	assert.Equal(t, len(taken), 1)
	assert.Equal(t, len(p.waiting), 0)

	p.requeue(taken)
	assert.Equal(t, len(p.waiting), 1)
}

// A pod refused long ago, such as one deleted while it waited, is dropped on
// the next refresh even when the fleet never changes.
func TestPool_PrunesStaleWaitersOnRefresh(t *testing.T) {
	stubFleet(t, nil, nil, nil)
	p := newPool(DefaultLupinePort)
	p.waiting["old"] = waiter{uid: "old", at: time.Now().Add(-2 * waitingTTL)}
	p.waiting["new"] = waiter{uid: "new", at: time.Now()}
	p.refresh(context.Background())
	_, oldKept := p.waiting["old"]
	_, newKept := p.waiting["new"]
	assert.Assert(t, !oldKept && newKept)
}

func TestApiNudgePods(t *testing.T) {
	prev := client.KubeClient
	t.Cleanup(func() { client.KubeClient = prev })

	client.KubeClient = nil
	w := waiter{uid: "uid-1", namespace: "ns", name: "p"}
	assert.Equal(t, len(apiNudgePods([]waiter{w}, 3)), 1, "no client: keep the pod")

	live := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "p"}}
	cs := fake.NewClientset(live)
	client.KubeClient = cs
	gone := waiter{uid: "uid-2", namespace: "ns", name: "gone"}
	assert.Equal(t, len(apiNudgePods([]waiter{w, gone}, 3)), 0, "patched or already gone")
	got, err := client.KubeClient.CoreV1().Pods("ns").Get(context.Background(), "p", metav1.GetOptions{})
	assert.NilError(t, err)
	assert.Equal(t, got.Annotations[RetryAnno], "3")

	cs.PrependReactor("patch", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("apiserver down")
	})
	assert.Equal(t, len(apiNudgePods([]waiter{w}, 4)), 1, "failed patch: keep the pod")
}

// A pod refused on a fleet that changed before it registered missed that
// change's nudge, so it is nudged at once instead of waiting.
func TestPool_NudgesPodRefusedOnStaleFleet(t *testing.T) {
	nudged := make(chan []waiter, 1)
	prev := nudgePods
	nudgePods = func(pods []waiter, _ uint64) []waiter { nudged <- pods; return nil }
	t.Cleanup(func() { nudgePods = prev })

	p := newPool(DefaultLupinePort)
	p.fleetRev = 2
	p.wait(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "p", UID: "uid-1"}}, 1)
	select {
	case got := <-nudged:
		assert.Equal(t, len(got), 1)
	case <-time.After(time.Second):
		t.Fatal("pod refused on a stale fleet was not nudged")
	}
	assert.Equal(t, len(p.waiting), 0)
}

// A pod's bookings add up per container, and a new booking for the same pod
// replaces the old one rather than piling onto it.
func TestPool_BookingsAddUpPerPod(t *testing.T) {
	p := newPool(DefaultLupinePort)
	p.book("uid-p", "", device.ContainerDevices{{UUID: "gpu-a/GPU-1", Usedmem: 1000}, {UUID: "gpu-a/GPU-1", Usedmem: 500}})
	p.book("uid-q", "", device.ContainerDevices{{UUID: "gpu-a/GPU-1", Usedmem: 2000}})

	assert.Equal(t, p.usage("gpu-a/GPU-1", ""), cardUsage{users: 3, mem: 3500})
	assert.Equal(t, p.usage("gpu-a/GPU-1", "uid-p"), cardUsage{users: 1, mem: 2000})

	p.book("uid-p", "", device.ContainerDevices{{UUID: "gpu-a/GPU-1", Usedmem: 100}})
	assert.Equal(t, p.usage("gpu-a/GPU-1", ""), cardUsage{users: 2, mem: 2100})

	p.release("uid-p")
	assert.Equal(t, p.usage("gpu-a/GPU-1", ""), cardUsage{users: 1, mem: 2000})
}

// Pods sharing a card each count for their own memory.
func TestPool_ReservationsAddUpSharedCards(t *testing.T) {
	shared := func(uid string, mem ...int64) corev1.Pod {
		ctrs := device.PodSingleDevice{}
		for _, m := range mem {
			ctrs = append(ctrs, device.ContainerDevices{{UUID: "gpu-a/GPU-1", Type: RemoteGPUCommonWord, Usedmem: m}})
		}
		return corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: uid, UID: types.UID(uid), Annotations: map[string]string{AllocatedAnnos: device.EncodePodSingleDevice(ctrs)}},
			Spec:       corev1.PodSpec{NodeName: "cpu-1"},
		}
	}
	stubFleet(t, []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)})},
		[]corev1.Pod{shared("uid-a", 1000, 2000), shared("uid-b", 4000)}, nil)
	p := newPool(DefaultLupinePort)
	p.snapshot(context.Background())
	assert.Equal(t, p.usage("gpu-a/GPU-1", ""), cardUsage{users: 3, mem: 7000})
	assert.Equal(t, p.usage("gpu-a/GPU-1", "uid-a"), cardUsage{users: 1, mem: 4000})
}

// The server reports every client on a card, the pods HAMi put there among
// them. Only a client no allocation accounts for keeps others off the card.
func TestPool_ClientsTheServerReportsAreMatchedToHolders(t *testing.T) {
	stubFleet(t, []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000), gpu("GPU-2", 40000)})},
		[]corev1.Pod{{
			ObjectMeta: metav1.ObjectMeta{Name: "a", UID: "uid-a", Annotations: map[string]string{AllocatedAnnos: device.EncodePodSingleDevice(device.PodSingleDevice{
				device.ContainerDevices{{UUID: "gpu-a/GPU-1", Type: RemoteGPUCommonWord, Usedmem: 1000}},
			})}},
			Spec: corev1.PodSpec{NodeName: "cpu-1"},
		}}, nil)
	report := map[string][]string{}
	prev := fetchBusyDevices
	fetchBusyDevices = func(context.Context, string) (map[string][]string, error) { return report, nil }
	t.Cleanup(func() { fetchBusyDevices = prev })
	p := newPool(DefaultLupinePort)
	read := func() {
		p.fetchedAt = time.Time{}
		p.snapshot(context.Background())
	}

	report = map[string][]string{"GPU-1": {"uid-a/app"}}
	read()
	assert.Assert(t, !p.usage("gpu-a/GPU-1", "").foreign, "the holder itself")

	report = map[string][]string{"GPU-1": {"uid-a/app", "uid-gone/app"}}
	read()
	assert.Assert(t, p.usage("gpu-a/GPU-1", "").foreign, "a workload that holds nothing here")

	// A released server names a client by address and hostname, and a pod's
	// hostname is its name. Another hostname on a held card is foreign, as is
	// any client on a card nobody holds.
	report = map[string][]string{"GPU-1": {"10.0.0.9:a:4026531836:7"}, "GPU-2": {"10.0.0.9:a:4026531836:8"}}
	read()
	assert.Assert(t, !p.usage("gpu-a/GPU-1", "").foreign, "the holder's own client")
	assert.Assert(t, p.usage("gpu-a/GPU-2", "").foreign, "the holder's name on a card it does not hold")

	report = map[string][]string{"GPU-1": {"10.0.0.9:a:4026531836:7", "10.0.0.9:by-hand:4026531836:9"}}
	read()
	assert.Assert(t, p.usage("gpu-a/GPU-1", "").foreign, "a client run by hand next to the holder")

	report = map[string][]string{"GPU-1": {"fd00::9:a:4026531836:7"}}
	read()
	assert.Assert(t, !p.usage("gpu-a/GPU-1", "").foreign, "an IPv6 address")
}

// A pod booked but not yet listed is matched by its hostname too, the way
// kubelet sets it.
func TestPool_BookedPodsClientIsMatchedByHostname(t *testing.T) {
	stubFleet(t, []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)})}, nil, nil)
	stubServerUsage(t, map[string][]string{"10.0.0.5:14833": {"GPU-1"}})
	p := newPool(DefaultLupinePort)
	p.snapshot(context.Background())
	p.mu.Lock()
	p.busy = map[string][]string{"gpu-a/GPU-1": {"10.0.0.9:worker:4026531836:7"}}
	p.mu.Unlock()
	assert.Assert(t, p.usage("gpu-a/GPU-1", "").foreign)
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "w", UID: "uid-w"}, Spec: corev1.PodSpec{Hostname: "worker"}}
	p.book(podKey(pod), podHostname(pod), device.ContainerDevices{{UUID: "gpu-a/GPU-1", Usedmem: 1000}})
	assert.Assert(t, !p.usage("gpu-a/GPU-1", "").foreign)

	long := strings.Repeat("a", 62) + "-b"
	assert.Equal(t, podHostname(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: long}}), strings.Repeat("a", 62))
	assert.Equal(t, podHostname(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "n"}, Spec: corev1.PodSpec{HostNetwork: true}}), "")
}

// A pod holds each card at its peak for as long as it lives. An ordinary init
// container runs alone, beside the sidecars before it, but kubelet runs it
// again when it recreates the sandbox, so its share stays held after it has
// succeeded.
func TestPool_PodHoldsEachCardAtItsPeakForItsLifetime(t *testing.T) {
	always := corev1.ContainerRestartPolicyAlways
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", UID: "uid-p", Annotations: map[string]string{AllocatedAnnos: device.EncodePodSingleDevice(device.PodSingleDevice{
			{{UUID: "gpu-a/GPU-2", Type: RemoteGPUCommonWord, Usedmem: 1000}},
			{{UUID: "gpu-a/GPU-1", Type: RemoteGPUCommonWord, Usedmem: 30000}},
			{{UUID: "gpu-a/GPU-1", Type: RemoteGPUCommonWord, Usedmem: 5000}, {UUID: "gpu-a/GPU-2", Type: RemoteGPUCommonWord, Usedmem: 2000}},
		})}},
		Spec: corev1.PodSpec{
			NodeName:       "cpu-1",
			InitContainers: []corev1.Container{{Name: "side", RestartPolicy: &always}, {Name: "init"}},
			Containers:     []corev1.Container{{Name: "app"}},
		},
	}
	nodes := []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000), gpu("GPU-2", 40000)})}
	for _, done := range []bool{false, true} {
		if done {
			pod.Status.InitContainerStatuses = []corev1.ContainerStatus{{Name: "init", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{}}}}
		}
		stubFleet(t, nodes, []corev1.Pod{pod}, nil)
		p := newPool(DefaultLupinePort)
		p.snapshot(context.Background())
		// The init container's 30000 is more than the app's 5000.
		u := p.usage("gpu-a/GPU-1", "")
		assert.Equal(t, u.mem, int64(30000), "init done=%v", done)
		assert.Equal(t, u.users, 1)
		// The sidecar and the app hold GPU-2 together.
		u = p.usage("gpu-a/GPU-2", "")
		assert.Equal(t, u.mem, int64(3000), "init done=%v", done)
		assert.Equal(t, u.users, 2)
	}
}

// With setHostnameAsFQDN, kubelet sets the pod's FQDN as its hostname, and
// the cluster domain at its end is the kubelet's, not known here.
func TestPool_HolderWithAnFQDNHostnameIsMatched(t *testing.T) {
	yes := true
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "ns", UID: "uid-p"},
		Spec:       corev1.PodSpec{Hostname: "worker", Subdomain: "svc", SetHostnameAsFQDN: &yes},
	}
	holders := map[string]share{"uid-p": {users: 1, host: podHostname(pod)}}
	assert.Assert(t, accountedFor("10.0.0.9:worker.svc.ns.svc.cluster.local:4026531836:7", holders))
	assert.Assert(t, accountedFor("10.0.0.9:worker.svc.ns.svc.example.org:4026531836:7", holders))
	assert.Assert(t, !accountedFor("10.0.0.9:worker:4026531836:7", holders))
	assert.Assert(t, !accountedFor("10.0.0.9:worker.svc.other.svc.cluster.local:4026531836:7", holders))

	// A pod name may contain dots, and hostnameOverride wins over the rest.
	dotted := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "worker.v1", Namespace: "ns"}, Spec: corev1.PodSpec{Subdomain: "svc", SetHostnameAsFQDN: &yes}}
	assert.Assert(t, accountedFor("10.0.0.9:worker.v1.svc.ns.svc.cluster.local:4026531836:7", map[string]share{"uid-d": {users: 1, host: podHostname(dotted)}}))
	name := "gpu-client"
	pod.Spec.HostnameOverride = &name
	holders = map[string]share{"uid-p": {users: 1, host: podHostname(pod)}}
	assert.Assert(t, accountedFor("10.0.0.9:gpu-client:4026531836:7", holders))
	assert.Assert(t, !accountedFor("10.0.0.9:worker.svc.ns.svc.cluster.local:4026531836:7", holders))
}

// Each pod's server gets a port of its own above the node's one: not one a
// placed client or a running server holds, nor one booked for another pod.
func TestPool_AssignsEachServerAFreePort(t *testing.T) {
	endpoint := func(name, ep, node string, phase corev1.PodPhase) corev1.Pod {
		return corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(name), Annotations: map[string]string{LupineServerAnno: ep}},
			Spec:       corev1.PodSpec{NodeName: node},
			Status:     corev1.PodStatus{Phase: phase},
		}
	}
	stubFleet(t, []corev1.Node{
		lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)}),
		lupineNode("gpu-b", "10.0.0.6", "24000", []*device.DeviceInfo{gpu("GPU-2", 40000)}),
	}, []corev1.Pod{
		endpoint("client", "10.0.0.5:14834", "cpu-1", corev1.PodRunning),
		endpoint("server", "10.0.0.5:14835", "gpu-a", corev1.PodRunning),
		endpoint("finished", "10.0.0.5:14836", "cpu-1", corev1.PodSucceeded),
		endpoint("unbound", "10.0.0.5:14837", "", corev1.PodPending),
	}, nil)
	p := newPool(DefaultLupinePort)
	p.snapshot(context.Background())

	assign := func(server, pod string) string {
		ep, ok := p.assignEndpoint(server, pod)
		assert.Assert(t, ok)
		return ep
	}
	assert.Equal(t, assign("gpu-a", "x"), "10.0.0.5:14836", "a finished pod gives its port back")
	assert.Equal(t, assign("gpu-a", "y"), "10.0.0.5:14837", "an unplaced pod holds none")
	assert.Equal(t, assign("gpu-a", "x"), "10.0.0.5:14836", "a pod's new booking replaces its last")
	p.release("x")
	assert.Equal(t, assign("gpu-a", "z"), "10.0.0.5:14836", "a released booking frees its port")
	assert.Equal(t, assign("gpu-b", "x"), "10.0.0.6:24001", "the node's label moves the range")
	_, ok := p.assignEndpoint("gpu-c", "x")
	assert.Assert(t, !ok, "an unknown server has no endpoint")
}

// A booking lasts until the pod list shows the port held, or heldTTL passes.
func TestPool_EndpointBookingLastsUntilThePodListHasIt(t *testing.T) {
	pods := []corev1.Pod{}
	stubFleet(t, []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)})}, nil, nil)
	listPods = func(context.Context) ([]corev1.Pod, error) { return pods, nil }
	p := newPool(DefaultLupinePort)
	p.snapshot(context.Background())
	ep, _ := p.assignEndpoint("gpu-a", "x")

	p.refreshNow(context.Background())
	assert.Equal(t, len(p.heldEndpoints), 1, "the pod list has not caught up")
	pods = []corev1.Pod{{
		ObjectMeta: metav1.ObjectMeta{Name: "x", Annotations: map[string]string{LupineServerAnno: ep}},
		Spec:       corev1.PodSpec{NodeName: "cpu-1"},
	}}
	p.forcedAt = time.Time{}
	p.refreshNow(context.Background())
	assert.Equal(t, len(p.heldEndpoints), 0)
	other, _ := p.assignEndpoint("gpu-a", "y")
	assert.Assert(t, other != ep, "the listed pod holds it")
}
