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
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
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
)

const maxScrapeBytes = 16 << 20

var memory = metrics.ContainerMemory
var memoryLimit = metrics.ContainerMemoryLimit
var deviceMemory = metrics.ContainerDeviceMemory
var utilization = metrics.ContainerUtilization
var hostMemory = metrics.HostMemory
var hostUtilization = metrics.HostUtilization
var success = prometheus.NewDesc("hami_remote_gpumonitor_collect_success", "Whether Lupine scraping and Pod attribution succeeded", []string{"node"}, nil)

// Collector never stores previous samples: failures cannot replay stale usage.
// Pods must be backed by a synchronized informer. Node is the GPU server node.
type Collector struct {
	Node, URL string
	Client    *http.Client
	Pods      func() ([]*corev1.Pod, error)
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{memory, memoryLimit, deviceMemory, utilization, hostMemory, hostUtilization, success} {
		ch <- d
	}
}
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	metrics, err := c.collect()
	value := 1.0
	if err != nil {
		value = 0
		klog.ErrorS(err, "remote GPU metrics collection failed", "node", c.Node)
	}
	for _, m := range metrics {
		ch <- m
	}
	ch <- prometheus.MustNewConstMetric(success, prometheus.GaugeValue, value, c.Node)
}

func (c *Collector) collect() ([]prometheus.Metric, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return nil, err
	}
	client := c.Client
	if client == nil {
		client = http.DefaultClient
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
	families, err := parser.TextToMetricFamilies(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	up := families["lupine_monitor_nvml_up"]
	if up == nil || len(up.Metric) != 1 || up.Metric[0].GetGauge().GetValue() != 1 {
		return nil, fmt.Errorf("lupine NVML monitoring unavailable")
	}
	pods, err := c.Pods()
	if err != nil {
		return nil, err
	}
	return adapt(c.Node, families, pods)
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
}

// Match UID, exact container name and GPU allocation on this server. A Pod
// recreated under the same name, or a sample from another server, cannot match.
func allocations(node string, pods []*corev1.Pod) map[string]map[string]allocation {
	out := map[string]map[string]allocation{}
	for _, pod := range pods {
		if pod.UID == "" || pod.Spec.NodeName == "" || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		pd, err := device.DecodePodDevices(map[string]string{remote.RemoteGPUDevice: remote.AllocatedAnnos}, pod.Annotations)
		if err != nil {
			continue
		}
		containers := append(append([]corev1.Container{}, pod.Spec.InitContainers...), pod.Spec.Containers...)
		for i, devices := range pd[remote.RemoteGPUDevice] {
			if i >= len(containers) {
				break
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
				for _, env := range containers[i].Env {
					if env.Name == "CUDA_DEVICE_MEMORY_LIMIT" && strings.HasSuffix(env.Value, "m") {
						mib, err := strconv.ParseUint(strings.TrimSuffix(env.Value, "m"), 10, 64)
						if err == nil && mib > 0 {
							limit = math.Min(limit, float64(mib)*1024*1024)
						}
					}
				}
				out[id][uuid] = allocation{[]string{pod.Namespace, pod.Name, containers[i].Name, strconv.Itoa(index), uuid}, limit}
			}
		}
	}
	return out
}

type sampleKey struct{ workload, uuid string }

func adapt(node string, families map[string]*dto.MetricFamily, pods []*corev1.Pod) ([]prometheus.Metric, error) {
	out := []prometheus.Metric{}
	emit := func(d *prometheus.Desc, v float64, ls []string) {
		out = append(out, prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v, ls...))
	}
	for source, desc := range map[string]*prometheus.Desc{"lupine_host_gpu_memory_used_bytes": hostMemory, "lupine_host_gpu_utilization_percent": hostUtilization} {
		seen := map[string]bool{}
		for _, m := range families[source].GetMetric() {
			l := labels(m)
			v, ok := gauge(m)
			if !ok || l["device_uuid"] == "" {
				continue
			}
			if seen[l["device_uuid"]] {
				return nil, fmt.Errorf("duplicate host GPU sample")
			}
			seen[l["device_uuid"]] = true
			if desc == hostUtilization && v > 100 {
				return nil, fmt.Errorf("invalid host utilization")
			}
			emit(desc, v, []string{node, l["device_index"], l["device_uuid"], l["device_type"]})
		}
	}
	assigned := allocations(node, pods)
	for source, desc := range map[string]*prometheus.Desc{"lupine_client_device_memory_used_bytes": memory, "lupine_client_device_utilization_percent": utilization} {
		sums := map[sampleKey]float64{}
		seen := map[[6]string]bool{}
		for _, m := range families[source].GetMetric() {
			l := labels(m)
			v, ok := gauge(m)
			if !ok {
				continue
			}
			if _, ok := assigned[l["client_id"]][l["device_uuid"]]; !ok {
				continue
			}
			if l["client_id"] == "" {
				continue
			}
			// Lupine already merges processes with identical source labels.
			// Different process names can still belong to the same container.
			key := [6]string{l["client_id"], l["client_address"], l["client_hostname"], l["client_name"], l["device_uuid"], l["device_index"]}
			if seen[key] {
				return nil, fmt.Errorf("duplicate client GPU sample")
			}
			seen[key] = true
			if desc == utilization && v > 100 {
				return nil, fmt.Errorf("invalid client utilization")
			}
			sums[sampleKey{l["client_id"], l["device_uuid"]}] += v
		}
		for key, v := range sums {
			a := assigned[key.workload][key.uuid]
			if desc == utilization {
				v = math.Min(v, 100)
			}
			if math.IsInf(v, 0) {
				return nil, fmt.Errorf("container GPU usage overflow")
			}
			emit(desc, v, a.labels)
			if desc == memory {
				emit(deviceMemory, v, a.labels)
			}
		}
	}
	for _, devices := range assigned {
		for _, a := range devices {
			if a.limit > 0 {
				emit(memoryLimit, a.limit, a.labels)
			}
		}
	}
	return out, nil
}
