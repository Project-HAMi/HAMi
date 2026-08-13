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
	"github.com/prometheus/client_golang/prometheus"
)

var (
	BindDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "hami",
			Subsystem: "scheduler",
			Name:      "bind_duration_seconds",
			Help:      "Time spent in each phase of the Bind workflow.",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"phase", "result"},
	)

	BindTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "hami",
			Subsystem: "scheduler",
			Name:      "bind_total",
			Help:      "Total number of Bind requests processed.",
		},
		[]string{"result"},
	)

	FilterDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "hami",
			Subsystem: "scheduler",
			Name:      "filter_duration_seconds",
			Help:      "Time spent processing Filter requests.",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"result"},
	)

	FilterTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "hami",
			Subsystem: "scheduler",
			Name:      "filter_total",
			Help:      "Total number of Filter requests processed.",
		},
		[]string{"result"},
	)

	ScoreDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "hami",
			Subsystem: "scheduler",
			Name:      "score_duration_seconds",
			Help:      "Time spent calculating scores for nodes.",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"result"},
	)

	ScoreTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "hami",
			Subsystem: "scheduler",
			Name:      "score_total",
			Help:      "Total number of Score calculations processed.",
		},
		[]string{"result"},
	)
)

func ResultLabel(err error) string {
	if err != nil {
		return "error"
	}
	return "success"
}
