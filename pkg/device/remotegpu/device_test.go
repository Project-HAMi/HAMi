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
	"time"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
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
func card(server, uuid string, mem int32) *device.DeviceUsage {
	return &device.DeviceUsage{
		ID:        deviceID(server, uuid),
		Type:      RemoteGPUCommonWord,
		Totalmem:  mem,
		Totalcore: 100,
		Count:     1,
		Health:    true,
	}
}

func request(nums, mem int32) device.ContainerDeviceRequest {
	return device.ContainerDeviceRequest{
		Nums:     nums,
		Type:     RemoteGPUCommonWord,
		Memreq:   mem,
		Coresreq: 100,
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

	fit, _, reason := dev.Fit(devices, request(2, 0), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, false)
	assert.Assert(t, reason != "", "expected a rejection reason")

	// One card is satisfiable, and the lower-sorting server wins deterministically.
	fit, allocated, _ := dev.Fit(devices, request(1, 0), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, true)
	assert.Equal(t, len(allocated[RemoteGPUCommonWord]), 1)
	assert.Equal(t, allocated[RemoteGPUCommonWord][0].UUID, "gpu-a/GPU-1")
}

func TestFit_AllocatesWholeCard(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	devices := []*device.DeviceUsage{card("gpu-a", "GPU-1", 40000)}

	// A 2000MB request still consumes the entire card.
	fit, allocated, _ := dev.Fit(devices, request(1, 2000), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, true)
	got := allocated[RemoteGPUCommonWord][0]
	assert.Equal(t, got.Usedmem, int32(40000))
	assert.Equal(t, got.Usedcores, int32(100))
}

func TestFit_RejectsUnfitCards(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())

	unhealthy := card("gpu-a", "GPU-1", 40000)
	unhealthy.Health = false
	busy := card("gpu-a", "GPU-2", 40000)
	busy.Used = 1
	small := card("gpu-a", "GPU-3", 1000)

	fit, _, reason := dev.Fit([]*device.DeviceUsage{unhealthy, busy, small}, request(1, 2000), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, false)
	assert.Assert(t, reason != "")
}

// A card booked by a pod that landed on a different client node is invisible to
// this node's usage view, so Fit must consult the cluster-wide reservation set.
func TestFit_HonorsClusterWideReservation(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	free := card("gpu-a", "GPU-1", 40000)

	fit, _, _ := dev.Fit([]*device.DeviceUsage{free}, request(1, 0), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, true)

	dev.pool.mu.Lock()
	dev.pool.inUse[free.ID] = struct{}{}
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
	}}
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
			assert.Equal(t, env.ValueFrom.FieldRef.FieldPath, "metadata.uid")
		}
	}
	assert.Equal(t, server, 1, "must replace, not duplicate")
	assert.Equal(t, workload, 1)
}

func TestMutateAdmission_IgnoresContainerWithoutRequest(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	ctr := &corev1.Container{}

	found, err := dev.MutateAdmission(ctr, &corev1.Pod{})
	assert.NilError(t, err)
	assert.Equal(t, found, false)
	assert.Equal(t, len(ctr.Env), 0)
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

	req := dev.GenerateResourceRequests(ctr)
	assert.Equal(t, req.Nums, int32(2))
	assert.Equal(t, req.Memreq, int32(2000))
	assert.Equal(t, req.Coresreq, int32(100))
	assert.Equal(t, req.Type, RemoteGPUCommonWord)

	assert.Equal(t, dev.GenerateResourceRequests(&corev1.Container{}).Nums, int32(0))
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
		Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
			"nvidia.com/remote-gpu":        resource.MustParse("1"),
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
	assert.Equal(t, env[ldPreloadEnv], libMountPath+"/libvgpu.so")
	assert.Equal(t, env[sharedCacheEnv], libMountPath+"/vgpu.cache")

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

// An allocation cannot span servers, so filling the emptiest server first keeps
// the deepest one deep and leaves somewhere for a later multi-card request to
// land. Picking the lowest-sorting server instead would hollow out one server
// while another sat idle.
func TestFit_SpreadsAcrossServers(t *testing.T) {
	dev := InitRemoteGPUDevice(testConfig())
	// gpu-a sorts first but has one card left; gpu-b has three.
	devices := []*device.DeviceUsage{
		card("gpu-a", "GPU-A1", 40000),
		card("gpu-b", "GPU-B1", 40000),
		card("gpu-b", "GPU-B2", 40000),
		card("gpu-b", "GPU-B3", 40000),
	}

	fit, allocated, _ := dev.Fit(devices, request(1, 0), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, true)
	assert.Equal(t, serverOf(allocated[RemoteGPUCommonWord][0].UUID), "gpu-b",
		"the emptiest server serves the request")

	// With the fleet level, the tie breaks on name so the choice is repeatable.
	level := []*device.DeviceUsage{
		card("gpu-a", "GPU-A1", 40000),
		card("gpu-b", "GPU-B1", 40000),
	}
	fit, allocated, _ = dev.Fit(level, request(1, 0), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, true)
	assert.Equal(t, serverOf(allocated[RemoteGPUCommonWord][0].UUID), "gpu-a")

	// Spreading must not hand out a server that cannot serve the whole request.
	// gpu-b has the most cards free but only gpu-a has two of the right size.
	small := card("gpu-b", "GPU-B4", 1000)
	mixed := []*device.DeviceUsage{
		card("gpu-a", "GPU-A1", 40000),
		card("gpu-a", "GPU-A2", 40000),
		small,
		card("gpu-b", "GPU-B5", 1000),
		card("gpu-b", "GPU-B6", 1000),
	}
	fit, allocated, _ = dev.Fit(mixed, request(2, 2000), &corev1.Pod{}, nil, nil)
	assert.Equal(t, fit, true)
	assert.Equal(t, len(allocated[RemoteGPUCommonWord]), 2)
	assert.Equal(t, serverOf(allocated[RemoteGPUCommonWord][0].UUID), "gpu-a")
}
