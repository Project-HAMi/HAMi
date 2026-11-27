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

	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

func restoreTopologyDefaults(t *testing.T) {
	t.Helper()
	labels := config.TopologyTierLabels
	mode := config.TopologyMode
	weight := config.TopologyScoreWeight
	t.Cleanup(func() {
		config.TopologyTierLabels = labels
		config.TopologyMode = mode
		config.TopologyScoreWeight = weight
	})
}

func TestValidateTopologyConfigAccepts(t *testing.T) {
	restoreTopologyDefaults(t)

	tests := []struct {
		name   string
		labels string
		mode   string
		weight float32
	}{
		{name: "empty disables the feature", labels: "", mode: util.TopologyModeSoft, weight: config.DefaultTopologyScoreWeight},
		{name: "canonical superpod labels", labels: util.RackIDLabel + "," + util.SuperPodIDLabel, mode: util.TopologyModeSoft, weight: 20},
		{name: "explicit levels", labels: util.RackIDLabel + "=1," + util.SuperPodIDLabel + "=2", mode: util.TopologyModeHard, weight: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config.TopologyTierLabels = tc.labels
			config.TopologyMode = tc.mode
			config.TopologyScoreWeight = tc.weight
			if err := validateTopologyConfig(); err != nil {
				t.Fatalf("validateTopologyConfig() = %v, want nil", err)
			}
		})
	}
}

func TestValidateTopologyConfigRejects(t *testing.T) {
	restoreTopologyDefaults(t)

	tests := []struct {
		name   string
		labels string
		mode   string
		weight float32
	}{
		{name: "duplicate tier key", labels: "leaf,leaf", mode: util.TopologyModeSoft, weight: 20},
		{name: "zero tier level", labels: "leaf=0", mode: util.TopologyModeSoft, weight: 20},
		{name: "duplicate tier level", labels: "leaf=1,spine=1", mode: util.TopologyModeSoft, weight: 20},
		{name: "annotation key", labels: "@hami.io/superpodid", mode: util.TopologyModeSoft, weight: 20},
		{name: "key with spaces", labels: "rack id", mode: util.TopologyModeSoft, weight: 20},
		{name: "level above the maximum", labels: "leaf=65", mode: util.TopologyModeSoft, weight: 20},
		{name: "mix explicit and plain keys", labels: "rack=2,superpodid", mode: util.TopologyModeSoft, weight: 20},
		{name: "invalid mode", labels: "", mode: "strict", weight: 20},
		{name: "negative score weight", labels: "", mode: util.TopologyModeSoft, weight: -1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config.TopologyTierLabels = tc.labels
			config.TopologyMode = tc.mode
			config.TopologyScoreWeight = tc.weight
			if err := validateTopologyConfig(); err == nil {
				t.Fatal("validateTopologyConfig() = nil, want error")
			}
		})
	}
}
