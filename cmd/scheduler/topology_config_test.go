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
	"math"
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
)

// restoreTopologyDefaults saves the package level topology flags so a test can
// change them without leaking state into the other tests in this package.
func restoreTopologyDefaults(t *testing.T) {
	t.Helper()
	mode, labels, weight := config.TopologyMode, config.TopologyTierLabels, config.TopologyScoreWeight
	t.Cleanup(func() {
		config.TopologyMode = mode
		config.TopologyTierLabels = labels
		config.TopologyScoreWeight = weight
	})
}

// TestValidateTopologyConfigAccepts covers the values an operator is expected
// to pass through the scheduler flags, including a hierarchy and the valid
// zero weight that disables the locality bonus.
func TestValidateTopologyConfigAccepts(t *testing.T) {
	restoreTopologyDefaults(t)

	tests := []struct {
		name     string
		mode     string
		labels   string
		weight   float32
		wantMode string
	}{
		{name: "empty labels and soft mode", mode: "soft", labels: "", weight: 0, wantMode: "soft"},
		{name: "hard mode is case and space insensitive", mode: " HARD ", labels: "leaf,spine", weight: 20, wantMode: "hard"},
		{name: "annotation tier and explicit level", mode: "Soft", labels: "@topology.example.com/leaf,spine=2", weight: 12.5, wantMode: "soft"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config.TopologyMode = tc.mode
			config.TopologyTierLabels = tc.labels
			config.TopologyScoreWeight = tc.weight

			if err := validateTopologyConfig(); err != nil {
				t.Fatalf("validateTopologyConfig() = %v, want nil", err)
			}
			if config.TopologyMode != tc.wantMode {
				t.Errorf("TopologyMode = %q, want %q", config.TopologyMode, tc.wantMode)
			}
		})
	}
}

// TestValidateTopologyConfigRejects covers the failures that would otherwise
// silently disable hard placement or invert the locality score.
func TestValidateTopologyConfigRejects(t *testing.T) {
	restoreTopologyDefaults(t)

	tests := []struct {
		name   string
		mode   string
		labels string
		weight float32
	}{
		{name: "unknown mode", mode: "strict", labels: "leaf"},
		{name: "empty mode", mode: "", labels: "leaf"},
		{name: "duplicate tier key", mode: "soft", labels: "leaf,leaf"},
		{name: "zero tier level", mode: "soft", labels: "leaf=0"},
		{name: "duplicate tier level", mode: "soft", labels: "leaf=1,spine=1"},
		{name: "negative weight", mode: "soft", labels: "leaf", weight: -1},
		{name: "not a number weight", mode: "soft", labels: "leaf", weight: float32(math.NaN())},
		{name: "infinite weight", mode: "soft", labels: "leaf", weight: float32(math.Inf(1))},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config.TopologyMode = tc.mode
			config.TopologyTierLabels = tc.labels
			config.TopologyScoreWeight = tc.weight

			if err := validateTopologyConfig(); err == nil {
				t.Fatal("validateTopologyConfig() = nil, want error")
			}
		})
	}
}
