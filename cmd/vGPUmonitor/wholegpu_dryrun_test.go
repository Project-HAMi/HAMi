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
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/Project-HAMi/HAMi/pkg/monitor/nvidia"
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

func Test_loadWholeGPUDryRunKubeConfig(t *testing.T) {
	t.Run("falls back to in-cluster when no home kubeconfig exists", func(t *testing.T) {
		t.Setenv("KUBECONFIG", "")
		t.Setenv("HOME", "")
		previous := clientcmd.RecommendedHomeFile
		clientcmd.RecommendedHomeFile = filepath.Join(t.TempDir(), "missing.yaml")
		t.Cleanup(func() { clientcmd.RecommendedHomeFile = previous })
		_, err := loadWholeGPUDryRunKubeConfig()
		if err != nil {
			assert.Assert(t, !strings.Contains(err.Error(), "explicit kubeconfig"),
				"empty HOME must not synthesize a .kube/config path: %v", err)
		}
	})

	t.Run("prefers an existing home kubeconfig", func(t *testing.T) {
		dir := t.TempDir()
		kubeconfig := filepath.Join(dir, ".kube", "config")
		assert.NilError(t, os.MkdirAll(filepath.Dir(kubeconfig), 0o755))
		assert.NilError(t, os.WriteFile(kubeconfig, []byte(""), 0o600))
		t.Setenv("KUBECONFIG", "")
		t.Setenv("HOME", dir)
		previous := clientcmd.RecommendedHomeFile
		clientcmd.RecommendedHomeFile = kubeconfig
		t.Cleanup(func() { clientcmd.RecommendedHomeFile = previous })
		_, err := loadWholeGPUDryRunKubeConfig()
		assert.Assert(t, err != nil)
		assert.Assert(t, strings.Contains(err.Error(), "explicit kubeconfig"))
	})
}

func TestWholeGPUDryRunMultiFileKubeconfig(t *testing.T) {
	contextPath := filepath.Join(t.TempDir(), "context.yaml")
	resourcesPath := filepath.Join(t.TempDir(), "resources.yaml")
	assert.NilError(t, clientcmd.WriteToFile(clientcmdapi.Config{
		CurrentContext: "test",
		Contexts:       map[string]*clientcmdapi.Context{"test": {Cluster: "test", AuthInfo: "test"}},
	}, contextPath))
	assert.NilError(t, clientcmd.WriteToFile(clientcmdapi.Config{
		Clusters:  map[string]*clientcmdapi.Cluster{"test": {Server: "https://test.example.com"}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"test": {Token: "synthetic-token"}},
	}, resourcesPath))
	t.Setenv("KUBECONFIG", strings.Join([]string{contextPath, resourcesPath}, string(os.PathListSeparator)))
	config, err := loadWholeGPUDryRunKubeConfig()
	assert.NilError(t, err)
	assert.Equal(t, config.Host, "https://test.example.com")
	assert.Equal(t, config.BearerToken, "synthetic-token")
}

func TestWholeGPUDryRunRejectsInvalidExplicitKubeconfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.yaml")
	assert.NilError(t, os.WriteFile(path, []byte("clusters: ["), 0600))
	t.Setenv("KUBECONFIG", path)
	_, err := loadWholeGPUDryRunKubeConfig()
	assert.ErrorContains(t, err, "explicit kubeconfig")
	assert.ErrorContains(t, err, "error loading config file")
	assert.Assert(t, !strings.Contains(err.Error(), "in-cluster"))
}
