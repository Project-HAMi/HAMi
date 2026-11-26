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
	t.Cleanup(func() {
		config.TopologyTierLabels = labels
	})
}

func TestValidateTopologyConfigAccepts(t *testing.T) {
	restoreTopologyDefaults(t)

	tests := []struct {
		name   string
		labels string
	}{
		{name: "empty disables the feature", labels: ""},
		{name: "canonical superpod labels", labels: util.RackIDLabel + "," + util.SuperPodIDLabel},
		{name: "explicit levels", labels: util.RackIDLabel + "=1," + util.SuperPodIDLabel + "=2"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config.TopologyTierLabels = tc.labels
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
	}{
		{name: "duplicate tier key", labels: "leaf,leaf"},
		{name: "zero tier level", labels: "leaf=0"},
		{name: "duplicate tier level", labels: "leaf=1,spine=1"},
		{name: "annotation key", labels: "@hami.io/superpodid"},
		{name: "key with spaces", labels: "rack id"},
		{name: "level above the maximum", labels: "leaf=65"},
		{name: "mix explicit and plain keys", labels: "rack=2,superpodid"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config.TopologyTierLabels = tc.labels
			if err := validateTopologyConfig(); err == nil {
				t.Fatal("validateTopologyConfig() = nil, want error")
			}
		})
	}
}
