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
	"os"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/Project-HAMi/HAMi/pkg/util"
)

func TestAllMetricDescriptorsIncludeNodeName(t *testing.T) {
	ch := make(chan *prometheus.Desc, 30)
	cc := ClusterManagerCollector{
		ClusterManager: &ClusterManager{
			LegacyMetrics: true,
		},
	}
	initLegacyDescriptors()

	cc.Describe(ch)
	close(ch)

	descCount := 0
	for desc := range ch {
		descCount++
		descStr := desc.String()
		if strings.Contains(descStr, "fqName: \"hami_") {
			if !strings.Contains(descStr, "node_name") {
				t.Errorf("standard descriptor %s does not contain node_name label", descStr)
			}
		} else {
			if !strings.Contains(descStr, "nodename") {
				t.Errorf("legacy descriptor %s does not contain nodename label", descStr)
			}
		}
	}

	expectedCount := 17 // 9 standard + 8 legacy descriptors
	if descCount != expectedCount {
		t.Errorf("expected exactly %d descriptors, got %d", expectedCount, descCount)
	}
}

func TestGetNodeName(t *testing.T) {
	// Environment variable set
	testNode := "test-gpu-node-01"
	t.Setenv(util.NodeNameEnvName, testNode)
	if name := getNodeName(); name != testNode {
		t.Errorf("expected getNodeName() to return %s, got %s", testNode, name)
	}

	// Environment variable unset
	os.Unsetenv(util.NodeNameEnvName)
	if name := getNodeName(); name != "" {
		t.Errorf("expected getNodeName() to return empty string when env var is unset, got %s", name)
	}
}

func TestSendMetric(t *testing.T) {
	ch := make(chan prometheus.Metric, 5)

	// Valid metric sending
	err := sendMetric(ch, hostGPUdesc, prometheus.GaugeValue, 1024, "node-1", "0", "gpu-uuid-1", "NVIDIA-A100")
	if err != nil {
		t.Errorf("expected sendMetric to succeed, got %v", err)
	}

	// Legacy metric sending
	initLegacyDescriptors()
	sendLegacyMetric(ch, legacyHostGPUdesc, prometheus.GaugeValue, 1024, "node-1", "0", "gpu-uuid-1", "NVIDIA-A100")

	close(ch)
	count := 0
	for range ch {
		count++
	}
	if count != 2 {
		t.Errorf("expected 2 metrics in channel, got %d", count)
	}
}
