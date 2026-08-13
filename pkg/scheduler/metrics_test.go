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

package scheduler

import (
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetricsRegistrationAndObservation(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	if err := reg.Register(BindDuration); err != nil {
		t.Fatalf("Failed to register BindDuration: %v", err)
	}
	if err := reg.Register(BindTotal); err != nil {
		t.Fatalf("Failed to register BindTotal: %v", err)
	}
	if err := reg.Register(FilterDuration); err != nil {
		t.Fatalf("Failed to register FilterDuration: %v", err)
	}
	if err := reg.Register(FilterTotal); err != nil {
		t.Fatalf("Failed to register FilterTotal: %v", err)
	}
	if err := reg.Register(ScoreDuration); err != nil {
		t.Fatalf("Failed to register ScoreDuration: %v", err)
	}
	if err := reg.Register(ScoreTotal); err != nil {
		t.Fatalf("Failed to register ScoreTotal: %v", err)
	}

	BindDuration.WithLabelValues("pod_lookup", "success").Observe(0.005)
	BindTotal.WithLabelValues("success").Inc()
	FilterDuration.WithLabelValues("success").Observe(0.01)
	FilterTotal.WithLabelValues("success").Inc()
	ScoreDuration.WithLabelValues("success").Observe(0.02)
	ScoreTotal.WithLabelValues("success").Inc()

	count, err := testutil.GatherAndCount(reg)
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}
	if count < 6 {
		t.Errorf("Expected at least 6 metric families gathered, got %d", count)
	}
}

func TestBindDurationLabelsAreBounded(t *testing.T) {
	validPhases := []string{"total", "pod_lookup", "node_lookup", "node_lock", "patch_annotations", "apiserver_bind"}
	validResults := []string{"success", "error"}

	for _, phase := range validPhases {
		for _, result := range validResults {
			BindDuration.WithLabelValues(phase, result).Observe(0.001)
		}
	}
}

func TestResultLabel(t *testing.T) {
	if got := ResultLabel(nil); got != "success" {
		t.Errorf("ResultLabel(nil) = %q, want %q", got, "success")
	}
	if got := ResultLabel(errors.New("lookup failed")); got != "error" {
		t.Errorf("ResultLabel(err) = %q, want %q", got, "error")
	}
}
