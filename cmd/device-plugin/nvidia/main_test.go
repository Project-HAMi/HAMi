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
	"encoding/json"
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
	if got := *config.Flags.Plugin.ContainerDriverRoot; got != "/" {
		t.Fatalf("ContainerDriverRoot = %q, want the toolkit-injected root /", got)
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
	if got := *config.Flags.Plugin.ContainerDriverRoot; got != "/" {
		t.Fatalf("ContainerDriverRoot = %q, want the toolkit-injected root /", got)
	}
}

func TestResolveNvidiaDriverRootUsesHostMountForCDI(t *testing.T) {
	tests := []struct {
		name     string
		contract string
		want     string
	}{
		{
			name: "host-installed driver",
			want: "",
		},
		{
			name:     "GPU Operator driver",
			contract: "NVIDIA_DRIVER_ROOT=/run/nvidia/driver\nNVIDIA_DEV_ROOT=/run/nvidia/driver\n",
			want:     "run/nvidia/driver",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contractPath := filepath.Join(t.TempDir(), "driver-ready")
			if tt.contract != "" {
				if err := os.WriteFile(contractPath, []byte(tt.contract), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			setDriverReadyFileForTest(t, contractPath)
			hostRoot := setHostContainerRootForTest(t, t.TempDir())
			driverRoot := autoNvidiaDriverRoot
			config := newDriverRootConfig(&driverRoot, &driverRoot, spec.DeviceListStrategyCDICRI)

			if err := resolveNvidiaDriverRoot(config); err != nil {
				t.Fatalf("resolveNvidiaDriverRoot() returned error: %v", err)
			}
			if got, want := *config.Flags.Plugin.ContainerDriverRoot, filepath.Join(hostRoot, tt.want); got != want {
				t.Fatalf("ContainerDriverRoot = %q, want %q", got, want)
			}
		})
	}
}

func TestResolveNvidiaDriverRootRequiresHostMountForCDI(t *testing.T) {
	setDriverReadyFileForTest(t, filepath.Join(t.TempDir(), "missing"))
	setHostContainerRootForTest(t, filepath.Join(t.TempDir(), "missing"))
	driverRoot := autoNvidiaDriverRoot
	config := newDriverRootConfig(&driverRoot, &driverRoot, spec.DeviceListStrategyCDIAnnotations)

	if err := resolveNvidiaDriverRoot(config); err == nil {
		t.Fatal("resolveNvidiaDriverRoot() returned nil, want missing host mount error")
	}
}

func TestResolveNvidiaDriverRootAcceptsCustomRootWithoutCDI(t *testing.T) {
	contractPath := filepath.Join(t.TempDir(), "driver-ready")
	if err := os.WriteFile(contractPath, []byte("NVIDIA_DRIVER_ROOT=/custom/driver\nNVIDIA_DEV_ROOT=/custom/driver\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	setDriverReadyFileForTest(t, contractPath)
	driverRoot := autoNvidiaDriverRoot
	config := newDriverRootConfig(&driverRoot, &driverRoot, spec.DeviceListStrategyEnvVar)

	if err := resolveNvidiaDriverRoot(config); err != nil {
		t.Fatalf("resolveNvidiaDriverRoot() returned error: %v", err)
	}
	if got := *config.Flags.NvidiaDevRoot; got != "/custom/driver" {
		t.Fatalf("NvidiaDevRoot = %q, want /custom/driver", got)
	}
	if got := *config.Flags.Plugin.ContainerDriverRoot; got != "/" {
		t.Fatalf("ContainerDriverRoot = %q, want /", got)
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
	setHostContainerRootForTest(t, t.TempDir())
	driverRoot := autoNvidiaDriverRoot
	config := newDriverRootConfig(&driverRoot, &driverRoot, spec.DeviceListStrategyCDICRI)

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

func newDriverRootConfig(driverRoot, devRoot *string, strategies ...string) *spec.Config {
	containerDriverRoot := spec.DefaultContainerDriverRoot
	config := &spec.Config{Flags: spec.Flags{CommandLineFlags: spec.CommandLineFlags{
		NvidiaDriverRoot: driverRoot,
		NvidiaDevRoot:    devRoot,
		Plugin: &spec.PluginCommandLineFlags{
			ContainerDriverRoot: &containerDriverRoot,
		},
	}}}
	if len(strategies) > 0 {
		// The device list strategy flag type is unexported, so populate it
		// through its JSON form as the config file loader does.
		data, err := json.Marshal(map[string][]string{"deviceListStrategy": strategies})
		if err != nil {
			panic(err)
		}
		if err := json.Unmarshal(data, config.Flags.Plugin); err != nil {
			panic(err)
		}
	}
	return config
}

func setHostContainerRootForTest(t *testing.T, path string) string {
	t.Helper()
	original := hostContainerRoot
	hostContainerRoot = path
	t.Cleanup(func() { hostContainerRoot = original })
	return path
}

func setDriverReadyFileForTest(t *testing.T, path string) {
	t.Helper()
	original := gpuOperatorDriverReadyFile
	gpuOperatorDriverReadyFile = path
	t.Cleanup(func() { gpuOperatorDriverReadyFile = original })
}
