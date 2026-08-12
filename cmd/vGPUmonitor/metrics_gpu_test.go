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
	"testing"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	nvmlmock "github.com/NVIDIA/go-nvml/pkg/nvml/mock"
	"github.com/prometheus/client_golang/prometheus"
)

func collectGPUUtilization(t *testing.T, dev nvml.Device) ([]prometheus.Metric, error) {
	t.Helper()
	ch := make(chan prometheus.Metric, 16)
	cc := ClusterManagerCollector{ClusterManager: &ClusterManager{}}
	err := cc.collectGPUUtilizationMetrics(ch, dev, 0)
	close(ch)
	var got []prometheus.Metric
	for m := range ch {
		got = append(got, m)
	}
	return got, err
}

func TestCollectGPUUtilizationMetrics_NotSupported(t *testing.T) {
	dev := &nvmlmock.Device{
		GetUtilizationRatesFunc: func() (nvml.Utilization, nvml.Return) {
			return nvml.Utilization{}, nvml.ERROR_NOT_SUPPORTED
		},
	}
	got, err := collectGPUUtilization(t, dev)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no metrics, got %d", len(got))
	}
}

func TestCollectGPUUtilizationMetrics_Error(t *testing.T) {
	dev := &nvmlmock.Device{
		GetUtilizationRatesFunc: func() (nvml.Utilization, nvml.Return) {
			return nvml.Utilization{}, nvml.ERROR_DRIVER_NOT_LOADED
		},
	}
	_, err := collectGPUUtilization(t, dev)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestCollectGPUUtilizationMetrics_Success(t *testing.T) {
	prev := legacyHostGPUUtilizationdesc
	legacyHostGPUUtilizationdesc = nil
	t.Cleanup(func() { legacyHostGPUUtilizationdesc = prev })

	dev := &nvmlmock.Device{
		GetUtilizationRatesFunc: func() (nvml.Utilization, nvml.Return) {
			return nvml.Utilization{Gpu: 75}, nvml.SUCCESS
		},
		GetUUIDFunc: func() (string, nvml.Return) {
			return "GPU-00000000-0000-0000-0000-000000000000", nvml.SUCCESS
		},
		GetNameFunc: func() (string, nvml.Return) {
			return "TestGPU", nvml.SUCCESS
		},
	}
	got, err := collectGPUUtilization(t, dev)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(got))
	}
	val, _ := gaugeValue(t, got[0])
	if val != 75 {
		t.Fatalf("expected utilization 75, got %v", val)
	}
}
