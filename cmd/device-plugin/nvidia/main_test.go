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
	"os"
	"path/filepath"
	"testing"

	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
)

func TestResolveNvidiaDriverRootFromGPUOperatorContract(t *testing.T) {
	contractPath := filepath.Join(t.TempDir(), "driver-ready")
	contract := "IS_HOST_DRIVER=false\n" +
		"NVIDIA_DRIVER_ROOT=/run/nvidia/driver\n" +
		"DRIVER_ROOT_CTR_PATH=/driver-root\n" +
		"NVIDIA_DEV_ROOT=/\n" +
		"DEV_ROOT_CTR_PATH=/host\n"
	if err := os.WriteFile(contractPath, []byte(contract), 0o600); err != nil {
		t.Fatal(err)
	}
	setDriverReadyFileForTest(t, contractPath)

	driverRoot := autoNvidiaDriverRoot
	// This mirrors the pointer alias created by the NVIDIA config loader.
	config := newDriverRootConfig(&driverRoot, &driverRoot)
	if err := resolveNvidiaDriverRoot(config); err != nil {
		t.Fatalf("resolveNvidiaDriverRoot() returned error: %v", err)
	}
	if got := *config.Flags.NvidiaDriverRoot; got != "/run/nvidia/driver" {
		t.Fatalf("NvidiaDriverRoot = %q, want /run/nvidia/driver", got)
	}
	if got := *config.Flags.NvidiaDevRoot; got != "/" {
		t.Fatalf("NvidiaDevRoot = %q, want /", got)
	}
	if config.Flags.NvidiaDriverRoot == config.Flags.NvidiaDevRoot {
		t.Fatal("driver and device roots still share a pointer")
	}
	if got := *config.Flags.Plugin.ContainerDriverRoot; got != "/host/run/nvidia/driver" {
		t.Fatalf("ContainerDriverRoot = %q, want /host/run/nvidia/driver", got)
	}
}

func TestResolveNvidiaDriverRootDefaultsToHostWithoutContract(t *testing.T) {
	setDriverReadyFileForTest(t, filepath.Join(t.TempDir(), "missing"))
	driverRoot := autoNvidiaDriverRoot
	config := newDriverRootConfig(&driverRoot, &driverRoot)

	if err := resolveNvidiaDriverRoot(config); err != nil {
		t.Fatalf("resolveNvidiaDriverRoot() returned error: %v", err)
	}
	if *config.Flags.NvidiaDriverRoot != "/" || *config.Flags.NvidiaDevRoot != "/" {
		t.Fatalf("roots = %q, %q; want /, /", *config.Flags.NvidiaDriverRoot, *config.Flags.NvidiaDevRoot)
	}
	if got := *config.Flags.Plugin.ContainerDriverRoot; got != "/host" {
		t.Fatalf("ContainerDriverRoot = %q, want /host", got)
	}
}

func TestResolveNvidiaDriverRootPreservesExplicitPaths(t *testing.T) {
	driverRoot := "/custom/driver"
	devRoot := "/custom/devices"
	config := newDriverRootConfig(&driverRoot, &devRoot)

	if err := resolveNvidiaDriverRoot(config); err != nil {
		t.Fatalf("resolveNvidiaDriverRoot() returned error: %v", err)
	}
	if driverRoot != "/custom/driver" || devRoot != "/custom/devices" {
		t.Fatalf("explicit paths changed: driverRoot=%q devRoot=%q", driverRoot, devRoot)
	}
	if got := *config.Flags.Plugin.ContainerDriverRoot; got != spec.DefaultContainerDriverRoot {
		t.Fatalf("ContainerDriverRoot = %q, want %q", got, spec.DefaultContainerDriverRoot)
	}
}

func TestResolveNvidiaDriverRootRejectsUnsupportedAutoRoot(t *testing.T) {
	contractPath := filepath.Join(t.TempDir(), "driver-ready")
	if err := os.WriteFile(contractPath, []byte("NVIDIA_DRIVER_ROOT=/custom/driver\nNVIDIA_DEV_ROOT=/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	setDriverReadyFileForTest(t, contractPath)
	driverRoot := autoNvidiaDriverRoot
	config := newDriverRootConfig(&driverRoot, &driverRoot)

	if err := resolveNvidiaDriverRoot(config); err == nil {
		t.Fatal("resolveNvidiaDriverRoot() returned nil, want unsupported root error")
	}
}

func TestResolveNvidiaDriverRootRejectsInvalidContract(t *testing.T) {
	contractPath := filepath.Join(t.TempDir(), "driver-ready")
	if err := os.WriteFile(contractPath, []byte("NVIDIA_DRIVER_ROOT=relative\nNVIDIA_DEV_ROOT=/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	setDriverReadyFileForTest(t, contractPath)
	driverRoot := autoNvidiaDriverRoot
	config := newDriverRootConfig(&driverRoot, &driverRoot)

	if err := resolveNvidiaDriverRoot(config); err == nil {
		t.Fatal("resolveNvidiaDriverRoot() returned nil, want invalid contract error")
	}
}

func newDriverRootConfig(driverRoot, devRoot *string) *spec.Config {
	containerDriverRoot := spec.DefaultContainerDriverRoot
	return &spec.Config{Flags: spec.Flags{CommandLineFlags: spec.CommandLineFlags{
		NvidiaDriverRoot: driverRoot,
		NvidiaDevRoot:    devRoot,
		Plugin: &spec.PluginCommandLineFlags{
			ContainerDriverRoot: &containerDriverRoot,
		},
	}}}
}

func setDriverReadyFileForTest(t *testing.T, path string) {
	t.Helper()
	original := gpuOperatorDriverReadyFile
	gpuOperatorDriverReadyFile = path
	t.Cleanup(func() { gpuOperatorDriverReadyFile = original })
}
