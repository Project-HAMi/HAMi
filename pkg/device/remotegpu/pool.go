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
	"cmp"
	"context"
	"maps"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

const (
	// LupineServerLabel marks a GPU node as serving its GPUs through lupine.
	// The NVIDIA device plugin applies it when the per-node ConfigMap sets
	// operatingmode to remote (peer to hami-core / mig). The label value is the
	// lupine listen port; an empty value means RemoteGPUConfig.DefaultPort.
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

	// RetryAnno is stamped on a pod that Fit turned away once the fleet
	// changes. kube-scheduler learns nothing about the fleet from an extender:
	// a lupine server that appears or returns is no event it requeues on, so
	// the pod would sit in the unschedulable queue until the next flush,
	// minutes away. Any update to the pod requeues it at once.
	RetryAnno = "hami.io/remote-gpu-retry"

	// waitingTTL drops a refused pod that never got a fleet change to retry
	// on, such as one deleted while it waited.
	waitingTTL = 10 * time.Minute
)

// Swappable for tests, mirroring how plugin/server.go injects getPendingPod.
var (
	listLupineNodes = apiListLupineNodes
	listPods        = apiListPods
	nudgePods       = apiNudgePods
)

// waiter is a pod Fit refused, remembered so it can be nudged on a fleet change.
type waiter struct {
	uid             types.UID
	namespace, name string
	at              time.Time
}

// apiNudgePods returns the pods it could not patch, so they are kept for the
// next fleet change instead of being dropped.
func apiNudgePods(pods []waiter, rev uint64) []waiter {
	c := client.GetClient()
	if c == nil {
		return pods
	}
	var failed []waiter
	payload := []byte(`{"metadata":{"annotations":{"` + RetryAnno + `":"` + strconv.FormatUint(rev, 10) + `"}}}`)
	for _, w := range pods {
		ctx, cancel := context.WithTimeout(context.Background(), poolFetchTimeout)
		_, err := c.CoreV1().Pods(w.namespace).Patch(ctx, w.name, types.MergePatchType, payload, metav1.PatchOptions{})
		cancel()
		if err != nil && !apierrors.IsNotFound(err) {
			klog.V(4).InfoS("remotegpu: failed to requeue waiting pod", "pod", w.namespace+"/"+w.name, "error", err)
			failed = append(failed, w)
		}
	}
	return failed
}

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
// that landed on a different client node. usage() closes that gap by reading
// the allocation back from pod annotations cluster wide.
type pool struct {
	mu          sync.Mutex
	defaultPort int

	fetchedAt    time.Time
	podsListed   bool // an actual successful Pod list established the reservation baseline
	forcedAt     time.Time
	refreshing   bool                  // a refresh is already fetching; do not start a second one
	refreshed    chan struct{}         // closed when the in-flight refresh installs its result
	fleetRev     uint64                // bumped whenever a refresh installs a different fleet
	endpoints    map[string]string     // lupine node name -> "host:port"
	serverLabels map[string]labels.Set // lupine node name -> labels from the same pool snapshot
	devices      []*device.DeviceInfo
	inUse        map[string]map[string]share   // device ID -> pod key -> what a live pod holds
	held         map[string]map[string]booking // device ID -> pod key -> booked since the last refresh
	busy         map[string][]string           // device ID -> client IDs the server reports on it
	waiting      map[types.UID]waiter          // pods Fit refused since the fleet last changed
}

// share is what one pod holds of one card: how many of its containers use it,
// how much memory they were given, in MiB, and whether one of them took the
// card whole. Partial grants that add up to the card's memory are not a whole
// grant. A negative mem stands for the whole card, for a booking made without
// knowing the card's size.
type share struct {
	users int
	mem   int64
	whole bool
	// host is the pod's hostname, which is how a Lupine server that does not
	// report LUPINE_WORKLOAD_ID names the pod's client.
	host string
}

func (s share) add(o share) share {
	sum := share{users: s.users + o.users, mem: s.mem + o.mem, whole: s.whole || o.whole, host: cmp.Or(s.host, o.host)}
	if s.mem < 0 || o.mem < 0 {
		sum.mem = -1
	}
	return sum
}

// maxShare is the larger of two shares in every respect, for a pod whose
// containers hold a card at different times.
func maxShare(a, b share) share {
	m := share{users: max(a.users, b.users), mem: max(a.mem, b.mem), whole: a.whole || b.whole, host: cmp.Or(a.host, b.host)}
	if a.mem < 0 || b.mem < 0 {
		m.mem = -1
	}
	return m
}

// grantShare is what one allocated container adds to a pod's share of a card.
// A whole card is granted with its cores, a memory share with none, and that
// is how the annotation tells the two apart.
func grantShare(d device.ContainerDevice) share {
	return share{users: 1, mem: d.Usedmem, whole: d.Usedmem < 0 || d.Usedcores > 0}
}

func newPool(defaultPort int) *pool {
	return &pool{
		defaultPort:  defaultPort,
		endpoints:    map[string]string{},
		serverLabels: map[string]labels.Set{},
		inUse:        map[string]map[string]share{},
		held:         map[string]map[string]booking{},
		busy:         map[string][]string{},
		waiting:      map[types.UID]waiter{},
	}
}

// wait remembers a pod Fit refused, so a fleet change can requeue it.
// A pod refused on an older fleet than rev missed that change's nudge, so it is
// nudged now.
func (p *pool) wait(pod *corev1.Pod, rev uint64) {
	if pod == nil || pod.UID == "" {
		return
	}
	w := waiter{uid: pod.UID, namespace: pod.Namespace, name: pod.Name, at: time.Now()}
	p.mu.Lock()
	defer p.mu.Unlock()
	if rev != p.fleetRev {
		go func(rev uint64) {
			if failed := nudgePods([]waiter{w}, rev); len(failed) > 0 {
				p.requeue(failed)
			}
		}(p.fleetRev)
		return
	}
	p.waiting[pod.UID] = w
}

// pruneWaiting forgets pods refused longer ago than waitingTTL. Callers hold p.mu.
func (p *pool) pruneWaiting() {
	for uid, w := range p.waiting {
		if time.Since(w.at) >= waitingTTL {
			delete(p.waiting, uid)
		}
	}
}

// takeWaiting hands over the pods worth nudging and forgets them. Callers hold p.mu.
func (p *pool) takeWaiting() []waiter {
	p.pruneWaiting()
	out := make([]waiter, 0, len(p.waiting))
	for _, w := range p.waiting {
		out = append(out, w)
	}
	clear(p.waiting)
	return out
}

// requeue puts back pods a nudge could not reach, for the next fleet change.
func (p *pool) requeue(pods []waiter) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, w := range pods {
		p.waiting[w.uid] = w
	}
}

// booking records what a pod was given of a card, so a scheduling attempt that
// falls through can hand it straight back instead of waiting out heldTTL.
type booking struct {
	at    time.Time
	share share
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
	p.pruneWaiting()
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
	previousDevices, previousEndpoints, podsListed := p.devices, p.endpoints, p.podsListed
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
	serverLabels := make(map[string]labels.Set, len(nodes))
	listedServers := make(map[string]struct{}, len(nodes))
	var devices []*device.DeviceInfo
	for i := range nodes {
		n := &nodes[i]
		listedServers[n.Name] = struct{}{}
		if !isReadyLupineNode(n) {
			klog.V(4).InfoS("remotegpu: skipping unavailable lupine server", "node", n.Name)
			continue
		}
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
		serverLabels[n.Name] = copyLabels(n.Labels)
		devices = append(devices, gpus...)
	}

	// Keep reconstructing reservations while server Nodes still exist, even if
	// none is currently eligible for new allocations. A NotReady server must
	// not make the allocations of its running Pods appear released.
	var inUse map[string]map[string]share
	// listed says the pod list actually ran. A booking is only expired against
	// a list that happened; expiring it against one that failed would hand a
	// bound pod's card to a second pod.
	listed := true
	if len(nodes) == 0 {
		inUse = map[string]map[string]share{}
	} else {
		inUse, listed = reservations(ctx, previousInUse)
	}
	if !listed {
		// Only unchanged servers covered by a successful Pod list can keep
		// serving. inUse retains that list's owners and held covers allocations
		// made by this scheduler since then; failed lists never expire holds.
		// This relies on the same single allocating scheduler as hold(): another
		// writer's unlisted bindings cannot be inferred from idle metrics.
		previous := map[string][]*device.DeviceInfo{}
		current := map[string][]*device.DeviceInfo{}
		uncertain := map[string]bool{}
		for _, d := range previousDevices {
			previous[serverOf(d.ID)] = append(previous[serverOf(d.ID)], d)
		}
		for _, d := range devices {
			current[serverOf(d.ID)] = append(current[serverOf(d.ID)], d)
		}
		// A previously busy server may have an unlisted bound owner even if
		// its metrics now say idle. Require a Pod list before reopening it.
		for id := range previousBusy {
			uncertain[serverOf(id)] = true
		}
		for id := range previousInUse {
			uncertain[serverOf(id)] = true
		}
		for server, cards := range current {
			uncertain[server] = uncertain[server] || !podsListed ||
				previousEndpoints[server] != endpoints[server] || !sameFleet(previous[server], cards)
		}
		for _, d := range devices {
			if uncertain[serverOf(d.ID)] {
				d.Health = false
			}
		}
	}
	// Ask every server concurrently: one unreachable server pays its own
	// metricsTimeout instead of adding it to everyone else's.
	busy := askServers(ctx, endpoints, previousBusy, listedServers)

	p.mu.Lock()
	defer p.mu.Unlock()
	if !sameFleet(p.devices, devices) {
		p.fleetRev++
		if waiting := p.takeWaiting(); len(waiting) > 0 {
			go func(rev uint64) {
				if failed := nudgePods(waiting, rev); len(failed) > 0 {
					p.requeue(failed)
				}
			}(p.fleetRev)
		}
	}
	p.endpoints = endpoints
	p.serverLabels = serverLabels
	p.devices = devices
	p.inUse = inUse
	// An empty Node list skips reservations(); it is not a successful Pod list.
	if listed && len(nodes) > 0 {
		p.podsListed = true
	}
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
		for id, pods := range p.held {
			for pod, b := range pods {
				if _, seen := p.inUse[id][pod]; seen || time.Since(b.at) > heldTTL {
					delete(pods, pod)
				}
			}
			if len(pods) == 0 {
				delete(p.held, id)
			}
		}
	}
	klog.V(4).InfoS("remotegpu: pool refreshed",
		"servers", len(endpoints), "devices", len(devices),
		"reserved", len(p.inUse), "busyOnServer", len(p.busy))
}

func copyLabels(in map[string]string) labels.Set {
	out := make(labels.Set, len(in))
	maps.Copy(out, in)
	return out
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

// isReadyLupineNode reports whether Kubernetes considers a Lupine server
// available for new allocations. A missing Ready condition is treated the
// same as Unknown: it does not establish that the server can accept work.
func isReadyLupineNode(n *corev1.Node) bool {
	for _, condition := range n.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
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
		// Count is the plugin's deviceSplitCount: how many containers may
		// share the card when each asks for part of its memory.
		if dup.Count < 1 {
			dup.Count = 1
		}
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
func reservations(ctx context.Context, previous map[string]map[string]share) (map[string]map[string]share, bool) {
	pods, err := listPods(ctx)
	if err != nil {
		klog.ErrorS(err, "remotegpu: failed to list pods, keeping previous reservations")
		return previous, false
	}
	held := map[string]map[string]share{}
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
		// A pod holds each card at its peak for as long as it lives: an
		// ordinary init container runs alone, beside the sidecars started
		// before it, but kubelet runs it again when it recreates the sandbox,
		// so its share is never given back to other pods.
		running, sidecars, initPeak := map[string]share{}, map[string]share{}, map[string]share{}
		for i, ctr := range strings.Split(raw, device.OnePodMultiContainerSplitSymbol) {
			devs, err := device.DecodeContainerDevices(ctr)
			if err != nil {
				klog.V(4).InfoS("remotegpu: skipping undecodable allocation annotation",
					"pod", klog.KObj(pod), "error", err)
				continue
			}
			mine := map[string]share{}
			for _, d := range devs {
				mine[d.UUID] = mine[d.UUID].add(grantShare(d))
			}
			for id, g := range mine {
				switch {
				case ordinaryInit(pod, i):
					initPeak[id] = maxShare(initPeak[id], sidecars[id].add(g))
				case i < len(pod.Spec.InitContainers):
					sidecars[id] = sidecars[id].add(g)
					running[id] = running[id].add(g)
				default:
					running[id] = running[id].add(g)
				}
			}
		}
		for id, peak := range initPeak {
			running[id] = maxShare(running[id], peak)
		}
		for id, g := range running {
			if held[id] == nil {
				held[id] = map[string]share{}
			}
			g.host = podHostname(pod)
			held[id][podKey(pod)] = g
		}
	}
	return held, true
}

// podKey names the pod a share belongs to. The UID is what identifies a pod;
// the name only stands in for pods that carry none, as built in tests.
func podKey(pod *corev1.Pod) string {
	if pod == nil {
		return ""
	}
	if pod.UID != "" {
		return string(pod.UID)
	}
	if pod.Name == "" {
		return ""
	}
	return pod.Namespace + "/" + pod.Name
}

// podHostname is the hostname the pod's containers see, as kubelet sets it:
// hostnameOverride, else spec.hostname or the pod name, cut to 63 characters.
// A host-network pod sees the node's hostname instead, which no pod is matched
// by. Without an override, setHostnameAsFQDN and a subdomain make it the pod's
// FQDN, whose cluster domain the scheduler does not know, so only the prefix
// up to it is returned, ending in a dot.
func podHostname(pod *corev1.Pod) string {
	if pod == nil || pod.Spec.HostNetwork {
		return ""
	}
	host := cmp.Or(pod.Spec.Hostname, pod.Name)
	if pod.Spec.HostnameOverride != nil {
		host = *pod.Spec.HostnameOverride
	}
	if len(host) > 63 {
		host = strings.TrimRight(host[:63], "-.")
	}
	if fqdn := pod.Spec.SetHostnameAsFQDN; pod.Spec.HostnameOverride == nil && fqdn != nil && *fqdn && pod.Spec.Subdomain != "" {
		host += "." + pod.Spec.Subdomain + "." + pod.Namespace + ".svc."
	}
	return host
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

// deviceHealth snapshots the current cards and their health, so callers with
// older usage can check both server identity and allocation eligibility.
func (p *pool) deviceHealth() map[string]bool {
	cards, _ := p.allocationSnapshot("")
	health := make(map[string]bool, len(cards))
	for id, c := range cards {
		health[id] = c.Health
	}
	return health
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

// hold books whole cards for a pod. See book.
func (p *pool) hold(pod types.UID, ids ...string) {
	devs := make(device.ContainerDevices, 0, len(ids))
	for _, id := range ids {
		devs = append(devs, device.ContainerDevice{UUID: id, Usedmem: -1})
	}
	p.book(string(pod), "", devs)
}

// book records what the scheduler has just allocated to a pod, one entry per
// container and card. reservations() only learns about an allocation once the
// pod carrying it turns up in a pod list, which is at most once per poolTTL;
// until then this is the only record that the card is spoken for. A booking
// replaces the pod's previous one, since every attempt allocates afresh.
// host is the pod's hostname, see share.
func (p *pool) book(pod, host string, devs device.ContainerDevices) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dropBookingLocked(pod)
	now := time.Now()
	for _, d := range devs {
		if p.held[d.UUID] == nil {
			p.held[d.UUID] = map[string]booking{}
		}
		b := p.held[d.UUID][pod]
		g := grantShare(d)
		g.host = host
		p.held[d.UUID][pod] = booking{at: now, share: b.share.add(g)}
	}
}

// booked reports whether the pod still has a booking, which it keeps from
// Filter until the pod list shows it bound or heldTTL runs out.
func (p *pool) booked(pod string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, pods := range p.held {
		if _, ok := pods[pod]; ok {
			return true
		}
	}
	return false
}

// release gives back every card booked for a pod. The scheduler calls this
// when an attempt it had already allocated for falls through, which is sooner
// than heldTTL would notice.
func (p *pool) release(pod types.UID) {
	if pod == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dropBookingLocked(string(pod))
}

func (p *pool) dropBookingLocked(pod string) {
	if pod == "" {
		return
	}
	for id, pods := range p.held {
		delete(pods, pod)
		if len(pods) == 0 {
			delete(p.held, id)
		}
	}
}

// reserved reports whether anything holds a device: a live pod anywhere in the
// cluster, a booking this scheduler made since the last refresh, or a client
// the server reports that no allocation accounts for.
func (p *pool) reserved(id string) bool {
	u := p.usage(id, "")
	return u.users > 0 || u.foreign
}

// cardUsage is what other pods hold of a card, as far as the pool knows.
type cardUsage struct {
	users int
	// mem is in MiB; a whole-card booking counts as the card's full memory.
	mem int64
	// whole is set when another pod took the card whole.
	whole bool
	// foreign is set when the server reports a client on the card that no
	// allocation accounts for. Nothing else may be put on such a card.
	foreign bool
}

// usage adds up what every pod except exclude holds of a device.
func (p *pool) usage(id, exclude string) cardUsage {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.usageLocked(id, exclude)
}

func (p *pool) usageLocked(id, exclude string) cardUsage {
	holders := map[string]share{}
	for pod, b := range p.held[id] {
		holders[pod] = b.share
	}
	// The annotation is what the pod was actually given; it wins over the
	// booking made for the same pod.
	maps.Copy(holders, p.inUse[id])
	var u cardUsage
	for pod, s := range holders {
		if pod == exclude && exclude != "" {
			continue
		}
		u.users += s.users
		u.whole = u.whole || s.whole
		if s.mem < 0 {
			u.mem += p.cardMemLocked(id)
		} else {
			u.mem += s.mem
		}
	}
	for _, client := range p.busy[id] {
		if !accountedFor(client, holders) {
			u.foreign = true
			break
		}
	}
	return u
}

// accountedFor reports whether a client the server reports on a card is one of
// the pods holding it. A Lupine server that supports LUPINE_WORKLOAD_ID reports
// what admission sets it to, "<pod UID>/<container>". Released servers report
// "<address>:<hostname>:<pid namespace>:<pid>" instead, and the hostname is the
// pod's. A client whose hostname is no holder's is foreign, however many
// holders the card has: it could be anything run by hand.
func accountedFor(client string, holders map[string]share) bool {
	if uid, _, workload := strings.Cut(client, "/"); workload && !strings.Contains(uid, ":") {
		_, ok := holders[uid]
		return ok
	}
	// The address may be IPv6, so count the fields from the end.
	fields := strings.Split(client, ":")
	if len(fields) < 4 {
		return false
	}
	host := fields[len(fields)-3]
	for _, s := range holders {
		if s.host == "" {
			continue
		}
		if s.host == host || (strings.HasSuffix(s.host, ".") && strings.HasPrefix(host, s.host)) {
			return true
		}
	}
	return false
}

// cardMemLocked is a card's memory in MiB, or a value no request fits next to
// when the card is not in the fleet. Callers hold p.mu.
func (p *pool) cardMemLocked(id string) int64 {
	for _, d := range p.devices {
		if d.ID == id {
			return d.Devmem
		}
	}
	return 1 << 40
}

// allocationSnapshot reads the current cards and what pods other than exclude
// hold of each under one lock. A refresh can clear a busy report while marking
// its cards unhealthy; mixing reads from either side of it would offer a card
// whose ownership is still unknown.
func (p *pool) allocationSnapshot(exclude string) (map[string]device.DeviceInfo, map[string]cardUsage) {
	p.mu.Lock()
	defer p.mu.Unlock()
	cards := make(map[string]device.DeviceInfo, len(p.devices))
	usage := make(map[string]cardUsage, len(p.devices))
	for _, d := range p.devices {
		cards[d.ID] = *d
		usage[d.ID] = p.usageLocked(d.ID, exclude)
	}
	return cards, usage
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
func askServers(ctx context.Context, endpoints map[string]string, previous map[string][]string, listedServers map[string]struct{}) map[string][]string {
	busy := map[string][]string{}
	// A server can temporarily disappear from endpoints because it is NotReady.
	// Its old usage is still safer than assuming its direct clients went away.
	// Drop only records for Nodes no longer returned by Kubernetes at all.
	for id, clients := range previous {
		if _, stillListed := listedServers[serverOf(id)]; stillListed {
			busy[id] = clients
		}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for node, endpoint := range endpoints {
		wg.Add(1)
		go func(node, endpoint string) {
			defer wg.Done()
			clients, err := fetchBusyDevices(ctx, endpoint)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				klog.V(4).InfoS("remotegpu: no usage from lupine server, keeping what it last reported",
					"node", node, "endpoint", endpoint, "error", err)
				return
			}
			// A successful response is authoritative, including an empty one.
			for id := range busy {
				if serverOf(id) == node {
					delete(busy, id)
				}
			}
			for uuid, ids := range clients {
				busy[deviceID(node, uuid)] = ids
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

// matchingServers returns only servers whose labels satisfy selector. Labels
// are captured with the same Node snapshot as the endpoints and devices, so a
// server that disappeared from the usable pool cannot match by stale metadata.
func (p *pool) matchingServers(selector labels.Selector, servers []string) []string {
	if selector == nil {
		return servers
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	matched := make([]string, 0, len(servers))
	for _, server := range servers {
		if selector.Matches(p.serverLabels[server]) {
			matched = append(matched, server)
		}
	}
	return matched
}

// isLupineNode reports whether the node serves the pool rather than consuming
// it. Such a node hosts lupine, not remote-gpu client pods.
func isLupineNode(n *corev1.Node) bool {
	_, ok := n.Labels[LupineServerLabel]
	return ok
}
