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
	dto "github.com/prometheus/client_model/go"

	"github.com/Project-HAMi/HAMi/pkg/monitor/nvidia"
)

func TestUnknownProcessMetricGaugeAndMissingSample(t *testing.T) {
	t.Setenv("NODE_NAME", "test-node")
	ch := make(chan prometheus.Metric, 2)
	if err := emitUnknownProcessCounts(ch, []nvidia.UnknownProcessCount{{UUID: "GPU-a", Count: 2}, {UUID: "GPU-b", Count: 0}}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []float64{2, 0} {
		metric := &dto.Metric{}
		if err := (<-ch).Write(metric); err != nil {
			t.Fatal(err)
		}
		if metric.Gauge == nil || metric.Gauge.GetValue() != expected || metric.Counter != nil {
			t.Fatalf("expected gauge %v, got %v", expected, metric)
		}
		if len(metric.Label) != 2 {
			t.Fatal("only node and device_uuid labels should be exposed")
		}
	}
	if err := emitUnknownProcessCounts(ch, nil); err != nil {
		t.Fatal(err)
	}
	if len(ch) != 0 {
		t.Fatal("failed/absent device inventory must not emit zero")
	}
}
