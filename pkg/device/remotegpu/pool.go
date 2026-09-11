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
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

const (
	// LupineServerLabel marks a GPU node as serving its GPUs through lupine.
	// The label value is the lupine listen port; an empty value means
	// RemoteGPUConfig.DefaultPort.
	LupineServerLabel = "hami.io/lupine-server"

	// nvidiaRegisterAnnos is where the stock HAMi NVIDIA device plugin already
	// publishes a node's GPUs. A lupine node runs that plugin unchanged, so the
	// pool needs no node-side component of its own.
	nvidiaRegisterAnnos = "hami.io/node-nvidia-register"

	// deviceIDSeparator joins the lupine node name to the physical GPU UUID.
	// It must stay out of the "," ":" ";" set that the pod annotation codec in
	// pkg/device uses as field and record separators. Node names are DNS-1123
	// subdomains, so they can never contain it either.
	deviceIDSeparator = "/"

	poolTTL = 15 * time.Second

	// heldTTL bounds how long a booking survives without the pod that caused
	// it showing up in the pod list. Two refresh cycles is enough for a bind
	// that worked; anything older means the bind did not.
	heldTTL = 2 * poolTTL

	// forceInterval rate limits the pod list a failing Fit may ask for. One
	// scheduling attempt calls Fit once per candidate node, and they all fail
	// for the same reason, so without this a single unschedulable pod would
	// list every pod in the cluster once per node.
	forceInterval = time.Second
)

// Swappable for tests, mirroring how plugin/server.go injects getPendingPod.
var (
	listLupineNodes = apiListLupineNodes
	listPods        = apiListPods
)

func apiListLupineNodes(ctx context.Context) ([]corev1.Node, error) {
	c := client.GetClient()
	if c == nil {
		return nil, errNoClient
	}
	list, err := c.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: LupineServerLabel})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

func apiListPods(ctx context.Context) ([]corev1.Pod, error) {
	c := client.GetClient()
	if c == nil {
		return nil, errNoClient
	}
	// ponytail: full pod list, filtered client side. Annotations are not
	// selectable server side. Swap for a shared informer if this ever runs on a
	// cluster where listing every pod every poolTTL is too expensive.
	list, err := c.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

// pool is the cluster-wide view of the lupine GPU fleet.
//
// It exists because the Devices interface is node scoped: GetNodeDevices is
// handed the *client* node, which owns none of these GPUs. Every client node
// therefore gets the same snapshot, and the scheduler's per-node usage
// accounting in getNodesUsage cannot see a reservation made on behalf of a pod
// that landed on a different client node. reserved() closes that gap by reading
// the allocation back from pod annotations cluster wide.
type pool struct {
	mu          sync.Mutex
	defaultPort int

	fetchedAt time.Time
	forcedAt  time.Time
	endpoints map[string]string // lupine node name -> "host:port"
	devices   []*device.DeviceInfo
	inUse     map[string]struct{}  // device ID held by a live pod
	held      map[string]time.Time // device ID booked since the last refresh
}

func newPool(defaultPort int) *pool {
	return &pool{
		defaultPort: defaultPort,
		endpoints:   map[string]string{},
		inUse:       map[string]struct{}{},
		held:        map[string]time.Time{},
	}
}

// deviceID names a GPU by the lupine node serving it, so that Fit can group
// candidates by server and PatchAnnotations can resolve the endpoint.
func deviceID(nodeName, uuid string) string {
	return nodeName + deviceIDSeparator + uuid
}

// serverOf returns the lupine node name encoded in a device ID.
func serverOf(id string) string {
	name, _, found := strings.Cut(id, deviceIDSeparator)
	if !found {
		return ""
	}
	return name
}

func (p *pool) refresh(ctx context.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.fetchedAt.IsZero() && time.Since(p.fetchedAt) < poolTTL {
		return
	}

	nodes, err := listLupineNodes(ctx)
	if err != nil {
		// Keep the last good snapshot: a transient API error must not empty the
		// pool and make every remote-gpu pod unschedulable.
		klog.ErrorS(err, "remotegpu: failed to list lupine server nodes, keeping previous pool")
		return
	}

	endpoints := make(map[string]string, len(nodes))
	var devices []*device.DeviceInfo
	for i := range nodes {
		n := &nodes[i]
		endpoint, ok := p.endpointOf(n)
		if !ok {
			continue
		}
		gpus, err := decodeNodeGPUs(n)
		if err != nil {
			klog.V(4).InfoS("remotegpu: skipping lupine node with undecodable GPU registration",
				"node", n.Name, "error", err)
			continue
		}
		endpoints[n.Name] = endpoint
		devices = append(devices, gpus...)
	}

	p.endpoints = endpoints
	p.devices = devices
	// Listing every pod is only worth it once a lupine fleet actually exists.
	if len(endpoints) == 0 {
		p.inUse = map[string]struct{}{}
	} else {
		p.inUse = p.reservations(ctx)
	}
	p.fetchedAt = time.Now()
	// A hold is only needed until the pod list catches up with it. Drop the
	// ones it has, and the ones so old that the pod they were taken for is
	// never going to appear, so a failed bind cannot leak a card forever.
	for id, at := range p.held {
		if _, seen := p.inUse[id]; seen || time.Since(at) > heldTTL {
			delete(p.held, id)
		}
	}
	klog.V(4).InfoS("remotegpu: pool refreshed",
		"servers", len(endpoints), "devices", len(devices), "reserved", len(p.inUse))
}

func (p *pool) endpointOf(n *corev1.Node) (string, bool) {
	host := ""
	for _, addr := range n.Status.Addresses {
		if addr.Type == corev1.NodeInternalIP && addr.Address != "" {
			host = addr.Address
			break
		}
	}
	if host == "" {
		klog.V(4).InfoS("remotegpu: lupine node has no InternalIP", "node", n.Name)
		return "", false
	}
	port := p.defaultPort
	if raw := n.Labels[LupineServerLabel]; raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 65535 {
			klog.ErrorS(err, "remotegpu: invalid lupine port label, falling back to default",
				"node", n.Name, "value", raw, "default", p.defaultPort)
		} else {
			port = parsed
		}
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), true
}

func decodeNodeGPUs(n *corev1.Node) ([]*device.DeviceInfo, error) {
	raw, ok := n.Annotations[nvidiaRegisterAnnos]
	if !ok {
		return nil, errNoRegistration
	}
	gpus, err := device.UnMarshalNodeDevices(raw)
	if err != nil {
		return nil, err
	}
	out := make([]*device.DeviceInfo, 0, len(gpus))
	for _, gpu := range gpus {
		if gpu == nil {
			continue
		}
		dup := *gpu
		dup.ID = deviceID(n.Name, gpu.ID)
		dup.DeviceVendor = RemoteGPUCommonWord
		dup.Type = RemoteGPUCommonWord
		// Whole-server allocation: the pool hands out entire cards, so a card
		// is never split and Count stays 1 regardless of the NVIDIA plugin's
		// configured deviceSplitCount.
		dup.Count = 1
		out = append(out, &dup)
	}
	if len(out) == 0 {
		return nil, errNoRegistration
	}
	return out, nil
}

// reservations reads back every allocation the scheduler has already written
// into a pod annotation, so a device booked for a pod on one client node is not
// handed out again to a pod landing on another.
func (p *pool) reservations(ctx context.Context) map[string]struct{} {
	pods, err := listPods(ctx)
	if err != nil {
		klog.ErrorS(err, "remotegpu: failed to list pods, keeping previous reservations")
		return p.inUse
	}
	held := map[string]struct{}{}
	for i := range pods {
		pod := &pods[i]
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		raw, ok := pod.Annotations[AllocatedAnnos]
		if !ok || raw == "" {
			continue
		}
		for ctr := range strings.SplitSeq(raw, device.OnePodMultiContainerSplitSymbol) {
			devs, err := device.DecodeContainerDevices(ctr)
			if err != nil {
				klog.V(4).InfoS("remotegpu: skipping undecodable allocation annotation",
					"pod", klog.KObj(pod), "error", err)
				continue
			}
			for _, d := range devs {
				held[d.UUID] = struct{}{}
			}
		}
	}
	return held
}

// snapshot returns the pool as DeviceInfo for GetNodeDevices.
func (p *pool) snapshot(ctx context.Context) []*device.DeviceInfo {
	p.refresh(ctx)
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*device.DeviceInfo, 0, len(p.devices))
	for _, d := range p.devices {
		dup := d.DeepCopy()
		out = append(out, &dup)
	}
	return out
}

// refreshNow re-reads the fleet without waiting for poolTTL to lapse, so a
// caller about to reject a pod over a booking can be sure the booking is real.
// It is rate limited to one read per forceInterval; a caller that hits the
// limit is already looking at data that fresh.
func (p *pool) refreshNow(ctx context.Context) {
	p.mu.Lock()
	if !p.forcedAt.IsZero() && time.Since(p.forcedAt) < forceInterval {
		p.mu.Unlock()
		return
	}
	p.forcedAt = time.Now()
	p.fetchedAt = time.Time{}
	p.mu.Unlock()
	p.refresh(ctx)
}

// hold books devices the scheduler has just allocated. reservations() only
// learns about an allocation once the pod carrying it turns up in a pod list,
// which is at most once per poolTTL; until then this is the only record that
// the card is spoken for.
func (p *pool) hold(ids ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	for _, id := range ids {
		p.held[id] = now
	}
}

// reserved reports whether a device is already held by a live pod anywhere in
// the cluster, or was booked by this scheduler since the last refresh.
func (p *pool) reserved(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.inUse[id]; ok {
		return true
	}
	_, ok := p.held[id]
	return ok
}

// endpoint returns the "host:port" a client should point LUPINE_SERVER at.
func (p *pool) endpoint(nodeName string) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ep, ok := p.endpoints[nodeName]
	return ep, ok
}

// isLupineNode reports whether the node serves the pool rather than consuming
// it. Such a node hosts lupine, not remote-gpu client pods.
func isLupineNode(n *corev1.Node) bool {
	_, ok := n.Labels[LupineServerLabel]
	return ok
}
