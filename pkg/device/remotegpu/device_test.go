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
	"runtime"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
)

func testConfig() RemoteGPUConfig {
	return RemoteGPUConfig{
		ResourceCountName:  "nvidia.com/remote-gpu",
		ResourceMemoryName: "nvidia.com/remote-gpu-memory",
		DefaultPort:        DefaultLupinePort,
		LibImage:           "projecthami/hami:test",
	}
}

// card builds a free device on the named lupine server.
func card(server, uuid string, mem int64) *device.DeviceUsage {
	return &device.DeviceUsage{
		ID:        deviceID(server, uuid),
		Type:      RemoteGPUCommonWord,
		Totalmem:  mem,
		Totalcore: 100,
		Count:     1,
		Health:    true,
	}
}

// registerFitDevices supplies the pool identity and health that accompany
// standalone Fit usage fixtures, as GetNodeDevices does in production.
func registerFitDevices(dev *RemoteGPUDevices, devices []*device.DeviceUsage) {
	dev.pool.mu.Lock()
	defer dev.pool.mu.Unlock()
	dev.pool.devices = make([]*device.DeviceInfo, 0, len(devices))
	for _, d := range devices {
		dev.pool.devices = append(dev.pool.devices, &device.DeviceInfo{
			ID: d.ID, Index: d.Index, Count: d.Count, Devmem: d.Totalmem, Devcore: d.Totalcore, Health: d.Health,
		})
		dev.pool.endpoints[serverOf(d.ID)] = "10.0.0.5:14833"
	}
}

func request(nums, mem int64) device.ContainerDeviceRequest {
	return device.ContainerDeviceRequest{
		Nums:     nums,
		Type:     RemoteGPUCommonWord,
		Memreq:   mem,
		Coresreq: 100,
	}
}

func setServerLabels(dev *RemoteGPUDevices, byServer map[string]map[string]string) {
	dev.pool.mu.Lock()
	defer dev.pool.mu.Unlock()
	dev.pool.serverLabels = make(map[string]labels.Set, len(byServer))
	for server, serverLabels := range byServer {
		dev.pool.serverLabels[server] = labels.Set(serverLabels)
	}
}

func requestingContainer(name string, nums, mem int64) corev1.Container {
	limits := corev1.ResourceList{
		"nvidia.com/remote-gpu": *resource.NewQuantity(nums, resource.DecimalSI),
	}
	if mem > 0 {
		limits["nvidia.com/remote-gpu-memory"] = *resource.NewQuantity(mem, resource.DecimalSI)
	}
	return corev1.Container{
		Name:      name,
		Resources: corev1.ResourceRequirements{Limits: limits},
	}
}

func TestFit_ConfinesAllocationToOneServer(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	// Two servers with one free card each: the fleet has two cards free, but a
	// two-card request must not be split across them.
	devices := []*device.DeviceUsage{
		card("gpu-a", "GPU-1", 40000),
		card("gpu-b", "GPU-2", 40000),
	}

	// Fit can be called without pod metadata; in that case it falls back to
	// validating the current container request.
	registerFitDevices(dev, devices)
	fit, _, reason := dev.Fit(devices, request(2, 0), nil, nil, nil)
	assert.Equal(t, fit, false)
	assert.Assert(t, reason != "", "expected a rejection reason")

	// One card is satisfiable. The servers are the same size, so the tie breaks
	// on name and the choice is repeatable.
	fit, allocated, _ := dev.Fit(devices, request(1, 0), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, true)
	assert.Equal(t, len(allocated[RemoteGPUCommonWord]), 1)
	assert.Equal(t, allocated[RemoteGPUCommonWord][0].UUID, "gpu-a/GPU-1")
}

func TestFit_ServerPoolSelectorFiltersCandidates(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	setServerLabels(dev, map[string]map[string]string{
		"gpu-a": {"gpu-pool": "training"},
		"gpu-b": {"gpu-pool": "inference"},
	})
	devices := []*device.DeviceUsage{
		card("gpu-a", "GPU-a", 40000),
		card("gpu-b", "GPU-b", 40000),
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
		LupineServerSelectorAnno: "gpu-pool=inference",
	}}}

	registerFitDevices(dev, devices)
	fit, allocated, reason := dev.Fit(devices, request(1, 0), pod, nil, nil)
	assert.Equal(t, fit, true, reason)
	assert.Equal(t, allocated[RemoteGPUCommonWord][0].UUID, "gpu-b/GPU-b")
}

func TestFit_ServerPoolSelectorRejectsNoMatchAndInvalidInput(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	setServerLabels(dev, map[string]map[string]string{"gpu-a": {"gpu-pool": "training"}})
	devices := []*device.DeviceUsage{card("gpu-a", "GPU-a", 40000)}

	tests := []struct {
		name     string
		selector string
		want     string
	}{
		{name: "no matching server", selector: "gpu-pool=inference", want: "no Lupine server matches"},
		{name: "set-based selector", selector: "gpu-pool in (inference)", want: "must be one equality selector"},
		{name: "invalid selector", selector: "=inference", want: "invalid " + LupineServerSelectorAnno},
		{name: "blank selector", selector: "   ", want: "must not be empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{LupineServerSelectorAnno: tt.selector}}}
			fit, allocated, reason := dev.Fit(devices, request(1, 0), pod, nil, nil)
			assert.Equal(t, fit, false)
			assert.Equal(t, len(allocated), 0)
			assert.Assert(t, cmp.Contains(reason, tt.want))
		})
	}
}

func TestFit_SelectorWithEmptyPoolReportsNoServer(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{LupineServerSelectorAnno: "gpu-pool=inference"}}}
	fit, _, reason := dev.Fit(nil, request(1, 0), pod, nil, nil)
	assert.Equal(t, fit, false)
	assert.Equal(t, reason, errNoPool.Error())
}

func TestFit_SelectorRejectsPreviouslyCommittedNonmatchingServer(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	setServerLabels(dev, map[string]map[string]string{"gpu-a": {"gpu-pool": "training"}, "gpu-b": {"gpu-pool": "inference"}})
	devices := []*device.DeviceUsage{card("gpu-a", "GPU-a", 40000), card("gpu-b", "GPU-b", 40000)}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{LupineServerSelectorAnno: "gpu-pool=inference"}}}
	allocated := device.PodDevices{RemoteGPUCommonWord: device.PodSingleDevice{device.ContainerDevices{{UUID: "gpu-a/GPU-a", Type: RemoteGPUCommonWord}}}}
	fit, _, reason := dev.Fit(devices, request(1, 0), pod, nil, &allocated)
	assert.Equal(t, fit, false)
	assert.Equal(t, reason, errNoMatchingLupineServer.Error())
}

func TestFit_SelectorIsRecheckedAfterForcedRefresh(t *testing.T) {
	free := card("gpu-a", "GPU-1", 40000)
	holder := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "holder", Annotations: map[string]string{
		AllocatedAnnos: device.EncodePodSingleDevice(device.PodSingleDevice{device.ContainerDevices{{UUID: free.ID, Type: RemoteGPUCommonWord}}}),
	}}, Spec: corev1.PodSpec{NodeName: "cpu-1"}}
	nodes := []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)})}
	nodes[0].Labels["gpu-pool"] = "inference"
	stubFleet(t, nodes, []corev1.Pod{holder}, nil)
	dev := InitRemoteGPUDevice(testConfig())
	dev.pool.snapshot(context.Background())
	nodes[0].Labels["gpu-pool"] = "training"
	stubFleet(t, nodes, nil, nil)
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{LupineServerSelectorAnno: "gpu-pool=inference"}}}
	fit, _, reason := dev.Fit([]*device.DeviceUsage{free}, request(1, 0), pod, nil, nil)
	assert.Equal(t, fit, false, "the refreshed server no longer matches")
	assert.Assert(t, reason != "")
}

func TestValidateReservedServer_ColdPool(t *testing.T) {
	nodes := []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)})}
	nodes[0].Labels["gpu-pool"] = "inference"
	stubFleet(t, nodes, nil, nil)
	dev := InitRemoteGPUDevice(testConfig())
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "uid-p", Annotations: map[string]string{LupineServerSelectorAnno: "gpu-pool=inference"}}}
	allocated := device.PodDevices{RemoteGPUCommonWord: device.PodSingleDevice{device.ContainerDevices{{UUID: "gpu-a/GPU-1", Type: RemoteGPUCommonWord}}}}
	dev.pool.book(podKey(pod), "", allocated[RemoteGPUCommonWord][0])
	assert.NilError(t, dev.ValidateReservedServer(pod, allocated))
}

func TestFit_MultiContainerStaysOnSelectedServer(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	devices := []*device.DeviceUsage{
		card("gpu-a", "GPU-small", 1000),
		card("gpu-b", "GPU-1", 40000),
		card("gpu-b", "GPU-2", 40000),
	}

	allocated := &device.PodDevices{}
	// The memory filter makes the first container select gpu-b even though
	// gpu-a sorts first.
	registerFitDevices(dev, devices)
	fit1, alloc1, _ := dev.Fit(devices, request(1, 2000), &corev1.Pod{}, nil, allocated)
	assert.Equal(t, fit1, true)
	assert.Equal(t, alloc1[RemoteGPUCommonWord][0].UUID, "gpu-b/GPU-1")
	assert.Equal(t, len(alloc1[RemoteGPUCommonWord]), 1, "a container takes only what it asked for")

	// Accumulate the first allocation and its usage as fitInDevices does.
	devices[1].Used = 1
	devices[1].Usedmem = 2000
	(*allocated)[RemoteGPUCommonWord] = append((*allocated)[RemoteGPUCommonWord], alloc1[RemoteGPUCommonWord])

	// gpu-a sorts first and is free, but the later container must stay on the
	// server the pod is already connected to.
	fit2, alloc2, _ := dev.Fit(devices, request(1, 0), &corev1.Pod{}, nil, allocated)
	assert.Equal(t, fit2, true)
	assert.Equal(t, len(alloc2[RemoteGPUCommonWord]), 1)
	assert.Equal(t, serverOf(alloc2[RemoteGPUCommonWord][0].UUID), "gpu-b")
}

// The first request fits either server, but only gpu-b can also serve the
// other container. Reordering the containers must not change the choice.
func TestFit_SelectsServerForEveryContainerRequest(t *testing.T) {
	orders := []struct {
		name       string
		containers []corev1.Container
	}{
		{
			name: "unconstrained first",
			containers: []corev1.Container{
				requestingContainer("small", 1, 0),
				requestingContainer("large", 1, 60000),
			},
		},
		{
			name: "memory constrained first",
			containers: []corev1.Container{
				requestingContainer("large", 1, 60000),
				requestingContainer("small", 1, 0),
			},
		},
	}

	for _, tt := range orders {
		t.Run(tt.name, func(t *testing.T) {
			dev := InitRemoteGPUDevice(testConfig())
			devices := []*device.DeviceUsage{
				card("gpu-a", "GPU-1", 40000),
				card("gpu-b", "GPU-2", 80000),
			}
			pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: tt.containers}}
			first, err := dev.GenerateResourceRequests(&pod.Spec.Containers[0])
			assert.NilError(t, err)

			registerFitDevices(dev, devices)
			fit, allocated, reason := dev.Fit(devices, first, pod, nil, nil)
			if !fit {
				t.Fatalf("Fit rejected a pod with a valid single-server placement: %s", reason)
			}
			assert.Equal(t, allocated[RemoteGPUCommonWord][0].UUID, "gpu-b/GPU-2")
		})
	}
}

// Combining the largest count and memory into one synthetic request would
// require two 60000 MB cards and reject this valid per-container placement.
func TestFit_ChecksCardCountAndMemoryPerContainer(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	devices := []*device.DeviceUsage{
		shareCard("gpu-a", "GPU-large", 80000, 10),
		shareCard("gpu-a", "GPU-small", 40000, 10),
	}
	pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{
		requestingContainer("memory", 1, 60000),
		requestingContainer("count", 2, 10000),
	}}}
	first, err := dev.GenerateResourceRequests(&pod.Spec.Containers[0])
	assert.NilError(t, err)

	registerFitDevices(dev, devices)
	fit, allocated, reason := dev.Fit(devices, first, pod, nil, nil)
	if !fit {
		t.Fatalf("Fit rejected independently satisfiable requests: %s", reason)
	}
	assert.Equal(t, len(allocated[RemoteGPUCommonWord]), 1)
	assert.Equal(t, allocated[RemoteGPUCommonWord][0].UUID, "gpu-a/GPU-large")
}

func TestFit_FailureReasonsCountCardsAndServers(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	devices := []*device.DeviceUsage{
		card("gpu-a", "GPU-a1", 40000),
		card("gpu-a", "GPU-a2", 40000),
		card("gpu-b", "GPU-b1", 40000),
	}

	registerFitDevices(dev, devices)
	fit, _, reason := dev.Fit(devices, request(2, 60000), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, false)
	reasons := common.ParseReason(reason)
	assert.Equal(t, reasons[common.CardInsufficientMemory], 3, "count undersized cards, not servers")
	assert.Equal(t, reasons[common.NodeInsufficientDevice], 2, "retain one insufficient-device reason per server")
}

func TestFit_FailureReasonsCountTakenAndSmallCards(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	occupied := card("gpu-a", "GPU-a1", 80000)
	occupied.Used = 1
	devices := []*device.DeviceUsage{
		occupied,
		card("gpu-a", "GPU-a2", 40000),
		card("gpu-b", "GPU-b1", 40000),
	}

	registerFitDevices(dev, devices)
	fit, _, reason := dev.Fit(devices, request(2, 60000), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, false)
	reasons := common.ParseReason(reason)
	assert.Equal(t, reasons[common.CardInsufficientMemory], 2)
	assert.Equal(t, reasons[common.CardTimeSlicingExhausted], 1, "the occupied card has no share left")
	assert.Equal(t, reasons[common.NodeInsufficientDevice], 2)

	fit, _, reason = dev.Fit(devices, request(2, 0), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, false)
	assert.Equal(t, common.ParseReason(reason)[common.ExclusiveDeviceAllocateConflict], 1, "a whole card cannot be taken from under a holder")
}

func TestFit_MultiContainerRejectsOtherServerFallback(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	devices := []*device.DeviceUsage{
		card("gpu-a", "GPU-1", 40000),
		card("gpu-b", "GPU-2", 80000),
	}

	allocated := &device.PodDevices{}
	registerFitDevices(dev, devices)
	fit1, alloc1, _ := dev.Fit(devices, request(1, 0), &corev1.Pod{}, nil, allocated)
	assert.Equal(t, fit1, true)
	assert.Equal(t, alloc1[RemoteGPUCommonWord][0].UUID, "gpu-a/GPU-1")

	devices[0].Used = 1
	(*allocated)[RemoteGPUCommonWord] = append((*allocated)[RemoteGPUCommonWord], alloc1[RemoteGPUCommonWord])

	// gpu-b can meet the second container's memory request, but the committed
	// gpu-a server cannot. Fit must reject rather than changing endpoints.
	fit2, alloc2, reason := dev.Fit(devices, request(1, 60000), &corev1.Pod{}, nil, allocated)
	assert.Equal(t, fit2, false, "multi-container pod must not span multiple lupine servers")
	assert.Equal(t, len(alloc2[RemoteGPUCommonWord]), 0)
	assert.Assert(t, reason != "")
}

func TestFit_MemoryRequestTakesItsShare(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	devices := []*device.DeviceUsage{card("gpu-a", "GPU-1", 40000)}

	registerFitDevices(dev, devices)
	fit, allocated, _ := dev.Fit(devices, request(1, 2000), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, true)
	got := allocated[RemoteGPUCommonWord][0]
	assert.Equal(t, got.Usedmem, int64(2000))
	assert.Equal(t, got.Usedcores, int64(0))

	// A count alone takes the card whole.
	fit, allocated, _ = dev.Fit(devices, request(1, 0), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, true)
	got = allocated[RemoteGPUCommonWord][0]
	assert.Equal(t, got.Usedmem, int64(40000))
	assert.Equal(t, got.Usedcores, int64(100))

	// Without HAMi-core to enforce it the memory request only filters, and
	// the card is taken whole.
	RemoteGPULibImage = ""
	t.Cleanup(func() { InitRemoteGPUDevice(testConfig()) })
	fit, allocated, _ = dev.Fit(devices, request(1, 2000), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, true)
	assert.Equal(t, allocated[RemoteGPUCommonWord][0].Usedmem, int64(40000))
}

func TestFit_RejectsUnfitCards(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())

	unhealthy := card("gpu-a", "GPU-1", 40000)
	unhealthy.Health = false
	busy := card("gpu-a", "GPU-2", 40000)
	busy.Used = 1
	small := card("gpu-a", "GPU-3", 1000)

	registerFitDevices(dev, []*device.DeviceUsage{unhealthy, busy, small})
	fit, _, reason := dev.Fit([]*device.DeviceUsage{unhealthy, busy, small}, request(1, 2000), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, false)
	assert.Assert(t, reason != "")
}

// A card booked by a pod that landed on a different client node is invisible to
// this node's usage view, so Fit must consult the cluster-wide reservation set.
func TestFit_HonorsClusterWideReservation(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	free := card("gpu-a", "GPU-1", 40000)

	registerFitDevices(dev, []*device.DeviceUsage{free})
	fit, _, _ := dev.Fit([]*device.DeviceUsage{free}, request(1, 0), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, true)

	dev.pool.mu.Lock()
	dev.pool.inUse[free.ID] = map[string]share{"other": {users: 1, mem: 40000}}
	dev.pool.mu.Unlock()

	fit, _, reason := dev.Fit([]*device.DeviceUsage{free}, request(1, 0), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, false)
	assert.Assert(t, reason != "")
}

// A card freed moments ago still reads as taken until the pool's snapshot ages
// out, and rejecting on that stale read is expensive: kube-scheduler parks the
// pod in its unschedulable queue until the next periodic flush. Measured on a
// real cluster before this re-read existed, the successor pod waited 5.5
// minutes for a card that was free the whole time.
func TestFit_RereadsBeforeRejectingOverAStaleBooking(t *testing.T) {
	free := card("gpu-a", "GPU-1", 40000)
	holder := corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "holder",
		Annotations: map[string]string{AllocatedAnnos: device.EncodePodSingleDevice(device.PodSingleDevice{
			device.ContainerDevices{{UUID: free.ID, Type: RemoteGPUCommonWord, Usedmem: 40000, Usedcores: 100}},
		})},
	}, Spec: corev1.PodSpec{NodeName: "cpu-1"}}
	nodes := []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)})}

	stubFleet(t, nodes, []corev1.Pod{holder}, nil)
	dev := InitRemoteGPUDevice(testConfig())
	dev.pool.snapshot(context.Background())

	fit, _, reason := dev.Fit([]*device.DeviceUsage{free}, request(1, 0), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, false, "the holder still has the card")
	assert.Assert(t, reason != "")

	// The holder goes away. poolTTL has not lapsed, so nothing but a forced
	// re-read can notice.
	stubFleet(t, nodes, nil, nil)
	dev.pool.forcedAt = dev.pool.forcedAt.Add(-forceInterval - time.Second)

	fit, allocated, _ := dev.Fit([]*device.DeviceUsage{free}, request(1, 0), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, true, "a freed card is handed out without waiting for poolTTL")
	assert.Equal(t, allocated[RemoteGPUCommonWord][0].UUID, free.ID)
}

func TestFit_RejectsMissingDeviceAfterRefresh(t *testing.T) {
	for _, forced := range []bool{false, true} {
		name := "normal refresh before initial fit"
		if forced {
			name = "forced refresh followed by cached fit"
		}
		t.Run(name, func(t *testing.T) {
			nodes := []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-old", 40000)})}
			stubFleet(t, nodes, nil, nil)
			busy := []string{}
			if forced {
				busy = append(busy, "GPU-old")
			}
			stubServerUsage(t, map[string][]string{"10.0.0.5:14833": busy})
			dev := InitRemoteGPUDevice(testConfig())
			clientNode := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "cpu-2"}}
			reported, err := dev.GetNodeDevices(clientNode)
			assert.NilError(t, err)
			assert.Equal(t, len(reported), 1)
			assert.Assert(t, reported[0].Health)
			cached := []*device.DeviceUsage{card("gpu-a", "GPU-old", 40000)}

			// Registration changes while another client node still has the old
			// usage. The endpoint survives, but ownership of the new card cannot
			// be reconstructed and its metrics currently report no connection.
			nodes[0] = lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-new", 40000)})
			podListCalls := 0
			listPods = func(context.Context) ([]corev1.Pod, error) {
				podListCalls++
				return nil, errors.New("pod list unavailable")
			}
			stubServerUsage(t, map[string][]string{"10.0.0.5:14833": {}})
			if !forced {
				dev.pool.fetchedAt = time.Time{}
				_, err = dev.GetNodeDevices(clientNode)
				assert.NilError(t, err)
			}

			fit, allocated, _ := dev.Fit(cached, request(1, 0), &corev1.Pod{}, nil, nil)
			assert.Check(t, !fit, "a stale card missing from the refreshed pool must not fit")
			assert.Check(t, len(allocated[RemoteGPUCommonWord]) == 0)
			assert.Equal(t, podListCalls, 1, "both paths must exercise the failed reservation refresh")
			_, known := dev.pool.deviceHealth()[cached[0].ID]
			assert.Assert(t, !known, "the cached card must be absent from the current pool")
			_, endpointExists := dev.pool.endpoint("gpu-a")
			assert.Assert(t, endpointExists, "an endpoint alone must not establish device identity")

			fit, allocated, _ = dev.Fit(cached, request(1, 0), &corev1.Pod{}, nil, nil)
			assert.Check(t, !fit, "a later cached call must not bypass the forced retry's identity check")
			assert.Check(t, len(allocated[RemoteGPUCommonWord]) == 0)
			assert.Equal(t, podListCalls, 1)
		})
	}
}

func TestFit_RejectsChangedServerFromCachedUsage(t *testing.T) {
	for _, change := range []string{"removed", "replaced"} {
		t.Run(change, func(t *testing.T) {
			nodes := []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000), gpu("GPU-2", 40000)})}
			stubFleet(t, nodes, nil, nil)
			dev := InitRemoteGPUDevice(testConfig())
			dev.pool.snapshot(context.Background())
			cached := []*device.DeviceUsage{card("gpu-a", "GPU-1", 40000), card("gpu-a", "GPU-2", 40000)}
			cards := []*device.DeviceInfo{gpu("GPU-1", 40000)}
			switch change {
			case "replaced":
				cards = append(cards, gpu("GPU-3", 40000))
			}
			nodes[0] = lupineNode("gpu-a", "10.0.0.5", "", cards)
			dev.pool.fetchedAt = time.Time{}
			dev.pool.snapshot(context.Background())
			for range 2 {
				fit, allocated, reason := dev.Fit(cached, request(1, 0), &corev1.Pod{}, nil, nil)
				assert.Check(t, !fit, "cached usage must describe the whole current server: %s", reason)
				assert.Equal(t, len(allocated[RemoteGPUCommonWord]), 0)
			}
		})
	}
}

func TestFit_FailedReservationRefreshBlocksStaleHealthyUsage(t *testing.T) {
	nodes := []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)})}
	stubFleet(t, nodes, nil, nil)
	stubServerUsage(t, map[string][]string{"10.0.0.5:14833": {"GPU-1"}})
	dev := InitRemoteGPUDevice(testConfig())
	clientNode := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "cpu-2"}}
	reported, err := dev.GetNodeDevices(clientNode)
	assert.NilError(t, err)
	assert.Equal(t, len(reported), 1)
	assert.Assert(t, reported[0].Health)
	usage := []*device.DeviceUsage{{
		ID: reported[0].ID, Index: reported[0].Index, Type: reported[0].Type,
		Count: reported[0].Count, Totalmem: reported[0].Devmem,
		Totalcore: reported[0].Devcore, Health: reported[0].Health,
	}}
	assert.Assert(t, dev.pool.reserved(usage[0].ID), "the previous server report must trigger the forced refresh")

	// This bound owner is absent from the pool's older Pod snapshot. Its
	// annotation must protect the server even before a CUDA client connects.
	owners := []corev1.Pod{{
		ObjectMeta: metav1.ObjectMeta{Name: "holder", Namespace: "workloads", UID: "holder-uid", Annotations: map[string]string{
			AllocatedAnnos: device.EncodePodSingleDevice(device.PodSingleDevice{{{
				UUID: usage[0].ID, Type: RemoteGPUCommonWord, Usedmem: 40000, Usedcores: 100,
			}}}),
		}},
		Spec: corev1.PodSpec{NodeName: "cpu-1"},
	}}
	podListCalls, metricsCalls := 0, 0
	podListErr := errors.New("pod list unavailable")
	listPods = func(context.Context) ([]corev1.Pod, error) {
		podListCalls++
		if podListErr != nil {
			return nil, podListErr
		}
		return owners, nil
	}
	fetchBusyDevices = func(context.Context, string) (map[string][]string, error) {
		metricsCalls++
		return nil, nil
	}

	fit, allocated, reason := dev.Fit(usage, request(1, 0), &corev1.Pod{}, nil, nil)
	assert.Check(t, !fit, "forced retry must reject stale healthy usage when reservation ownership is unknown")
	assert.Check(t, len(allocated[RemoteGPUCommonWord]) == 0, "the occupied server must not be allocated")
	assert.Check(t, cmp.Contains(reason, common.CardNotHealth))
	assert.Equal(t, podListCalls, 1, "Fit must have forced a reservation refresh")
	assert.Equal(t, metricsCalls, 1, "the forced refresh must have cleared the old busy report")

	// Another candidate node can still carry the old healthy scheduler view.
	fit, _, _ = dev.Fit(usage, request(1, 0), &corev1.Pod{}, nil, nil)
	assert.Check(t, !fit, "cached usage must also be blocked without another forced refresh")
	assert.Equal(t, podListCalls, 1)
	assert.Assert(t, usage[0].Health, "fitting must not rewrite the caller's cached usage")

	podListErr = nil
	dev.pool.fetchedAt = time.Time{}
	_, err = dev.GetNodeDevices(clientNode)
	assert.NilError(t, err)
	assert.Assert(t, dev.pool.reserved(usage[0].ID), "recovery must restore the bound owner's reservation")
	fit, _, _ = dev.Fit(usage, request(1, 0), &corev1.Pod{}, nil, nil)
	assert.Check(t, !fit, "a successful Pod list must not release an existing owner")

	owners = nil
	dev.pool.fetchedAt = time.Time{}
	_, err = dev.GetNodeDevices(clientNode)
	assert.NilError(t, err)
	fit, allocated, reason = dev.Fit(usage, request(1, 0), &corev1.Pod{}, nil, nil)
	assert.Assert(t, fit, reason)
	assert.Equal(t, len(allocated[RemoteGPUCommonWord]), 1)
	assert.Equal(t, allocated[RemoteGPUCommonWord][0].UUID, usage[0].ID)
}

func TestFit_ConcurrentRefreshKeepsHealthAndReservationsConsistent(t *testing.T) {
	stubFleet(t, []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)})}, nil, nil)
	busy := true
	listPods = func(context.Context) ([]corev1.Pod, error) {
		if busy {
			return nil, nil
		}
		return nil, errors.New("pod list unavailable")
	}
	fetchBusyDevices = func(context.Context, string) (map[string][]string, error) {
		if busy {
			return map[string][]string{"GPU-1": {"10.0.0.9:host:4026531836:42"}}, nil
		}
		return nil, nil
	}
	dev := InitRemoteGPUDevice(testConfig())
	dev.pool.snapshot(context.Background())
	usage := map[string][]*device.DeviceUsage{"gpu-a": {card("gpu-a", "GPU-1", 40000)}}

	// Every installed snapshot forbids allocation: either the server reports
	// a client, or a failed Pod list makes its newly idle cards uncertain.
	// A reader mixing healthy cards from the first snapshot with cleared
	// reservations from the second can incorrectly allocate this server.
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			busy = !busy
			dev.pool.mu.Lock()
			dev.pool.fetchedAt = time.Time{}
			dev.pool.mu.Unlock()
			dev.pool.refresh(context.Background())
			runtime.Gosched()
		}
	}()
	unexpectedFits := 0
	for range 10000 {
		req := request(1, 0)
		fit, _, _, _ := dev.tryFit(usage, []string{"gpu-a"}, req, []phasedRequest{{request: req}}, &corev1.Pod{}, nil)
		if fit {
			unexpectedFits++
		}
		runtime.Gosched()
	}
	close(stop)
	<-done // Wait before stub cleanup or reading the writer's state.
	assert.Equal(t, unexpectedFits, 0, "Fit must use health and reservations from one pool snapshot")
}

func TestFit_FailedPodListLeavesCardsBesideABusyUnregisteredOneFree(t *testing.T) {
	stubFleet(t, []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)})}, nil, nil)
	dev := InitRemoteGPUDevice(testConfig())
	dev.pool.snapshot(context.Background())
	listPods = func(context.Context) ([]corev1.Pod, error) { return nil, errors.New("pod list unavailable") }
	// The server can report a client on a card that registration does not
	// currently advertise. That client cannot see the cards it was not given,
	// so the others stay free.
	stubServerUsage(t, map[string][]string{"10.0.0.5:14833": {"GPU-unregistered"}})
	dev.pool.fetchedAt = time.Time{}
	dev.pool.snapshot(context.Background())
	assert.Assert(t, dev.pool.deviceHealth()["gpu-a/GPU-1"], "the known free card retains its health")
	dev.pool.forcedAt = time.Now() // Also exercise cached Fit without another read.
	for range 2 {
		fit, allocated, reason := dev.Fit([]*device.DeviceUsage{card("gpu-a", "GPU-1", 40000)}, request(1, 0), &corev1.Pod{}, nil, nil)
		assert.Check(t, fit, "a busy card outside usage leaves the registered card free: %s", reason)
		assert.Equal(t, len(allocated[RemoteGPUCommonWord]), 1)
	}
	listPods = func(context.Context) ([]corev1.Pod, error) { return nil, nil }
	stubServerUsage(t, map[string][]string{"10.0.0.5:14833": {}})
	dev.pool.fetchedAt = time.Time{}
	dev.pool.snapshot(context.Background())
	fit, _, reason := dev.Fit([]*device.DeviceUsage{card("gpu-a", "GPU-1", 40000)}, request(1, 0), &corev1.Pod{}, nil, nil)
	assert.Check(t, fit, "successful recovery releases the server: %s", reason)
}

// The forced re-read clears the booking that caused the refusal, but it can
// also take the server away. Retrying with the pre-refresh view would then
// read the card as merely unbooked and bind a pod to a server with no endpoint
// left to name.
func TestFit_DoesNotHandOutAServerTheForcedRefreshRemoved(t *testing.T) {
	free := card("gpu-a", "GPU-1", 40000)
	holder := corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "holder",
		Annotations: map[string]string{AllocatedAnnos: device.EncodePodSingleDevice(device.PodSingleDevice{
			device.ContainerDevices{{UUID: free.ID, Type: RemoteGPUCommonWord, Usedmem: 40000, Usedcores: 100}},
		})},
	}, Spec: corev1.PodSpec{NodeName: "cpu-1"}}
	nodes := []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)})}

	stubFleet(t, nodes, []corev1.Pod{holder}, nil)
	dev := InitRemoteGPUDevice(testConfig())
	dev.pool.snapshot(context.Background())

	// The holder goes away and so does the server serving the card.
	stubFleet(t, nil, nil, nil)
	dev.pool.forcedAt = dev.pool.forcedAt.Add(-forceInterval - time.Second)

	fit, _, reason := dev.Fit([]*device.DeviceUsage{free}, request(1, 0), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, false, "a card whose server has gone is not free, it is absent")
	assert.Assert(t, reason != "")
	_, ok := dev.pool.endpoint("gpu-a")
	assert.Assert(t, !ok, "and there is no endpoint left to point a client at")

	fit, _, _ = dev.Fit([]*device.DeviceUsage{free}, request(1, 0), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, false, "a cached read must also reject a card absent from an empty pool")
}

// The webhook hands out &pod.Spec.InitContainers[i], so anything appended to
// that slice while the pointer is live moves the array out from under it. An
// init container that asks for a card must still come back wired up.
func TestMutateAdmission_WiresAnInitContainerThatAsksForACard(t *testing.T) {
	RemoteGPULibImage = "hami:latest"
	t.Cleanup(func() { RemoteGPULibImage = "" })
	dev := InitRemoteGPUDevice(testConfig())
	pod := &corev1.Pod{Spec: corev1.PodSpec{InitContainers: []corev1.Container{{
		Name: "warmup",
		Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
			corev1.ResourceName(RemoteGPUResourceCount):  resource.MustParse("1"),
			corev1.ResourceName(RemoteGPUResourceMemory): resource.MustParse("2000"),
		}},
	}}}}

	found, err := dev.MutateAdmission(&pod.Spec.InitContainers[0], pod)
	assert.NilError(t, err)
	assert.Equal(t, found, true)

	warmup := pod.Spec.InitContainers[0]
	assert.Equal(t, warmup.Name, "warmup", "the delivery container is appended after it")
	assert.Equal(t, envValue(&warmup, memoryLimitEnv), "2000m")
	assert.Equal(t, envValue(&warmup, ldPreloadEnv), libMountPath+"/libvgpu.so")
	assert.Equal(t, len(warmup.VolumeMounts), 1, "the library still gets mounted")
	var server bool
	for _, env := range warmup.Env {
		if env.Name == lupineServerEnv {
			server = env.ValueFrom != nil
		}
	}
	assert.Assert(t, server, "LUPINE_SERVER must survive the append too")
	assert.Equal(t, len(pod.Spec.InitContainers), 2, "and the delivery container is there")
}

// A workload that sets its own LD_PRELOAD keeps it; HAMi-core goes in front.
func TestMutateAdmission_KeepsAnExistingLdPreload(t *testing.T) {
	RemoteGPULibImage = "hami:latest"
	t.Cleanup(func() { RemoteGPULibImage = "" })
	dev := InitRemoteGPUDevice(testConfig())
	ctr := &corev1.Container{
		Env: []corev1.EnvVar{{Name: ldPreloadEnv, Value: "/opt/theirs.so"}},
		Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
			corev1.ResourceName(RemoteGPUResourceCount):  resource.MustParse("1"),
			corev1.ResourceName(RemoteGPUResourceMemory): resource.MustParse("2000"),
		}},
	}

	_, err := dev.MutateAdmission(ctr, &corev1.Pod{})
	assert.NilError(t, err)
	assert.Equal(t, envValue(ctr, ldPreloadEnv), libMountPath+"/libvgpu.so /opt/theirs.so")
}

// An env var carries a literal or a source, never both, so a sourced
// LD_PRELOAD cannot be composed with HAMi-core's. Saying so beats replacing
// the EnvVar and quietly losing the preload the workload asked for.
func TestMutateAdmission_RefusesASourcedLdPreload(t *testing.T) {
	RemoteGPULibImage = "hami:latest"
	t.Cleanup(func() { RemoteGPULibImage = "" })
	dev := InitRemoteGPUDevice(testConfig())
	ctr := &corev1.Container{
		Name: "app",
		Env: []corev1.EnvVar{{Name: ldPreloadEnv, ValueFrom: &corev1.EnvVarSource{
			ConfigMapKeyRef: &corev1.ConfigMapKeySelector{Key: "preload"},
		}}},
		Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
			corev1.ResourceName(RemoteGPUResourceCount):  resource.MustParse("1"),
			corev1.ResourceName(RemoteGPUResourceMemory): resource.MustParse("2000"),
		}},
	}
	pod := &corev1.Pod{}

	_, err := dev.MutateAdmission(ctr, pod)
	assert.ErrorContains(t, err, ldPreloadEnv)
	assert.Assert(t, envOf(ctr, ldPreloadEnv).ValueFrom != nil, "the workload's source is left alone")
	assert.Equal(t, len(ctr.VolumeMounts), 0, "and nothing was half-applied")
	assert.Equal(t, len(pod.Spec.InitContainers), 0)
}

// Cards are handed out one by one, so a card the forced refresh adds to a
// server takes nothing from the cards the candidate described.
func TestFit_HandsOutACardOfAServerThatGainedOne(t *testing.T) {
	free := card("gpu-a", "GPU-1", 40000)
	holder := corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "holder",
		Annotations: map[string]string{AllocatedAnnos: device.EncodePodSingleDevice(device.PodSingleDevice{
			device.ContainerDevices{{UUID: free.ID, Type: RemoteGPUCommonWord, Usedmem: 40000, Usedcores: 100}},
		})},
	}, Spec: corev1.PodSpec{NodeName: "cpu-1"}}
	oneCard := []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)})}
	stubFleet(t, oneCard, []corev1.Pod{holder}, nil)
	dev := InitRemoteGPUDevice(testConfig())
	dev.pool.snapshot(context.Background())

	// The holder goes away and a second card turns up on the same server.
	stubFleet(t, []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "",
		[]*device.DeviceInfo{gpu("GPU-1", 40000), gpu("GPU-2", 40000)})}, nil, nil)
	dev.pool.forcedAt = dev.pool.forcedAt.Add(-forceInterval - time.Second)

	fit, allocated, reason := dev.Fit([]*device.DeviceUsage{free}, request(1, 0), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, true, reason)
	assert.Equal(t, allocated[RemoteGPUCommonWord][0].UUID, free.ID)
}

func TestFit_SkipsDevicesWithoutServerPrefix(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	orphan := &device.DeviceUsage{ID: "GPU-no-server", Totalmem: 40000, Totalcore: 100, Health: true}

	fit, _, _ := dev.Fit([]*device.DeviceUsage{orphan}, request(1, 0), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, false)
}

func TestMutateAdmission_ReplacesLiteralServerEnv(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	ctr := &corev1.Container{
		Env: []corev1.EnvVar{{Name: lupineServerEnv, Value: "placeholder:14833"}},
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceName(RemoteGPUResourceCount): resource.MustParse("2"),
			},
		},
	}

	found, err := dev.MutateAdmission(ctr, &corev1.Pod{})
	assert.NilError(t, err)
	assert.Equal(t, found, true)

	var server, workload int
	for _, env := range ctr.Env {
		switch env.Name {
		case lupineServerEnv:
			server++
			assert.Assert(t, env.Value == "", "literal value must be dropped")
			assert.Assert(t, env.ValueFrom != nil && env.ValueFrom.FieldRef != nil)
			assert.Equal(t, env.ValueFrom.FieldRef.FieldPath, "metadata.annotations['"+LupineServerAnno+"']")
		case lupineWorkloadIDEnv:
			workload++
			assert.Equal(t, env.Value, "$(HAMI_REMOTE_POD_UID)/"+ctr.Name)
			assert.Assert(t, env.ValueFrom == nil)
		}
	}
	assert.Equal(t, server, 1, "must replace, not duplicate")
	assert.Equal(t, workload, 1)
}

func TestMutateAdmission_LeavesSelectorValidationToWebhook(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	ctr := &corev1.Container{Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
		corev1.ResourceName(RemoteGPUResourceCount): resource.MustParse("1"),
	}}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
		LupineServerSelectorAnno: "gpu-pool in (inference)",
	}}}

	found, err := dev.MutateAdmission(ctr, pod)
	assert.NilError(t, err)
	assert.Equal(t, found, true)
}

func TestValidateReservedServer(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	setServerLabels(dev, map[string]map[string]string{
		"gpu-a": {"gpu-pool": "inference"},
	})
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "uid-p", Annotations: map[string]string{
		LupineServerSelectorAnno: "gpu-pool=inference",
	}}}
	allocated := device.PodDevices{RemoteGPUCommonWord: device.PodSingleDevice{
		device.ContainerDevices{{UUID: deviceID("gpu-a", "GPU-1"), Type: RemoteGPUCommonWord}},
	}}
	dev.pool.book(podKey(pod), "", allocated[RemoteGPUCommonWord][0])

	assert.NilError(t, dev.ValidateReservedServer(pod, allocated))

	pod.Annotations[LupineServerSelectorAnno] = "gpu-pool=training"
	assert.ErrorContains(t, dev.ValidateReservedServer(pod, allocated), "reserved Lupine server \"gpu-a\"")
}

func TestMutateAdmission_IgnoresContainerWithoutRequest(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	ctr := &corev1.Container{}

	found, err := dev.MutateAdmission(ctr, &corev1.Pod{})
	assert.NilError(t, err)
	assert.Equal(t, found, false)
	assert.Equal(t, len(ctr.Env), 0)
}

func TestPodRequestsRemoteGPURequiresPositiveCount(t *testing.T) {
	tests := []struct {
		name string
		pod  *corev1.Pod
		want bool
	}{
		{
			name: "zero count app container",
			pod:  &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{requestingContainer("app", 0, 0)}}},
		},
		{
			name: "zero count init container",
			pod:  &corev1.Pod{Spec: corev1.PodSpec{InitContainers: []corev1.Container{requestingContainer("init", 0, 0)}}},
		},
		{
			name: "positive app container",
			pod:  &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{requestingContainer("app", 1, 0)}}},
			want: true,
		},
		{
			name: "positive init container",
			pod:  &corev1.Pod{Spec: corev1.PodSpec{InitContainers: []corev1.Container{requestingContainer("init", 1, 0)}}},
			want: true,
		},
	}

	InitRemoteGPUDevice(testConfig())
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, PodRequestsRemoteGPU(tt.pod), tt.want)
		})
	}
}

func TestGenerateResourceRequests(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	ctr := &corev1.Container{
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceName(RemoteGPUResourceCount):  resource.MustParse("2"),
				corev1.ResourceName(RemoteGPUResourceMemory): resource.MustParse("2000"),
			},
		},
	}

	req, err := dev.GenerateResourceRequests(ctr)
	assert.NilError(t, err)
	assert.Equal(t, req.Nums, int64(2))
	assert.Equal(t, req.Memreq, int64(2000))
	assert.Equal(t, req.Coresreq, int64(100))
	assert.Equal(t, req.Type, RemoteGPUCommonWord)

	empty, err := dev.GenerateResourceRequests(&corev1.Container{})
	assert.NilError(t, err)
	assert.Equal(t, empty.Nums, int64(0))
}

func TestPatchAnnotations_WritesResolvedEndpoint(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	dev.pool.mu.Lock()
	dev.pool.endpoints["gpu-a"] = "10.0.0.5:14833"
	dev.pool.mu.Unlock()

	pd := device.PodDevices{
		RemoteGPUCommonWord: device.PodSingleDevice{
			device.ContainerDevices{{
				UUID: deviceID("gpu-a", "GPU-1"), Type: RemoteGPUCommonWord, Usedmem: 40000, Usedcores: 100,
			}},
		},
	}
	annos := map[string]string{}

	got := dev.PatchAnnotations(&corev1.Pod{}, &annos, pd)
	assert.Equal(t, got[LupineServerAnno], "10.0.0.5:14833")
	assert.Assert(t, got[InRequestAnnos] != "")
	assert.Equal(t, got[AllocatedAnnos], got[InRequestAnnos])
}

func TestPatchAnnotations_NoAllocation(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	annos := map[string]string{}

	got := dev.PatchAnnotations(&corev1.Pod{}, &annos, device.PodDevices{})
	assert.Equal(t, len(got), 0)
}

// The shared handshake would evict the pool: no device plugin runs on a client
// node to answer it.
func TestCheckHealth_AlwaysHealthy(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	health, needUpdate := dev.CheckHealth(RemoteGPUDevice, &corev1.Node{})
	assert.Equal(t, health, true)
	assert.Equal(t, needUpdate, true)
}

// The memory request only means something once HAMi-core is in the container to
// hold the workload to it, and on a GPU-less node no device plugin is there to
// put it in. Verified on a real cluster: without this a pod asking for 2000MB
// allocated 8GB unhindered.
func TestMutateAdmission_ArmsTheMemoryLimit(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	ctr := &corev1.Container{
		Name: "app",
		Env:  []corev1.EnvVar{{Name: memoryLimitEnv + "_1", Value: "99999m"}},
		Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
			"nvidia.com/remote-gpu":        resource.MustParse("2"),
			"nvidia.com/remote-gpu-memory": resource.MustParse("2000"),
		}},
	}
	pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{*ctr}}}

	found, err := dev.MutateAdmission(ctr, pod)
	assert.NilError(t, err)
	assert.Equal(t, found, true)

	env := map[string]string{}
	for _, e := range ctr.Env {
		env[e.Name] = e.Value
	}
	assert.Equal(t, env[memoryLimitEnv], "2000m")
	assert.Equal(t, env[memoryLimitEnv+"_0"], "2000m", "a per-device limit is set too, so the workload cannot set its own")
	assert.Equal(t, env[memoryLimitEnv+"_1"], "2000m", "one the workload set is replaced")
	assert.Equal(t, env[ldPreloadEnv], libMountPath+"/libvgpu.so")
	assert.Equal(t, env[sharedCacheEnv], libMountPath+"/app.cache", "each container keeps its own limit")

	assert.Equal(t, len(pod.Spec.InitContainers), 1)
	assert.Equal(t, pod.Spec.InitContainers[0].Image, "projecthami/hami:test")
	assert.Equal(t, len(pod.Spec.Volumes), 1)
	assert.Equal(t, len(ctr.VolumeMounts), 1)

	// A second container asking for a remote GPU shares the one copy.
	other := &corev1.Container{Name: "sidecar", Resources: ctr.Resources}
	_, err = dev.MutateAdmission(other, pod)
	assert.NilError(t, err)
	assert.Equal(t, len(pod.Spec.InitContainers), 1)
	assert.Equal(t, len(pod.Spec.Volumes), 1)
	assert.Equal(t, len(other.VolumeMounts), 1)
}

// Without a memory request there is no limit to enforce, and without an image
// there is nothing to enforce it with. Neither should drag HAMi-core in.
func TestMutateAdmission_SkipsEnforcementWhenNotAsked(t *testing.T) {
	countOnly := corev1.ResourceRequirements{Limits: corev1.ResourceList{
		"nvidia.com/remote-gpu": resource.MustParse("1"),
	}}
	withMem := corev1.ResourceRequirements{Limits: corev1.ResourceList{
		"nvidia.com/remote-gpu":        resource.MustParse("1"),
		"nvidia.com/remote-gpu-memory": resource.MustParse("2000"),
	}}

	dev := InitRemoteGPUDevice(testConfig())
	ctr := &corev1.Container{Name: "app", Resources: countOnly}
	pod := &corev1.Pod{}
	_, err := dev.MutateAdmission(ctr, pod)
	assert.NilError(t, err)
	assert.Equal(t, len(pod.Spec.InitContainers), 0)

	cfg := testConfig()
	cfg.LibImage = ""
	dev = InitRemoteGPUDevice(cfg)
	t.Cleanup(func() { InitRemoteGPUDevice(testConfig()) })
	ctr = &corev1.Container{Name: "app", Resources: withMem}
	pod = &corev1.Pod{}
	_, err = dev.MutateAdmission(ctr, pod)
	assert.NilError(t, err)
	assert.Equal(t, len(pod.Spec.InitContainers), 0)
}

// CUDA_VISIBLE_DEVICES limits the pod to its own cards, so the rest of the
// server stays free for other pods.
func TestFit_TakesOnlyTheCardsAskedFor(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	devices := []*device.DeviceUsage{
		card("gpu-a", "GPU-A1", 40000),
		card("gpu-a", "GPU-A2", 40000),
		card("gpu-a", "GPU-A3", 40000),
	}

	registerFitDevices(dev, devices)
	fit, allocated, _ := dev.Fit(devices, request(1, 0), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, true)
	assert.Equal(t, len(allocated[RemoteGPUCommonWord]), 1)
}

// The server with the fewest usable cards that can serve the request leaves
// the deeper servers for requests that need them.
func TestFit_PrefersTheSmallestSufficientServer(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	// gpu-a sorts first and is the larger of the two.
	devices := []*device.DeviceUsage{
		card("gpu-a", "GPU-A1", 40000),
		card("gpu-a", "GPU-A2", 40000),
		card("gpu-a", "GPU-A3", 40000),
		card("gpu-b", "GPU-B1", 40000),
	}

	registerFitDevices(dev, devices)
	fit, allocated, _ := dev.Fit(devices, request(1, 0), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, true)
	assert.Equal(t, serverOf(allocated[RemoteGPUCommonWord][0].UUID), "gpu-b")

	// Two cards no longer fit on the small server, so the large one is used.
	fit, allocated, _ = dev.Fit(devices, request(2, 0), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, true)
	assert.Equal(t, serverOf(allocated[RemoteGPUCommonWord][0].UUID), "gpu-a")
	assert.Equal(t, len(allocated[RemoteGPUCommonWord]), 2)
}

// A card in use leaves the rest of its server on offer.
func TestFit_OtherCardsOfABusyServerStayOnOffer(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	busy := card("gpu-a", "GPU-A1", 40000)
	busy.Used = 1
	devices := []*device.DeviceUsage{
		busy,
		card("gpu-a", "GPU-A2", 40000),
		card("gpu-b", "GPU-B1", 40000),
	}

	registerFitDevices(dev, devices)
	fit, allocated, _ := dev.Fit(devices, request(1, 0), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, true)
	assert.Equal(t, allocated[RemoteGPUCommonWord][0].UUID, "gpu-a/GPU-A2")

	fit, _, reason := dev.Fit([]*device.DeviceUsage{busy}, request(1, 0), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, false, "the busy card itself is not on offer")
	assert.Assert(t, reason != "")
}

// Cards too small for the request do not count towards it.
func TestFit_CountsOnlyCardsThatMeetTheRequest(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	devices := []*device.DeviceUsage{
		card("gpu-a", "GPU-A1", 40000),
		card("gpu-a", "GPU-A2", 1000),
	}

	registerFitDevices(dev, devices)
	fit, allocated, _ := dev.Fit(devices, request(1, 2000), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, true)
	assert.Equal(t, len(allocated[RemoteGPUCommonWord]), 1)
	assert.Equal(t, allocated[RemoteGPUCommonWord][0].UUID, "gpu-a/GPU-A1")

	fit, _, reason := dev.Fit(devices, request(2, 2000), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, false, "only one card is big enough")
	assert.Assert(t, reason != "")
}

func TestMutateAdmission_RejectsUserWorkloadIdentity(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	ctr := &corev1.Container{Name: "worker", Env: []corev1.EnvVar{{Name: lupineWorkloadIDEnv, Value: "user-value"}, {Name: remotePodUIDEnv, Value: "user-uid"}}, Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceName(RemoteGPUResourceCount): resource.MustParse("1")}}}
	pod := &corev1.Pod{}
	_, err := dev.MutateAdmission(ctr, pod)
	assert.ErrorContains(t, err, lupineWorkloadIDEnv)
	assert.Equal(t, ctr.Env[0].Value, "user-value")
	assert.Equal(t, pod.Labels[RemoteGPUPodLabel], "")
}

// Reinvocation keeps a single ordered pair, and each container gets its own identity.
func TestMutateAdmissionWorkloadIdentity(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	for _, name := range []string{"worker", "sidecar"} {
		ctr := &corev1.Container{Name: name, Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceName(RemoteGPUResourceCount): resource.MustParse("1")}}}
		pod := &corev1.Pod{}
		for range 2 {
			_, err := dev.MutateAdmission(ctr, pod)
			assert.NilError(t, err)
		}
		assert.Equal(t, pod.Labels[RemoteGPUPodLabel], "true")
		var uidIndex, workloadIndex int
		var uidCount, workloadCount int
		for i, env := range ctr.Env {
			switch env.Name {
			case remotePodUIDEnv:
				uidIndex = i
				uidCount++
				assert.Equal(t, env.ValueFrom.FieldRef.FieldPath, "metadata.uid")
			case lupineWorkloadIDEnv:
				workloadIndex = i
				workloadCount++
				assert.Equal(t, env.Value, "$("+remotePodUIDEnv+")/"+name)
			}
		}
		assert.Equal(t, uidCount, 1)
		assert.Equal(t, workloadCount, 1)
		assert.Assert(t, uidIndex < workloadIndex)
	}
}

// shareCard is a free card that up to count containers may share.
func shareCard(server, uuid string, mem, count int64) *device.DeviceUsage {
	d := card(server, uuid, mem)
	d.Count = count
	return d
}

// holderPod is a pod on another client node that holds part of a card.
func holderPod(uid, uuid string, mem, cores int64) corev1.Pod {
	return corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: uid, UID: types.UID(uid),
		Annotations: map[string]string{AllocatedAnnos: device.EncodePodSingleDevice(device.PodSingleDevice{
			device.ContainerDevices{{UUID: uuid, Type: RemoteGPUCommonWord, Usedmem: mem, Usedcores: cores}},
		})},
	}, Spec: corev1.PodSpec{NodeName: "cpu-1"}}
}

// Pods on different client nodes share a card by memory. The node's own usage
// view sees none of them, so the cluster-wide pool does the accounting.
func TestFit_PodsShareACardByMemory(t *testing.T) {
	stubFleet(t, []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)})},
		[]corev1.Pod{holderPod("uid-a", "gpu-a/GPU-1", 30000, 0)}, nil)
	dev := InitRemoteGPUDevice(testConfig())
	dev.pool.snapshot(context.Background())
	devices := []*device.DeviceUsage{shareCard("gpu-a", "GPU-1", 40000, 10)}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "b", UID: "uid-b"}}

	fit, allocated, reason := dev.Fit(devices, request(1, 8000), pod, nil, nil)
	assert.Equal(t, fit, true, reason)
	assert.Equal(t, allocated[RemoteGPUCommonWord][0].Usedmem, int64(8000))

	fit, _, reason = dev.Fit(devices, request(1, 12000), pod, nil, nil)
	assert.Equal(t, fit, false, "only 10000 MiB is left")
	assert.Equal(t, common.ParseReason(reason)[common.CardInsufficientMemory], 1)

	fit, _, reason = dev.Fit(devices, request(1, 0), pod, nil, nil)
	assert.Equal(t, fit, false, "a whole card cannot be taken while another pod is on it")
	assert.Equal(t, common.ParseReason(reason)[common.ExclusiveDeviceAllocateConflict], 1)
}

// A pod that took a card whole leaves no room to share it, and the split count
// bounds how many pods share one card however little each asks for.
func TestFit_WholeCardsAndSplitCountBoundSharing(t *testing.T) {
	split := gpu("GPU-2", 40000)
	split.Count = 2
	stubFleet(t, []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000), split})},
		[]corev1.Pod{
			holderPod("uid-whole", "gpu-a/GPU-1", 40000, 100),
			holderPod("uid-s1", "gpu-a/GPU-2", 1000, 0),
			holderPod("uid-s2", "gpu-a/GPU-2", 1000, 0),
		}, nil)
	dev := InitRemoteGPUDevice(testConfig())
	dev.pool.snapshot(context.Background())
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "c", UID: "uid-c"}}

	fit, _, reason := dev.Fit([]*device.DeviceUsage{shareCard("gpu-a", "GPU-1", 40000, 10)}, request(1, 1000), pod, nil, nil)
	assert.Equal(t, fit, false)
	assert.Equal(t, common.ParseReason(reason)[common.ExclusiveDeviceAllocateConflict], 1)

	fit, _, reason = dev.Fit([]*device.DeviceUsage{shareCard("gpu-a", "GPU-2", 40000, 2)}, request(1, 1000), pod, nil, nil)
	assert.Equal(t, fit, false)
	assert.Equal(t, common.ParseReason(reason)[common.CardTimeSlicingExhausted], 1)
}

// The cards of a server are handed to different pods: a server with one card
// taken still serves a second pod from the other.
func TestFit_SplitsAServerBetweenPods(t *testing.T) {
	stubFleet(t, []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000), gpu("GPU-2", 40000)})},
		[]corev1.Pod{holderPod("uid-a", "gpu-a/GPU-1", 40000, 100)}, nil)
	dev := InitRemoteGPUDevice(testConfig())
	dev.pool.snapshot(context.Background())
	devices := []*device.DeviceUsage{shareCard("gpu-a", "GPU-1", 40000, 10), shareCard("gpu-a", "GPU-2", 40000, 10)}

	fit, allocated, reason := dev.Fit(devices, request(1, 0), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "uid-b"}}, nil, nil)
	assert.Equal(t, fit, true, reason)
	assert.Equal(t, allocated[RemoteGPUCommonWord][0].UUID, "gpu-a/GPU-2")

	fit, _, _ = dev.Fit(devices, request(2, 0), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "uid-b"}}, nil, nil)
	assert.Equal(t, fit, false, "only one card of the server is left")
}

// A shared request fills the fullest card first, so whole cards stay whole for
// requests that need one.
func TestFit_SharedRequestPacksTheFullestCard(t *testing.T) {
	stubFleet(t, []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000), gpu("GPU-2", 40000)})},
		[]corev1.Pod{holderPod("uid-a", "gpu-a/GPU-2", 20000, 0)}, nil)
	dev := InitRemoteGPUDevice(testConfig())
	dev.pool.snapshot(context.Background())
	devices := []*device.DeviceUsage{shareCard("gpu-a", "GPU-1", 40000, 10), shareCard("gpu-a", "GPU-2", 40000, 10)}

	fit, allocated, reason := dev.Fit(devices, request(1, 5000), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "uid-b"}}, nil, nil)
	assert.Equal(t, fit, true, reason)
	assert.Equal(t, allocated[RemoteGPUCommonWord][0].UUID, "gpu-a/GPU-2")
}

// Partial grants of one pod that add up to the card's memory are not a whole
// grant: the next container of that pod still needs memory that is free.
func TestFit_PartialGrantsOfAPodAreNotAWholeCard(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	devices := []*device.DeviceUsage{shareCard("gpu-a", "GPU-1", 40000, 10)}
	registerFitDevices(dev, devices)
	prior := device.PodDevices{RemoteGPUCommonWord: device.PodSingleDevice{
		{{UUID: "gpu-a/GPU-1", Type: RemoteGPUCommonWord, Usedmem: 20000}},
		{{UUID: "gpu-a/GPU-1", Type: RemoteGPUCommonWord, Usedmem: 20000}},
	}}

	fit, _, reason := dev.Fit(devices, request(1, 20000), &corev1.Pod{}, nil, &prior)
	assert.Equal(t, fit, false, "the card is already fully given out")
	assert.Equal(t, common.ParseReason(reason)[common.CardInsufficientMemory], 1)
}

// Containers that each fit a server alone may not fit it together. The first
// container's server must take all of them, since the rest are pinned to it.
func TestFit_QualifiesAServerForThePodsContainersTogether(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	devices := []*device.DeviceUsage{
		shareCard("gpu-a", "GPU-A1", 40000, 10),
		shareCard("gpu-b", "GPU-B1", 40000, 10),
		shareCard("gpu-b", "GPU-B2", 40000, 10),
	}
	registerFitDevices(dev, devices)
	pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{
		requestingContainer("first", 1, 30000),
		requestingContainer("second", 1, 30000),
	}}}
	first, err := dev.GenerateResourceRequests(&pod.Spec.Containers[0])
	assert.NilError(t, err)

	fit, allocated, reason := dev.Fit(devices, first, pod, nil, nil)
	assert.Equal(t, fit, true, reason)
	assert.Equal(t, serverOf(allocated[RemoteGPUCommonWord][0].UUID), "gpu-b",
		"gpu-a is smaller, but has room for one container only")

	prior := device.PodDevices{RemoteGPUCommonWord: device.PodSingleDevice{allocated[RemoteGPUCommonWord]}}
	fit, allocated, reason = dev.Fit(devices, first, pod, nil, &prior)
	assert.Equal(t, fit, true, reason)
	assert.Equal(t, serverOf(allocated[RemoteGPUCommonWord][0].UUID), "gpu-b")
}

// What a card offers is read with who holds it. A cached view from before
// the card's split count shrank must not let one more pod on.
func TestFit_UsesTheCurrentSplitCount(t *testing.T) {
	split := gpu("GPU-1", 40000)
	split.Count = 2
	stubFleet(t, []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{split})},
		[]corev1.Pod{holderPod("uid-a", "gpu-a/GPU-1", 1000, 0), holderPod("uid-b", "gpu-a/GPU-1", 1000, 0)}, nil)
	dev := InitRemoteGPUDevice(testConfig())
	dev.pool.snapshot(context.Background())

	fit, _, reason := dev.Fit([]*device.DeviceUsage{shareCard("gpu-a", "GPU-1", 40000, 10)}, request(1, 1000),
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "uid-c"}}, nil, nil)
	assert.Equal(t, fit, false)
	assert.Equal(t, common.ParseReason(reason)[common.CardTimeSlicingExhausted], 1)
}

// A whole grant keeps the card whole even after the card reads larger than
// the grant recorded.
func TestFit_NoSharingBesideAWholeGrantOnAGrownCard(t *testing.T) {
	stubFleet(t, []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 80000)})},
		[]corev1.Pod{holderPod("uid-whole", "gpu-a/GPU-1", 40000, 100)}, nil)
	dev := InitRemoteGPUDevice(testConfig())
	dev.pool.snapshot(context.Background())

	fit, _, reason := dev.Fit([]*device.DeviceUsage{shareCard("gpu-a", "GPU-1", 80000, 10)}, request(1, 10000),
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "uid-c"}}, nil, nil)
	assert.Equal(t, fit, false)
	assert.Equal(t, common.ParseReason(reason)[common.ExclusiveDeviceAllocateConflict], 1)
}

// An ordinary init container is done before the app containers start, so the
// two need the card one after the other, not together. A sidecar keeps
// running beside them.
func TestFit_OrdinaryInitContainerGivesItsCardBack(t *testing.T) {
	always := corev1.ContainerRestartPolicyAlways
	for _, sidecar := range []bool{false, true} {
		dev := InitRemoteGPUDevice(testConfig())
		devices := []*device.DeviceUsage{shareCard("gpu-a", "GPU-1", 40000, 10)}
		registerFitDevices(dev, devices)
		init := requestingContainer("init", 1, 30000)
		if sidecar {
			init.RestartPolicy = &always
		}
		pod := &corev1.Pod{Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{init},
			Containers:     []corev1.Container{requestingContainer("app", 1, 30000)},
		}}
		first, err := dev.GenerateResourceRequests(&pod.Spec.InitContainers[0])
		assert.NilError(t, err)

		fit, allocated, reason := dev.Fit(devices, first, pod, nil, nil)
		assert.Equal(t, fit, !sidecar, "sidecar=%v: %s", sidecar, reason)
		if sidecar {
			continue
		}
		prior := device.PodDevices{RemoteGPUCommonWord: device.PodSingleDevice{allocated[RemoteGPUCommonWord]}}
		second, err := dev.GenerateResourceRequests(&pod.Spec.Containers[0])
		assert.NilError(t, err)
		fit, _, reason = dev.Fit(devices, second, pod, nil, &prior)
		assert.Equal(t, fit, true, reason)
	}
}

// Bind refuses a pod whose booking ran out after Filter: other pods may have
// been put on its cards since.
func TestValidateReservedServer_RequiresTheBooking(t *testing.T) {
	stubFleet(t, []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)})}, nil, nil)
	dev := InitRemoteGPUDevice(testConfig())
	dev.pool.snapshot(context.Background())
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", UID: "uid-p"}}
	allocated := device.PodDevices{RemoteGPUCommonWord: device.PodSingleDevice{
		{{UUID: "gpu-a/GPU-1", Type: RemoteGPUCommonWord, Usedmem: 1000}},
	}}
	annos := map[string]string{}
	dev.PatchAnnotations(pod, &annos, allocated)
	assert.NilError(t, dev.ValidateReservedServer(pod, allocated))

	dev.pool.release(pod.UID)
	assert.ErrorContains(t, dev.ValidateReservedServer(pod, allocated), "booking")
}

// A later container of the pod may be the one another pod is in the way of.
// That is reason enough to re-read the pool before refusing.
func TestFit_RereadsWhenALaterContainerIsBlocked(t *testing.T) {
	nodes := []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-A", 40000), gpu("GPU-B", 40000)})}
	stubFleet(t, nodes, []corev1.Pod{holderPod("uid-gone", "gpu-a/GPU-B", 20000, 0)}, nil)
	dev := InitRemoteGPUDevice(testConfig())
	dev.pool.snapshot(context.Background())
	stubFleet(t, nodes, nil, nil) // the holder has gone, but the pool has not read it yet

	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "uid-p"}, Spec: corev1.PodSpec{Containers: []corev1.Container{
		requestingContainer("small", 1, 5000),
		requestingContainer("large", 2, 30000),
	}}}
	first, err := dev.GenerateResourceRequests(&pod.Spec.Containers[0])
	assert.NilError(t, err)
	devices := []*device.DeviceUsage{shareCard("gpu-a", "GPU-A", 40000, 10), shareCard("gpu-a", "GPU-B", 40000, 10)}
	fit, _, reason := dev.Fit(devices, first, pod, nil, nil)
	assert.Equal(t, fit, true, reason)
}

// A pod's earlier booking from a failed attempt is not someone else's usage.
func TestFit_IgnoresThePodsOwnBooking(t *testing.T) {
	stubFleet(t, nil, nil, nil)
	dev := InitRemoteGPUDevice(testConfig())
	dev.pool.hold("uid-b", "gpu-a/GPU-1")
	devices := []*device.DeviceUsage{shareCard("gpu-a", "GPU-1", 40000, 10)}
	registerFitDevices(dev, devices)

	fit, _, reason := dev.Fit(devices, request(1, 0), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "uid-b"}}, nil, nil)
	assert.Equal(t, fit, true, reason)
	fit, _, _ = dev.Fit(devices, request(1, 0), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "uid-c"}}, nil, nil)
	assert.Equal(t, fit, false, "but it is for every other pod")
}

// The client applies CUDA_VISIBLE_DEVICES to the server's cards, which is what
// keeps pods sharing a server out of each other's cards. Each container gets
// its own list, keyed by name: the webhook may move containers around, and a
// pod-wide list would show a container the cards its sibling was given.
func TestPatchAnnotations_ListsTheVisibleCards(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	annos := map[string]string{}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "uid-a"}, Spec: corev1.PodSpec{
		InitContainers: []corev1.Container{{Name: "lib"}},
		Containers:     []corev1.Container{{Name: "a"}, {Name: "b"}, {Name: "cpu"}},
	}}
	got := dev.PatchAnnotations(pod, &annos, device.PodDevices{
		RemoteGPUCommonWord: device.PodSingleDevice{
			device.ContainerDevices{},
			device.ContainerDevices{{Idx: 2, UUID: "gpu-a/GPU-2", Usedmem: 1000}, {Idx: 1, UUID: "gpu-a/GPU-1", Usedmem: 1000}},
			device.ContainerDevices{{Idx: 3, UUID: "gpu-a/GPU-3", Usedmem: 1000}},
			device.ContainerDevices{},
		},
	})
	assert.Equal(t, got[VisibleDevicesAnnoPrefix+"a"], "2,1")
	assert.Equal(t, got[VisibleDevicesAnnoPrefix+"b"], "3")
	_, ok := got[VisibleDevicesAnnoPrefix+"lib"]
	assert.Assert(t, !ok, "a container without cards gets no list")
	_, ok = got[VisibleDevicesAnnoPrefix+"cpu"]
	assert.Assert(t, !ok)
	for key := range got {
		if strings.HasPrefix(key, VisibleDevicesAnnoPrefix) {
			assert.Equal(t, len(validation.IsQualifiedName(key)), 0, key)
		}
	}
	u := dev.pool.usage("gpu-a/GPU-2", "")
	assert.Equal(t, u.users, 1)
	assert.Equal(t, u.mem, int64(1000))
}

func TestMutateAdmission_WiresTheVisibleCards(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	ctr := &corev1.Container{
		Name: "app",
		Env:  []corev1.EnvVar{{Name: visibleDevicesEnv, Value: "0"}},
		Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
			corev1.ResourceName(RemoteGPUResourceCount): resource.MustParse("1"),
		}},
	}
	_, err := dev.MutateAdmission(ctr, &corev1.Pod{})
	assert.NilError(t, err)
	var found int
	for _, env := range ctr.Env {
		if env.Name == visibleDevicesEnv {
			found++
			assert.Equal(t, env.Value, "", "a literal value is replaced")
			assert.Equal(t, env.ValueFrom.FieldRef.FieldPath, "metadata.annotations['"+VisibleDevicesAnnoPrefix+"app']")
		}
	}
	assert.Equal(t, found, 1)
}
