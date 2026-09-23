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
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

// TestResolveVGPUCacheConfigReusesExistingEnvironment checks that explicit shared-cache settings
// override defaults.
func TestResolveVGPUCacheConfigReusesExistingEnvironment(t *testing.T) {
	t.Setenv(vgpuCacheRootEnvName, "/var/lib/hami/containers")
	t.Setenv(vgpuCacheGracePeriodEnvName, "7m")
	config := resolveVGPUCacheConfig()
	if config.Root != "/var/lib/hami/containers" {
		t.Fatalf("Root = %q", config.Root)
	}
	if config.GracePeriod != 7*time.Minute {
		t.Fatalf("GracePeriod = %v", config.GracePeriod)
	}
	if config.ScanInterval != defaultVGPUCacheScanInterval {
		t.Fatalf("ScanInterval = %v", config.ScanInterval)
	}
}

// TestResolveVGPUCacheConfigDefaultsFromHookPath checks cache path fallback and handling of
// invalid grace periods.
func TestResolveVGPUCacheConfigDefaultsFromHookPath(t *testing.T) {
	t.Setenv(vgpuCacheRootEnvName, "")
	t.Setenv(vgpuCacheGracePeriodEnvName, "invalid")
	t.Setenv("HOOK_PATH", "/opt/hami")
	config := resolveVGPUCacheConfig()
	if config.Root != "/opt/hami/vgpu/containers" {
		t.Fatalf("Root = %q", config.Root)
	}
	if config.GracePeriod != defaultVGPUCacheGracePeriod {
		t.Fatalf("GracePeriod = %v", config.GracePeriod)
	}
}

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

// TestVGPUCacheAbandonEvent reports the real Node identity without listing
// events, and a failing event write does not retry or affect the caller.
func TestVGPUCacheAbandonEvent(t *testing.T) {
	for _, failWrite := range []bool{false, true} {
		t.Run(fmt.Sprintf("failWrite=%v", failWrite), func(t *testing.T) {
			kubeClient := fake.NewSimpleClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "gpu-node", UID: "node-uid"}})
			created := make(chan *corev1.Event, 1)
			kubeClient.PrependReactor("create", "events", func(action clienttesting.Action) (bool, runtime.Object, error) {
				create, ok := action.(clienttesting.CreateAction)
				if !ok {
					return true, nil, errors.New("unexpected action")
				}
				event, ok := create.GetObject().(*corev1.Event)
				if !ok {
					return true, nil, errors.New("unexpected object")
				}
				created <- event.DeepCopy()
				if failWrite {
					return true, nil, errors.New("event API unavailable")
				}
				return true, event, nil
			})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			report := startVGPUCacheEvents(ctx, kubeClient, "gpu-node")
			report("/cache/pod_gpu", 5, errors.New("permission denied"))
			select {
			case event := <-created:
				require.Equal(t, "VGPUCacheCleanupAbandoned", event.Reason)
				require.Equal(t, corev1.EventTypeWarning, event.Type)
				require.Equal(t, "Node", event.InvolvedObject.Kind)
				require.Equal(t, "node-uid", string(event.InvolvedObject.UID))
				require.Equal(t, "gpu-node", event.InvolvedObject.Name)
				require.Contains(t, event.Message, "/cache/pod_gpu")
				require.Contains(t, event.Message, "5 deletion failures")
				require.Contains(t, event.Message, "permission denied")
			case <-time.After(time.Second):
				t.Fatal("event was not reported")
			}
			// A second notification stays queued behind the rate limit.
			report("/cache/another_gpu", 5, errors.New("permission denied"))
			select {
			case <-created:
				t.Fatal("notification was not rate limited")
			case <-time.After(30 * time.Millisecond):
			}
			cancel()
			actions := kubeClient.Actions()
			require.Len(t, actions, 2)
			require.Equal(t, "get", actions[0].GetVerb())
			require.Equal(t, "create", actions[1].GetVerb())
		})
	}
}

// TestVGPUCacheEventQueueDoesNotBlock keeps reporting bounded when the API is
// stuck; once cancelled the worker drops pending notifications without retrying.
func TestVGPUCacheEventQueueDoesNotBlock(t *testing.T) {
	kubeClient := fake.NewSimpleClientset()
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	kubeClient.PrependReactor("get", "nodes", func(clienttesting.Action) (bool, runtime.Object, error) {
		close(entered)
		<-release
		return true, nil, errors.New("Node API unavailable")
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	report := startVGPUCacheEvents(ctx, kubeClient, "gpu-node")
	report("/cache/first_gpu", 5, os.ErrPermission)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	queued := make(chan struct{})
	go func() {
		for range 32 {
			report("/cache/other_gpu", 5, os.ErrPermission)
		}
		close(queued)
	}()
	select {
	case <-queued:
	case <-time.After(time.Second):
		t.Fatal("full notification queue blocked cleanup")
	}
}
