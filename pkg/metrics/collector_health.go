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
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Component label values used by the collector health metrics. Each HAMi
// binary that exporters Prometheus metrics reports its own scrape health
// under one of these values.
const (
	ComponentScheduler   = "scheduler"
	ComponentVGPUMonitor = "vgpumonitor"
)

// Phase label values used by hami_collector_errors_total. A phase pinpoints
// the collection stage that failed, so operators can alert on the specific
// failure instead of discovering an empty dashboard after the fact.
const (
	// PhaseGPUInfo covers NVML initialisation, device enumeration and
	// per-device metric collection in the vGPUmonitor.
	PhaseGPUInfo = "gpu_info"
	// PhasePodContainerInfo covers listing pods on the node and reading
	// their per-container device usage in the vGPUmonitor.
	PhasePodContainerInfo = "pod_container_info"
	// PhaseMIGInfo covers decoding and exporting MIG allocation identities
	// in the vGPUmonitor.
	PhaseMIGInfo = "mig_info"
	// PhaseListScheduledPods covers reading the scheduler's cache of
	// scheduled pods.
	PhaseListScheduledPods = "list_scheduled_pods"
	// PhaseSendMetric covers failures to materialise a single metric sample
	// (prometheus.NewConstMetric errors) in any collector.
	PhaseSendMetric = "send_metric"
)

// CollectorHealthRecorder exposes the scrape-path health of a Prometheus
// collector as metrics: how long each scrape takes, how often a collection
// phase fails, and when the collector last completed a scrape. It follows the
// self-instrumentation convention used by node_exporter
// (node_scrape_collector_duration_seconds) and the Prometheus server itself
// (prometheus_ rule evaluation metrics): every exporter should be able to
// answer "did my own collection work?" with a query, not a log grep.
//
// All methods are safe to call on a nil receiver so collectors can treat the
// recorder as optional in tests.
type CollectorHealthRecorder struct {
	duration *prometheus.HistogramVec
	errors   *prometheus.CounterVec
	lastRun  *prometheus.GaugeVec
}

// NewCollectorHealthRecorder creates a new recorder. Callers must register it
// directly with a Prometheus registry or embed it in a composite collector.
func NewCollectorHealthRecorder() *CollectorHealthRecorder {
	r := &CollectorHealthRecorder{
		duration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "hami_collector_duration_seconds",
				Help: "Duration of a single metrics collection pass in seconds.",
				// Default buckets (5ms-10s) bracket the gap between a
				// healthy sub-second scrape and the scrape timeout it
				// must not approach.
				Buckets: prometheus.DefBuckets,
			},
			[]string{"component"},
		),
		errors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hami_collector_errors_total",
				Help: "Total number of errors encountered while collecting metrics, by component and phase.",
			},
			[]string{"component", "phase"},
		),
		lastRun: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "hami_collector_last_run_timestamp_seconds",
				Help: "Unix timestamp of the last error-free metrics collection pass.",
			},
			[]string{"component"},
		),
	}
	return r
}

// Describe sends the descriptors of the underlying metrics to the provided channel.
func (r *CollectorHealthRecorder) Describe(ch chan<- *prometheus.Desc) {
	if r == nil {
		return
	}
	r.duration.Describe(ch)
	r.errors.Describe(ch)
	r.lastRun.Describe(ch)
}

// Collect sends the metric values of the underlying metrics to the provided channel.
func (r *CollectorHealthRecorder) Collect(ch chan<- prometheus.Metric) {
	if r == nil {
		return
	}
	r.duration.Collect(ch)
	r.errors.Collect(ch)
	r.lastRun.Collect(ch)
}

// ObserveDuration records the wall-clock duration of a single scrape.
// It is always called, regardless of whether individual phases failed.
func (r *CollectorHealthRecorder) ObserveDuration(component string, start time.Time) {
	if r == nil {
		return
	}
	r.duration.WithLabelValues(component).Observe(time.Since(start).Seconds())
}

// StampLastRun marks the current time as the last successful (error-free)
// scrape for the given component. Only call this when all collection phases
// completed without errors.
func (r *CollectorHealthRecorder) StampLastRun(component string) {
	if r == nil {
		return
	}
	r.lastRun.WithLabelValues(component).SetToCurrentTime()
}

// RecordError increments the per-phase error counter for the given component.
func (r *CollectorHealthRecorder) RecordError(component, phase string) {
	if r == nil {
		return
	}
	r.errors.WithLabelValues(component, phase).Inc()
}
