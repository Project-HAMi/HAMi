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

package scheduler

import (
	"testing"
	"time"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/remotegpu"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

// These tests drive the real scoring path (buildNodeUsage -> calcScore ->
// fitInDevices -> Fit -> AddResourceUsage) with only the apiserver faked, so
// they cover the seams a package-local unit test cannot: DeviceVendor keying,
// request-type matching in getNodeResources, the DevicesMap lookup in
// fitInDevices, and the device-ID round trip after Fit.

func setupRemoteGPUScheduler(t *testing.T, objects ...runtime.Object) *remotegpu.RemoteGPUDevices {
	t.Helper()

	prevClient := client.KubeClient
	prevMap := device.DevicesMap
	prevPolicy := device.GPUSchedulerPolicy
	prevInReq := device.InRequestDevices
	prevSupport := device.SupportDevices
	prevHandshake := util.HandshakeAnnos
	t.Cleanup(func() {
		client.KubeClient = prevClient
		device.DevicesMap = prevMap
		device.GPUSchedulerPolicy = prevPolicy
		device.InRequestDevices = prevInReq
		device.SupportDevices = prevSupport
		util.HandshakeAnnos = prevHandshake
	})

	client.KubeClient = fake.NewClientset(objects...)
	device.InRequestDevices = map[string]string{}
	device.SupportDevices = map[string]string{}
	util.HandshakeAnnos = map[string]string{}
	device.GPUSchedulerPolicy = util.GPUSchedulerPolicyBinpack.String()

	dev := remotegpu.InitRemoteGPUDevice(remotegpu.RemoteGPUConfig{
		ResourceCountName:  "nvidia.com/remote-gpu",
		ResourceMemoryName: "nvidia.com/remote-gpu-memory",
		DefaultPort:        remotegpu.DefaultLupinePort,
	})
	device.DevicesMap = map[string]device.Devices{dev.CommonWord(): dev}
	return dev
}

func lupineServerNode(name, ip string, gpus []*device.DeviceInfo) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Labels:      map[string]string{remotegpu.LupineServerLabel: ""},
			Annotations: map[string]string{"hami.io/node-nvidia-register": device.MarshalNodeDevices(gpus)},
		},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: ip}},
		},
	}
}

func gpulessNode(name string) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

func fleetGPU(uuid string, mem int32) *device.DeviceInfo {
	return &device.DeviceInfo{
		ID: uuid, Count: 10, Devmem: mem, Devcore: 100,
		Type: "NVIDIA-A100-SXM4-40GB", Health: true, Mode: "hami-core",
	}
}

func remoteGPUPod(name string, cards, mem int64) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", UID: k8stypes.UID("uid-" + name)},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "app",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/remote-gpu":        *resource.NewQuantity(cards, resource.DecimalSI),
						"nvidia.com/remote-gpu-memory": *resource.NewQuantity(mem, resource.DecimalSI),
					},
				},
			}},
		},
	}
}

// nodeUsageFor builds the scheduler's per-node view exactly the way register()
// does, so the DeviceVendor key the backend sets is the one scoring reads back.
func nodeUsageFor(t *testing.T, dev *remotegpu.RemoteGPUDevices, node *corev1.Node, task *corev1.Pod) *NodeUsage {
	t.Helper()
	infos, err := dev.GetNodeDevices(*node)
	assert.NilError(t, err)
	nodeInfo := &device.NodeInfo{ID: node.Name, Node: node, Devices: map[string][]device.DeviceInfo{}}
	for _, info := range infos {
		nodeInfo.Devices[info.DeviceVendor] = append(nodeInfo.Devices[info.DeviceVendor], *info)
	}
	return buildNodeUsage(nodeInfo, task)
}

// A pod asking for a remote GPU must be placeable on a node that owns no GPU,
// and the lupine server node must not be offered as a host for it.
func TestRemoteGPU_GPUlessNodeFitsAndLupineNodeDoesNot(t *testing.T) {
	lupine := lupineServerNode("gpu-a", "10.0.0.5", []*device.DeviceInfo{
		fleetGPU("GPU-aaa", 40000), fleetGPU("GPU-bbb", 40000),
	})
	dev := setupRemoteGPUScheduler(t, lupine, gpulessNode("cpu-1"), gpulessNode("cpu-2"))

	// Zero devices rather than an error: register() prunes a stale cache entry
	// only on a successful empty result, so a node that becomes a lupine server
	// stops offering the pool instead of keeping whatever it last advertised.
	served, err := dev.GetNodeDevices(*lupine)
	assert.NilError(t, err)
	assert.Equal(t, len(served), 0, "the lupine server node must not host client pods")

	task := remoteGPUPod("client", 2, 2000)
	nodes := map[string]*NodeUsage{
		"cpu-1": nodeUsageFor(t, dev, gpulessNode("cpu-1"), task),
		"cpu-2": nodeUsageFor(t, dev, gpulessNode("cpu-2"), task),
	}
	// Both GPU-less nodes see the same fleet.
	assert.Equal(t, len(nodes["cpu-1"].Devices.DeviceLists), 2)

	reqs := device.Resourcereqs(task)
	assert.Equal(t, reqs[0]["RemoteGPU"].Nums, int32(2), "GenerateResourceRequests must reach Resourcereqs")

	s := NewScheduler()
	failed := map[string]string{}
	scores, err := s.calcScore(&nodes, reqs, task, failed)
	assert.NilError(t, err)
	assert.Equal(t, len(scores.NodeList), 2, "every GPU-less node is a candidate")

	best := scores.NodeList[0]
	allocated := best.Devices["RemoteGPU"]
	assert.Equal(t, len(allocated), 1, "one container")
	assert.Equal(t, len(allocated[0]), 2, "two whole cards")
	assert.Equal(t, allocated[0][0].UUID, "gpu-a/GPU-aaa")
	assert.Equal(t, allocated[0][0].Usedmem, int32(40000), "whole card, not the 2000 requested")

	annos := map[string]string{}
	dev.PatchAnnotations(task, &annos, best.Devices)
	assert.Equal(t, annos["hami.io/lupine-server"], "10.0.0.5:14833")
	assert.Assert(t, annos["hami.io/remote-gpu-devices-allocated"] != "")
}

// Two lupine servers with one free card each must not satisfy a two-card
// request: the client holds a single connection to a single server.
func TestRemoteGPU_AllocationNeverSpansTwoServers(t *testing.T) {
	dev := setupRemoteGPUScheduler(t,
		lupineServerNode("gpu-a", "10.0.0.5", []*device.DeviceInfo{fleetGPU("GPU-aaa", 40000)}),
		lupineServerNode("gpu-b", "10.0.0.6", []*device.DeviceInfo{fleetGPU("GPU-bbb", 40000)}),
		gpulessNode("cpu-1"),
	)

	task := remoteGPUPod("client", 2, 2000)
	nodes := map[string]*NodeUsage{"cpu-1": nodeUsageFor(t, dev, gpulessNode("cpu-1"), task)}
	assert.Equal(t, len(nodes["cpu-1"].Devices.DeviceLists), 2, "fleet has two free cards overall")

	s := NewScheduler()
	failed := map[string]string{}
	scores, err := s.calcScore(&nodes, device.Resourcereqs(task), task, failed)
	assert.NilError(t, err)
	assert.Equal(t, len(scores.NodeList), 0, "two cards on two servers must not be combined")
	assert.Assert(t, failed["cpu-1"] != "")
}

// The card held by a pod that landed on another client node is invisible to
// this node's usage view. Without the cluster-wide reservation read, both
// nodes would hand out the same card.
func TestRemoteGPU_ReservationBlocksSecondClientNode(t *testing.T) {
	held := device.EncodePodSingleDevice(device.PodSingleDevice{
		device.ContainerDevices{{
			UUID: "gpu-a/GPU-aaa", Type: "RemoteGPU", Usedmem: 40000, Usedcores: 100,
		}},
	})
	running := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "first", Namespace: "default",
			Annotations: map[string]string{"hami.io/remote-gpu-devices-allocated": held},
		},
		Spec:   corev1.PodSpec{NodeName: "cpu-1"},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	dev := setupRemoteGPUScheduler(t,
		lupineServerNode("gpu-a", "10.0.0.5", []*device.DeviceInfo{fleetGPU("GPU-aaa", 40000)}),
		gpulessNode("cpu-1"), gpulessNode("cpu-2"), running,
	)

	task := remoteGPUPod("second", 1, 2000)
	nodes := map[string]*NodeUsage{
		"cpu-1": nodeUsageFor(t, dev, gpulessNode("cpu-1"), task),
		"cpu-2": nodeUsageFor(t, dev, gpulessNode("cpu-2"), task),
	}

	s := NewScheduler()
	failed := map[string]string{}
	scores, err := s.calcScore(&nodes, device.Resourcereqs(task), task, failed)
	assert.NilError(t, err)
	assert.Equal(t, len(scores.NodeList), 0, "the only card is already held by another pod")
	assert.Assert(t, failed["cpu-2"] != "", "the holder landed on cpu-1, yet cpu-2 must also be refused")
}

// A finished pod releases its card back to the fleet.
func TestRemoteGPU_FinishedPodReleasesCard(t *testing.T) {
	held := device.EncodePodSingleDevice(device.PodSingleDevice{
		device.ContainerDevices{{UUID: "gpu-a/GPU-aaa", Type: "RemoteGPU", Usedmem: 40000, Usedcores: 100}},
	})
	done := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "first", Namespace: "default",
			Annotations: map[string]string{"hami.io/remote-gpu-devices-allocated": held},
		},
		Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
	}
	dev := setupRemoteGPUScheduler(t,
		lupineServerNode("gpu-a", "10.0.0.5", []*device.DeviceInfo{fleetGPU("GPU-aaa", 40000)}),
		gpulessNode("cpu-1"), done,
	)

	task := remoteGPUPod("second", 1, 2000)
	nodes := map[string]*NodeUsage{"cpu-1": nodeUsageFor(t, dev, gpulessNode("cpu-1"), task)}

	s := NewScheduler()
	scores, err := s.calcScore(&nodes, device.Resourcereqs(task), task, map[string]string{})
	assert.NilError(t, err)
	assert.Equal(t, len(scores.NodeList), 1)
}

// register() is what actually populates the scheduler cache in production.
// Run it rather than hand-building NodeInfo, so the DeviceVendor key and the
// CheckHealth contract are exercised on the real path.
func TestRemoteGPU_RegisterPopulatesClientNodesOnly(t *testing.T) {
	lupine := lupineServerNode("gpu-a", "10.0.0.5", []*device.DeviceInfo{
		fleetGPU("GPU-aaa", 40000), fleetGPU("GPU-bbb", 40000),
	})
	cpu1, cpu2 := gpulessNode("cpu-1"), gpulessNode("cpu-2")
	setupRemoteGPUScheduler(t, lupine, cpu1, cpu2)

	s := NewScheduler()
	s.kubeClient = client.KubeClient
	factory := informers.NewSharedInformerFactoryWithOptions(client.KubeClient, time.Hour)
	s.nodeLister = factory.Core().V1().Nodes().Lister()
	s.podLister = factory.Core().V1().Pods().Lister()
	indexer := factory.Core().V1().Nodes().Informer().GetIndexer()
	for _, n := range []*corev1.Node{lupine, cpu1, cpu2} {
		assert.NilError(t, indexer.Add(n))
	}

	s.register(labels.Everything())

	for _, name := range []string{"cpu-1", "cpu-2"} {
		info, err := s.GetNode(name)
		assert.NilError(t, err, "GPU-less node must be registered as a remote-gpu host")
		assert.Equal(t, len(info.Devices["RemoteGPU"]), 2, "node %s sees the whole fleet", name)
		assert.Equal(t, info.Devices["RemoteGPU"][0].ID, "gpu-a/GPU-aaa")
	}

	_, err := s.GetNode("gpu-a")
	assert.Assert(t, err != nil, "the lupine server node must not enter the cache as a client host")
}

// A card too small for the request is filtered out even though allocation is
// whole-card.
func TestRemoteGPU_MemoryRequestFiltersCards(t *testing.T) {
	dev := setupRemoteGPUScheduler(t,
		lupineServerNode("gpu-a", "10.0.0.5", []*device.DeviceInfo{fleetGPU("GPU-small", 1000)}),
		gpulessNode("cpu-1"),
	)

	task := remoteGPUPod("client", 1, 40000)
	nodes := map[string]*NodeUsage{"cpu-1": nodeUsageFor(t, dev, gpulessNode("cpu-1"), task)}

	s := NewScheduler()
	failed := map[string]string{}
	scores, err := s.calcScore(&nodes, device.Resourcereqs(task), task, failed)
	assert.NilError(t, err)
	assert.Equal(t, len(scores.NodeList), 0)
	assert.Assert(t, failed["cpu-1"] != "")
}
