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

package main

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	versionmetrics "github.com/Project-HAMi/HAMi/pkg/metrics"
	"github.com/Project-HAMi/HAMi/pkg/monitor/nvidia"
)

func TestVGPUMonitorCollectorHealth(t *testing.T) {
	// Ensure node name env is unset to trigger pod_container_info and mig_info errors.
	t.Setenv("NODE_NAME", "")
	reg := prometheus.NewPedanticRegistry()
	rec := versionmetrics.NewCollectorHealthRecorder()
	// Construct a minimal ClusterManager with the health recorder.
	cl := &nvidia.ContainerLister{} // zero-value provides Lock/Unlock methods.
	cm := &ClusterManager{Zone: "testzone", containerLister: cl, health: rec}
	cc := ClusterManagerCollector{ClusterManager: cm}

	reg.MustRegister(cc)

	// Gather metrics from registry.
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}
	// Verify that errors_total contains pod_container_info and mig_info.
	var foundPodInfo, foundMigInfo bool
	var foundDuration bool
	for _, mf := range mfs {
		switch mf.GetName() {
		case "hami_collector_errors_total":
			for _, m := range mf.GetMetric() {
				comp := ""
				phase := ""
				for _, lp := range m.GetLabel() {
					if lp.GetName() == "component" {
						comp = lp.GetValue()
					}
					if lp.GetName() == "phase" {
						phase = lp.GetValue()
					}
				}
				if comp == versionmetrics.ComponentVGPUMonitor && phase == versionmetrics.PhasePodContainerInfo {
					foundPodInfo = true
				}
				if comp == versionmetrics.ComponentVGPUMonitor && phase == versionmetrics.PhaseMIGInfo {
					foundMigInfo = true
				}
			}
		case "hami_collector_duration_seconds":
			if len(mf.GetMetric()) != 1 {
				t.Fatalf("expected one duration histogram, got %d", len(mf.GetMetric()))
			}
			if mf.GetMetric()[0].GetHistogram().GetSampleCount() != 1 {
				t.Fatalf("expected duration histogram sample count 1")
			}
			foundDuration = true
		case "hami_collector_last_run_timestamp_seconds":
			// Since all phases failed, last_run should NOT be stamped.
			t.Fatalf("hami_collector_last_run_timestamp_seconds should not be set when all phases failed")
		}
	}
	if !foundPodInfo {
		t.Fatalf("hami_collector_errors_total missing pod_container_info error")
	}
	if !foundMigInfo {
		t.Fatalf("hami_collector_errors_total missing mig_info error")
	}
	if !foundDuration {
		t.Fatalf("hami_collector_duration_seconds not found")
	}
}
