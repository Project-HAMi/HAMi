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

	"github.com/prometheus/client_golang/prometheus"
)

// collectDescribe drains a collector's Describe method into a slice.
func collectDescribe(cc ClusterManagerCollector) []*prometheus.Desc {
	ch := make(chan *prometheus.Desc)
	go func() {
		cc.Describe(ch)
		close(ch)
	}()
	var got []*prometheus.Desc
	for d := range ch {
		got = append(got, d)
	}
	return got
}

// TestDescribeCoversAllCollectedDescriptors guards the Prometheus Collector
// contract: Describe must announce the superset of every descriptor Collect
// can emit. ctrDeviceLastKernelDesc and ctrDeviceMigInfo are emitted only
// conditionally in Collect, which is exactly why they were previously missing
// here; this test fails on the pre-fix code (9 descriptors) and passes with
// the full set.
func TestDescribeCoversAllCollectedDescriptors(t *testing.T) {
	cc := ClusterManagerCollector{ClusterManager: &ClusterManager{LegacyMetrics: false}}

	want := map[*prometheus.Desc]bool{
		hostGPUdesc:                false,
		hostGPUUtilizationdesc:     false,
		ctrvGPUdesc:                false,
		ctrvGPUlimitdesc:           false,
		ctrDeviceMemorydesc:        false,
		ctrDeviceUtilizationdesc:   false,
		ctrDeviceMemoryContextDesc: false,
		ctrDeviceMemoryModuleDesc:  false,
		ctrDeviceMemoryBufferDesc:  false,
		ctrDeviceLastKernelDesc:    false,
		ctrDeviceMigInfo:           false,
	}

	got := collectDescribe(cc)
	if len(got) != len(want) {
		t.Errorf("Describe() emitted %d descriptors, want %d", len(got), len(want))
	}
	for _, d := range got {
		if _, ok := want[d]; !ok {
			t.Errorf("Describe() emitted an unexpected descriptor: %v", d)
			continue
		}
		want[d] = true
	}
	for d, seen := range want {
		if !seen {
			t.Errorf("Describe() did not announce descriptor: %v", d)
		}
	}
}

// TestDescribeIncludesLegacyDescriptors verifies the legacy branch still
// announces its eight additional descriptors when legacy metrics are enabled.
func TestDescribeIncludesLegacyDescriptors(t *testing.T) {
	initLegacyDescriptors()
	cc := ClusterManagerCollector{ClusterManager: &ClusterManager{LegacyMetrics: true}}

	const wantCount = 11 + 8 // modern superset + legacy descriptors
	if got := len(collectDescribe(cc)); got != wantCount {
		t.Errorf("Describe() with legacy metrics emitted %d descriptors, want %d", got, wantCount)
	}
}
