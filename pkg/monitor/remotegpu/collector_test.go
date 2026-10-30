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

package remotegpu

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/Project-HAMi/HAMi/pkg/device"
	remote "github.com/Project-HAMi/HAMi/pkg/device/remotegpu"
)

func allocatedPod() *corev1.Pod {
	pd := device.PodDevices{remote.RemoteGPUDevice: device.PodSingleDevice{
		{}, {{UUID: "gpu-node/GPU-a", Type: remote.RemoteGPUDevice, Usedmem: 4096}}, {{UUID: "gpu-node/GPU-a", Type: remote.RemoteGPUDevice, Usedmem: 4096}},
	}}
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: types.UID("uid-1"), Namespace: "ns", Name: "worker", Annotations: map[string]string{remote.AllocatedAnnos: device.EncodePodSingleDevice(pd[remote.RemoteGPUDevice])}}, Spec: corev1.PodSpec{NodeName: "cpu-node", InitContainers: []corev1.Container{{Name: "setup"}}, Containers: []corev1.Container{{Name: "a"}, {Name: "b"}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
}

const fixture = `# TYPE lupine_monitor_nvml_up gauge
lupine_monitor_nvml_up 1
# TYPE lupine_host_gpu_memory_used_bytes gauge
lupine_host_gpu_memory_used_bytes{device_index="0",device_uuid="GPU-a",device_type="A100"} 1000
# TYPE lupine_host_gpu_utilization_percent gauge
lupine_host_gpu_utilization_percent{device_index="0",device_uuid="GPU-a",device_type="A100"} 70
# TYPE lupine_client_device_memory_used_bytes gauge
lupine_client_device_memory_used_bytes{client_id="uid-1/a",client_name="p1",device_uuid="GPU-a"} 100
lupine_client_device_memory_used_bytes{client_id="uid-1/a",client_name="p2",device_uuid="GPU-a"} 200
lupine_client_device_memory_used_bytes{client_id="uid-1/b",client_name="p3",device_uuid="GPU-a"} 400
lupine_client_device_memory_used_bytes{client_id="7",device_uuid="GPU-a"} 9999
lupine_client_device_memory_used_bytes{client_id="uid-1/a",client_name="bad-card",device_uuid="GPU-other"} 9999
lupine_client_device_memory_used_bytes{client_id="previous-uid/a",client_name="old-pod",device_uuid="GPU-a"} 9999
# TYPE lupine_client_device_utilization_percent gauge
lupine_client_device_utilization_percent{client_id="uid-1/a",client_name="p1",device_uuid="GPU-a"} 60
lupine_client_device_utilization_percent{client_id="uid-1/a",client_name="p2",device_uuid="GPU-a"} 50
`

func metricsFor(t *testing.T, c *Collector) map[string]*dto.MetricFamily {
	t.Helper()
	r := prometheus.NewRegistry()
	r.MustRegister(c)
	fs, err := r.Gather()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*dto.MetricFamily{}
	for _, f := range fs {
		out[f.GetName()] = f
	}
	return out
}
func TestAttributionAndAggregation(t *testing.T) {
	checkAttribution(t, fixture)
}

func TestUpstreamAggregatedProcesses(t *testing.T) {
	aggregated := strings.ReplaceAll(fixture, `client_name="p2"`, `client_name="p1"`)
	aggregated = strings.ReplaceAll(aggregated, `device_uuid="GPU-a"} 100`, `device_uuid="GPU-a"} 300`)
	aggregated = strings.ReplaceAll(aggregated, `lupine_client_device_memory_used_bytes{client_id="uid-1/a",client_name="p1",device_uuid="GPU-a"} 200`+"\n", "")
	aggregated = strings.ReplaceAll(aggregated, `device_uuid="GPU-a"} 60`, `device_uuid="GPU-a"} 100`)
	aggregated = strings.ReplaceAll(aggregated, `lupine_client_device_utilization_percent{client_id="uid-1/a",client_name="p1",device_uuid="GPU-a"} 50`+"\n", "")
	checkAttribution(t, aggregated)
}

func checkAttribution(t *testing.T, fixture string) {
	t.Helper()
	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(strings.NewReader(fixture))
	if err != nil {
		t.Fatal(err)
	}
	ms, err := adapt("gpu-node", families, []*corev1.Pod{allocatedPod()})
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]float64{}
	for _, m := range ms {
		d := &dto.Metric{}
		if err := m.Write(d); err != nil {
			t.Fatal(err)
		}
		l := labels(d)
		values[m.Desc().String()+"/"+l["container"]] = d.GetGauge().GetValue()
	}
	check := func(desc *prometheus.Desc, ctr string, want float64) {
		t.Helper()
		if got := values[desc.String()+"/"+ctr]; got != want {
			t.Fatalf("%s/%s: got %v want %v", desc, ctr, got, want)
		}
	}
	check(memory, "a", 300)
	check(memory, "b", 400)
	check(deviceMemory, "a", 300)
	check(utilization, "a", 100)
	check(memoryLimit, "a", 4096*1024*1024)
	for _, m := range ms {
		d := &dto.Metric{}
		_ = m.Write(d)
		l := labels(d)
		if l["container"] != "" && (l["vdevice_index"] != "0" || l["namespace"] != "ns" || l["pod"] != "worker" || l["device_uuid"] != "GPU-a") {
			t.Fatalf("unexpected labels: %v", l)
		}
	}
	// Same Pod name with a new UID must not receive the old process's usage.
	pod := allocatedPod()
	pod.UID = "uid-2"
	ms, err = adapt("gpu-node", families, []*corev1.Pod{pod})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range ms {
		if m.Desc() == memory {
			t.Fatal("recreated Pod inherited stale usage")
		}
	}
	if got := allocations("another-server", []*corev1.Pod{allocatedPod()}); len(got) != 0 {
		t.Fatal("accepted allocation on another server")
	}
	pod = allocatedPod()
	pod.Status.Phase = corev1.PodSucceeded
	if len(allocations("gpu-node", []*corev1.Pod{pod})) != 0 {
		t.Fatal("accepted completed Pod")
	}
}
func TestScrapeFailureDoesNotReplayUsage(t *testing.T) {
	body, status := fixture, http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status); fmt.Fprint(w, body) }))
	defer server.Close()
	c := &Collector{Node: "gpu-node", URL: server.URL, Pods: func() ([]*corev1.Pod, error) { return []*corev1.Pod{allocatedPod()}, nil }}
	fs := metricsFor(t, c)
	if fs["hami_remote_gpumonitor_collect_success"].Metric[0].GetGauge().GetValue() != 1 {
		t.Fatal("initial scrape failed")
	}
	for _, test := range []struct {
		name, body string
		status     int
	}{
		{"HTTP failure", fixture, 500}, {"NVML failure", strings.Replace(fixture, "lupine_monitor_nvml_up 1", "lupine_monitor_nvml_up 0", 1), 200}, {"malformed", "invalid{", 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			body, status = test.body, test.status
			fs := metricsFor(t, c)
			if len(fs) != 1 || fs["hami_remote_gpumonitor_collect_success"].Metric[0].GetGauge().GetValue() != 0 {
				t.Fatalf("failed scrape emitted usage: %v", fs)
			}
		})
	}
	body, status = fixture, 200
	c.Pods = func() ([]*corev1.Pod, error) { return nil, fmt.Errorf("cache not synced") }
	if len(metricsFor(t, c)) != 1 {
		t.Fatal("unsynced cache emitted usage")
	}
}

func TestQuotaAndInitContainerAllocation(t *testing.T) {
	pod := allocatedPod()
	pod.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "CUDA_DEVICE_MEMORY_LIMIT", Value: "1024m"}}
	got := allocations("gpu-node", []*corev1.Pod{pod})
	if got["uid-1/a"]["GPU-a"].limit != 1024*1024*1024 {
		t.Fatal("ignored HAMi-core quota")
	}
	pd := device.PodSingleDevice{{{UUID: "gpu-node/GPU-a", Type: remote.RemoteGPUDevice, Usedmem: 2048}}, {}, {}}
	pod.Annotations[remote.AllocatedAnnos] = device.EncodePodSingleDevice(pd)
	got = allocations("gpu-node", []*corev1.Pod{pod})
	if len(got) != 1 || got["uid-1/setup"]["GPU-a"].labels[3] != "0" {
		t.Fatalf("incorrect init container mapping: %v", got)
	}
}
func TestDuplicateSourceSeriesRejected(t *testing.T) {
	parser := expfmt.NewTextParser(model.UTF8Validation)
	text := fixture + `lupine_client_device_memory_used_bytes{client_id="uid-1/a",client_name="p1",device_uuid="GPU-a"} 100` + "\n"
	fs, err := parser.TextToMetricFamilies(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	if ms, err := adapt("gpu-node", fs, []*corev1.Pod{allocatedPod()}); err == nil || len(ms) != 0 {
		t.Fatal("duplicate process sample was double counted")
	}
}
