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

package routes

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	// FilterDuration is a histogram to measure the time taken by the filter phase of the scheduler.
	FilterDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "hami_scheduler_filter_duration_seconds",
			Help: "Duration of the filter phase in the scheduler extender",
			// Buckets from 10ms to 10s
			Buckets: prometheus.ExponentialBucketsRange(0.01, 10, 10),
		},
		[]string{"node", "result"}, // result: allowed, denied
	)

	// FilterDenials is a counter to track the number of pod denials during the filter phase.
	FilterDenials = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "hami_scheduler_filter_denials_total",
			Help: "Total number of scheduling filter denials",
		},
		[]string{"node", "reason"}, // reason why it was denied
	)
)

// RegisterMetrics registers the prometheus metrics defined in this package.
func RegisterMetrics(registry prometheus.Registerer) {
	registry.MustRegister(FilterDuration)
	registry.MustRegister(FilterDenials)
}
