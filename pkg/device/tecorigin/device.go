/*
Copyright 2026 The HAMi Authors.

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

// Package tecorigin implements the HAMi device backend for Tecorigin GPUs.
//
// Model: one DeviceInfo per physical card, cores tracked as a bitmask
// (4 cores -> 0b1111 == 15). The mask reaches the device plugin as the Usedcores
// field of HAMi's per-container allocation annotation
// ("<uuid>,<type>,<usedmem>,<usedcores>"), which the plugin expands into the
// global core indexes of TECO_VISIBLE_DEVICES. A mask names any subset of the
// card's cores — the runtime accepts an arbitrary set — so an allocation is not
// restricted to a contiguous run.
//
// Resource: tecorigin.com/gpucore is a CORE count. The node advertises the sum
// of all cards' cores (8 cards x 4 cores = 32).
//
// Node annotation: hami.io/node-tecorigin-register carries HAMi's node-device
// CSV (see parseNodeDevices), the same layout hygon and iluvatar publish, so
// the tecorigin device plugin needs no private format of its own. The id is the
// card serial number the vendor library reports; it is opaque to this backend
// and is what a pod pins through tecorigin.com/use-gpuuuid.
//
// The request always carries Nums=1 + TotalCoresreq=N so that
// fitInDevices' `int(k.Nums) > len(typeDevices)` check (score.go) never
// compares a core count against a card count. The real card count is derived
// inside Fit.
package tecorigin

import (
	"errors"
	"fmt"
	"math/bits"
	"sort"
	"strconv"
	"strings"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

const (
	// TecoDevice is the key this backend registers under in device.DevicesMap
	// and the type HAMi puts on a request. TecoCommonWord is the vendor word
	// stored in DeviceInfo.Type/DeviceVendor and looked up in the annotation.
	// Both are the same string on purpose: HAMi identifies a backend by its
	// vendor word, and score.getNodeResources matches a request type against
	// DeviceInfo.Type as a substring.
	TecoDevice     = "Tecorigin"
	TecoCommonWord = "Tecorigin"

	// Node annotations published by the tecorigin device plugin. The register
	// annotation is the device list. The handshake annotation follows the HAMi
	// convention but no longer gates anything: CheckHealth and NodeCleanUp only
	// remove it (device.RemoveObsoleteHandshake).
	HandshakeAnnosHami = "hami.io/node-handshake-tecorigin"
	RegisterAnnosHami  = "hami.io/node-tecorigin-register"

	// TecoCoreIDsAnnos is OPTIONAL troubleshooting data published by the device
	// plugin at startup as {"<uuid>":[global core ids]}. GetNodeDevices copies it
	// into DeviceInfo.CustomInfo["coreIDs"] and logs it at V(4). Scheduling never
	// reads it: masks are card-local, so the global index only matters when a
	// TECO_VISIBLE_DEVICES entry has to be traced back to a card by hand.
	TecoCoreIDsAnnos = "hami.io/node-tecorigin-cores"

	// Pod annotations that pin a workload to (or away from) specific cards.
	// Values are comma-separated card serials matched exactly against
	// DeviceInfo.ID by device.CheckUUID, i.e. the strings `teco-smi` prints.
	TecoUseUUID   = "tecorigin.com/use-gpuuuid"
	TecoNoUseUUID = "tecorigin.com/nouse-gpuuuid"

	// nodeMode is DeviceInfo.Mode for cards registered by this backend. It is
	// the value every non-MIG HAMi backend writes.
	nodeMode = "hami-core"

	// maxDevmemMiB bounds a plausible per-card memory. Devmem is MiB in the node
	// annotation, and 1 TiB is far above any card this backend serves, so a value
	// that only makes sense in bytes is rejected instead of silently booking
	// 1024x the memory.
	maxDevmemMiB = int64(1) << 20

	// maxCoresPerCard bounds ONE card, not the node: the Tecorigin cards this
	// backend serves expose 3 or 4 cores, so 4 is the exact bit width we ever
	// touch. A core count above this is rejected while parsing the node annotation,
	// which keeps a malformed value from silently producing a bogus mask.
	maxCoresPerCard = int64(4)
)

type TecoConfig struct {
	// ResourceCountName holds a CORE count: tecorigin.com/gpucore: 32 means 32 cores.
	ResourceCountName string `yaml:"resourceCountName"`
}

type TecoDevices struct {
	resourceCountName string
}

func InitTecoDevice(config TecoConfig) *TecoDevices {
	if _, ok := device.InRequestDevices[TecoCommonWord]; !ok {
		device.InRequestDevices[TecoCommonWord] = fmt.Sprintf("hami.io/%s-devices-to-allocate", strings.ToLower(TecoCommonWord))
		device.SupportDevices[TecoCommonWord] = fmt.Sprintf("hami.io/%s-devices-allocated", strings.ToLower(TecoCommonWord))
		util.HandshakeAnnos[TecoCommonWord] = HandshakeAnnosHami
	}
	return &TecoDevices{resourceCountName: config.ResourceCountName}
}

func (t *TecoDevices) CommonWord() string { return TecoCommonWord }

func (t *TecoDevices) GetResourceNames() device.ResourceNames {
	return device.ResourceNames{ResourceCountName: t.resourceCountName}
}

// ---------------------------------------------------------------------------
// node annotation parsing
// ---------------------------------------------------------------------------

// parseNodeDevices decodes RegisterAnnosHami into one DeviceInfo per card.
//
// The wire format is HAMi's node-device CSV, one segment per card, each
// terminated by ':' (device.OneContainerMultiDeviceSplitSymbol):
//
//	3CP100241200184,4,15296,15,Tecorigin,0,true,0,hami-core:
//
// The field order is id,count,memory(MiB),devcore(mask),type,numa,health,index,mode,
// exactly what device.DecodeNodeDevices reads. All nine fields are required:
// <index> is the card's physical position and HAMi's scoring breaks ties on it,
// so the 7-field form (which leaves every card at index 0) is rejected.
func parseNodeDevices(raw string) ([]*device.DeviceInfo, error) {
	segs := nodeAnnotationSegments(raw)
	if len(segs) == 0 {
		return nil, errors.New("empty tecorigin node annotation")
	}
	// Every segment must carry all nine fields: a 7-field tail would decode with
	// index 0 and collide with the first card.
	for _, seg := range segs {
		if fields := strings.Count(seg, ",") + 1; fields != 9 {
			return nil, fmt.Errorf("unexpected tecorigin node annotation %q: want 9 fields, got %d", seg, fields)
		}
	}
	// device.DecodeNodeDevices takes ':' as the device separator and needs a
	// trailing one, which is also how device.EncodeNodeDevices writes it.
	return decodeHAMINodeDevices(strings.Join(segs, ":") + ":")
}

// nodeAnnotationSegments splits a node annotation into card segments and trims
// every field. device.DecodeNodeDevices does not trim whitespace itself.
func nodeAnnotationSegments(raw string) []string {
	var segs []string
	for seg := range strings.SplitSeq(raw, ":") {
		fields := strings.Split(seg, ",")
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		// A segment of separators only (",,") carries no card.
		if joined := strings.Join(fields, ","); strings.Trim(joined, ",") != "" {
			segs = append(segs, joined)
		}
	}
	return segs
}

// decodeHAMINodeDevices decodes the HAMi CSV and normalizes the fields whose
// meaning is tecorigin-specific.
func decodeHAMINodeDevices(csv string) ([]*device.DeviceInfo, error) {
	devices, err := device.DecodeNodeDevices(csv)
	if err != nil {
		return nil, err
	}
	if len(devices) == 0 {
		return nil, errors.New("empty tecorigin node annotation")
	}
	for _, d := range devices {
		if d.ID == "" {
			return nil, errors.New("tecorigin node annotation has a card without uuid")
		}
		if d.Count <= 0 || d.Count > maxCoresPerCard {
			return nil, fmt.Errorf("invalid core count %d for card %q (want 1-%d)", d.Count, d.ID, maxCoresPerCard)
		}
		if d.Devmem < 0 || d.Devmem > maxDevmemMiB {
			return nil, fmt.Errorf("invalid memory %d for card %q: Devmem is MiB, not bytes", d.Devmem, d.ID)
		}
		// Derive the mask from the core count instead of trusting the field: a
		// teco card exposes its cores as the lowest N bits, so a plugin that
		// writes the core count here cannot corrupt placement.
		d.Devcore = maskOf(d.Count)
		// Type must stay the vendor common word: score.getNodeResources
		// matches a pod's requested backend against DeviceInfo.Type by
		// substring, so a model name here would reject every teco request.
		d.Type = TecoCommonWord
		// The HAMi CSV has no vendor field: the scheduler groups devices by
		// DeviceVendor and the pod-side encoding uses it as the container
		// device type, so it must name this backend.
		d.DeviceVendor = TecoCommonWord
		if d.Mode == "" {
			d.Mode = nodeMode
		}
	}
	return devices, nil
}

func firstAnnotation(n corev1.Node, key string) (string, bool) {
	if v, ok := n.Annotations[key]; ok && strings.TrimSpace(v) != "" {
		return v, true
	}
	return "", false
}

// maskOf returns the all-cores-set mask for a card, or 0 when the core count
// is out of range (callers reject such cards during parsing).
func maskOf(cores int64) int64 {
	if cores <= 0 || cores > maxCoresPerCard {
		return 0
	}
	return (int64(1) << uint(cores)) - 1
}

func popcountMask(m int64) int64 { return int64(bits.OnesCount64(uint64(m))) }

// lowestBits returns the mask of the lowest n free bits, or false when the card
// has fewer than n free cores.
//
// The tecorigin runtime takes an arbitrary core set, so the bits need not be
// adjacent. Taking from the bottom keeps a partially used card compact and
// leaves its highest cores free.
func lowestBits(free int64, n int64) (int64, bool) {
	if n <= 0 || n > maxCoresPerCard || popcountMask(free) < n {
		return 0, false
	}
	var out int64
	for i := range maxCoresPerCard {
		if n == 0 {
			break
		}
		if bit := int64(1) << uint(i); free&bit != 0 {
			out |= bit
			n--
		}
	}
	return out, n == 0
}

// coreIDsForTroubleshooting parses TecoCoreIDsAnnos. The keys are the ids from
// the register annotation (card serials) and must equal DeviceInfo.ID for the
// merge in GetNodeDevices to apply. Never used for scheduling decisions.
func coreIDsForTroubleshooting(n corev1.Node) map[string][]int64 {
	raw, ok := n.Annotations[TecoCoreIDsAnnos]
	if !ok || strings.TrimSpace(raw) == "" {
		return nil
	}
	out := make(map[string][]int64)
	body := strings.Trim(strings.TrimSpace(raw), "{}")
	flush := func(pair string) {
		kv := strings.SplitN(pair, ":", 2)
		if len(kv) != 2 {
			return
		}
		uuid := strings.Trim(strings.TrimSpace(kv[0]), `"`)
		list := strings.Trim(strings.TrimSpace(kv[1]), "[]")
		if uuid == "" || list == "" {
			return
		}
		var ids []int64
		for s := range strings.SplitSeq(list, ",") {
			if v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil {
				ids = append(ids, v)
			}
		}
		if len(ids) > 0 {
			out[uuid] = ids
		}
	}
	// Split on commas outside the per-card id lists, so
	// {"a":[0,1],"b":[2]} yields two pairs rather than three fragments.
	depth, start := 0, 0
	for i := range len(body) {
		switch body[i] {
		case '[', '{':
			depth++
		case ']', '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				flush(body[start:i])
				start = i + 1
			}
		}
	}
	flush(body[start:])
	if len(out) == 0 {
		return nil
	}
	return out
}

// ---------------------------------------------------------------------------
// device interface
// ---------------------------------------------------------------------------

// GetNodeDevices decodes the register annotation and drops unhealthy cards. It
// returns an error (and no devices) when not a single card is healthy, so
// register() never caches a device list that cannot be placed on.
func (t *TecoDevices) GetNodeDevices(n corev1.Node) ([]*device.DeviceInfo, error) {
	raw, ok := firstAnnotation(n, RegisterAnnosHami)
	if !ok {
		return []*device.DeviceInfo{}, errors.New("annotation not found: " + RegisterAnnosHami)
	}
	devices, err := parseNodeDevices(raw)
	if err != nil {
		klog.ErrorS(err, "failed to parse tecorigin card annotation", "node", n.Name)
		return []*device.DeviceInfo{}, err
	}

	coreIDs := coreIDsForTroubleshooting(n)
	res := make([]*device.DeviceInfo, 0, len(devices))
	for _, d := range devices {
		if d == nil || !d.Health {
			if d != nil {
				klog.InfoS("skip unhealthy tecorigin card", "node", n.Name, "card", d.ID)
			}
			continue
		}
		if ids, ok := coreIDs[d.ID]; ok {
			if d.CustomInfo == nil {
				d.CustomInfo = map[string]any{}
			}
			d.CustomInfo["coreIDs"] = ids
			// The only consumer of the map: resolving a core index from
			// TECO_VISIBLE_DEVICES back to the card that owns it.
			klog.V(4).InfoS("tecorigin card core ids", "node", n.Name, "card", d.ID, "cores", ids)
		}
		res = append(res, d)
	}
	if len(res) == 0 {
		return res, errors.New("no healthy tecorigin card found on node " + n.Name)
	}
	return res, nil
}

// CheckHealth reports whether the node stays in the scheduler cache, and
// whether its device list must be read again.
//
// The second value is load-bearing: Scheduler.register only calls addNode when
// needUpdate is true, so a backend that never asks for an update never gets its
// nodes into the cache, and every pod asking for its resource fails the
// extender filter with "node unregistered". Registration is therefore driven by
// the advertised resource and the device-plugin annotations (the device list
// plus the optional core-id map): the first observation and any change to them
// ask for an update, an unchanged node does not.
//
// Cards whose healthy flag is false are dropped by GetNodeDevices, so a node
// where every card is unhealthy is reported unhealthy and leaves the cache
// instead of being scheduled against a stale device list. The obsolete
// handshake annotation is removed here as well.
func (t *TecoDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	if n == nil {
		return false, false
	}
	device.RemoveObsoleteHandshake(devType, n)

	allocatable := int64(0)
	if t.resourceCountName != "" {
		if qty, ok := n.Status.Allocatable[corev1.ResourceName(t.resourceCountName)]; ok {
			allocatable = qty.Value()
		}
	}

	// Both annotations shape DeviceInfo, and AllocatableHealth fingerprints the
	// observed state, so a change to either one forces a re-read.
	raw, _ := firstAnnotation(*n, RegisterAnnosHami)
	coreIDsRaw, _ := firstAnnotation(*n, TecoCoreIDsAnnos)
	healthyCards := int64(0)
	if devices, err := parseNodeDevices(raw); err == nil {
		for _, d := range devices {
			if d != nil && d.Health {
				healthyCards++
			}
		}
	}

	healthy, needUpdate := device.AllocatableHealth(
		devType, n.Name, allocatable, healthyCards, raw+"\n"+coreIDsRaw)
	if !healthy {
		return false, needUpdate
	}
	return healthyCards > 0, needUpdate
}

// NodeCleanUp runs when the node leaves the device list. It schedules the
// handshake annotation for deletion, following the HAMi convention that the
// handshake is transient state rather than part of the device list.
func (t *TecoDevices) NodeCleanUp(nn string) error {
	_ = util.MarkAnnotationsToDelete(HandshakeAnnosHami, nn)
	return nil
}

func (t *TecoDevices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	if t.resourceCountName == "" {
		return false, nil
	}
	_, ok := ctr.Resources.Limits[corev1.ResourceName(t.resourceCountName)]
	return ok, nil
}

func (t *TecoDevices) LockNode(n *corev1.Node, p *corev1.Pod) error {
	if !device.PodRequiresDevice(t, p) {
		return nil
	}
	return nodelock.LockNode(n.Name, nodelock.NodeLockKey, p)
}

func (t *TecoDevices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
	if !device.PodRequiresDevice(t, p) {
		return nil
	}
	return nodelock.ReleaseNodeLock(n.Name, nodelock.NodeLockKey, p, false)
}

// GenerateResourceRequests turns tecorigin.com/gpucore: N into "N cores".
//
// Nums stays 1 on purpose: see the package comment.
func (t *TecoDevices) GenerateResourceRequests(ctr *corev1.Container) (device.ContainerDeviceRequest, error) {
	q, ok := lookupQuantity(ctr, t.resourceCountName)
	if !ok {
		return device.ContainerDeviceRequest{}, nil
	}
	n, isInt := q.AsInt64()
	if isInt && n == 0 {
		// An explicit zero means "no device", not an invalid request.
		return device.ContainerDeviceRequest{}, nil
	}
	if !isInt || n < 0 {
		return device.ContainerDeviceRequest{}, &device.ErrInvalidDeviceRequest{
			Container: ctr.Name, Device: "tecorigin",
			Reason: fmt.Sprintf("%s must be a non-negative integer", t.resourceCountName),
		}
	}
	return device.ContainerDeviceRequest{
		Nums:             1,
		Type:             TecoDevice,
		TotalCoresreq:    n,
		MemPercentagereq: 101, // HAMi's "no memory requirement" sentinel: cores are the only dimension
	}, nil
}

func lookupQuantity(ctr *corev1.Container, name string) (resource.Quantity, bool) {
	if name == "" {
		return resource.Quantity{}, false
	}
	if v, found := ctr.Resources.Limits[corev1.ResourceName(name)]; found {
		return v, true
	}
	if v, found := ctr.Resources.Requests[corev1.ResourceName(name)]; found {
		return v, true
	}
	return resource.Quantity{}, false
}

func (t *TecoDevices) ScoreNode(node *corev1.Node, podDevices device.PodSingleDevice,
	previous []*device.DeviceUsage, policy string) float32 {
	return 0
}

// AddResourceUsage books one container on a card: Used counts containers while
// Count holds the card's core count, so Fit's "Count <= Used" check is what caps
// how many containers may share one card.
func (t *TecoDevices) AddResourceUsage(pod *corev1.Pod, n *device.DeviceUsage, ctr *device.ContainerDevice) error {
	n.Used++
	n.Usedcores = t.AccumulateCores(n.Usedcores, ctr.Usedcores)
	n.Usedmem += ctr.Usedmem
	return nil
}

// CoreMaskAccounting: Usedcores holds a bitmask, so accumulate with OR and
// count with popcount. Without this the framework would add masks numerically.
func (t *TecoDevices) AccumulateCores(used, allocated int64) int64 { return used | allocated }

func (t *TecoDevices) CountAllocatedCores(allocated int64) int64 { return popcountMask(allocated) }

// ---------------------------------------------------------------------------
// placement
// ---------------------------------------------------------------------------

// tecoCandidate is one card Fit may place on. freeMask holds the free cores and
// freeCores their count; every free core is usable.
type tecoCandidate struct {
	dev       *device.DeviceUsage
	freeMask  int64
	freeCores int64
}

// chooseCards picks which cards satisfy want cores.
//
// Two goals, in order:
//  1. fewest cards — a pod that fits one card is never spread over two;
//  2. among the smallest sets, the least total free capacity is used, so cards
//     that are already partly allocated get filled first and whole cards stay
//     whole (fewer half-empty cards left behind).
//
// Goal 2 is exact and does not depend on the node size: a card has at most
// maxCoresPerCard free cores, so it falls into one of a few buckets keyed by
// that count, and cards in the same bucket are interchangeable. The only
// decision is how many cards to take from each bucket — e.g. a 6-core request
// prefers two 3-core cards over two 4-core ones because 3+3 wastes nothing.
//
// Returns indexes into cands, ordered by descending free capacity (ties broken
// by index), or false when no set can serve the request.
func chooseCards(cands []tecoCandidate, want int64, maxCards int64) ([]int, bool) {
	n := len(cands)
	if n == 0 || want <= 0 {
		return nil, false
	}

	// 1) Minimal card count: the k emptiest cards must cover want.
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return cands[order[a]].freeCores > cands[order[b]].freeCores
	})
	kMin, acc := -1, int64(0)
	for k := 1; k <= n; k++ {
		acc += cands[order[k-1]].freeCores
		if acc >= want {
			kMin = k
			break
		}
	}
	if kMin < 0 || int64(kMin) > maxCards {
		// Either no set covers the request, or covering it needs more cards than
		// the physical minimum — the pod would be spread, so refuse the node.
		return nil, false
	}

	// 2) Bucket the cards by how many cores they have free. freeCores never
	// exceeds maxCoresPerCard because DeviceInfo.Devcore is derived from the
	// card's core count. Buckets keep candidate order, so entry 0 of a bucket is
	// the lowest-indexed card with that many free cores.
	byFreeCores := make([][]int, maxCoresPerCard+1)
	for i, cand := range cands {
		freeCores := int(cand.freeCores)
		byFreeCores[freeCores] = append(byFreeCores[freeCores], i)
	}

	// 3) Ask for the cheapest split of kMin cards, then take the lowest-indexed
	// cards from each bucket.
	_, counts, found := bestSplit(byFreeCores, 0, kMin, want)
	if !found {
		return nil, false
	}

	// 4) Materialise the win: the first counts[freeCores] cards of each bucket,
	// i.e. the lowest-indexed ones, which keeps the choice stable.
	out := make([]int, 0, kMin)
	for freeCores := 1; freeCores <= int(maxCoresPerCard); freeCores++ {
		out = append(out, byFreeCores[freeCores][:counts[freeCores]]...)
	}
	sort.SliceStable(out, func(a, b int) bool {
		if cands[out[a]].freeCores != cands[out[b]].freeCores {
			return cands[out[a]].freeCores > cands[out[b]].freeCores
		}
		return out[a] < out[b]
	})
	return out, true
}

// bestSplit returns how many cards to take from each free-core bucket when
// exactly cards cards must cover at least need cores, considering only buckets
// byFreeCores[from:] (bucket w holds the cards with w free cores).
//
// It minimises the total free capacity of the split, which is what makes a
// 6-core request prefer two 3-core cards over two 4-core ones. Cards inside a
// bucket are interchangeable, so the per-bucket counts fully identify the pick.
//
// For example, with buckets {2: [A], 3: [B], 4: [C]} and a request of 2 cards
// covering 5 cores, the cheapest split is {2: 1, 3: 1}: A plus B totals 5,
// while any split that uses C totals 6 or more.
//
// total is that minimised capacity, counts[freeCores] is how many cards of that
// size to use, and ok is false when the remaining buckets cannot supply that
// many cards or cannot cover need.
func bestSplit(byFreeCores [][]int, from, cards int, need int64) (total int64, counts [maxCoresPerCard + 1]int, ok bool) {
	if cards == 0 {
		// Every card is placed; the split works only if they covered need.
		return 0, counts, need <= 0
	}
	if from > int(maxCoresPerCard) {
		// Out of buckets with cards still to place.
		return 0, counts, false
	}

	// take is how many cards of this size to use, capped by both the bucket and
	// the cards still to place.
	bestTotal := int64(-1)
	for take := 0; take <= min(cards, len(byFreeCores[from])); take++ {
		subTotal, subCounts, subOK := bestSplit(byFreeCores, from+1, cards-take, need-int64(take*from))
		if !subOK {
			continue
		}
		subCounts[from] = take
		if candidate := subTotal + int64(take*from); bestTotal < 0 || candidate < bestTotal {
			bestTotal, counts = candidate, subCounts
		}
	}
	if bestTotal < 0 {
		return 0, counts, false
	}
	return bestTotal, counts, true
}

// Fit places the request.
//
// Rules, in order:
//  1. Same-card first: a request that fits one card must never be spread.
//  2. Fewest cards; ties broken by leaving the least fragmented cards behind.
//  3. Cores taken from a card are its lowest free cores, whatever their
//     position: the runtime accepts an arbitrary mask, so a 2-core request on a
//     card whose free cores are 0b0101 is served as 0b0101, not refused.
func (t *TecoDevices) Fit(devices []*device.DeviceUsage, request device.ContainerDeviceRequest,
	pod *corev1.Pod, nodeInfo *device.NodeInfo, allocated *device.PodDevices) (bool, map[string]device.ContainerDevices, string) {

	tmpDevs := make(map[string]device.ContainerDevices)
	reasons := make(map[string]int)
	klog.V(4).InfoS("allocating tecorigin devices", "pod", klog.KObj(pod), "request", request)

	if request.TotalCoresreq <= 0 {
		return false, tmpDevs, common.GenReason(map[string]int{common.CardInsufficientCore: 1}, len(devices))
	}

	cands := make([]tecoCandidate, 0, len(devices))
	for _, dev := range devices {
		if !dev.Health {
			reasons[common.CardNotHealth]++
			continue
		}
		if dev.Count <= dev.Used {
			reasons[common.CardTimeSlicingExhausted]++
			continue
		}
		if !device.CheckUUID(pod.GetAnnotations(), dev.ID, TecoUseUUID, TecoNoUseUUID, TecoCommonWord) {
			reasons[common.CardUUIDMismatch]++
			continue
		}
		free := dev.Totalcore &^ dev.Usedcores
		if free == 0 {
			reasons[common.CardInsufficientCore]++
			continue
		}
		cands = append(cands, tecoCandidate{
			dev: dev, freeMask: free, freeCores: popcountMask(free),
		})
	}
	if len(cands) == 0 {
		return false, tmpDevs, common.GenReason(reasons, len(devices))
	}

	maxCoresHere := int64(0)
	for _, c := range cands {
		if cc := popcountMask(c.dev.Totalcore); cc > maxCoresHere {
			maxCoresHere = cc
		}
	}
	if maxCoresHere == 0 {
		return false, tmpDevs, common.GenReason(map[string]int{common.CardInsufficientCore: 1}, len(devices))
	}
	// Physical floor on card count for the biggest card present. Exceeding it
	// means the request can only be served by spreading.
	maxCards := (request.TotalCoresreq + maxCoresHere - 1) / maxCoresHere

	chosen, ok := chooseCards(cands, request.TotalCoresreq, maxCards)
	if !ok {
		// Two distinct causes share one reason: too few free cores, or a request
		// that would need more cards than the physical minimum (the pod would be
		// spread). Log the free capacity so the two can be told apart.
		free := int64(0)
		for _, c := range cands {
			free += c.freeCores
		}
		klog.V(4).InfoS("tecorigin fit failed: not enough free cores, or the pod would be spread",
			"pod", klog.KObj(pod), "want", request.TotalCoresreq,
			"freeCores", free, "candidates", len(cands), "maxCards", maxCards)
		reasons[common.CardInsufficientCore]++
		return false, tmpDevs, common.GenReason(reasons, len(devices))
	}

	remaining := request.TotalCoresreq
	for _, idx := range chosen {
		if remaining == 0 {
			break
		}
		c := cands[idx]
		take := min(c.freeCores, remaining)
		mask, ok := lowestBits(c.freeMask, take)
		if !ok {
			continue
		}
		tmpDevs[request.Type] = append(tmpDevs[request.Type], device.ContainerDevice{
			Idx: int(c.dev.Index), UUID: c.dev.ID, Type: request.Type, Usedcores: mask,
		})
		remaining -= take
	}

	if remaining > 0 {
		reasons[common.CardInsufficientCore]++
		return false, tmpDevs, common.GenReason(reasons, len(devices))
	}
	return true, tmpDevs, ""
}

// PatchAnnotations writes the chosen cards into both of HAMi's device
// annotations: hami.io/tecorigin-devices-to-allocate (pending, consumed by the
// device plugin) and hami.io/tecorigin-devices-allocated (the record HAMi keeps
// for the pod).
func (t *TecoDevices) PatchAnnotations(pod *corev1.Pod, annoinput *map[string]string, pd device.PodDevices) map[string]string {
	devlist, ok := pd[TecoDevice]
	if !ok || len(devlist) == 0 {
		return *annoinput
	}
	encoded := device.EncodePodSingleDevice(devlist)
	(*annoinput)[device.InRequestDevices[TecoDevice]] = encoded
	(*annoinput)[device.SupportDevices[TecoDevice]] = encoded
	return *annoinput
}
