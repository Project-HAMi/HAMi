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
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/metrics"
	"github.com/Project-HAMi/HAMi/pkg/scheduler"
	"github.com/Project-HAMi/HAMi/pkg/util/leaderelection"
)

// fakeSchedulerProvider implements schedulerMetricsProvider for testing.
type fakeSchedulerProvider struct {
	nodeUsage *map[string]*scheduler.NodeUsage
	quotaMgr  *device.QuotaManager
	podMgr    *device.PodManager
}

func (f *fakeSchedulerProvider) InspectAllNodesUsage() *map[string]*scheduler.NodeUsage {
	if f.nodeUsage == nil {
		empty := make(map[string]*scheduler.NodeUsage)
		return &empty
	}
	return f.nodeUsage
}

func (f *fakeSchedulerProvider) GetQuotaManager() *device.QuotaManager {
	if f.quotaMgr == nil {
		return device.NewQuotaManager()
	}
	return f.quotaMgr
}

func (f *fakeSchedulerProvider) GetPodManager() *device.PodManager {
	if f.podMgr == nil {
		return device.NewPodManager()
	}
	return f.podMgr
}

func (f *fakeSchedulerProvider) GetLeaderManager() leaderelection.LeaderManager {
	return leaderelection.NewDummyLeaderManager(true)
}

func (f *fakeSchedulerProvider) IsSynced() bool {
	return true
}

func TestSchedulerCollectorHealth(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	rec := metrics.NewCollectorHealthRecorder()

	// Construct a minimal ClusterManager with the health recorder.
	provider := &fakeSchedulerProvider{}
	cm := &ClusterManager{
		Zone:   "testzone",
		health: rec,
	}
	cc := ClusterManagerCollector{
		ClusterManager:  cm,
		metricsProvider: provider,
	}
	reg.MustRegister(cc)

	// Gather metrics from registry.
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	// Verify that health metrics are present.
	var foundDuration, foundLastRun bool
	for _, mf := range mfs {
		switch mf.GetName() {
		case "hami_collector_duration_seconds":
			// Should be a histogram with one sample.
			if len(mf.GetMetric()) != 1 {
				t.Fatalf("expected one duration histogram, got %d", len(mf.GetMetric()))
			}
			if mf.GetMetric()[0].GetHistogram().GetSampleCount() < 1 {
				t.Fatalf("expected duration histogram sample count >= 1, got %d", mf.GetMetric()[0].GetHistogram().GetSampleCount())
			}
			foundDuration = true
		case "hami_collector_last_run_timestamp_seconds":
			// Scheduler stamps last_run unconditionally (no per-phase error tracking in Collect).
			if len(mf.GetMetric()) != 1 {
				t.Fatalf("expected one last_run gauge, got %d", len(mf.GetMetric()))
			}
			ts := mf.GetMetric()[0].GetGauge().GetValue()
			if ts <= 0 {
				t.Fatalf("unexpected last_run timestamp %v", ts)
			}
			if ts > float64(time.Now().Unix()+5) {
				t.Fatalf("last_run timestamp %v appears in future", ts)
			}
			foundLastRun = true
		}
	}
	if !foundDuration {
		t.Fatalf("hami_collector_duration_seconds not found")
	}
	if !foundLastRun {
		t.Fatalf("hami_collector_last_run_timestamp_seconds not found")
	}
}
