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

	"gotest.tools/v3/assert"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
)

func TestValidateSchedulerPolicyFlags(t *testing.T) {
	tests := []struct {
		name           string
		nodePolicy     string
		gpuPolicy      string
		wantErr        bool
		wantNodePolicy string
	}{
		{name: "defaults", nodePolicy: "binpack", gpuPolicy: "spread", wantNodePolicy: "binpack"},
		{name: "spread node policy", nodePolicy: "spread", gpuPolicy: "binpack", wantNodePolicy: "spread"},
		{name: "gpu policy chain", nodePolicy: "binpack", gpuPolicy: "binpack,numa", wantNodePolicy: "binpack"},
		{name: "padded node policy is canonicalized", nodePolicy: " spread ", gpuPolicy: "spread", wantNodePolicy: "spread"},
		{name: "wrong case node policy", nodePolicy: "Spread", gpuPolicy: "spread", wantErr: true},
		{name: "misspelled node policy", nodePolicy: "spraed", gpuPolicy: "spread", wantErr: true},
		{name: "empty node policy", nodePolicy: "", gpuPolicy: "spread", wantErr: true},
		{name: "wrong case gpu policy", nodePolicy: "binpack", gpuPolicy: "Binpack", wantErr: true},
		{name: "misspelled gpu policy", nodePolicy: "binpack", gpuPolicy: "binpak", wantErr: true},
		{name: "misspelled gpu policy in chain", nodePolicy: "binpack", gpuPolicy: "binpack,nmua", wantErr: true},
		{name: "empty gpu policy", nodePolicy: "binpack", gpuPolicy: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config.NodeSchedulerPolicy = tt.nodePolicy
			device.GPUSchedulerPolicy = tt.gpuPolicy

			err := validateSchedulerPolicyFlags()
			if tt.wantErr {
				assert.ErrorContains(t, err, "invalid --")
				return
			}
			assert.NilError(t, err)
			assert.Equal(t, tt.wantNodePolicy, config.NodeSchedulerPolicy)
		})
	}
}
