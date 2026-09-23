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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
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

	// poolFetchTimeout caps the API and server calls one refresh makes. The
	// client they go through carries no timeout by default, and both entry
	// points run under a scheduler lock: Fit under allocLock, GetNodeDevices
	// under the register lock. An apiserver that stops answering would
	// otherwise hold either for as long as it stays quiet.
	poolFetchTimeout = 10 * time.Second
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
	// ResourceVersion "0" serves the read from the apiserver's watch cache. A
	// pool refresh runs on the scheduling hot path, under a scheduler lock, and
	// a card that changed hands microseconds ago is already covered by held.
	list, err := c.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: LupineServerLabel, ResourceVersion: "0",
	})
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
	// ponytail: full pod list, filtered client side, served from the watch
	// cache. Annotations are not selectable server side. Swap for the
	// scheduler's own pod informer if this ever runs on a cluster where
	// listing every pod every poolTTL is too expensive; pkg/device cannot
	// reach that lister today.
	list, err := c.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{ResourceVersion: "0"})
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

	fetchedAt  time.Time
	forcedAt   time.Time
	refreshing bool              // a refresh is already fetching; do not start a second one
	refreshed  chan struct{}     // closed when the in-flight refresh installs its result
	fleetRev   uint64            // bumped whenever a refresh installs a different fleet
	endpoints  map[string]string // lupine node name -> "host:port"
	devices    []*device.DeviceInfo
	inUse      map[string]struct{} // device ID held by a live pod
	held       map[string]booking  // device ID booked since the last refresh
	busy       map[string]struct{} // device ID a server reports a client on
}

func newPool(defaultPort int) *pool {
	return &pool{
		defaultPort: defaultPort,
		endpoints:   map[string]string{},
		inUse:       map[string]struct{}{},
		held:        map[string]booking{},
		busy:        map[string]struct{}{},
	}
}

// booking records who a card was set aside for, so a scheduling attempt that
// falls through can hand it straight back instead of waiting out heldTTL.
type booking struct {
	at  time.Time
	pod types.UID
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

// refresh rebuilds the pool from the API server and the lupine fleet.
//
// The fetch itself (the node list, the pod list, and one call per lupine
// server) runs with the lock released: it is the slow, network-bound part,
// and holding the lock across it would stall every other pool operation
// (hold, reserved, endpoint, snapshot) for as long as the slowest or least
// reachable server takes to time out. Only the decision to fetch (the TTL and
// in-flight checks) and the swap of the new state in run under the lock.
func (p *pool) refresh(ctx context.Context) {
	p.mu.Lock()
	if !p.fetchedAt.IsZero() && time.Since(p.fetchedAt) < poolTTL {
		p.mu.Unlock()
		return
	}
	if p.refreshing {
		// Another goroutine is already fetching; its result will be at least
		// as fresh as anything a second concurrent fetch could produce.
		inFlight, coldStart := p.refreshed, p.fetchedAt.IsZero()
		p.mu.Unlock()
		if coldStart && inFlight != nil {
			// The pool has never held anything, so returning now would report
			// an empty fleet rather than one not read yet, and every caller
			// would see "no devices" instead of waiting a moment.
			select {
			case <-inFlight:
			case <-ctx.Done():
			}
		}
		return
	}
	p.refreshing = true
	done := make(chan struct{})
	p.refreshed = done
	previousInUse := p.inUse
	previousBusy := p.busy
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.refreshing = false
		p.refreshed = nil
		p.mu.Unlock()
		close(done)
	}()

	ctx, cancel := context.WithTimeout(ctx, poolFetchTimeout)
	defer cancel()

	nodes, err := listLupineNodes(ctx)
	if err != nil {
		// Keep the last good snapshot: a transient API error must not empty the
		// pool and make every remote-gpu pod unschedulable.
		//
		// Stamp the attempt all the same. register calls GetNodeDevices once
		// per node, and without this a failing or hanging apiserver would cost
		// one full poolFetchTimeout per node, all of it under the register
		// lock, instead of one attempt per poolTTL.
		p.mu.Lock()
		p.fetchedAt = time.Now()
		p.mu.Unlock()
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

	// Listing every pod is only worth it once a lupine fleet actually exists.
	var inUse map[string]struct{}
	// listed says the pod list actually ran. A booking is only expired against
	// a list that happened; expiring it against one that failed would hand a
	// bound pod's card to a second pod.
	listed := true
	if len(endpoints) == 0 {
		inUse = map[string]struct{}{}
	} else {
		inUse, listed = reservations(ctx, previousInUse)
	}
	// Ask every server concurrently: one unreachable server pays its own
	// metricsTimeout instead of adding it to everyone else's.
	busy := askServers(ctx, endpoints, previousBusy)

	p.mu.Lock()
	defer p.mu.Unlock()
	if !sameFleet(p.devices, devices) {
		p.fleetRev++
	}
	p.endpoints = endpoints
	p.devices = devices
	p.inUse = inUse
	p.busy = busy
	p.fetchedAt = time.Now()
	// A hold is only needed until the pod list catches up with it. Drop the
	// ones it has, and the ones so old that the pod they were taken for is
	// never going to appear, so a failed bind cannot leak a card forever.
	//
	// Only against a list that ran, though: a pod list that failed has seen
	// nothing, so ageing a booking out on its word would free the card of a
	// pod that did bind and let a second pod onto it.
	if listed {
		for id, b := range p.held {
			if _, seen := p.inUse[id]; seen || time.Since(b.at) > heldTTL {
				delete(p.held, id)
			}
		}
	}
	klog.V(4).InfoS("remotegpu: pool refreshed",
		"servers", len(endpoints), "devices", len(devices),
		"reserved", len(p.inUse), "busyOnServer", len(p.busy))
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
		switch {
		case err != nil:
			klog.ErrorS(err, "remotegpu: unreadable lupine port label, falling back to default",
				"node", n.Name, "value", raw, "default", p.defaultPort)
		case parsed <= 0 || parsed > 65535:
			// No error to report here: the label parsed, it just names no port.
			klog.InfoS("remotegpu: lupine port label out of range, falling back to default",
				"node", n.Name, "value", raw, "default", p.defaultPort)
		default:
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
		// The plugin publishes one registration annotation whatever mode it
		// runs in. A node part way through switching, or running a plugin
		// still in a local mode, lists cards it has not handed to lupine;
		// taking those into the pool would offer the same card here and to
		// the node's own kubelet.
		if gpu == nil || gpu.Mode != nvidia.RemoteMode {
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
// handed out again to a pod landing on another. previous is returned unchanged
// on a list error, so a transient API error does not free every reservation,
// and the second return says whether the list ran at all.
func reservations(ctx context.Context, previous map[string]struct{}) (map[string]struct{}, bool) {
	pods, err := listPods(ctx)
	if err != nil {
		klog.ErrorS(err, "remotegpu: failed to list pods, keeping previous reservations")
		return previous, false
	}
	held := map[string]struct{}{}
	for i := range pods {
		pod := &pods[i]
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		// Only pods that landed somewhere. Filter writes the allocation
		// annotation before Bind runs and nothing clears it when Bind fails,
		// so counting an unplaced pod would have it reserve its own cards
		// against its next attempt and never become schedulable again. The gap
		// between the annotation and the binding is what held covers.
		if pod.Spec.NodeName == "" {
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
	return held, true
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

// sameFleet reports whether two snapshots describe the same cards.
//
// Cards are compared on their whole scheduling contract rather than a chosen
// few fields. A card that only flipped to unhealthy is still a change, and
// missing it would leave CheckHealth reporting nothing to do while the
// scheduler went on allocating from the healthy copy it had cached.
func sameFleet(a, b []*device.DeviceInfo) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, d := range a {
		counts[device.MarshalNodeDevices([]*device.DeviceInfo{d})]++
	}
	for _, d := range b {
		key := device.MarshalNodeDevices([]*device.DeviceInfo{d})
		counts[key]--
		if counts[key] < 0 {
			return false
		}
	}
	return true
}

// revision changes whenever a refresh installs a different fleet, which is how
// a caller tells "nothing moved" from "not looked at yet".
func (p *pool) revision() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fleetRev
}

// deviceIDs is the set of cards the pool currently carries, so a caller
// holding a pre-refresh view can tell which of its cards survived.
func (p *pool) deviceIDs() map[string]struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	ids := make(map[string]struct{}, len(p.devices))
	for _, d := range p.devices {
		ids[d.ID] = struct{}{}
	}
	return ids
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
	if p.refreshing {
		// A refresh is already fetching, so this call makes no read of its
		// own. Stamping the force anyway would spend the allowance on nothing
		// and block the next caller from a read that would have been real.
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
func (p *pool) hold(pod types.UID, ids ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	for _, id := range ids {
		p.held[id] = booking{at: now, pod: pod}
	}
}

// release gives back every card booked for a pod. The scheduler calls this
// when an attempt it had already allocated for falls through, which is sooner
// than heldTTL would notice and the difference a whole server is held for.
func (p *pool) release(pod types.UID) {
	if pod == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, b := range p.held {
		if b.pod == pod {
			delete(p.held, id)
		}
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
	if _, ok := p.held[id]; ok {
		return true
	}
	_, ok := p.busy[id]
	return ok
}

// askServers collects the cards each lupine server reports a client on. A
// server that cannot be reached keeps whatever it last reported rather than
// dropping to nothing, the same way an API error keeps the previous snapshot:
// a monitoring endpoint going quiet is not evidence that the cards behind it
// were freed, and forgetting them would hand a card an outside client is
// holding to a pod as well.
//
// Every server is asked concurrently: fetchBusyDevices carries its own
// metricsTimeout, and asking sequentially would make one unreachable server
// add its timeout to every other server's, one at a time.
func askServers(ctx context.Context, endpoints map[string]string, previous map[string]struct{}) map[string]struct{} {
	busy := map[string]struct{}{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for node, endpoint := range endpoints {
		wg.Add(1)
		go func(node, endpoint string) {
			defer wg.Done()
			uuids, err := fetchBusyDevices(ctx, endpoint)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				klog.V(4).InfoS("remotegpu: no usage from lupine server, keeping what it last reported",
					"node", node, "endpoint", endpoint, "error", err)
				// ponytail: a scan of the previous set per unreachable server.
				// Index it by server if a fleet ever grows large enough for
				// this to show up.
				for id := range previous {
					if serverOf(id) == node {
						busy[id] = struct{}{}
					}
				}
				return
			}
			for uuid := range uuids {
				busy[deviceID(node, uuid)] = struct{}{}
			}
		}(node, endpoint)
	}
	wg.Wait()
	return busy
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
