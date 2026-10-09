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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/Project-HAMi/HAMi/pkg/monitor/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

// testKubeConfig points at a non-existent server; client construction never
// dials it, so it safely exercises the kubeconfig loading paths.
const testKubeConfig = `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://dryrun.example.invalid:6443
  name: dryrun-test
contexts:
- context:
    cluster: dryrun-test
    user: dryrun-test
  name: dryrun-test
current-context: dryrun-test
users:
- name: dryrun-test
  user:
    token: dryrun-test-token
`

func writeKubeConfig(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "config")
	assert.NilError(t, os.WriteFile(p, []byte(testKubeConfig), 0600))
	return p
}

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

func TestWholeGPUDryRunCommandRequiresNodeName(t *testing.T) {
	t.Setenv(util.NodeNameEnvName, "")
	err := newWholeGPUDryRunCommand().Execute()
	assert.ErrorContains(t, err, util.NodeNameEnvName)
}

func TestLoadWholeGPUDryRunKubeConfig(t *testing.T) {
	t.Run("explicit KUBECONFIG wins", func(t *testing.T) {
		t.Setenv("KUBECONFIG", writeKubeConfig(t, t.TempDir()))
		config, err := loadWholeGPUDryRunKubeConfig()
		assert.NilError(t, err)
		assert.Equal(t, config.Host, "https://dryrun.example.invalid:6443")
	})

	t.Run("missing explicit KUBECONFIG is an error", func(t *testing.T) {
		t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "absent"))
		_, err := loadWholeGPUDryRunKubeConfig()
		assert.ErrorContains(t, err, "failed to load explicit kubeconfig")
	})

	t.Run("falls back to HOME/.kube/config when it exists", func(t *testing.T) {
		home := t.TempDir()
		assert.NilError(t, os.MkdirAll(filepath.Join(home, ".kube"), 0755))
		writeKubeConfig(t, filepath.Join(home, ".kube"))
		t.Setenv("KUBECONFIG", "")
		t.Setenv("HOME", home)
		config, err := loadWholeGPUDryRunKubeConfig()
		assert.NilError(t, err)
		assert.Equal(t, config.Host, "https://dryrun.example.invalid:6443")
	})

	t.Run("no kubeconfig falls back to in-cluster config", func(t *testing.T) {
		t.Setenv("KUBECONFIG", "")
		t.Setenv("HOME", t.TempDir())
		t.Setenv("KUBERNETES_SERVICE_HOST", "")
		t.Setenv("KUBERNETES_SERVICE_PORT", "")
		_, err := loadWholeGPUDryRunKubeConfig()
		assert.ErrorContains(t, err, "in-cluster config")
	})
}

func TestNewWholeGPUDryRunKubernetesClient(t *testing.T) {
	t.Run("builds a client from KUBECONFIG", func(t *testing.T) {
		t.Setenv("KUBECONFIG", writeKubeConfig(t, t.TempDir()))
		client, err := newWholeGPUDryRunKubernetesClient()
		assert.NilError(t, err)
		assert.Assert(t, client != nil)
	})

	t.Run("wraps config load errors", func(t *testing.T) {
		t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "absent"))
		_, err := newWholeGPUDryRunKubernetesClient()
		assert.ErrorContains(t, err, "failed to load Kubernetes configuration")
	})
}
