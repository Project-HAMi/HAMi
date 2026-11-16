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
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
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

	// RemoteGPUPodLabel marks a client Pod the remote GPU monitor should watch.
	RemoteGPUPodLabel = "hami.io/remote-gpu"

	// LupineServerAnno carries the scheduler's placement decision to the
	// client container through the downward API. There is no device plugin on
	// a GPU-less client node to inject it at Allocate time.
	LupineServerAnno = "hami.io/lupine-endpoint"

	// VisibleDevicesAnnoPrefix, followed by a container name, lists the
	// indices of the GPUs the scheduler gave that container on its Lupine
	// server. It reaches the client as CUDA_VISIBLE_DEVICES, which the Lupine
	// client applies to the server's devices, so several pods, and containers
	// of one pod, can share a server without seeing each other's cards in
	// CUDA. The container name is the key's name part, as in the AppArmor
	// annotation, because admission may reorder containers. Indices rather
	// than UUIDs, because the Lupine build the chart pins ignores GPU UUIDs
	// there (fixed upstream in lupinemachines/lupine#1135). They are the
	// device plugin's NVML indices, which the server's CUDA ordinals follow
	// under CUDA_DEVICE_ORDER=PCI_BUS_ID.
	VisibleDevicesAnnoPrefix = "remote-gpu-visible-devices.hami.io/"

	// LupineServerSelectorAnno optionally limits a RemoteGPU pod to Lupine
	// servers with one exact Node-label match, for example
	// "gpu-pool=inference". It selects the remote server, not the client node.
	LupineServerSelectorAnno = "hami.io/remote-server"

	lupineServerEnv     = "LUPINE_SERVER"
	visibleDevicesEnv   = "CUDA_VISIBLE_DEVICES"
	lupineWorkloadIDEnv = "LUPINE_WORKLOAD_ID"
	remotePodUIDEnv     = "HAMI_REMOTE_POD_UID"

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
	RemoteGPUResourceCount           string
	RemoteGPUResourceMemory          string
	RemoteGPUClientImage             string
	RemoteGPULibImage                string
	RemoteGPUSessionImage            string
	RemoteGPUSessionImagePullSecrets []string

	errNoClient               = errors.New("kubernetes client is not initialized")
	errNoRegistration         = errors.New("node has no decodable GPU registration")
	errNoPool                 = errors.New("no lupine server available in the cluster")
	errNoMatchingLupineServer = errors.New("no Lupine server matches " + LupineServerSelectorAnno)
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
	RemoteGPUClientImage = config.ClientImage
	RemoteGPUSessionImage = config.SessionImage
	RemoteGPUSessionImagePullSecrets = append([]string(nil), config.SessionImagePullSecrets...)
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
// The shared device.CheckHealth judges a node by the device resource in its
// Allocatable. A client node runs no device plugin and so advertises none, and
// the lupine fleet's own liveness is already covered by the pool refresh, so
// using the shared check here would evict the whole pool.
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

// MutateAdmission wires LUPINE_SERVER and CUDA_VISIBLE_DEVICES to the
// annotations the scheduler writes in PatchAnnotations. A literal value in the
// pod spec is replaced: neither the endpoint nor the cards are known until
// placement.
func (dev *RemoteGPUDevices) MutateAdmission(ctr *corev1.Container, pod *corev1.Pod) (bool, error) {
	// The webhook visits every init container before app containers. An init
	// container may have caused addLibDelivery to append the library copy.
	// Move it after app mutation, when the copy also exists for a single app,
	// and after the init loop so its indexes stay valid.
	if pod != nil {
		for i := range pod.Spec.Containers {
			if ctr == &pod.Spec.Containers[i] {
				defer moveLibDeliveryFirst(pod)
				break
			}
		}
	}
	if _, ok := resourceValue(ctr, RemoteGPUResourceCount); !ok {
		return false, nil
	}
	if err := armClientLibraries(ctr, pod); err != nil {
		return false, err
	}
	needsLib, err := armMemoryLimit(ctr, pod)
	if err != nil {
		return false, err
	}
	for _, wire := range [][2]string{{lupineServerEnv, LupineServerAnno}, {visibleDevicesEnv, VisibleDevicesAnnoPrefix + ctr.Name}} {
		env, anno := wire[0], wire[1]
		setEnv(ctr, corev1.EnvVar{
			Name: env,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					APIVersion: "v1",
					FieldPath:  fmt.Sprintf("metadata.annotations['%s']", anno),
				},
			},
		})
	}
	// Kubelet expands the UID before launching the container. Admission cannot
	// know the UID of a new Pod; container names are stable across restarts.
	// A value the user already set to something else would make Lupine report
	// an identity the monitor cannot match, so admission refuses it.
	if err := rejectForeignWorkloadIdentity(ctr); err != nil {
		return false, err
	}
	if pod != nil {
		if pod.Labels == nil {
			pod.Labels = map[string]string{}
		}
		pod.Labels[RemoteGPUPodLabel] = "true"
	}
	filtered := ctr.Env[:0]
	for _, env := range ctr.Env {
		if env.Name != remotePodUIDEnv && env.Name != lupineWorkloadIDEnv {
			filtered = append(filtered, env)
		}
	}
	ctr.Env = append(filtered, corev1.EnvVar{
		Name: remotePodUIDEnv,
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "metadata.uid"},
		},
	}, corev1.EnvVar{Name: lupineWorkloadIDEnv, Value: "$(" + remotePodUIDEnv + ")/" + ctr.Name})
	// Last, because it appends to pod.Spec.InitContainers and ctr may point
	// into that slice: the webhook hands out &pod.Spec.InitContainers[i] for an
	// init container that asks for a card. Appending can move the backing
	// array, and every write above would then land in the abandoned one.
	if needsLib {
		addLibDelivery(pod)
	}
	if clientInjectionEnabled(pod) {
		addClientDelivery(pod)
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
		if existing.Value != "" {
			// Reinvocation must not add a second copy of HAMi-core.
			var other []string
			for entry := range strings.FieldsSeq(existing.Value) {
				if entry != preload {
					other = append(other, entry)
				}
			}
			if len(other) > 0 {
				preload += " " + strings.Join(other, " ")
			}
		}
	}
	mountLib(ctr)
	setEnv(ctr, corev1.EnvVar{Name: ldPreloadEnv, Value: preload})
	// The unindexed limit is HAMi-core's fallback for every device, which is
	// what a request spread evenly over the allocated cards means here.
	setEnv(ctr, corev1.EnvVar{Name: memoryLimitEnv, Value: fmt.Sprintf("%vm", mem)})
	// HAMi-core prefers a per-device limit to that fallback. One the workload
	// sets, by env or envFrom, would let it past its share of a card other pods
	// use, so each device the container sees gets the same limit explicitly.
	count, _ := resourceValue(ctr, RemoteGPUResourceCount)
	for i := range count {
		setEnv(ctr, corev1.EnvVar{Name: fmt.Sprintf("%s_%d", memoryLimitEnv, i), Value: fmt.Sprintf("%vm", mem)})
	}
	// The scheduler divides a shared card by memory only, and HAMi-core
	// defaults an unset SM limit to the whole device, so no core limit is set.
	//
	// A per-container cache rather than the node-wide one the device plugin
	// uses: pods sharing a card run on different nodes and cannot share a
	// file. Each container holds itself to its own limit, and the scheduler
	// keeps the limits on a card within the card. Containers of one pod share
	// the delivery volume, and one file there would hold them to a single
	// limit between them.
	setEnv(ctr, corev1.EnvVar{Name: sharedCacheEnv, Value: libMountPath + "/" + ctr.Name + ".cache"})
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

// moveLibDeliveryFirst runs the generated library copy before every other init
// container. Only a pod that asks for a remote GPU has one, so a user's own
// container with the same name and command is left where it is.
func moveLibDeliveryFirst(pod *corev1.Pod) {
	if !requestsRemoteGPU(pod) {
		return
	}
	for i := range pod.Spec.InitContainers {
		lib := pod.Spec.InitContainers[i]
		if lib.Name == libVolumeName && len(lib.Command) == 3 &&
			lib.Command[0] == "sh" && lib.Command[1] == "-c" &&
			lib.Command[2] == "cp "+libSourceGlob+" "+libMountPath+"/libvgpu.so" {
			if i > 0 {
				copy(pod.Spec.InitContainers[1:i+1], pod.Spec.InitContainers[:i])
				pod.Spec.InitContainers[0] = lib
			}
			return
		}
	}
}

// requestsRemoteGPU mirrors when armMemoryLimit asks for the library copy: a
// remote GPU count together with a memory limit.
func requestsRemoteGPU(pod *corev1.Pod) bool {
	if RemoteGPULibImage == "" {
		return false
	}
	for _, ctrs := range [][]corev1.Container{pod.Spec.InitContainers, pod.Spec.Containers} {
		for i := range ctrs {
			if _, ok := resourceValue(&ctrs[i], RemoteGPUResourceCount); !ok {
				continue
			}
			if mem, ok := resourceValue(&ctrs[i], RemoteGPUResourceMemory); ok && mem > 0 {
				return true
			}
		}
	}
	return false
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
	var memreq int64
	if mem, ok := resourceValue(ctr, RemoteGPUResourceMemory); ok && mem > 0 {
		memreq = mem
	}
	return device.ContainerDeviceRequest{
		Nums: count,
		Type: RemoteGPUCommonWord,
		// Memreq shares a card when HAMi-core can enforce it, and only filters
		// cards otherwise. Cores are never divided.
		Memreq:   memreq,
		Coresreq: 100,
	}, nil
}

// WaitForFleet remembers a pod the scheduler could not place, so that the
// pool requeues it once the lupine fleet changes. Fit is not enough for this:
// with no lupine server registered, no node offers a card and Fit never runs.
// rev is the FleetRevision the placement attempt ran against; a pod refused on
// an older fleet is requeued at once instead of waiting for the next change.
func (dev *RemoteGPUDevices) WaitForFleet(pod *corev1.Pod, rev uint64) {
	dev.pool.wait(pod, rev)
}

// FleetRevision changes whenever the lupine fleet does.
func (dev *RemoteGPUDevices) FleetRevision() uint64 {
	return dev.pool.revision()
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
	var booked device.ContainerDevices
	for i, ctrDevs := range devList {
		visible := make([]string, 0, len(ctrDevs))
		for _, d := range ctrDevs {
			allocated = append(allocated, d.UUID)
			booked = append(booked, d)
			visible = append(visible, strconv.Itoa(d.Idx))
		}
		// Each container sees only its own cards. Allocations are indexed
		// by container, init containers first, the same way they were fitted.
		if name := containerName(pod, i); name != "" && len(ctrDevs) > 0 {
			(*annoinput)[VisibleDevicesAnnoPrefix+name] = strings.Join(visible, ",")
		}
	}
	dev.pool.book(podKey(pod), podHostname(pod), booked)

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

// containerName names the container at position i of a pod's allocation,
// counting init containers first.
func containerName(pod *corev1.Pod, i int) string {
	if pod == nil || i < 0 {
		return ""
	}
	if i < len(pod.Spec.InitContainers) {
		return pod.Spec.InitContainers[i].Name
	}
	if i -= len(pod.Spec.InitContainers); i < len(pod.Spec.Containers) {
		return pod.Spec.Containers[i].Name
	}
	return ""
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
// booking back here frees its cards in that moment rather than at the end of
// heldTTL.
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

// Fit hands out cards from one lupine server.
//
// Several pods can use one server: each sees only its own cards, through
// CUDA_VISIBLE_DEVICES. A request that names its memory shares a card with
// other such requests when HAMi-core can hold each to its limit; one that does
// not takes whole cards. A pod never spans two servers, because each client
// container is pointed at exactly one, which is why a request for more cards
// than any single server has free goes unfilled even when the fleet holds
// enough.
func (dev *RemoteGPUDevices) Fit(devices []*device.DeviceUsage, request device.ContainerDeviceRequest, pod *corev1.Pod, _ *device.NodeInfo, allocated *device.PodDevices) (bool, map[string]device.ContainerDevices, string) {
	selector, err := lupineServerSelector(pod)
	if err != nil {
		return false, map[string]device.ContainerDevices{}, err.Error()
	}
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
	if selector != nil && len(servers) == 0 {
		return false, map[string]device.ContainerDevices{}, errNoPool.Error()
	}
	servers = dev.pool.matchingServers(selector, servers)
	if selector != nil && len(servers) == 0 {
		return false, map[string]device.ContainerDevices{}, errNoMatchingLupineServer.Error()
	}

	// A previous container fixes this pod to one server. HAMi currently
	// injects one LUPINE_SERVER endpoint into each client container, so a
	// later container must use cards served by that same endpoint.
	prior, own := committedAllocation(allocated, pod)
	requirements := []phasedRequest{{request: request}}
	if prior != "" {
		if len(dev.pool.matchingServers(selector, []string{prior})) == 0 {
			return false, map[string]device.ContainerDevices{}, errNoMatchingLupineServer.Error()
		}
		servers = []string{prior}
	} else if podRequests, err := dev.podRequests(pod); err != nil {
		return false, map[string]device.ContainerDevices{}, err.Error()
	} else if len(podRequests) > 0 {
		// The first container chooses the endpoint for the whole pod. Qualify
		// candidates against each container's request independently so a later
		// init, sidecar, or app container cannot invalidate that choice.
		requirements = podRequests
	}

	fit, tmpDevs, reason, contended := dev.tryFit(byServer, servers, request, requirements, pod, own)
	if !fit && contended {
		// Only other pods stood in the way, and what they hold comes from a
		// snapshot up to poolTTL old: a card freed moments ago still reads as
		// taken. Refusing here is expensive, because kube-scheduler parks the
		// pod until its next periodic flush of the unschedulable queue,
		// minutes away. Pay for a fresh read instead of guessing wrong.
		dev.pool.refreshNow(context.Background())
		// Only the servers the refresh left intact are retried. If none are,
		// the first refusal stands rather than becoming a refusal with nothing
		// to say: the pod cannot be placed either way, and kube-scheduler
		// still needs a reason for it.
		cards, _ := dev.pool.allocationSnapshot("")
		if intact := dev.pool.matchingServers(selector, dev.serversStillWhole(byServer, servers, cards)); len(intact) > 0 {
			fit, tmpDevs, reason, _ = dev.tryFit(byServer, intact, request, requirements, pod, own)
		}
	}
	if fit {
		return true, tmpDevs, ""
	}
	return false, tmpDevs, common.GenReason(reason, len(devices))
}

// ValidateReservedServer verifies that a reserved RemoteGPU allocation is
// still booked and still belongs to a server selected by the Pod. Bind calls
// this after Filter has recorded the allocation, before Kubernetes commits the
// Pod to its node.
func (dev *RemoteGPUDevices) ValidateReservedServer(pod *corev1.Pod, allocated device.PodDevices) error {
	selector, err := lupineServerSelector(pod)
	if err != nil {
		return err
	}
	server, _ := committedAllocation(&allocated, pod)
	if server == "" {
		if selector != nil && PodRequestsRemoteGPU(pod) {
			return fmt.Errorf("no Lupine server is reserved for RemoteGPU pod")
		}
		return nil
	}
	if selector != nil {
		if err := dev.reservedServerMatches(selector, server); err != nil {
			return err
		}
	}
	// The booking is what keeps the pod's cards from other pods until it is
	// bound. One that ran out before Bind, after heldTTL, may have let another
	// pod onto them, so the pod is fitted again instead.
	if !dev.pool.booked(podKey(pod)) {
		return fmt.Errorf("the RemoteGPU booking for this pod has expired before Bind")
	}
	return nil
}

// reservedServerMatches checks the reserved server against the pod's selector.
func (dev *RemoteGPUDevices) reservedServerMatches(selector labels.Selector, server string) error {
	// After a scheduler restart the pool may not yet have been populated by
	// registration. Load it before deciding that a reserved server mismatches.
	dev.pool.mu.Lock()
	cold := dev.pool.fetchedAt.IsZero() && len(dev.pool.serverLabels) == 0
	dev.pool.mu.Unlock()
	if cold {
		dev.pool.refresh(context.Background())
	}
	if len(dev.pool.matchingServers(selector, []string{server})) == 0 {
		return fmt.Errorf("reserved Lupine server %q no longer matches %s", server, LupineServerSelectorAnno)
	}
	return nil
}

// lupineServerSelector parses the optional pod-level server selector. The
// feature deliberately accepts one equality requirement only: RemoteGPU
// server pools are named groups, not a general placement policy language.
func lupineServerSelector(pod *corev1.Pod) (labels.Selector, error) {
	if pod == nil || pod.Annotations == nil {
		return nil, nil
	}
	raw, found := pod.Annotations[LupineServerSelectorAnno]
	if !found {
		return nil, nil
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("invalid %s: must not be empty", LupineServerSelectorAnno)
	}
	selector, err := labels.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", LupineServerSelectorAnno, err)
	}
	requirements, selectable := selector.Requirements()
	if !selectable || len(requirements) != 1 || (requirements[0].Operator() != selection.Equals && requirements[0].Operator() != selection.DoubleEquals) {
		return nil, fmt.Errorf("invalid %s: must be one equality selector such as gpu-pool=inference", LupineServerSelectorAnno)
	}
	return selector, nil
}

// ValidateLupineServerSelector validates the optional server-pool annotation
// without mutating the Pod. The admission webhook also uses it for updates.
func ValidateLupineServerSelector(pod *corev1.Pod) error {
	_, err := lupineServerSelector(pod)
	return err
}

// PodRequestsRemoteGPU reports whether any container requests a RemoteGPU.
// It is used by update admission to preserve the create-time rule: an
// unrelated Pod may carry no RemoteGPU selector semantics at all.
func PodRequestsRemoteGPU(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	for i := range pod.Spec.InitContainers {
		if count, found := resourceValue(&pod.Spec.InitContainers[i], RemoteGPUResourceCount); found && count > 0 {
			return true
		}
	}
	for i := range pod.Spec.Containers {
		if count, found := resourceValue(&pod.Spec.Containers[i], RemoteGPUResourceCount); found && count > 0 {
			return true
		}
	}
	return false
}

// phasedRequest is one container's request and when it runs. An ordinary
// init container runs alone, beside the sidecars started before it, and is
// done before the next container starts. Sidecars and app containers run
// together.
type phasedRequest struct {
	request      device.ContainerDeviceRequest
	ordinaryInit bool
}

// ordinaryInit reports whether allocation entry i of a pod, which counts init
// containers first, is an init container that runs to completion rather than
// a sidecar.
func ordinaryInit(pod *corev1.Pod, i int) bool {
	return pod != nil && i >= 0 && i < len(pod.Spec.InitContainers) && !util.IsSidecarContainer(&pod.Spec.InitContainers[i])
}

// podRequests returns every RemoteGPU request in the order the containers
// start, without merging container boundaries. Native sidecars live in
// InitContainers, so walking both pod container lists covers ordinary init
// containers, sidecars, and app containers while preserving each request's
// count and memory combination.
func (dev *RemoteGPUDevices) podRequests(pod *corev1.Pod) ([]phasedRequest, error) {
	if pod == nil {
		return nil, nil
	}
	requests := make([]phasedRequest, 0, len(pod.Spec.InitContainers)+len(pod.Spec.Containers))
	add := func(ctr *corev1.Container, ordinary bool) error {
		request, err := dev.GenerateResourceRequests(ctr)
		if err != nil {
			return err
		}
		if request.Nums > 0 {
			requests = append(requests, phasedRequest{request: request, ordinaryInit: ordinary})
		}
		return nil
	}
	for i := range pod.Spec.InitContainers {
		if err := add(&pod.Spec.InitContainers[i], ordinaryInit(pod, i)); err != nil {
			return nil, err
		}
	}
	for i := range pod.Spec.Containers {
		if err := add(&pod.Spec.Containers[i], false); err != nil {
			return nil, err
		}
	}
	return requests, nil
}

// committedAllocation returns the lupine server already chosen by a previous
// container of this pod, and what those containers hold of each card while
// the next one runs. An ordinary init container is done by then, so it fixes
// the server but holds nothing.
func committedAllocation(allocated *device.PodDevices, pod *corev1.Pod) (string, map[string]share) {
	if allocated == nil {
		return "", nil
	}
	var server string
	own := map[string]share{}
	for i, ctrList := range (*allocated)[RemoteGPUCommonWord] {
		for _, d := range ctrList {
			if s := serverOf(d.UUID); s != "" {
				if server == "" {
					server = s
				}
				if !ordinaryInit(pod, i) {
					own[d.UUID] = own[d.UUID].add(grantShare(d))
				}
			}
		}
	}
	return server, own
}

// serversStillWhole drops servers the pool no longer has, or no longer has
// every card of.
//
// A refresh can clear the bookings that made tryFit refuse, but it can also
// remove a server or one of its cards. Retrying with the pre-refresh view
// would then read a card the fleet no longer has as merely unbooked, and
// PatchAnnotations would find no endpoint for the server naming it. A card the
// server gained does not matter: cards are handed out one by one.
func (dev *RemoteGPUDevices) serversStillWhole(byServer map[string][]*device.DeviceUsage, servers []string, live map[string]device.DeviceInfo) []string {
	kept := make([]string, 0, len(servers))
	for _, server := range servers {
		if _, ok := dev.pool.endpoint(server); !ok {
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

// shareable reports whether a request may take part of a card. It must name
// its memory, and HAMi-core must be delivered to hold it to that; without the
// library the memory request is only a filter and the card is taken whole.
func shareable(request device.ContainerDeviceRequest) bool {
	return request.Memreq > 0 && RemoteGPULibImage != ""
}

// cardState is one card as a request sees it.
type cardState struct {
	d *device.DeviceUsage
	// others is what other pods hold. own is what earlier containers of this
	// pod hold, which the request may share without limit on users.
	others cardUsage
	own    share
}

func (c cardState) usedMem() int64 {
	if c.own.whole {
		return c.d.Totalmem
	}
	return c.others.mem + c.own.mem
}

// check reports why a card cannot serve one container of the request, or ""
// when it can. contended says the reason is something another pod holds,
// which a fresh read of the pool may show has gone.
func (c cardState) check(request device.ContainerDeviceRequest) (reason string, contended bool) {
	switch {
	case !c.d.Health:
		return common.CardNotHealth, false
	case c.others.foreign:
		return common.ExclusiveDeviceAllocateConflict, true
	case request.Memreq > 0 && c.d.Totalmem < request.Memreq:
		return common.CardInsufficientMemory, false
	case c.others.users == 0 && c.own.whole:
		// An earlier container of this pod took the card whole. It is the
		// pod's alone, and its containers may share it as they like.
		return "", false
	case !shareable(request):
		// A whole card: nobody else may be on it.
		if c.others.users > 0 {
			return common.ExclusiveDeviceAllocateConflict, true
		}
	case c.others.whole:
		// Another pod took the card whole. Its grant records the card's
		// memory when it was made, which a larger card now would read as
		// room to share.
		return common.ExclusiveDeviceAllocateConflict, true
	case int64(c.others.users+c.own.users) >= c.d.Count:
		return common.CardTimeSlicingExhausted, c.others.users > 0
	case c.d.Totalmem-c.usedMem() < request.Memreq:
		return common.CardInsufficientMemory, c.others.users > 0
	}
	return "", false
}

// grant is what one container is given of a card.
func grant(c cardState, request device.ContainerDeviceRequest) device.ContainerDevice {
	if shareable(request) {
		return device.ContainerDevice{Idx: int(c.d.Index), UUID: c.d.ID, Type: request.Type, Usedmem: request.Memreq}
	}
	return device.ContainerDevice{Idx: int(c.d.Index), UUID: c.d.ID, Type: request.Type, Usedmem: c.d.Totalmem, Usedcores: c.d.Totalcore}
}

// pick returns the cards one container of the request would be given, or nil
// when the server cannot serve it, and how many cards could serve it. reason,
// when not nil, collects why the others were passed over. Cards this pod
// already uses come first, then the fullest, so a shared request fills one
// card before it starts on the next and whole cards stay whole.
func pick(cards []cardState, request device.ContainerDeviceRequest, reason map[string]int) (picked []int, usableCards int, contended bool) {
	usable := make([]int, 0, len(cards))
	for i, c := range cards {
		why, busy := c.check(request)
		if why != "" {
			if reason != nil {
				reason[why]++
			}
			contended = contended || busy
			continue
		}
		usable = append(usable, i)
	}
	if int64(len(usable)) < request.Nums {
		return nil, len(usable), contended
	}
	sort.SliceStable(usable, func(i, j int) bool {
		a, b := cards[usable[i]], cards[usable[j]]
		if (a.own.users > 0) != (b.own.users > 0) {
			return a.own.users > 0
		}
		if fa, fb := a.d.Totalmem-a.usedMem(), b.d.Totalmem-b.usedMem(); fa != fb {
			return fa < fb
		}
		return a.d.Index < b.d.Index
	})
	return usable[:request.Nums], len(usable), contended
}

// tryFit picks the server and cards the request should come from. It reads
// pool reservations, so the same call can answer differently before and after
// a refresh.
func (dev *RemoteGPUDevices) tryFit(byServer map[string][]*device.DeviceUsage, servers []string, request device.ContainerDeviceRequest, requirements []phasedRequest, pod *corev1.Pod, own map[string]share) (bool, map[string]device.ContainerDevices, map[string]int, bool) {
	tmpDevs := map[string]device.ContainerDevices{}
	reason := map[string]int{}
	contended := false
	// Usage may predate a pool refresh, including the forced refresh in Fit.
	// Missing or changed cards and failed reservation reads must not be
	// treated as free just because the caller's cached usage still reports
	// them healthy. Cards and holders are read under one lock.
	live, holders := dev.pool.allocationSnapshot(podKey(pod))
	whole := map[string]bool{}
	for _, server := range dev.serversStillWhole(byServer, servers, live) {
		whole[server] = true
	}

	type candidate struct {
		server string
		cards  []cardState
		picked []int // the cards this container gets
		usable int   // cards that could serve it
	}
	candidates := make([]candidate, 0, len(servers))
	for _, server := range servers {
		if !whole[server] {
			reason[common.CardNotHealth] += len(byServer[server])
			continue
		}
		c := candidate{server: server, cards: make([]cardState, 0, len(byServer[server]))}
		for _, d := range byServer[server] {
			// What the card offers comes from the same read as who holds it.
			// The caller's copy may predate a refresh that changed its split
			// count or memory, and sharing by those would overcommit it.
			lc := live[d.ID]
			cur := *d
			cur.Index, cur.Count, cur.Totalmem, cur.Totalcore = lc.Index, lc.Count, lc.Devmem, lc.Devcore
			cur.Health = d.Health && lc.Health
			d = &cur
			st := cardState{d: d, others: holders[d.ID], own: own[d.ID]}
			// The node's usage view covers pods on this client node, which the
			// pool counts too, plus this pod's earlier containers. Taking the
			// larger of the two views never counts a pod twice.
			ownMem := max(st.own.mem, 0)
			st.others.users = max(st.others.users, int(d.Used)-st.own.users)
			st.others.mem = max(st.others.mem, d.Usedmem-ownMem)
			c.cards = append(c.cards, st)
		}
		picked, usable, busy := pick(c.cards, request, reason)
		contended = contended || busy
		fits, busy := podFits(c.cards, requirements)
		contended = contended || busy
		if picked == nil || !fits {
			reason[common.NodeInsufficientDevice]++
			continue
		}
		c.picked, c.usable = picked, usable
		candidates = append(candidates, c)
	}

	// The server with the fewest usable cards that still serves the request
	// leaves the deeper servers for requests that need them. servers arrives
	// sorted by name and the sort is stable, so equal servers are picked the
	// same way on every call, which repeated Filter calls for one pod rely on.
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].usable < candidates[j].usable
	})
	if len(candidates) == 0 {
		return false, tmpDevs, reason, contended
	}
	c := candidates[0]
	for _, i := range c.picked {
		tmpDevs[request.Type] = append(tmpDevs[request.Type], grant(c.cards[i], request))
	}
	klog.V(4).InfoS("remotegpu: allocated from a lupine server",
		"pod", klog.KObj(pod), "server", c.server, "cards", request.Nums, "shared", shareable(request))
	return true, tmpDevs, reason, contended
}

// podFits reports whether the server can serve every container request of the
// pod, as the later containers will be fitted: in order, each on the cards
// the ones still running before it left. Capacity is checked at the pod's
// peak, so an ordinary init container is fitted beside the sidecars before it
// and then gives its cards back. Requests are deliberately not collapsed: a
// server with one large and one small card can satisfy 1x large-memory and 2x
// low-memory requests even though it cannot satisfy a synthetic 2x
// large-memory request.
//
// It also reports whether another pod stood in the way of a request, which a
// fresh read of the pool may show has gone.
func podFits(cards []cardState, requests []phasedRequest) (fits, contended bool) {
	cards = slices.Clone(cards)
	for _, r := range requests {
		picked, _, busy := pick(cards, r.request, nil)
		if picked == nil {
			return false, busy
		}
		if r.ordinaryInit {
			continue
		}
		for _, i := range picked {
			cards[i].own = cards[i].own.add(grantShare(grant(cards[i], r.request)))
		}
	}
	return true, false
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

// rejectForeignWorkloadIdentity refuses a workload identity admission would
// otherwise replace. The monitor matches Lupine's client_id to the Pod UID
// and container name, so a different value cannot be attributed.
func rejectForeignWorkloadIdentity(ctr *corev1.Container) error {
	wantID := "$(" + remotePodUIDEnv + ")/" + ctr.Name
	for _, env := range ctr.Env {
		switch env.Name {
		case remotePodUIDEnv:
			if !canonicalPodUIDEnv(env) {
				return fmt.Errorf("container %q sets %s; leave it unset so admission can use the Pod UID", ctr.Name, remotePodUIDEnv)
			}
		case lupineWorkloadIDEnv:
			if env.ValueFrom != nil || env.Value != wantID {
				return fmt.Errorf("container %q sets %s to %q; leave it unset so admission can use the Pod UID and container name", ctr.Name, lupineWorkloadIDEnv, env.Value)
			}
		}
	}
	return nil
}

func canonicalPodUIDEnv(env corev1.EnvVar) bool {
	if env.Value != "" || env.ValueFrom == nil || env.ValueFrom.FieldRef == nil {
		return false
	}
	src := env.ValueFrom
	if src.ConfigMapKeyRef != nil || src.SecretKeyRef != nil || src.ResourceFieldRef != nil {
		return false
	}
	return src.FieldRef.FieldPath == "metadata.uid"
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
