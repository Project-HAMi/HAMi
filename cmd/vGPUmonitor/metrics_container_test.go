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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/monitor/nvidia"
)

const testUUID = "GPU-12345678-1234-1234-1234-123456789012" // 40 chars, valid UTF-8

func collectContainer(t *testing.T, cu *nvidia.ContainerUsage, nowSec int64) ([]prometheus.Metric, error) {
	t.Helper()
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "trainer-0"}}
	ctr := corev1.Container{Name: "worker"}
	cc := ClusterManagerCollector{ClusterManager: &ClusterManager{}}
	ch := make(chan prometheus.Metric, 64)
	err := cc.collectContainerMetrics(ch, pod, ctr, cu, nowSec)
	close(ch)
	var got []prometheus.Metric
	for m := range ch {
		got = append(got, m)
	}
	return got, err
}

func gaugeValue(t *testing.T, m prometheus.Metric) (float64, map[string]string) {
	t.Helper()
	var d dto.Metric
	if err := m.Write(&d); err != nil {
		t.Fatal(err)
	}
	labels := map[string]string{}
	for _, p := range d.GetLabel() {
		labels[p.GetName()] = p.GetValue()
	}
	return d.GetGauge().GetValue(), labels
}

func TestCollectContainerMetrics(t *testing.T) {
	cu := &nvidia.ContainerUsage{Info: &stubInfo{
		uuids:      []string{testUUID},
		total:      []uint64{8000},
		limit:      []uint64{10000},
		ctxSize:    []uint64{1000},
		modSize:    []uint64{500},
		bufSize:    []uint64{300},
		smUtil:     []uint64{42},
		lastKernel: 100,
	}}

	metrics, err := collectContainer(t, cu, 160)
	if err != nil {
		t.Fatal(err)
	}

	want := map[*prometheus.Desc]float64{
		ctrvGPUdesc:                8000,
		ctrvGPUlimitdesc:           10000,
		ctrDeviceMemorydesc:        8000,
		ctrDeviceUtilizationdesc:   42,
		ctrDeviceMemoryContextDesc: 1000,
		ctrDeviceMemoryModuleDesc:  500,
		ctrDeviceMemoryBufferDesc:  300,
		ctrDeviceLastKernelDesc:    60, // nowSec - lastKernel = 160 - 100
	}
	if len(metrics) != len(want) {
		t.Fatalf("got %d metrics, want %d", len(metrics), len(want))
	}
	for _, m := range metrics {
		exp, ok := want[m.Desc()]
		if !ok {
			t.Errorf("unexpected metric: %s", m.Desc())
			continue
		}
		delete(want, m.Desc())
		v, labels := gaugeValue(t, m)
		if v != exp {
			t.Errorf("%s = %v, want %v", m.Desc(), v, exp)
		}
		if labels["namespace"] != "team-a" || labels["pod"] != "trainer-0" ||
			labels["container"] != "worker" || labels["vdevice_index"] != "0" ||
			labels["device_uuid"] != testUUID {
			t.Errorf("%s has wrong labels: %v", m.Desc(), labels)
		}
	}
	if len(want) != 0 {
		t.Errorf("expected metrics not emitted: %v", want)
	}
}

func TestCollectContainerMetricsNoKernelActivity(t *testing.T) {
	cu := &nvidia.ContainerUsage{Info: &stubInfo{uuids: []string{testUUID}}}
	metrics, err := collectContainer(t, cu, 160)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range metrics {
		if m.Desc() == ctrDeviceLastKernelDesc {
			t.Error("last-kernel metric should not be emitted when no kernel has run")
		}
	}
	if len(metrics) != 7 {
		t.Errorf("got %d metrics, want 7", len(metrics))
	}
}

func TestCollectContainerMetricsBadInput(t *testing.T) {
	if _, err := collectContainer(t, nil, 0); err == nil {
		t.Error("nil ContainerUsage should return an error")
	}
	if _, err := collectContainer(t, &nvidia.ContainerUsage{}, 0); err == nil {
		t.Error("nil ContainerUsage.Info should return an error")
	}
	short := &nvidia.ContainerUsage{Info: &stubInfo{uuids: []string{"gpu-short"}}}
	if _, err := collectContainer(t, short, 0); err == nil {
		t.Error("a UUID shorter than 40 chars should return an error")
	}
}

func TestCollectContainerMetricsSkipsInvalidUTF8UUID(t *testing.T) {
	bad := "\xff" + strings.Repeat("a", 39) // 40 bytes, not valid UTF-8
	cu := &nvidia.ContainerUsage{Info: &stubInfo{uuids: []string{bad}}}
	metrics, err := collectContainer(t, cu, 0)
	if err != nil {
		t.Fatalf("invalid UTF-8 UUID should be skipped, not error: %v", err)
	}
	if len(metrics) != 0 {
		t.Errorf("got %d metrics for an invalid UUID, want 0", len(metrics))
	}
}
