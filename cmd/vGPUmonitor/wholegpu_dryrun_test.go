/*
Copyright 2025 The HAMi Authors.

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
	"bytes"
	"strings"
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/monitor/nvidia"
	"gotest.tools/v3/assert"
)

func TestWriteWholeGPUDryRunReport(t *testing.T) {
	var output bytes.Buffer
	writeWholeGPUDryRunReport(&output, &nvidia.WholeGPUDryRunReport{
		Node:                "node-a",
		ScannedPods:         2,
		CandidateContainers: 2,
		ConfirmedContainers: 1,
		Containers: []nvidia.WholeGPUContainerReport{{
			Namespace: "workloads",
			Pod:       "training",
			Container: "main",
			Devices: []nvidia.WholeGPUDeviceReport{{
				Index:       0,
				UUID:        "GPU-aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
				MemoryUsed:  4096,
				MemoryTotal: 8192,
				SMUtil:      42,
				Error:       "GetUtilizationRates failed: Not Supported",
			}},
		}},
		Diagnostics: []nvidia.WholeGPUDiagnostic{{
			Namespace: "workloads",
			Pod:       "partial",
			Container: "main",
			Status:    "not-whole-gpu",
			Message:   "allocation is partial",
		}},
	})

	got := output.String()
	for _, want := range []string{
		"whole-gpu dry-run",
		"node: node-a",
		"mode: read-only",
		"scanned_pods: 2",
		"candidate_containers: 2",
		"confirmed_whole_gpu_containers: 1",
		"namespace=workloads pod=training container=main",
		"device index=0 uuid=GPU-aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa memory_used=4096 memory_total=8192 sm_util=42",
		"GetUtilizationRates failed: Not Supported",
		"status=not-whole-gpu",
	} {
		assert.Assert(t, strings.Contains(got, want), "missing output %q in:\n%s", want, got)
	}
}

func TestNewWholeGPUDryRunCommand(t *testing.T) {
	command := newWholeGPUDryRunCommand()
	assert.Equal(t, command.Use, "whole-gpu")
	assert.Assert(t, command.Flags().Lookup("namespace") != nil)
	assert.Assert(t, command.Flags().Lookup("pod") != nil)
	assert.Assert(t, command.Flags().Lookup("container") != nil)
}
