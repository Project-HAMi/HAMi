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

package tools

import (
	"strings"
	"testing"
)

func TestQueryFor(t *testing.T) {
	cases := []struct {
		metric string
		node   string
		want   string
	}{
		{"hami_gpu_memory_allocated_bytes", "", "hami_gpu_memory_allocated_bytes"},
		{"hami_gpu_memory_allocated_bytes", "gpu-node-1", `hami_gpu_memory_allocated_bytes{node="gpu-node-1"}`},
	}
	for _, c := range cases {
		got := queryFor(c.metric, c.node)
		if got != c.want {
			t.Errorf("queryFor(%q, %q) = %q, want %q", c.metric, c.node, got, c.want)
		}
	}
}

func TestAllowedMetrics_NonEmptyAndNoBlankNames(t *testing.T) {
	if len(allowedMetrics) == 0 {
		t.Fatal("allowedMetrics must not be empty")
	}
	for name, desc := range allowedMetrics {
		if name == "" {
			t.Errorf("found an empty metric name")
		}
		if desc == "" {
			t.Errorf("metric %q has an empty description", name)
		}
		if !strings.HasPrefix(name, "hami_") {
			t.Errorf("metric %q does not start with the expected hami_ prefix", name)
		}
	}
}

func TestMetricsToolDescription_ListsAllowedMetrics(t *testing.T) {
	desc := metricsToolDescription()
	for name := range allowedMetrics {
		if !strings.Contains(desc, name) {
			t.Errorf("tool description missing metric %q", name)
		}
	}
}
