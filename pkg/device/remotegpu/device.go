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

// Package remotegpu schedules pods onto GPUs that live on a different node,
// served over the network by a lupine server. A client pod runs on a node with
// no GPU of its own and reaches the fleet through LUPINE_SERVER.
//
// Unlike every other backend here, the devices this one reports do not belong
// to the node they are reported for. See pool for what that costs.
package remotegpu

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/ccoveille/go-safecast/v2"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

const (
	RemoteGPUDevice     = "RemoteGPU"
	RemoteGPUCommonWord = "RemoteGPU"

	HandshakeAnnos = "hami.io/node-handshake-remote-gpu"
	InRequestAnnos = "hami.io/remote-gpu-devices-to-allocate"
	AllocatedAnnos = "hami.io/remote-gpu-devices-allocated"

	// LupineServerAnno carries the scheduler's placement decision to the
	// client container through the downward API. There is no device plugin on
	// a GPU-less client node to inject it at Allocate time.
	LupineServerAnno = "hami.io/lupine-server"

	lupineServerEnv     = "LUPINE_SERVER"
	lupineWorkloadIDEnv = "LUPINE_WORKLOAD_ID"

	// DefaultLupinePort is lupine's listen port when a server node does not
	// override it through the LupineServerLabel value.
	DefaultLupinePort = 14833

	// HAMi-core arrives in the client pod through this volume, since no device
	// plugin runs on a node that owns no GPU. The name is shared by the volume
	// and the init container that fills it, so both are easy to recognise in a
	// mutated pod spec.
	libVolumeName = "hami-remote-gpu-lib"
	libMountPath  = "/hami-remote-gpu"
	libSourceGlob = "/k8s-vgpu/lib/nvidia/libvgpu.so.*"

	ldPreloadEnv   = "LD_PRELOAD"
	memoryLimitEnv = "CUDA_DEVICE_MEMORY_LIMIT"
	sharedCacheEnv = "CUDA_DEVICE_MEMORY_SHARED_CACHE"
)

var (
	RemoteGPUResourceCount  string
	RemoteGPUResourceMemory string
	RemoteGPULibImage       string

	errNoClient       = errors.New("kubernetes client is not initialized")
	errNoRegistration = errors.New("node has no decodable GPU registration")
	errNoPool         = errors.New("no lupine server available in the cluster")
)

type RemoteGPUDevices struct {
	pool *pool
}

func InitRemoteGPUDevice(config RemoteGPUConfig) *RemoteGPUDevices {
	RemoteGPUResourceCount = config.ResourceCountName
	RemoteGPUResourceMemory = config.ResourceMemoryName
	RemoteGPULibImage = config.LibImage
	port := config.DefaultPort
	if port <= 0 || port > 65535 {
		port = DefaultLupinePort
	}
	if _, ok := device.InRequestDevices[RemoteGPUDevice]; !ok {
		device.InRequestDevices[RemoteGPUDevice] = InRequestAnnos
		device.SupportDevices[RemoteGPUDevice] = AllocatedAnnos
		util.HandshakeAnnos[RemoteGPUDevice] = HandshakeAnnos
	}
	return &RemoteGPUDevices{pool: newPool(port)}
}

func (dev *RemoteGPUDevices) CommonWord() string {
	return RemoteGPUCommonWord
}

func (dev *RemoteGPUDevices) GetResourceNames() device.ResourceNames {
	return device.ResourceNames{
		ResourceCountName:  RemoteGPUResourceCount,
		ResourceMemoryName: RemoteGPUResourceMemory,
		ResourceCoreName:   "",
	}
}

// CheckHealth always reports healthy and always asks for an update.
//
// The shared device.CheckHealth handshake expects a device plugin on the node
// to answer a "Requesting_" annotation. A client node runs no such plugin, and
// the lupine fleet's own liveness is already covered by the pool refresh, so
// running the handshake here would time out after 60s and evict the whole pool.
func (dev *RemoteGPUDevices) CheckHealth(_ string, _ *corev1.Node) (bool, bool) {
	return true, true
}

func (dev *RemoteGPUDevices) NodeCleanUp(_ string) error {
	return nil
}

// GetNodeDevices reports the cluster-wide lupine pool for every node that can
// host a client pod. n is the *client* node and owns none of these GPUs.
func (dev *RemoteGPUDevices) GetNodeDevices(n corev1.Node) ([]*device.DeviceInfo, error) {
	// Unconfigured resource name means the feature is off; do not poll the API.
	if RemoteGPUResourceCount == "" {
		return nil, errNoPool
	}
	// Serving the pool, and a fleet with no server left, both mean "no devices
	// here right now" rather than a failed lookup. Scheduler.register only
	// prunes a stale cache entry when this succeeds with zero devices, and
	// skips the pruning entirely on error, so reporting an error would leave
	// the node advertising GPUs it can no longer reach: a client node that has
	// just been relabelled as a lupine server, or every client node once the
	// last server goes away.
	if isLupineNode(&n) {
		return nil, nil
	}
	return dev.pool.snapshot(context.Background()), nil
}

// MutateAdmission wires LUPINE_SERVER to the annotation the scheduler writes in
// PatchAnnotations. A literal value in the pod spec is replaced: the endpoint
// is not known until placement.
func (dev *RemoteGPUDevices) MutateAdmission(ctr *corev1.Container, pod *corev1.Pod) (bool, error) {
	if _, ok := resourceValue(ctr, RemoteGPUResourceCount); !ok {
		return false, nil
	}
	armMemoryLimit(ctr, pod)
	setEnv(ctr, corev1.EnvVar{
		Name: lupineServerEnv,
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{
				APIVersion: "v1",
				FieldPath:  fmt.Sprintf("metadata.annotations['%s']", LupineServerAnno),
			},
		},
	})
	// Pod UID is the workload identity lupine's usage endpoint groups by.
	setEnv(ctr, corev1.EnvVar{
		Name: lupineWorkloadIDEnv,
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "metadata.uid"},
		},
	})
	return true, nil
}

// armMemoryLimit puts HAMi-core in front of the client's CUDA calls, so the
// memory the pod asked for is the memory it can take.
//
// On a node that owns its GPUs the device plugin does this at Allocate time. A
// client node owns none, runs no plugin, and never sees an Allocate call, so
// the library has to arrive with the pod. HAMi-core resolves the real driver
// with dlopen("libcuda.so.1"), which lands on the lupine client library the
// workload image already puts on its search path, and enforcement then happens
// before anything goes out on the wire.
func armMemoryLimit(ctr *corev1.Container, pod *corev1.Pod) {
	if RemoteGPULibImage == "" || pod == nil {
		return
	}
	mem, ok := resourceValue(ctr, RemoteGPUResourceMemory)
	if !ok || mem <= 0 {
		// Nothing was asked for, so there is nothing to hold the pod to.
		return
	}
	addLibDelivery(pod)
	mountLib(ctr)

	setEnv(ctr, corev1.EnvVar{Name: ldPreloadEnv, Value: libMountPath + "/libvgpu.so"})
	// The unindexed limit is HAMi-core's fallback for every device, which is
	// what a request spread evenly over the allocated cards means here.
	setEnv(ctr, corev1.EnvVar{Name: memoryLimitEnv, Value: fmt.Sprintf("%vm", mem)})
	// Whole-card allocation leaves nothing to divide, and HAMi-core already
	// defaults an unset SM limit to the whole device, so no core limit is set.
	//
	// A per-pod cache rather than the node-wide one the device plugin uses:
	// the pod holds its cards outright, so it has no peers to account against.
	setEnv(ctr, corev1.EnvVar{Name: sharedCacheEnv, Value: libMountPath + "/vgpu.cache"})
}

// addLibDelivery gives the pod somewhere to put HAMi-core and an init container
// that fetches it. Both are named after the volume so a pod whose containers
// each ask for a remote GPU collects one copy, not one per container.
func addLibDelivery(pod *corev1.Pod) {
	for i := range pod.Spec.Volumes {
		if pod.Spec.Volumes[i].Name == libVolumeName {
			return
		}
	}
	pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
		Name:         libVolumeName,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	})
	pod.Spec.InitContainers = append(pod.Spec.InitContainers, corev1.Container{
		Name:  libVolumeName,
		Image: RemoteGPULibImage,
		// The library is published under a version suffix, and the glob keeps
		// this from having to track the chart's image tag.
		Command:      []string{"sh", "-c", "cp " + libSourceGlob + " " + libMountPath + "/libvgpu.so"},
		VolumeMounts: []corev1.VolumeMount{{Name: libVolumeName, MountPath: libMountPath}},
	})
}

func mountLib(ctr *corev1.Container) {
	for i := range ctr.VolumeMounts {
		if ctr.VolumeMounts[i].Name == libVolumeName {
			return
		}
	}
	// Writable, because HAMi-core keeps its accounting cache alongside.
	ctr.VolumeMounts = append(ctr.VolumeMounts, corev1.VolumeMount{
		Name:      libVolumeName,
		MountPath: libMountPath,
	})
}

func (dev *RemoteGPUDevices) GenerateResourceRequests(ctr *corev1.Container) device.ContainerDeviceRequest {
	count, ok := resourceValue(ctr, RemoteGPUResourceCount)
	if !ok || count <= 0 {
		return device.ContainerDeviceRequest{}
	}
	nums, err := safecast.Convert[int32](count)
	if err != nil {
		klog.ErrorS(err, "remotegpu: device count out of range", "value", count)
		return device.ContainerDeviceRequest{}
	}
	var memreq int32
	if mem, ok := resourceValue(ctr, RemoteGPUResourceMemory); ok && mem > 0 {
		memreq, err = safecast.Convert[int32](mem)
		if err != nil {
			klog.ErrorS(err, "remotegpu: memory request out of range", "value", mem)
			return device.ContainerDeviceRequest{}
		}
	}
	return device.ContainerDeviceRequest{
		Nums: nums,
		Type: RemoteGPUCommonWord,
		// ponytail: whole-server allocation for the first cut. Memreq filters
		// candidate cards but never splits one, so cores are always the whole
		// card. Relax both when lupine can scope a connection to a subset of
		// the server's GPUs.
		Memreq:   memreq,
		Coresreq: 100,
	}
}

func (dev *RemoteGPUDevices) PatchAnnotations(_ *corev1.Pod, annoinput *map[string]string, pd device.PodDevices) map[string]string {
	devList, ok := pd[RemoteGPUCommonWord]
	if !ok || len(devList) == 0 {
		return *annoinput
	}
	deviceStr := device.EncodePodSingleDevice(devList)
	(*annoinput)[InRequestAnnos] = deviceStr
	(*annoinput)[AllocatedAnnos] = deviceStr

	// Book the cards straight away. The pool rebuilds its reservation set from
	// pod annotations at most once per poolTTL, so without this two pods
	// scheduled inside the same window both see this card as free.
	var allocated []string
	for _, ctrDevs := range devList {
		for _, d := range ctrDevs {
			allocated = append(allocated, d.UUID)
		}
	}
	dev.pool.hold(allocated...)

	// Fit refuses to span two servers, so any allocated device names the one
	// the client must connect to.
	for _, id := range allocated {
		if ep, ok := dev.pool.endpoint(serverOf(id)); ok {
			(*annoinput)[LupineServerAnno] = ep
			return *annoinput
		}
	}
	klog.ErrorS(nil, "remotegpu: allocated devices resolve to no known lupine endpoint", "devices", deviceStr)
	return *annoinput
}

// LockNode and ReleaseNodeLock are deliberately no-ops. The shared node lock
// exists so a node's device plugin can serialise Allocate against the
// scheduler; a GPU-less client node has no such plugin, so a lock taken here
// would never be released.
func (dev *RemoteGPUDevices) LockNode(_ *corev1.Node, _ *corev1.Pod) error {
	return nil
}

func (dev *RemoteGPUDevices) ReleaseNodeLock(_ *corev1.Node, _ *corev1.Pod) error {
	return nil
}

func (dev *RemoteGPUDevices) ScoreNode(_ *corev1.Node, _ device.PodSingleDevice, _ []*device.DeviceUsage, _ string) float32 {
	return 0
}

func (dev *RemoteGPUDevices) AddResourceUsage(_ *corev1.Pod, n *device.DeviceUsage, ctr *device.ContainerDevice) error {
	n.Used++
	n.Usedmem += ctr.Usedmem
	n.Usedcores += ctr.Usedcores
	return nil
}

// Fit hands out one lupine server, with every card on it.
//
// A client sees every GPU of every server it is pointed at, and nothing on the
// wire narrows that down, so the server is the smallest thing that can be given
// to one pod without giving it to another as well. Naming a second server would
// only widen what the pod can reach, which is why a request for more cards than
// any single server has goes unfilled even when the fleet holds enough.
func (dev *RemoteGPUDevices) Fit(devices []*device.DeviceUsage, request device.ContainerDeviceRequest, pod *corev1.Pod, _ *device.NodeInfo, _ *device.PodDevices) (bool, map[string]device.ContainerDevices, string) {
	byServer := map[string][]*device.DeviceUsage{}
	servers := make([]string, 0, len(devices))
	for _, d := range devices {
		server := serverOf(d.ID)
		if server == "" {
			klog.V(5).InfoS("remotegpu: skipping device with no server prefix", "device", d.ID)
			continue
		}
		if _, seen := byServer[server]; !seen {
			servers = append(servers, server)
		}
		byServer[server] = append(byServer[server], d)
	}
	// Map iteration order is random; sort so equal-fitting servers are picked
	// deterministically across Filter calls.
	sort.Strings(servers)

	fit, tmpDevs, reason := dev.tryFit(byServer, servers, request, pod)
	if !fit && reason[common.ExclusiveDeviceAllocateConflict] > 0 {
		// Only a booking stood in the way, and bookings come from a snapshot
		// up to poolTTL old: a card freed moments ago still reads as taken.
		// Refusing here is expensive, because kube-scheduler parks the pod
		// until its next periodic flush of the unschedulable queue, minutes
		// away. Pay for a fresh read instead of guessing wrong.
		dev.pool.refreshNow(context.Background())
		fit, tmpDevs, reason = dev.tryFit(byServer, servers, request, pod)
	}
	if fit {
		return true, tmpDevs, ""
	}
	return false, tmpDevs, common.GenReason(reason, len(devices))
}

// tryFit picks the server the request should come from. It reads pool
// reservations, so the same call can answer differently before and after a
// refresh.
func (dev *RemoteGPUDevices) tryFit(byServer map[string][]*device.DeviceUsage, servers []string, request device.ContainerDeviceRequest, pod *corev1.Pod) (bool, map[string]device.ContainerDevices, map[string]int) {
	tmpDevs := map[string]device.ContainerDevices{}
	reason := map[string]int{}

	// A server is taken whole or not at all. Pointing a client at one exposes
	// every GPU that server owns, and nothing on the wire narrows that down, so
	// leaving a card behind would leave it visible to this pod and free for the
	// next one. The design puts the boundary in the same place: a GPU node is
	// managed by lupine entirely, and runs one server.
	type candidate struct {
		server string
		cards  []*device.DeviceUsage // every healthy card, all of them allocated
		usable int32                 // how many of them meet the request
	}
	candidates := make([]candidate, 0, len(servers))
	for _, server := range servers {
		c := candidate{server: server}
		taken := false
		for _, d := range byServer[server] {
			switch {
			case !d.Health:
				// An unhealthy card is still visible to whoever holds the
				// server, but it is not one this request can count on.
				reason[common.CardNotHealth]++
			case d.Used > 0 || dev.pool.reserved(d.ID):
				// Used covers pods already booked on this client node; reserved
				// covers pods booked on any other client node, which the
				// scheduler's per-node usage view cannot see, and clients the
				// server itself reports.
				reason[common.ExclusiveDeviceAllocateConflict]++
				taken = true
			case request.Memreq > 0 && d.Totalmem < request.Memreq:
				reason[common.CardInsufficientMemory]++
				c.cards = append(c.cards, d)
			default:
				c.cards = append(c.cards, d)
				c.usable++
			}
		}
		if taken {
			// One card in use means the server is, whatever the rest look like.
			continue
		}
		candidates = append(candidates, c)
	}

	// Among the servers that can serve the request, take the smallest. Whole
	// servers are the unit, so the smallest sufficient one leaves the deeper
	// servers intact for requests that need them. servers arrives sorted by
	// name and the sort is stable, so equal servers are picked the same way on
	// every call, which repeated Filter calls for one pod rely on.
	sort.SliceStable(candidates, func(i, j int) bool {
		return len(candidates[i].cards) < len(candidates[j].cards)
	})

	for _, c := range candidates {
		if c.usable < request.Nums {
			reason[common.NodeInsufficientDevice]++
			continue
		}
		for _, d := range c.cards {
			tmpDevs[request.Type] = append(tmpDevs[request.Type], device.ContainerDevice{
				Idx:  int(d.Index),
				UUID: d.ID,
				Type: request.Type,
				// Whole card: the request's memory is a filter, not a split.
				Usedmem:   d.Totalmem,
				Usedcores: d.Totalcore,
			})
		}
		klog.V(4).InfoS("remotegpu: allocated a lupine server",
			"pod", klog.KObj(pod), "server", c.server, "cards", len(c.cards), "requested", request.Nums)
		return true, tmpDevs, reason
	}
	return false, tmpDevs, reason
}

// resourceValue reads a resource from limits, falling back to requests.
func resourceValue(ctr *corev1.Container, name string) (int64, bool) {
	if name == "" || ctr == nil {
		return 0, false
	}
	v, ok := ctr.Resources.Limits[corev1.ResourceName(name)]
	if !ok {
		v, ok = ctr.Resources.Requests[corev1.ResourceName(name)]
	}
	if !ok {
		return 0, false
	}
	n, ok := v.AsInt64()
	return n, ok
}

// setEnv replaces an env var of the same name rather than appending a second
// entry with the same key.
func setEnv(ctr *corev1.Container, env corev1.EnvVar) {
	for i := range ctr.Env {
		if ctr.Env[i].Name == env.Name {
			ctr.Env[i] = env
			return
		}
	}
	ctr.Env = append(ctr.Env, env)
}
