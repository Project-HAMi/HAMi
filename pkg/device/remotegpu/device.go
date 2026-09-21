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
	"sync"

	"github.com/ccoveille/go-safecast/v2"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
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
	LupineServerAnno = "hami.io/lupine-endpoint"

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
	RemoteGPUSessionImage   string

	errNoClient       = errors.New("kubernetes client is not initialized")
	errNoRegistration = errors.New("node has no decodable GPU registration")
	errNoPool         = errors.New("no lupine server available in the cluster")
)

type RemoteGPUDevices struct {
	pool *pool

	// seenRev is the pool revision each node last took a copy of, so a fleet
	// that has not moved is not re-registered for every node on every tick.
	seenMu  sync.Mutex
	seenRev map[string]uint64
}

func InitRemoteGPUDevice(config RemoteGPUConfig) *RemoteGPUDevices {
	RemoteGPUResourceCount = config.ResourceCountName
	RemoteGPUResourceMemory = config.ResourceMemoryName
	RemoteGPULibImage = config.LibImage
	RemoteGPUSessionImage = config.SessionImage
	port := config.DefaultPort
	if port <= 0 || port > 65535 {
		port = DefaultLupinePort
	}
	if _, ok := device.InRequestDevices[RemoteGPUDevice]; !ok {
		device.InRequestDevices[RemoteGPUDevice] = InRequestAnnos
		device.SupportDevices[RemoteGPUDevice] = AllocatedAnnos
		util.HandshakeAnnos[RemoteGPUDevice] = HandshakeAnnos
	}
	return &RemoteGPUDevices{pool: newPool(port), seenRev: map[string]uint64{}}
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
// CheckHealth reports an update only once the fleet has actually moved.
//
// Every client node is handed the same cluster-wide pool, so answering "yes"
// unconditionally made register deep copy every card for every node in the
// cluster on every tick, under both the register lock and the node manager's.
func (dev *RemoteGPUDevices) CheckHealth(_ string, n *corev1.Node) (bool, bool) {
	if n == nil {
		return true, true
	}
	rev := dev.pool.revision()
	dev.seenMu.Lock()
	defer dev.seenMu.Unlock()
	if last, seen := dev.seenRev[n.Name]; seen && last == rev {
		return true, false
	}
	dev.seenRev[n.Name] = rev
	return true, true
}

// NodeCleanUp forgets a node that has gone, so its entry does not outlive it.
func (dev *RemoteGPUDevices) NodeCleanUp(nodeName string) error {
	dev.seenMu.Lock()
	defer dev.seenMu.Unlock()
	delete(dev.seenRev, nodeName)
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
	needsLib, err := armMemoryLimit(ctr, pod)
	if err != nil {
		return false, err
	}
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
	// Last, because it appends to pod.Spec.InitContainers and ctr may point
	// into that slice: the webhook hands out &pod.Spec.InitContainers[i] for an
	// init container that asks for a card. Appending can move the backing
	// array, and every write above would then land in the abandoned one.
	if needsLib {
		addLibDelivery(pod)
	}
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
// It writes only to ctr and reports whether the pod still needs the delivery
// volume and init container; the caller adds those once it is done with ctr.
func armMemoryLimit(ctr *corev1.Container, pod *corev1.Pod) (bool, error) {
	if RemoteGPULibImage == "" || pod == nil {
		return false, nil
	}
	mem, ok := resourceValue(ctr, RemoteGPUResourceMemory)
	if !ok || mem <= 0 {
		// Nothing was asked for, so there is nothing to hold the pod to.
		return false, nil
	}

	// Prepended, not replaced: a workload that sets its own LD_PRELOAD keeps
	// it, and HAMi-core still gets in front of the CUDA calls.
	preload := libMountPath + "/libvgpu.so"
	if existing := envOf(ctr, ldPreloadEnv); existing != nil {
		if existing.ValueFrom != nil {
			// An env var carries a literal or a source, never both, so there
			// is nothing to prepend to. Refuse the pod rather than drop the
			// preload it asked for and enforce nothing it can see.
			return false, fmt.Errorf("container %q takes %s from a source, which cannot be combined with the %s memory limit: set it literally, or drop the %s request",
				ctr.Name, ldPreloadEnv, RemoteGPUCommonWord, RemoteGPUResourceMemory)
		}
		if existing.Value != "" && existing.Value != preload {
			preload += " " + existing.Value
		}
	}
	mountLib(ctr)
	setEnv(ctr, corev1.EnvVar{Name: ldPreloadEnv, Value: preload})
	// The unindexed limit is HAMi-core's fallback for every device, which is
	// what a request spread evenly over the allocated cards means here.
	setEnv(ctr, corev1.EnvVar{Name: memoryLimitEnv, Value: fmt.Sprintf("%vm", mem)})
	// Whole-card allocation leaves nothing to divide, and HAMi-core already
	// defaults an unset SM limit to the whole device, so no core limit is set.
	//
	// A per-pod cache rather than the node-wide one the device plugin uses:
	// the pod holds its cards outright, so it has no peers to account against.
	setEnv(ctr, corev1.EnvVar{Name: sharedCacheEnv, Value: libMountPath + "/vgpu.cache"})
	return true, nil
}

// envOf returns a container's entry for one variable, if it has one.
func envOf(ctr *corev1.Container, name string) *corev1.EnvVar {
	for i := range ctr.Env {
		if ctr.Env[i].Name == name {
			return &ctr.Env[i]
		}
	}
	return nil
}

// envValue returns a container's literal value for one variable.
func envValue(ctr *corev1.Container, name string) string {
	if env := envOf(ctr, name); env != nil {
		return env.Value
	}
	return ""
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

func (dev *RemoteGPUDevices) GenerateResourceRequests(ctr *corev1.Container) (device.ContainerDeviceRequest, error) {
	count, ok := resourceValue(ctr, RemoteGPUResourceCount)
	if !ok || count <= 0 {
		// No device requested (or an explicit zero) is device-less, not
		// invalid. See the nvidia backend.
		return device.ContainerDeviceRequest{}, nil
	}
	nums, err := safecast.Convert[int32](count)
	if err != nil {
		klog.ErrorS(err, "remotegpu: device count out of range", "value", count)
		return device.ContainerDeviceRequest{}, &device.ErrInvalidDeviceRequest{Container: ctr.Name, Device: "remotegpu", Reason: fmt.Sprintf("device count %d is out of range", count)}
	}
	var memreq int32
	if mem, ok := resourceValue(ctr, RemoteGPUResourceMemory); ok && mem > 0 {
		memreq, err = safecast.Convert[int32](mem)
		if err != nil {
			klog.ErrorS(err, "remotegpu: memory request out of range", "value", mem)
			return device.ContainerDeviceRequest{}, &device.ErrInvalidDeviceRequest{Container: ctr.Name, Device: "remotegpu", Reason: fmt.Sprintf("memory request %d is out of range", mem)}
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
	}, nil
}

func (dev *RemoteGPUDevices) PatchAnnotations(pod *corev1.Pod, annoinput *map[string]string, pd device.PodDevices) map[string]string {
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
	dev.pool.hold(podUID(pod), allocated...)

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

// LockNode is deliberately a no-op. The shared node lock exists so a node's
// device plugin can serialise Allocate against the scheduler; a GPU-less
// client node has no such plugin, so a lock taken here would never be
// released.
func (dev *RemoteGPUDevices) LockNode(_ *corev1.Node, _ *corev1.Pod) error {
	return nil
}

// ReleaseNodeLock has no lock to release, but it is the hook the scheduler
// calls when an attempt it had already allocated for falls through. Giving the
// booking back here frees a whole server in that moment rather than at the end
// of heldTTL.
func (dev *RemoteGPUDevices) ReleaseNodeLock(_ *corev1.Node, pod *corev1.Pod) error {
	dev.pool.release(podUID(pod))
	return nil
}

// podUID is the identity a booking is filed under.
func podUID(pod *corev1.Pod) types.UID {
	if pod == nil {
		return ""
	}
	return pod.UID
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
		// Only the servers the refresh left intact are retried. If none are,
		// the first refusal stands rather than becoming a refusal with nothing
		// to say: the pod cannot be placed either way, and kube-scheduler
		// still needs a reason for it.
		if intact := dev.serversStillWhole(byServer, servers); len(intact) > 0 {
			fit, tmpDevs, reason = dev.tryFit(byServer, intact, request, pod)
		}
	}
	if fit {
		return true, tmpDevs, ""
	}
	return false, tmpDevs, common.GenReason(reason, len(devices))
}

// serversStillWhole drops the servers the forced refresh changed under us.
//
// The refresh clears the bookings that made tryFit refuse, but it can also
// remove a server or one of its cards. Retrying with the pre-refresh view
// would then read a card the fleet no longer has as merely unbooked, and
// PatchAnnotations would find no endpoint for the server naming it. A server
// is handed out whole, so one that lost a card is no longer the thing the
// candidate described either.
func (dev *RemoteGPUDevices) serversStillWhole(byServer map[string][]*device.DeviceUsage, servers []string) []string {
	live := dev.pool.deviceIDs()
	liveCards := map[string]int{}
	for id := range live {
		liveCards[serverOf(id)]++
	}
	kept := make([]string, 0, len(servers))
	for _, server := range servers {
		if _, ok := dev.pool.endpoint(server); !ok {
			continue
		}
		// The same cards and no others. A server that gained one is not the
		// server the candidate described either: tryFit would allocate the
		// stale subset and leave the new card free for a second pod, which is
		// the thing whole-server allocation exists to prevent.
		if liveCards[server] != len(byServer[server]) {
			continue
		}
		intact := true
		for _, d := range byServer[server] {
			if _, ok := live[d.ID]; !ok {
				intact = false
				break
			}
		}
		if intact {
			kept = append(kept, server)
		}
	}
	return kept
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
