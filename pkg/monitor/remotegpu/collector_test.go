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
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/Project-HAMi/HAMi/pkg/device"
	remote "github.com/Project-HAMi/HAMi/pkg/device/remotegpu"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

func allocatedPod() *corev1.Pod {
	pd := device.PodDevices{remote.RemoteGPUDevice: device.PodSingleDevice{
		{}, {{UUID: "gpu-node/GPU-a", Type: remote.RemoteGPUDevice, Usedmem: 4096}}, {{UUID: "gpu-node/GPU-a", Type: remote.RemoteGPUDevice, Usedmem: 4096}},
	}}
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: types.UID("uid-1"), Namespace: "ns", Name: "worker", Annotations: map[string]string{
		remote.AllocatedAnnos:        device.EncodePodSingleDevice(pd[remote.RemoteGPUDevice]),
		util.AssignedNodeAnnotations: "cpu-node",
	}}, Spec: corev1.PodSpec{NodeName: "cpu-node", InitContainers: []corev1.Container{{Name: "setup"}}, Containers: []corev1.Container{{Name: "a"}, {Name: "b"}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
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
	ms, dropped := adapt("gpu-node", families, []*corev1.Pod{allocatedPod()})
	if len(dropped) != 0 {
		t.Fatalf("dropped samples: %v", dropped)
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
	ms, _ = adapt("gpu-node", families, []*corev1.Pod{pod})
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
	pod = allocatedPod()
	pod.Status.Phase = corev1.PodFailed
	if len(allocations("gpu-node", []*corev1.Pod{pod})) != 0 {
		t.Fatal("accepted failed Pod")
	}
}

// servedPod is allocatedPod with its own Lupine server at the given test server.
func servedPod(t *testing.T, server *httptest.Server, uid string) *corev1.Pod {
	t.Helper()
	_, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	pod := allocatedPod()
	pod.UID, pod.Name = types.UID(uid), "worker-"+uid
	pod.Annotations[remote.LupineServerAnno] = net.JoinHostPort("10.0.0.5", port)
	return pod
}

func TestScrapeFailureDoesNotReplayUsage(t *testing.T) {
	body, status := fixture, http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status); fmt.Fprint(w, body) }))
	defer server.Close()
	pod := servedPod(t, server, "uid-1")
	c := &Collector{Node: "gpu-node", Host: "127.0.0.1", Pods: func() ([]*corev1.Pod, error) { return []*corev1.Pod{pod}, nil }}
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
	got := allocations("gpu-node", []*corev1.Pod{pod})
	if got["uid-1/a"]["GPU-a"].limit != 4096*1024*1024 {
		t.Fatal("limit is not the container's grant")
	}
	pd := device.PodSingleDevice{{{UUID: "gpu-node/GPU-a", Type: remote.RemoteGPUDevice, Usedmem: 2048}}, {}, {}}
	pod.Annotations[remote.AllocatedAnnos] = device.EncodePodSingleDevice(pd)
	got = allocations("gpu-node", []*corev1.Pod{pod})
	if len(got) != 1 || got["uid-1/setup"]["GPU-a"].labels[3] != "0" {
		t.Fatalf("incorrect init container mapping: %v", got)
	}
}
func TestDuplicateSourceSeriesDropped(t *testing.T) {
	parser := expfmt.NewTextParser(model.UTF8Validation)
	text := fixture + `lupine_client_device_memory_used_bytes{client_id="uid-1/a",client_name="p1",device_uuid="GPU-a"} 100` + "\n"
	fs, err := parser.TextToMetricFamilies(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	ms, dropped := adapt("gpu-node", fs, []*corev1.Pod{allocatedPod()})
	if dropped["duplicate"] != 1 {
		t.Fatalf("duplicate count: %v", dropped)
	}
	for _, m := range ms {
		if m.Desc() != memory {
			continue
		}
		d := &dto.Metric{}
		if err := m.Write(d); err != nil {
			t.Fatal(err)
		}
		if labels(d)["container"] == "a" && d.GetGauge().GetValue() != 300 {
			t.Fatalf("duplicate sample changed container usage to %v", d.GetGauge().GetValue())
		}
	}
}

func TestServerSeries(t *testing.T) {
	text := fixture + `# TYPE lupine_host_gpu_memory_total_bytes gauge
lupine_host_gpu_memory_total_bytes{device_index="0",device_uuid="GPU-a",device_type="A100"} 4000
lupine_host_gpu_memory_total_bytes{device_index="1",device_uuid="GPU-idle",device_type="A100"} 4000
# TYPE lupine_server_connection_info gauge
lupine_server_connection_info{client_id="uid-1/a",connection_kind="cuda"} 1
lupine_server_connection_info{client_id="uid-1/a",connection_kind="nvml"} 1
lupine_server_connection_info{client_id="7",connection_kind="cuda"} 1
`
	text = strings.Replace(text, `lupine_client_device_utilization_percent{client_id="uid-1/a",client_name="p2",device_uuid="GPU-a"} 50`,
		`lupine_client_device_utilization_percent{client_id="uid-1/a",client_name="p2",device_uuid="GPU-a"} 50
lupine_client_device_utilization_percent{client_id="7",device_uuid="GPU-a"} 30`, 1)
	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	ms, dropped := adapt("gpu-node", families, []*corev1.Pod{allocatedPod()})
	if len(dropped) != 0 {
		t.Fatalf("dropped samples: %v", dropped)
	}
	values := map[string]float64{}
	for _, m := range ms {
		d := &dto.Metric{}
		if err := m.Write(d); err != nil {
			t.Fatal(err)
		}
		values[m.Desc().String()+"/"+labels(d)["device_uuid"]] = d.GetGauge().GetValue()
	}
	for _, c := range []struct {
		desc *prometheus.Desc
		uuid string
		want float64
	}{
		{hostMemoryTotal, "GPU-a", 4000},
		{hostMemoryTotal, "GPU-idle", 4000},
		{clients, "GPU-a", 4},
		{clients, "GPU-other", 1},
		{clients, "GPU-idle", 0},
		// The anonymous client and the Pod UID that no longer exists.
		{unattributedMemory, "GPU-a", 2 * 9999},
		// A known workload on a card it was not given.
		{unattributedMemory, "GPU-other", 9999},
		{unattributedMemory, "GPU-idle", 0},
		{unattributedUtilization, "GPU-a", 30},
		{connections, "", 2},
	} {
		got, ok := values[c.desc.String()+"/"+c.uuid]
		if !ok || got != c.want {
			t.Errorf("%s %s: got %v (present %v), want %v", c.desc, c.uuid, got, ok, c.want)
		}
	}
}

// A host series without device_type must not blank the type another host
// series gave the card. Families are read in map order, so repeat the read.
func TestServerSeriesKeepGPUTypeFromAnotherFamily(t *testing.T) {
	text := fixture + `# TYPE lupine_host_gpu_memory_total_bytes gauge
lupine_host_gpu_memory_total_bytes{device_index="0",device_uuid="GPU-a"} 4000
`
	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	for range 30 {
		ms, _ := adapt("gpu-node", families, []*corev1.Pod{allocatedPod()})
		for _, m := range ms {
			if m.Desc() != clients {
				continue
			}
			d := &dto.Metric{}
			if err := m.Write(d); err != nil {
				t.Fatal(err)
			}
			if l := labels(d); l["device_uuid"] == "GPU-a" && l["device_type"] != "A100" {
				t.Fatalf("device_type %q, want A100", l["device_type"])
			}
		}
	}
}

// A released Lupine server names the client by hostname and address instead
// of LUPINE_WORKLOAD_ID. The pod holding the card is not a stranger on it; a
// client with another hostname is, and so is the pod on a card it lacks.
// Pods in two namespaces can share a name, and then also the node's address
// after SNAT, so a client either of them could be stays unattributed.
func TestUnattributedSkipsTheHoldersAnonymousClient(t *testing.T) {
	holder := allocatedPod()
	holder.Status.PodIP, holder.Status.HostIP = "10.42.1.5", "10.0.0.9"
	twin := holder.DeepCopy()
	twin.UID, twin.Namespace, twin.Status.PodIP = "uid-twin", "other", "10.42.1.6"
	twin.Annotations[remote.AllocatedAnnos] = device.EncodePodSingleDevice(device.PodSingleDevice{
		{}, {{UUID: "gpu-node/GPU-other", Type: remote.RemoteGPUDevice, Usedmem: 4096}},
	})
	text := fixture + `lupine_client_device_memory_used_bytes{client_id="10.42.1.5:worker:4026531836:1",client_address="10.42.1.5",client_hostname="worker",device_uuid="GPU-a"} 300
lupine_client_device_memory_used_bytes{client_id="10.0.0.9:worker:4026531836:4",client_address="10.0.0.9",client_hostname="worker",device_uuid="GPU-a"} 30
lupine_client_device_memory_used_bytes{client_id="10.42.1.5:shell:4026531836:2",client_address="10.42.1.5",client_hostname="shell",device_uuid="GPU-a"} 50
lupine_client_device_memory_used_bytes{client_id="10.42.1.5:worker:4026531836:3",client_address="10.42.1.5",client_hostname="worker",device_uuid="GPU-other"} 70
lupine_client_device_memory_used_bytes{client_id="10.42.1.6:worker:4026531836:5",client_address="10.42.1.6",client_hostname="worker",device_uuid="GPU-a"} 7
lupine_client_device_memory_used_bytes{client_id="gone-uid/a",client_address="10.42.1.5",client_hostname="worker",device_uuid="GPU-a"} 11
`
	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	unattributedOn := func(pods ...*corev1.Pod) map[string]float64 {
		ms, _ := adapt("gpu-node", families, pods)
		got := map[string]float64{}
		for _, m := range ms {
			if m.Desc() != unattributedMemory {
				continue
			}
			d := &dto.Metric{}
			if err := m.Write(d); err != nil {
				t.Fatal(err)
			}
			got[labels(d)["device_uuid"]] = d.GetGauge().GetValue()
		}
		return got
	}
	// The fixture's own strangers: client "7" and a Pod UID that is gone, and
	// another gone Pod UID with the holder's hostname and address.
	strangers := 2*9999.0 + 11
	got := unattributedOn(holder)
	if want := strangers + 50 + 7; got["GPU-a"] != want {
		t.Errorf("GPU-a: got %v, want %v", got["GPU-a"], want)
	}
	if want := 9999.0 + 70; got["GPU-other"] != want {
		t.Errorf("GPU-other: got %v, want %v", got["GPU-other"], want)
	}
	// The twin shares the hostname and, after SNAT, the node's address.
	got = unattributedOn(holder, twin)
	if want := strangers + 30 + 50 + 7; got["GPU-a"] != want {
		t.Errorf("GPU-a with a same-named pod: got %v, want %v", got["GPU-a"], want)
	}
}

// The client's hostname is whatever kubelet set: hostnameOverride first, or
// with setHostnameAsFQDN the pod's FQDN, whose cluster domain at the end is
// the kubelet's, not known here. A pod name may itself contain dots.
func TestUnattributedMatchesTheHostnameKubeletSets(t *testing.T) {
	yes := true
	fqdn := allocatedPod()
	fqdn.Name, fqdn.Spec.Subdomain, fqdn.Spec.SetHostnameAsFQDN = "worker.v1", "svc", &yes
	override := allocatedPod()
	name := "gpu-client"
	override.Spec.HostnameOverride, override.Spec.Subdomain, override.Spec.SetHostnameAsFQDN = &name, "svc", &yes
	for _, c := range []struct {
		pod       *corev1.Pod
		own, near string
	}{
		{fqdn, "worker.v1.svc.ns.svc.cluster.local", "worker.v1.svc.other.svc.cluster.local"},
		{override, "gpu-client", "worker"},
	} {
		c.pod.Status.PodIP = "10.42.1.5"
		text := fixture + `lupine_client_device_memory_used_bytes{client_id="10.42.1.5:` + c.own + `:4026531836:1",client_address="10.42.1.5",client_hostname="` + c.own + `",device_uuid="GPU-a"} 300
lupine_client_device_memory_used_bytes{client_id="10.42.1.5:` + c.near + `:4026531836:2",client_address="10.42.1.5",client_hostname="` + c.near + `",device_uuid="GPU-a"} 50
`
		parser := expfmt.NewTextParser(model.UTF8Validation)
		families, err := parser.TextToMetricFamilies(strings.NewReader(text))
		if err != nil {
			t.Fatal(err)
		}
		ms, _ := adapt("gpu-node", families, []*corev1.Pod{c.pod})
		for _, m := range ms {
			if m.Desc() != unattributedMemory {
				continue
			}
			d := &dto.Metric{}
			if err := m.Write(d); err != nil {
				t.Fatal(err)
			}
			// The fixture's own strangers, and a hostname the pod does not have.
			if want := 2*9999.0 + 50; labels(d)["device_uuid"] == "GPU-a" && d.GetGauge().GetValue() != want {
				t.Errorf("%s: got %v, want %v", c.own, d.GetGauge().GetValue(), want)
			}
		}
	}
}

func TestInvalidUtilizationDropped(t *testing.T) {
	parser := expfmt.NewTextParser(model.UTF8Validation)
	text := strings.Replace(fixture, `device_uuid="GPU-a"} 60`, `device_uuid="GPU-a"} 150`, 1)
	fs, err := parser.TextToMetricFamilies(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	_, dropped := adapt("gpu-node", fs, []*corev1.Pod{allocatedPod()})
	if dropped["invalid_utilization"] != 1 {
		t.Fatalf("invalid utilization count: %v", dropped)
	}
}

func TestAllocationsFollowSchedulerAccounting(t *testing.T) {
	pending := allocatedPod()
	pending.Spec.NodeName = ""
	pending.Status.Phase = corev1.PodPending
	if len(allocations("gpu-node", []*corev1.Pod{pending})) == 0 {
		t.Fatal("dropped a scheduled Pod that is still Pending")
	}

	unassigned := allocatedPod()
	delete(unassigned.Annotations, util.AssignedNodeAnnotations)
	if len(allocations("gpu-node", []*corev1.Pod{unassigned})) != 0 {
		t.Fatal("counted a Pod the scheduler has not assigned")
	}

	now := metav1.Now()
	terminating := allocatedPod()
	terminating.DeletionTimestamp = &now
	if len(allocations("gpu-node", []*corev1.Pod{terminating})) == 0 {
		t.Fatal("dropped a terminating Pod")
	}

	pod := allocatedPod()
	pod.Annotations[remote.AllocatedAnnos] = device.EncodePodSingleDevice(device.PodSingleDevice{
		{{UUID: "gpu-node/GPU-a", Type: remote.RemoteGPUDevice, Usedmem: 2048}},
		{{UUID: "gpu-node/GPU-a", Type: remote.RemoteGPUDevice, Usedmem: 4096}},
		{},
	})
	if _, ok := allocations("gpu-node", []*corev1.Pod{pod})["uid-1/setup"]; !ok {
		t.Fatal("running init container allocation was dropped")
	}
	pod.Status.InitContainerStatuses = []corev1.ContainerStatus{{
		Name:  "setup",
		State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}},
	}}
	got := allocations("gpu-node", []*corev1.Pod{pod})
	if _, ok := got["uid-1/setup"]; ok {
		t.Fatal("finished init container still occupies a limit")
	}
	if _, ok := got["uid-1/a"]; !ok {
		t.Fatal("app container limit was dropped with the init container")
	}

	always := corev1.ContainerRestartPolicyAlways
	pod.Spec.InitContainers[0].RestartPolicy = &always
	if _, ok := allocations("gpu-node", []*corev1.Pod{pod})["uid-1/setup"]; !ok {
		t.Fatal("sidecar init container was released")
	}
}

// Each pod on the node has its own server. A card two servers share is
// reported once, with the memory each server's HAMi-core reports added up and
// the node's index for the card, and a server that cannot be read leaves out
// only its pod.
func TestScrapesEveryServerOnTheNode(t *testing.T) {
	// HAMi-core in each server reports the pod's share as the card's memory.
	first := fixture + "# TYPE lupine_host_gpu_memory_total_bytes gauge\nlupine_host_gpu_memory_total_bytes{device_index=\"0\",device_uuid=\"GPU-a\"} 1048576000\n"
	other := strings.ReplaceAll(first, "uid-1/", "uid-2/")
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, first) }))
	defer up.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, other) }))
	defer second.Close()
	pods := []*corev1.Pod{servedPod(t, up, "uid-1"), servedPod(t, second, "uid-2")}
	c := &Collector{Node: "gpu-node", Host: "127.0.0.1", Pods: func() ([]*corev1.Pod, error) { return pods, nil },
		Cards: func() (map[string]Card, error) { return map[string]Card{"GPU-a": {Index: "3", Memory: 4000}}, nil }}
	fs := metricsFor(t, c)
	if got := fs["hami_remote_gpumonitor_collect_success"].Metric[0].GetGauge().GetValue(); got != 1 {
		t.Fatalf("success = %v", got)
	}
	if _, ok := fs["hami_remote_gpumonitor_dropped_samples"]; ok {
		t.Fatal("a card two servers report was counted as a duplicate")
	}
	if n := len(fs["hami_host_gpu_memory_used_bytes"].GetMetric()); n != 1 {
		t.Fatalf("host series = %d, want 1", n)
	}
	host := fs["hami_host_gpu_memory_used_bytes"].Metric[0]
	if host.GetGauge().GetValue() != 2000 || labels(host)["device_index"] != "3" {
		t.Fatalf("host memory = %v at index %q, want 2000 at 3", host.GetGauge().GetValue(), labels(host)["device_index"])
	}
	if total := fs["hami_host_gpu_memory_total_bytes"].Metric[0]; total.GetGauge().GetValue() != 4000*1024*1024 || labels(total)["device_index"] != "3" {
		t.Fatalf("host total = %v at index %q, want the registered 4000 MiB at 3", total.GetGauge().GetValue(), labels(total)["device_index"])
	}
	if got := fs["hami_host_gpu_utilization_ratio"].Metric[0].GetGauge().GetValue(); got != 70 {
		t.Fatalf("host utilization = %v", got)
	}
	if n := len(fs["hami_vgpu_memory_used_bytes"].GetMetric()); n != 4 {
		t.Fatalf("container series = %d, want 4", n)
	}
	// Client IDs are each server's own: both servers' client 7 and stale
	// previous-uid/a are four clients, not two duplicates.
	onCard := func(name string) float64 {
		for _, m := range fs[name].GetMetric() {
			if labels(m)["device_uuid"] == "GPU-a" {
				return m.GetGauge().GetValue()
			}
		}
		t.Fatalf("no %s for GPU-a", name)
		return 0
	}
	if got := onCard("hami_remote_gpu_clients"); got != 8 {
		t.Fatalf("clients = %v, want 8", got)
	}
	if got := onCard("hami_remote_gpu_unattributed_memory_used_bytes"); got != 4*9999 {
		t.Fatalf("unattributed memory = %v, want %v", got, 4*9999)
	}

	second.Close()
	fs = metricsFor(t, c)
	if got := fs["hami_remote_gpumonitor_collect_success"].Metric[0].GetGauge().GetValue(); got != 0 {
		t.Fatalf("success with a server down = %v", got)
	}
	if n := len(fs["hami_vgpu_memory_used_bytes"].GetMetric()); n != 2 {
		t.Fatalf("container series with a server down = %d, want 2", n)
	}
}

// Every server answers only once all of them are being scraped, so the
// collection succeeds only if the scrapes run at the same time.
func TestScrapesServersInParallel(t *testing.T) {
	const n = 4
	var arrived atomic.Int32
	all := make(chan struct{})
	var pods []*corev1.Pod
	for i := range n {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if arrived.Add(1) == n {
				close(all)
			}
			select {
			case <-all:
				fmt.Fprint(w, strings.ReplaceAll(fixture, "uid-1/", fmt.Sprintf("uid-%d/", i)))
			case <-r.Context().Done():
			}
		}))
		defer server.Close()
		pods = append(pods, servedPod(t, server, fmt.Sprintf("uid-%d", i)))
	}
	c := &Collector{Node: "gpu-node", Host: "127.0.0.1", Client: &http.Client{Timeout: time.Second},
		Pods: func() ([]*corev1.Pod, error) { return pods, nil }}
	fs := metricsFor(t, c)
	if got := fs["hami_remote_gpumonitor_collect_success"].Metric[0].GetGauge().GetValue(); got != 1 {
		t.Fatalf("success = %v, the servers were not scraped in parallel", got)
	}
	if got := len(fs["hami_vgpu_memory_used_bytes"].GetMetric()); got != 2*n {
		t.Fatalf("container series = %d, want %d", got, 2*n)
	}
}
