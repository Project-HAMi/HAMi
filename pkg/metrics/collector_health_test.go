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

package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestCollectorHealthRecorderBasic(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	// Create recorder and register metrics on this registry.
	rec := NewCollectorHealthRecorder()
	reg.MustRegister(rec)

	// Record a few errors on different phases.
	rec.RecordError(ComponentVGPUMonitor, PhaseGPUInfo)
	rec.RecordError(ComponentVGPUMonitor, PhaseGPUInfo)
	rec.RecordError(ComponentVGPUMonitor, PhaseSendMetric)

	// Simulate a scrape duration and stamp last run.
	start := time.Now().Add(-500 * time.Millisecond)
	rec.ObserveDuration(ComponentVGPUMonitor, start)
	rec.StampLastRun(ComponentVGPUMonitor)

	// Verify errors_total metric family.
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}
	var errorsFound bool
	var durationFound bool
	var lastRunFound bool
	for _, mf := range mfs {
		switch mf.GetName() {
		case "hami_collector_errors_total":
			errorsFound = true
			// Verify expected label combos and counts.
			expected := map[string]float64{
				"vgpumonitor|gpu_info":    2,
				"vgpumonitor|send_metric": 1,
			}
			for _, m := range mf.GetMetric() {
				var comp, phase string
				for _, lp := range m.GetLabel() {
					if lp.GetName() == "component" {
						comp = lp.GetValue()
					}
					if lp.GetName() == "phase" {
						phase = lp.GetValue()
					}
				}
				key := comp + "|" + phase
				want, ok := expected[key]
				if !ok {
					t.Fatalf("unexpected error metric label combo %s|%s", comp, phase)
				}
				if got := m.GetCounter().GetValue(); got != want {
					t.Fatalf("error count for %s|%s = %v, want %v", comp, phase, got, want)
				}
				delete(expected, key)
			}
			if len(expected) != 0 {
				t.Fatalf("some expected error metrics were not observed: %v", expected)
			}
		case "hami_collector_duration_seconds":
			durationFound = true
			// Expect a histogram with a single sample count.
			if len(mf.GetMetric()) != 1 {
				t.Fatalf("expected one histogram metric, got %d", len(mf.GetMetric()))
			}
			hist := mf.GetMetric()[0].GetHistogram()
			if hist.GetSampleCount() != 1 {
				t.Fatalf("expected histogram sample count 1, got %d", hist.GetSampleCount())
			}
		case "hami_collector_last_run_timestamp_seconds":
			lastRunFound = true
			// Gauge should have a value > start.
			if len(mf.GetMetric()) != 1 {
				t.Fatalf("expected one gauge metric for last run, got %d", len(mf.GetMetric()))
			}
			val := mf.GetMetric()[0].GetGauge().GetValue()
			if val < float64(start.Unix()) {
				t.Fatalf("last run timestamp %v is before start %v", val, start.Unix())
			}
		}
	}
	if !errorsFound {
		t.Fatalf("hami_collector_errors_total metric family not found")
	}
	if !durationFound {
		t.Fatalf("hami_collector_duration_seconds metric family not found")
	}
	if !lastRunFound {
		t.Fatalf("hami_collector_last_run_timestamp_seconds metric family not found")
	}
}

func TestCollectorHealthRecorderNilSafety(t *testing.T) {
	var rec *CollectorHealthRecorder
	// Should not panic.
	rec.RecordError(ComponentScheduler, PhaseListScheduledPods)
	rec.ObserveDuration(ComponentScheduler, time.Now())
	rec.StampLastRun(ComponentScheduler)
}
