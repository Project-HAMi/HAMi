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
	"github.com/prometheus/client_golang/prometheus"

	"github.com/Project-HAMi/HAMi/pkg/version"
)

// NewBuildInfoCollector returns a collector that exports metrics about current version
// information.
func NewBuildInfoCollector() prometheus.Collector {
	return prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "hami_build_info",
			Help: "hami build metadata exposed as labels with a constant value of 1.",
			ConstLabels: prometheus.Labels{
				"version":    version.Version().Version,
				"revision":   version.Version().Revision,
				"build_date": version.Version().BuildDate,
				"go_version": version.Version().GoVersion,
				"compiler":   version.Version().Compiler,
				"platform":   version.Version().Platform,
			},
		},
		func() float64 { return 1 },
	)
}
