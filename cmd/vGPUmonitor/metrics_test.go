/*
Copyright 2024 The HAMi Authors.

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

package main

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Project-HAMi/HAMi/pkg/monitor/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

func TestDescribeCollectSync(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()

	t.Setenv(util.NodeNameEnvName, "test-node")
	client := fake.NewSimpleClientset()
	informerFactory := informers.NewSharedInformerFactory(client, 0)
	podLister := informerFactory.Core().V1().Pods().Lister()

	c := &ClusterManager{
		Zone:            "test-zone",
		LegacyMetrics:   false,
		PodLister:       podLister,
		containerLister: &nvidia.ContainerLister{},
	}
	cc := ClusterManagerCollector{ClusterManager: c}

	if err := reg.Register(cc); err != nil {
		t.Fatalf("Failed to register ClusterManagerCollector (non-legacy): %v", err)
	}

	if _, err := reg.Gather(); err != nil {
		t.Errorf("Gather failed (non-legacy): %v", err)
	}

	regLegacy := prometheus.NewPedanticRegistry()
	cLegacy := &ClusterManager{
		Zone:            "test-zone-legacy",
		LegacyMetrics:   true,
		PodLister:       podLister,
		containerLister: &nvidia.ContainerLister{},
	}
	initLegacyDescriptors()
	ccLegacy := ClusterManagerCollector{ClusterManager: cLegacy}

	if err := regLegacy.Register(ccLegacy); err != nil {
		t.Fatalf("Failed to register ClusterManagerCollector (legacy): %v", err)
	}
	if _, err := regLegacy.Gather(); err != nil {
		t.Errorf("Gather failed (legacy): %v", err)
	}
}

// TestCollectContainerMetricsOffsetNoUnderflow guards the offset label of the
// Device_memory_desc_of_container legacy metric. The offset used to be derived as
// total - context - module - buffer, an unsigned subtraction that underflows to a
// value near MaxUint64 whenever context+module+buffer momentarily exceeds total
// (the four sizes are summed independently over the shared region and are not an
// atomic snapshot). The collector now reports the offset recorded by the CUDA hook
// directly, so the label must equal that recorded value regardless of how the
// other sizes relate to total.
func TestCollectContainerMetricsOffsetNoUnderflow(t *testing.T) {
	initLegacyDescriptors()

	const recordedOffset = 4096
	// context+module+buffer (150) deliberately exceeds total (100); the old
	// derivation would underflow here.
	info := &stubInfo{
		uuids:   []string{"GPU-00000000-1111-2222-3333-444444444444"},
		total:   []uint64{100},
		limit:   []uint64{100},
		ctxSize: []uint64{60},
		modSize: []uint64{50},
		bufSize: []uint64{40},
		offset:  []uint64{recordedOffset},
	}
	c := &nvidia.ContainerUsage{PodUID: "uid", ContainerName: "ctr", Info: info}
	pod := &corev1.Pod{}
	pod.Namespace = "ns"
	pod.Name = "pod"

	ch := make(chan prometheus.Metric, 64)
	cc := ClusterManagerCollector{}
	if err := cc.collectContainerMetrics(ch, pod, corev1.Container{Name: "ctr"}, c, 0); err != nil {
		t.Fatalf("collectContainerMetrics returned error: %v", err)
	}
	close(ch)

	var got string
	found := false
	for m := range ch {
		if !strings.Contains(m.Desc().String(), "Device_memory_desc_of_container") {
			continue
		}
		var dm dto.Metric
		if err := m.Write(&dm); err != nil {
			t.Fatalf("failed to write metric: %v", err)
		}
		for _, l := range dm.GetLabel() {
			if l.GetName() == "offset" {
				got = l.GetValue()
				found = true
			}
		}
	}
	if !found {
		t.Fatal("Device_memory_desc_of_container metric with an offset label was not emitted")
	}
	if got != "4096" {
		t.Errorf("offset label = %q, want %q (recorded offset must be reported verbatim, not derived and underflowed)", got, "4096")
	}
}
