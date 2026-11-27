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

// Package remotegpu adapts Lupine metrics to HAMi's GPU monitoring schema.
package remotegpu

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	remote "github.com/Project-HAMi/HAMi/pkg/device/remotegpu"
	"github.com/Project-HAMi/HAMi/pkg/monitor/metrics"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

const maxScrapeBytes = 16 << 20

var defaultHTTPClient = &http.Client{Timeout: 3 * time.Second}

var memory = metrics.ContainerMemory
var memoryLimit = metrics.ContainerMemoryLimit
var utilization = metrics.ContainerUtilization
var hostMemory = metrics.HostMemory
var hostUtilization = metrics.HostUtilization
var success = prometheus.NewDesc("hami_remote_gpumonitor_collect_success", "Whether Lupine scraping and Pod attribution succeeded", []string{"node"}, nil)
var droppedSamples = prometheus.NewDesc("hami_remote_gpumonitor_dropped_samples", "Lupine samples omitted from the last scrape", []string{"node", "reason"}, nil)

var deviceLabels = []string{"node", "device_index", "device_uuid", "device_type"}
var hostMemoryTotal = prometheus.NewDesc("hami_host_gpu_memory_total_bytes", "GPU device memory capacity in bytes", deviceLabels, nil)
var clients = prometheus.NewDesc("hami_remote_gpu_clients", "Lupine clients using memory on the GPU", deviceLabels, nil)
var unattributedMemory = prometheus.NewDesc("hami_remote_gpu_unattributed_memory_used_bytes", "GPU memory used by Lupine clients that match no HAMi allocation", deviceLabels, nil)
var unattributedUtilization = prometheus.NewDesc("hami_remote_gpu_unattributed_utilization_ratio", "GPU utilization of Lupine clients that match no HAMi allocation (0-100)", deviceLabels, nil)
var connections = prometheus.NewDesc("hami_remote_gpu_server_connections", "Lupine clients connected to the server", []string{"node"}, nil)

// Collector never stores previous samples: failures cannot replay stale usage.
// Pods must be backed by a synchronized informer. Node is the GPU server node.
// Each client pod placed on it has its own Lupine server there, reached at
// Host on the port of the pod's endpoint.
type Collector struct {
	Node, Host string
	Client     *http.Client
	Pods       func() ([]*corev1.Pod, error)
	// Indices maps the node's GPU UUIDs to their NVML indices. A pod's server
	// numbers only the cards it sees, so its own device_index is not the node's.
	Indices func() (map[string]string, error)

	mu          sync.Mutex
	lastFailure string
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{memory, memoryLimit, utilization, hostMemory, hostUtilization, success, droppedSamples,
		hostMemoryTotal, clients, unattributedMemory, unattributedUtilization, connections} {
		ch <- d
	}
}
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	samples, err := c.collect()
	value := 1.0
	if err != nil {
		value = 0
		c.recordFailure(err)
	} else {
		c.recordSuccess()
	}
	for _, m := range samples {
		ch <- m
	}
	ch <- prometheus.MustNewConstMetric(success, prometheus.GaugeValue, value, c.Node)
}

func (c *Collector) recordFailure(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	msg := err.Error()
	if c.lastFailure == msg {
		return
	}
	c.lastFailure = msg
	klog.ErrorS(err, "remote GPU metrics collection failed", "node", c.Node)
}

func (c *Collector) recordSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastFailure == "" {
		return
	}
	c.lastFailure = ""
	klog.InfoS("remote GPU metrics collection recovered", "node", c.Node)
}

func (c *Collector) collect() ([]prometheus.Metric, error) {
	pods, err := c.Pods()
	if err != nil {
		return nil, err
	}
	// A pod whose server cannot be read is left out, rather than reported
	// with no usage, and the collection counts as failed.
	byServer := map[string][]*corev1.Pod{}
	for _, pod := range pods {
		if url := c.serverURL(pod); url != "" {
			byServer[url] = append(byServer[url], pod)
		}
	}
	// Servers are scraped in parallel, so a few slow ones cost one timeout,
	// not one each, and the collection fits in the Prometheus scrape timeout.
	urls := slices.Sorted(maps.Keys(byServer))
	results := make([]map[string]*dto.MetricFamily, len(urls))
	errs := make([]error, len(urls))
	var wg sync.WaitGroup
	for i, url := range urls {
		wg.Go(func() { results[i], errs[i] = c.scrape(url) })
	}
	wg.Wait()
	families := map[string]*dto.MetricFamily{}
	var scraped []*corev1.Pod
	var failed error
	for i, url := range urls {
		if errs[i] != nil {
			failed = errors.Join(failed, fmt.Errorf("%s: %w", url, errs[i]))
			continue
		}
		merge(families, results[i], url)
		scraped = append(scraped, byServer[url]...)
	}
	if c.Indices != nil && len(scraped) > 0 {
		indices, err := c.Indices()
		if err != nil {
			return nil, err
		}
		for name, f := range families {
			if !strings.HasPrefix(name, "lupine_host_") {
				continue
			}
			for _, m := range f.Metric {
				for _, l := range m.Label {
					if l.GetName() == "device_index" {
						if index, ok := indices[labels(m)["device_uuid"]]; ok {
							l.Value = &index
						}
					}
				}
			}
		}
	}
	samples, dropped := adapt(c.Node, families, scraped)
	if failed != nil {
		// The connections of the servers that answered are not the node's.
		samples = slices.DeleteFunc(samples, func(m prometheus.Metric) bool { return m.Desc() == connections })
	}
	for reason, n := range dropped {
		samples = append(samples, prometheus.MustNewConstMetric(droppedSamples, prometheus.GaugeValue, float64(n), c.Node, reason))
	}
	return samples, failed
}

// serverURL is the metrics endpoint of a pod's Lupine server, when the pod
// has cards on this node.
func (c *Collector) serverURL(pod *corev1.Pod) string {
	if len(allocations(c.Node, []*corev1.Pod{pod})) == 0 {
		return ""
	}
	_, port, err := net.SplitHostPort(pod.Annotations[remote.LupineServerAnno])
	if err != nil {
		return ""
	}
	return "http://" + net.JoinHostPort(c.Host, port) + "/metrics"
}

func (c *Collector) scrape(url string) (map[string]*dto.MetricFamily, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := c.Client
	if client == nil {
		client = defaultHTTPClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("lupine returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxScrapeBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxScrapeBytes {
		return nil, fmt.Errorf("lupine metrics exceeded size limit")
	}
	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	up := families["lupine_monitor_nvml_up"]
	if up == nil || len(up.Metric) != 1 || up.Metric[0].GetGauge().GetValue() != 1 {
		return nil, fmt.Errorf("lupine NVML monitoring unavailable")
	}
	return families, nil
}

// serverLabel tags each client series with the server that reported it. A
// client ID is the server's own, so two servers can each report a client 7
// that is not the same client. It never leaves the collector.
const serverLabel = "hami_lupine_server"

// merge adds one server's families to the node's. Servers that share a card
// each report it once. HAMi-core in a server that shares a card reports only
// that server's memory, so theirs add up to the card's; utilization is the
// card's own in every report.
func merge(into, from map[string]*dto.MetricFamily, server string) {
	for name, f := range from {
		dst := into[name]
		if dst == nil {
			dst = &dto.MetricFamily{Name: f.Name, Help: f.Help, Type: f.Type}
			into[name] = dst
		}
		if !strings.HasPrefix(name, "lupine_host_") {
			for _, m := range f.Metric {
				m.Label = append(m.Label, &dto.LabelPair{Name: new(serverLabel), Value: new(server)})
			}
			dst.Metric = append(dst.Metric, f.Metric...)
			continue
		}
		for _, m := range f.Metric {
			uuid := labels(m)["device_uuid"]
			i := slices.IndexFunc(dst.Metric, func(o *dto.Metric) bool { return labels(o)["device_uuid"] == uuid })
			if i < 0 {
				dst.Metric = append(dst.Metric, m)
				continue
			}
			have, add := dst.Metric[i].GetGauge(), m.GetGauge()
			if have == nil || add == nil {
				continue
			}
			v := add.GetValue()
			switch name {
			case "lupine_host_gpu_memory_used_bytes":
				v += have.GetValue()
			case "lupine_host_gpu_utilization_percent":
				v = max(v, have.GetValue())
			default:
				continue
			}
			have.Value = &v
		}
	}
}

func labels(m *dto.Metric) map[string]string {
	out := map[string]string{}
	for _, l := range m.Label {
		out[l.GetName()] = l.GetValue()
	}
	return out
}
func gauge(m *dto.Metric) (float64, bool) {
	if m.Gauge == nil {
		return 0, false
	}
	v := m.Gauge.GetValue()
	return v, !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0
}

type allocation struct {
	labels []string
	limit  float64
	// pod is the UID of the pod holding it.
	pod string
}

// workloadID reports whether a client ID is LUPINE_WORKLOAD_ID as admission
// sets it, "<pod UID>/<container>", rather than the anonymous
// "<address>:<hostname>:<pid namespace>:<pid>".
func workloadID(id string) bool {
	uid, _, ok := strings.Cut(id, "/")
	return ok && !strings.Contains(uid, ":")
}

// clientIdentities are the ways a Lupine server that does not report
// LUPINE_WORKLOAD_ID can name the pod's client: its hostname, as kubelet sets
// it, with the address its connection comes from, the pod's own or, after
// SNAT, its node's. A hostname alone is not unique: pods in two namespaces
// can share a name. A host-network pod sees the node's hostname and has none.
// The hostname follows kubelet: hostnameOverride, else spec.hostname or the
// pod name, cut to 63 characters. With setHostnameAsFQDN and a subdomain it
// is the pod's FQDN, whose cluster domain is not known here, so the identity
// keeps the prefix up to it, ending in a dot, and holds matches it as such.
func clientIdentities(pod *corev1.Pod) []string {
	if pod.Spec.HostNetwork {
		return nil
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
	addrs := []string{pod.Status.PodIP, pod.Status.HostIP}
	for _, ip := range pod.Status.PodIPs {
		addrs = append(addrs, ip.IP)
	}
	for _, ip := range pod.Status.HostIPs {
		addrs = append(addrs, ip.IP)
	}
	var out []string
	for _, addr := range addrs {
		if addr != "" && !slices.Contains(out, host+"@"+addr) {
			out = append(out, host+"@"+addr)
		}
	}
	return out
}

// allocations follows the scheduler's podManager. A Pod counts once HAMi has
// written hami.io/vgpu-node, including before it is bound and while it is
// still Pending or terminating. Succeeded, Failed, and deleted Pods do not.
// After non-sidecar init containers succeed, their devices are released.
func allocations(node string, pods []*corev1.Pod) map[string]map[string]allocation {
	out := map[string]map[string]allocation{}
	for _, pod := range pods {
		if pod == nil || pod.UID == "" || util.IsPodInTerminatedState(pod) {
			continue
		}
		if pod.Annotations[util.AssignedNodeAnnotations] == "" {
			continue
		}
		pd, err := device.DecodePodDevices(map[string]string{remote.RemoteGPUDevice: remote.AllocatedAnnos}, pod.Annotations)
		if err != nil {
			continue
		}
		containers := append(append([]corev1.Container{}, pod.Spec.InitContainers...), pod.Spec.Containers...)
		numInit := len(pod.Spec.InitContainers)
		releaseInit := util.AllNonSidecarInitContainersSucceeded(pod)
		for i, devices := range pd[remote.RemoteGPUDevice] {
			if i >= len(containers) {
				break
			}
			if releaseInit && i < numInit && !util.IsSidecarContainer(&containers[i]) {
				continue
			}
			for index, d := range devices {
				server, uuid, ok := strings.Cut(d.UUID, "/")
				if !ok || server != node || uuid == "" {
					continue
				}
				id := string(pod.UID) + "/" + containers[i].Name
				if out[id] == nil {
					out[id] = map[string]allocation{}
				}
				limit := float64(d.Usedmem) * 1024 * 1024
				out[id][uuid] = allocation{[]string{pod.Namespace, pod.Name, containers[i].Name, strconv.Itoa(index), uuid}, limit, string(pod.UID)}
			}
		}
	}
	return out
}

type sampleKey struct{ workload, uuid string }

func adapt(node string, families map[string]*dto.MetricFamily, pods []*corev1.Pod) ([]prometheus.Metric, map[string]int) {
	out := []prometheus.Metric{}
	dropped := map[string]int{}
	drop := func(reason string) { dropped[reason]++ }
	emit := func(d *prometheus.Desc, v float64, ls []string) {
		out = append(out, prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v, ls...))
	}
	gpus := map[string][2]string{} // device UUID -> index and type, from the host series
	// A series may leave a label out. Fill each from the first series that has
	// it, so the per-card series do not depend on which family is read first.
	note := func(uuid, index, typ string) {
		g := gpus[uuid]
		if g[0] == "" {
			g[0] = index
		}
		if g[1] == "" {
			g[1] = typ
		}
		gpus[uuid] = g
	}
	for _, f := range []struct {
		source string
		desc   *prometheus.Desc
	}{{"lupine_host_gpu_memory_used_bytes", hostMemory}, {"lupine_host_gpu_utilization_percent", hostUtilization}, {"lupine_host_gpu_memory_total_bytes", hostMemoryTotal}} {
		source, desc := f.source, f.desc
		seen := map[string]bool{}
		for _, m := range families[source].GetMetric() {
			l := labels(m)
			v, ok := gauge(m)
			if !ok || l["device_uuid"] == "" {
				continue
			}
			if seen[l["device_uuid"]] {
				drop("duplicate")
				continue
			}
			seen[l["device_uuid"]] = true
			if desc == hostUtilization && v > 100 {
				drop("invalid_utilization")
				continue
			}
			note(l["device_uuid"], l["device_index"], l["device_type"])
			emit(desc, v, []string{node, l["device_index"], l["device_uuid"], l["device_type"]})
		}
	}
	assigned := allocations(node, pods)
	// Released Lupine servers name a client by address and hostname rather
	// than LUPINE_WORKLOAD_ID. Such a client is still not a stranger when
	// every pod it could be holds the card, only one whose usage cannot be
	// split by container. When one of them does not, it stays unattributed.
	identPods := map[string]map[string]bool{}               // identity -> pod UIDs
	identHolders := map[string]map[string]map[string]bool{} // identity -> device UUID -> pod UIDs
	held := map[string]map[string]bool{}                    // pod UID -> device UUIDs
	for _, devices := range assigned {
		for uuid, a := range devices {
			if held[a.pod] == nil {
				held[a.pod] = map[string]bool{}
			}
			held[a.pod][uuid] = true
		}
	}
	for _, pod := range pods {
		if pod == nil || util.IsPodInTerminatedState(pod) {
			continue
		}
		uid := string(pod.UID)
		for _, ident := range clientIdentities(pod) {
			if identPods[ident] == nil {
				identPods[ident] = map[string]bool{}
				identHolders[ident] = map[string]map[string]bool{}
			}
			identPods[ident][uid] = true
			for uuid := range held[uid] {
				if identHolders[ident][uuid] == nil {
					identHolders[ident][uuid] = map[string]bool{}
				}
				identHolders[ident][uuid][uid] = true
			}
		}
	}
	holds := func(uuid, host, addr string) bool {
		ident := host + "@" + addr
		if identPods[ident] == nil {
			// A pod FQDN: its identity is the prefix before the cluster domain.
			for id := range identPods {
				prefix, a, _ := strings.Cut(id, "@")
				if a == addr && strings.HasSuffix(prefix, ".") && strings.HasPrefix(host, prefix) {
					ident = id
					break
				}
			}
		}
		return len(identPods[ident]) > 0 && len(identHolders[ident][uuid]) == len(identPods[ident])
	}
	// Usage no HAMi allocation accounts for: a session, a client set up by
	// hand, or anything else on the card.
	unattributed := map[*prometheus.Desc]map[string]float64{memory: {}, utilization: {}}
	cardClients := map[string]map[string]struct{}{}
	for source, desc := range map[string]*prometheus.Desc{"lupine_client_device_memory_used_bytes": memory, "lupine_client_device_utilization_percent": utilization} {
		sums := map[sampleKey]float64{}
		seen := map[[7]string]bool{}
		for _, m := range families[source].GetMetric() {
			l := labels(m)
			v, ok := gauge(m)
			uuid := l["device_uuid"]
			if !ok || uuid == "" {
				continue
			}
			// Lupine already merges processes with identical source labels.
			// Different process names can still belong to the same container.
			key := [7]string{l[serverLabel], l["client_id"], l["client_address"], l["client_hostname"], l["client_name"], uuid, l["device_index"]}
			if seen[key] {
				drop("duplicate")
				continue
			}
			seen[key] = true
			if desc == utilization && v > 100 {
				drop("invalid_utilization")
				continue
			}
			note(uuid, l["device_index"], "")
			if desc == memory {
				if cardClients[uuid] == nil {
					cardClients[uuid] = map[string]struct{}{}
				}
				cardClients[uuid][l[serverLabel]+" "+l["client_id"]] = struct{}{}
			}
			if _, ok := assigned[l["client_id"]][uuid]; !ok {
				// A workload ID that matches no allocation is a stranger, even
				// with a holder's hostname and address, as after a pod is
				// recreated under the same name.
				if workloadID(l["client_id"]) || !holds(uuid, l["client_hostname"], l["client_address"]) {
					unattributed[desc][uuid] += v
				}
				continue
			}
			sums[sampleKey{l["client_id"], uuid}] += v
		}
		for key, v := range sums {
			a := assigned[key.workload][key.uuid]
			if desc == utilization {
				v = math.Min(v, 100)
			}
			if math.IsInf(v, 0) {
				drop("overflow")
				continue
			}
			emit(desc, v, a.labels)
		}
	}
	// Per-card series cover every card, at zero when nothing is there, so an
	// alert on them reads a value rather than an absent series.
	for uuid, g := range gpus {
		ls := []string{node, g[0], uuid, g[1]}
		emit(clients, float64(len(cardClients[uuid])), ls)
		emit(unattributedUtilization, math.Min(unattributed[utilization][uuid], 100), ls)
		if v := unattributed[memory][uuid]; !math.IsInf(v, 0) {
			emit(unattributedMemory, v, ls)
		}
	}
	connected := map[string]struct{}{}
	for _, m := range families["lupine_server_connection_info"].GetMetric() {
		if l := labels(m); l["client_id"] != "" {
			connected[l[serverLabel]+" "+l["client_id"]] = struct{}{}
		}
	}
	emit(connections, float64(len(connected)), []string{node})
	for _, devices := range assigned {
		for _, a := range devices {
			if a.limit > 0 {
				emit(memoryLimit, a.limit, a.labels)
			}
		}
	}
	return out, dropped
}
