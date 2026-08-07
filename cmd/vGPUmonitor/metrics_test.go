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
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Project-HAMi/HAMi/pkg/monitor/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

func TestDescribeCollectSync(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()

	t.Setenv(util.NodeNameEnvName, "test-node")
	client := fake.NewSimpleClientset()
	informerFactory := informers.NewSharedInformerFactory(client, 0)
	podLister := informerFactory.Core().V1().Pods().Lister()

	c := &ClusterManager{
		Zone:            "test-zone",
		LegacyMetrics:   false,
		PodLister:       podLister,
		containerLister: &nvidia.ContainerLister{},
	}
	cc := ClusterManagerCollector{ClusterManager: c}

	if err := reg.Register(cc); err != nil {
		t.Fatalf("Failed to register ClusterManagerCollector (non-legacy): %v", err)
	}

	if _, err := reg.Gather(); err != nil {
		t.Errorf("Gather failed (non-legacy): %v", err)
	}

	regLegacy := prometheus.NewPedanticRegistry()
	cLegacy := &ClusterManager{
		Zone:            "test-zone-legacy",
		LegacyMetrics:   true,
		PodLister:       podLister,
		containerLister: &nvidia.ContainerLister{},
	}
	initLegacyDescriptors()
	ccLegacy := ClusterManagerCollector{ClusterManager: cLegacy}

	if err := regLegacy.Register(ccLegacy); err != nil {
		t.Fatalf("Failed to register ClusterManagerCollector (legacy): %v", err)
	}
	if _, err := regLegacy.Gather(); err != nil {
		t.Errorf("Gather failed (legacy): %v", err)
	}
}

func TestSendMetric(t *testing.T) {
	desc := prometheus.NewDesc("hami_test_metric", "test metric", []string{"label"}, nil)
	ch := make(chan prometheus.Metric, 1)

	if err := sendMetric(ch, desc, prometheus.GaugeValue, 1, "value"); err != nil {
		t.Fatalf("sendMetric returned unexpected error: %v", err)
	}
	select {
	case <-ch:
	default:
		t.Fatal("expected a metric to be sent on the channel")
	}

	// Supplying the wrong number of label values makes NewConstMetric fail,
	// and sendMetric must surface that error instead of sending on the channel.
	if err := sendMetric(ch, desc, prometheus.GaugeValue, 1); err == nil {
		t.Fatal("expected sendMetric to return an error for mismatched labels")
	}
	select {
	case <-ch:
		t.Fatal("did not expect a metric to be sent on error")
	default:
	}
}

func TestSendLegacyMetric(t *testing.T) {
	ch := make(chan prometheus.Metric, 1)

	// A nil descriptor means the legacy metric is disabled; sendLegacyMetric
	// must no-op rather than panic or send a metric.
	sendLegacyMetric(ch, nil, prometheus.GaugeValue, 1, "value")
	select {
	case <-ch:
		t.Fatal("did not expect a metric to be sent for a nil descriptor")
	default:
	}

	desc := prometheus.NewDesc("hami_test_legacy_metric", "test legacy metric", []string{"label"}, nil)

	// sendLegacyMetric logs and swallows errors from sendMetric rather than panicking.
	sendLegacyMetric(ch, desc, prometheus.GaugeValue, 1)
	select {
	case <-ch:
		t.Fatal("did not expect a metric to be sent when sendMetric errors")
	default:
	}

	sendLegacyMetric(ch, desc, prometheus.GaugeValue, 1, "value")
	select {
	case <-ch:
	default:
		t.Fatal("expected a metric to be sent on the channel")
	}
}
