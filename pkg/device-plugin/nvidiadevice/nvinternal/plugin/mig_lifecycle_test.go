/*
 * Copyright (c) 2026, HAMi. All rights reserved.
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
 */

package plugin

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	nvmlmock "github.com/NVIDIA/go-nvml/pkg/nvml/mock"
	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/rm"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

// Unix socket paths are limited to about 100 bytes, including the temporary
// directory. macOS testing.TempDir paths may already exceed that limit.
func lifecycleSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hami-nvml-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	return dir
}

func lifecyclePlugin(t *testing.T) (*NvidiaDevicePlugin, *nvmlmock.Interface) {
	t.Helper()
	return lifecyclePluginWithMode(t, nvidia.MigMode)
}

func lifecyclePluginWithMode(t *testing.T, mode string) (*NvidiaDevicePlugin, *nvmlmock.Interface) {
	t.Helper()
	setupFakeClient(t)
	lib := &nvmlmock.Interface{
		InitFunc:           func() nvml.Return { return nvml.SUCCESS },
		ShutdownFunc:       func() nvml.Return { return nvml.SUCCESS },
		DeviceGetCountFunc: func() (int, nvml.Return) { return 0, nvml.SUCCESS },
	}
	p := &NvidiaDevicePlugin{
		ctx: context.Background(), operatingMode: mode,
		migMgr: newMigInstanceManager(lib), socket: filepath.Join(lifecycleSocketDir(t), "gpu.sock"),
		rm: &rm.ResourceManagerMock{
			ResourceFunc: func() spec.ResourceName { return "nvidia.com/gpu" },
			DevicesFunc:  func() rm.Devices { return rm.Devices{} },
			CheckHealthFunc: func(stop <-chan interface{}, _ chan<- *rm.Device) error {
				<-stop
				return nil
			},
		},
	}
	t.Cleanup(func() {
		if err := p.Stop(); err != nil {
			t.Error(err)
		}
	})
	return p, lib
}

func TestMigPluginStartupRollback(t *testing.T) {
	for _, stage := range []string{"init", "inventory", "serve", "register"} {
		t.Run(stage, func(t *testing.T) {
			p, lib := lifecyclePlugin(t)
			kubeletSocket := ""
			switch stage {
			case "init":
				lib.InitFunc = func() nvml.Return { return nvml.ERROR_LIBRARY_NOT_FOUND }
			case "inventory":
				lib.DeviceGetCountFunc = func() (int, nvml.Return) { return 0, nvml.ERROR_UNKNOWN }
			case "serve":
				p.socket = filepath.Join(t.TempDir(), "missing", "gpu.sock")
			case "register":
				kubeletSocket = filepath.Join(lifecycleSocketDir(t), "kubelet.sock")
				listener, err := net.Listen("unix", kubeletSocket)
				if err != nil {
					t.Fatal(err)
				}
				server := grpc.NewServer()
				// The default registration method returns Unimplemented.
				kubeletdevicepluginv1beta1.RegisterRegistrationServer(server, &kubeletdevicepluginv1beta1.UnimplementedRegistrationServer{})
				go func() { _ = server.Serve(listener) }()
				t.Cleanup(server.Stop)
			}
			if err := p.Start(kubeletSocket); err == nil {
				t.Fatal("expected startup error")
			}
			if p.server != nil || p.stop != nil || p.runCtx.Err() == nil {
				t.Fatal("startup failure left cycle resources active")
			}
			wantShutdown := 1
			if stage == "init" {
				wantShutdown = 0
			}
			if len(lib.InitCalls()) != 1 || len(lib.ShutdownCalls()) != wantShutdown {
				t.Fatal("startup failure leaked or over-released NVML")
			}
			if err := p.Stop(); err != nil {
				t.Fatal(err)
			}
			if len(lib.ShutdownCalls()) != wantShutdown {
				t.Fatal("repeated Stop released NVML")
			}
		})
	}
}

func TestMigPluginStartStopRestart(t *testing.T) {
	p, lib := lifecyclePlugin(t)
	for cycle := 1; cycle <= 2; cycle++ {
		if err := p.Start(""); err != nil {
			t.Fatal(err)
		}
		ctx := p.runCtx
		for i := 0; i < 3; i++ {
			if _, err := p.getAPIDevices(); err != nil {
				t.Fatal(err)
			}
			if _, _, err := p.topologyScore(nil); err != nil {
				t.Fatal(err)
			}
		}
		if len(lib.InitCalls()) != cycle || len(lib.ShutdownCalls()) != cycle-1 {
			t.Fatal("registration or topology scoring changed session ownership")
		}
		if err := p.Stop(); err != nil {
			t.Fatal(err)
		}
		if ctx.Err() == nil {
			t.Fatal("old cycle context is still active")
		}
		if len(lib.ShutdownCalls()) != cycle {
			t.Fatal("Stop did not release NVML")
		}
		if err := p.Stop(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMigPluginStopWaitsForWorkersDespiteSocketError(t *testing.T) {
	p, lib := lifecyclePlugin(t)
	p.initialize()
	if err := p.migMgr.Init(); err != nil {
		t.Fatal(err)
	}
	// A non-empty directory makes socket removal fail reliably, even as root.
	p.socket = t.TempDir()
	if err := os.WriteFile(filepath.Join(p.socket, "keep"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	workerCanceled, releaseWorker := make(chan struct{}), make(chan struct{})
	p.workers.Add(1)
	go func() {
		defer p.workers.Done()
		<-p.runCtx.Done()
		close(workerCanceled)
		<-releaseWorker
	}()
	stopped := make(chan error, 1)
	go func() { stopped <- p.Stop() }()
	select {
	case <-workerCanceled:
	case <-time.After(time.Second):
		close(releaseWorker)
		t.Fatal("worker was not canceled")
	}
	if len(lib.ShutdownCalls()) != 0 {
		close(releaseWorker)
		t.Fatal("NVML closed before worker finished")
	}
	close(releaseWorker)
	select {
	case err := <-stopped:
		if err == nil {
			t.Fatal("expected socket removal error")
		}
	case <-time.After(time.Second):
		t.Fatal("Stop did not finish")
	}
	if len(lib.ShutdownCalls()) != 1 || p.server != nil {
		t.Fatal("socket error skipped cleanup")
	}
}

func TestMigReconcilerStopsWithCycle(t *testing.T) {
	p, _ := lifecyclePlugin(t)
	p.initialize()
	p.workers.Add(1)
	go func() { defer p.workers.Done(); p.runMigAnnotationReconciler(time.Hour) }()
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}
	if p.ctx.Err() != nil {
		t.Fatal("Stop canceled the parent context")
	}
}

func TestHamiCorePluginStartStopRestart(t *testing.T) {
	p, lib := lifecyclePluginWithMode(t, nvidia.HamiCoreMode)
	for cycle := 1; cycle <= 2; cycle++ {
		if err := p.Start(""); err != nil {
			t.Fatal(err)
		}
		if _, err := p.getAPIDevices(); err != nil {
			t.Fatal(err)
		}
		if len(lib.InitCalls()) != cycle || len(lib.ShutdownCalls()) != cycle-1 {
			t.Fatal("hami-core registration changed session ownership")
		}
		if err := p.Stop(); err != nil {
			t.Fatal(err)
		}
		if len(lib.ShutdownCalls()) != cycle {
			t.Fatal("Stop did not release NVML")
		}
	}
}

// TestApplyStartupMigModeDisableRequiresReset checks that disabling MIG reports GPUs that
// require a reset.
func TestApplyStartupMigModeDisableRequiresReset(t *testing.T) {
	afterSet := false
	dev := &nvmlmock.Device{
		GetNameFunc: func() (string, nvml.Return) { return "NVIDIA A100-SXM4-40GB", nvml.SUCCESS },
		GetMigModeFunc: func() (int, int, nvml.Return) {
			if afterSet {
				return nvml.DEVICE_MIG_ENABLE, nvml.DEVICE_MIG_DISABLE, nvml.SUCCESS
			}
			return nvml.DEVICE_MIG_ENABLE, nvml.DEVICE_MIG_ENABLE, nvml.SUCCESS
		},
		SetMigModeFunc: func(int) (nvml.Return, nvml.Return) {
			afterSet = true
			return nvml.SUCCESS, nvml.SUCCESS
		},
		GetGpuInstanceProfileInfoFunc: func(int) (nvml.GpuInstanceProfileInfo, nvml.Return) {
			return nvml.GpuInstanceProfileInfo{}, nvml.ERROR_NOT_SUPPORTED
		},
		GetMaxMigDeviceCountFunc: func() (int, nvml.Return) { return 0, nvml.SUCCESS },
	}
	lib := &nvmlmock.Interface{
		InitFunc:                   func() nvml.Return { return nvml.SUCCESS },
		ShutdownFunc:               func() nvml.Return { return nvml.SUCCESS },
		DeviceGetCountFunc:         func() (int, nvml.Return) { return 1, nvml.SUCCESS },
		DeviceGetHandleByIndexFunc: func(int) (nvml.Device, nvml.Return) { return dev, nvml.SUCCESS },
	}
	p := &NvidiaDevicePlugin{
		ctx:           context.Background(),
		operatingMode: nvidia.HamiCoreMode,
		migMgr:        newMigInstanceManager(lib),
		schedulerConfig: nvidia.NvidiaConfig{
			MigProfileAllowlist: []device.AllowedMigProfiles{{
				Models:   []string{"A100"},
				Profiles: []string{"1g.5gb"},
			}},
		},
	}
	if err := p.migMgr.Init(); err != nil {
		t.Fatal(err)
	}
	defer p.migMgr.Shutdown()
	p.listNodePods = func() ([]*corev1.Pod, error) { return nil, nil }
	p.listLiveNodePods = p.listNodePods
	err := p.applyStartupMigMode(1, []string{"NVIDIA A100-SXM4-40GB"})
	if err == nil {
		t.Fatal("expected pending-reset error")
	}
	if !errors.Is(err, errMigModeNeedsReset) {
		t.Fatalf("applyStartupMigMode error = %v, want errors.Is(errMigModeNeedsReset)", err)
	}
	if !strings.Contains(err.Error(), "disable MIG for hami-core") {
		t.Fatalf("wrapped error = %v", err)
	}
	if !strings.Contains(err.Error(), "reboot the VM") || !strings.Contains(err.Error(), nvidiaMIGGettingStartedURL) {
		t.Fatalf("operator guidance missing from error: %v", err)
	}
}

func a100HamiCorePlugin(t *testing.T, dev nvml.Device) (*NvidiaDevicePlugin, *nvmlmock.Interface) {
	t.Helper()
	lib := &nvmlmock.Interface{
		InitFunc:                   func() nvml.Return { return nvml.SUCCESS },
		ShutdownFunc:               func() nvml.Return { return nvml.SUCCESS },
		DeviceGetCountFunc:         func() (int, nvml.Return) { return 1, nvml.SUCCESS },
		DeviceGetHandleByIndexFunc: func(int) (nvml.Device, nvml.Return) { return dev, nvml.SUCCESS },
	}
	p := &NvidiaDevicePlugin{
		ctx:           context.Background(),
		operatingMode: nvidia.HamiCoreMode,
		migMgr:        newMigInstanceManager(lib),
		schedulerConfig: nvidia.NvidiaConfig{
			MigProfileAllowlist: []device.AllowedMigProfiles{{
				Models:   []string{"A100"},
				Profiles: []string{"1g.5gb"},
			}},
		},
		rm: &rm.ResourceManagerMock{DevicesFunc: func() rm.Devices { return rm.Devices{} }},
	}
	if err := p.migMgr.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.migMgr.Shutdown() })
	return p, lib
}

func idleA100Device(setCalls *int, afterSetCurrent, afterSetPending int) *nvmlmock.Device {
	afterSet := false
	return &nvmlmock.Device{
		GetNameFunc: func() (string, nvml.Return) { return "NVIDIA A100-SXM4-40GB", nvml.SUCCESS },
		GetMigModeFunc: func() (int, int, nvml.Return) {
			if afterSet {
				return afterSetCurrent, afterSetPending, nvml.SUCCESS
			}
			return nvml.DEVICE_MIG_ENABLE, nvml.DEVICE_MIG_ENABLE, nvml.SUCCESS
		},
		SetMigModeFunc: func(int) (nvml.Return, nvml.Return) {
			afterSet = true
			if setCalls != nil {
				*setCalls++
			}
			return nvml.SUCCESS, nvml.SUCCESS
		},
		GetGpuInstanceProfileInfoFunc: func(int) (nvml.GpuInstanceProfileInfo, nvml.Return) {
			return nvml.GpuInstanceProfileInfo{}, nvml.ERROR_NOT_SUPPORTED
		},
		GetMaxMigDeviceCountFunc: func() (int, nvml.Return) { return 0, nvml.SUCCESS },
		GetComputeRunningProcessesFunc: func() ([]nvml.ProcessInfo, nvml.Return) {
			return nil, nvml.SUCCESS
		},
		GetGraphicsRunningProcessesFunc: func() ([]nvml.ProcessInfo, nvml.Return) {
			return nil, nvml.SUCCESS
		},
	}
}

// TestApplyStartupMigModeDisableSucceeds checks that idle GPUs can leave MIG mode at startup.
func TestApplyStartupMigModeDisableSucceeds(t *testing.T) {
	setCalls := 0
	p, _ := a100HamiCorePlugin(t, idleA100Device(&setCalls, nvml.DEVICE_MIG_DISABLE, nvml.DEVICE_MIG_DISABLE))
	p.listNodePods = func() ([]*corev1.Pod, error) { return nil, nil }
	p.listLiveNodePods = p.listNodePods
	if err := p.applyStartupMigMode(1, []string{"NVIDIA A100-SXM4-40GB"}); err != nil {
		t.Fatal(err)
	}
	if setCalls != 1 {
		t.Fatalf("SetMigMode calls = %d, want 1", setCalls)
	}
}

// TestApplyStartupMigModeDisableIgnoresProfileAllowlist checks that the profile allowlist does
// not restrict disabling MIG.
func TestApplyStartupMigModeDisableIgnoresProfileAllowlist(t *testing.T) {
	setCalls := 0
	p, _ := a100HamiCorePlugin(t, idleA100Device(&setCalls, nvml.DEVICE_MIG_DISABLE, nvml.DEVICE_MIG_DISABLE))
	p.listNodePods = func() ([]*corev1.Pod, error) { return nil, nil }
	p.listLiveNodePods = p.listNodePods
	if err := p.applyStartupMigMode(1, []string{"NVIDIA H100 80GB HBM3"}); err != nil {
		t.Fatal(err)
	}
	if setCalls != 1 {
		t.Fatalf("SetMigMode calls = %d, want 1 for MIG-capable GPU outside MigProfileAllowlist", setCalls)
	}
}

// TestApplyStartupMigModeDisableFailsClosedWhenAllocationLookupFails checks that unreadable Pod
// state prevents disabling MIG on potentially busy GPUs.
func TestApplyStartupMigModeDisableFailsClosedWhenAllocationLookupFails(t *testing.T) {
	setCalls := 0
	p, _ := a100HamiCorePlugin(t, idleA100Device(&setCalls, nvml.DEVICE_MIG_DISABLE, nvml.DEVICE_MIG_DISABLE))
	p.listNodePods = func() ([]*corev1.Pod, error) { return nil, errors.New("snapshot unavailable") }
	p.listLiveNodePods = p.listNodePods
	err := p.applyStartupMigMode(1, []string{"NVIDIA A100-SXM4-40GB"})
	if err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("applyStartupMigMode error = %v, want in-use fail-closed", err)
	}
	if errors.Is(err, errMigModeNeedsReset) {
		t.Fatalf("allocation lookup failure must not look like a GPU reset: %v", err)
	}
	if setCalls != 0 {
		t.Fatalf("SetMigMode calls = %d, want 0", setCalls)
	}
}

func TestRegistrationRequiresRunningMigManagerSession(t *testing.T) {
	p, _ := lifecyclePluginWithMode(t, nvidia.HamiCoreMode)
	nvmlInitCalls := 0
	originalInit := nvmlInit
	nvmlInit = func() nvml.Return {
		nvmlInitCalls++
		return nvml.SUCCESS
	}
	scoreCalls := 0
	originalScore := calculateGPUScore
	calculateGPUScore = func([]string) (nvidia.ListDeviceScore, bool, error) {
		scoreCalls++
		return nil, false, nil
	}
	defer func() {
		nvmlInit = originalInit
		calculateGPUScore = originalScore
	}()

	if _, err := p.getAPIDevices(); err == nil {
		t.Fatal("getAPIDevices succeeded without an NVML session")
	}
	if _, _, err := p.topologyScore(nil); err == nil {
		t.Fatal("topologyScore succeeded without an NVML session")
	}
	if err := p.migMgr.Init(); err != nil {
		t.Fatal(err)
	}
	p.migMgr.Shutdown()
	if _, err := p.getAPIDevices(); err == nil {
		t.Fatal("getAPIDevices succeeded after Shutdown")
	}
	if _, _, err := p.topologyScore(nil); err == nil {
		t.Fatal("topologyScore succeeded after Shutdown")
	}
	if nvmlInitCalls != 0 || scoreCalls != 0 {
		t.Fatalf("fell back to package NVML (init=%d score=%d)", nvmlInitCalls, scoreCalls)
	}
}

// TestMigReconcilerTicksAndStopsAfterSnapshotRead checks periodic reconciliation and shutdown
// without a nested apply lock.
func TestMigReconcilerTicksAndStopsAfterSnapshotRead(t *testing.T) {
	p, _ := lifecyclePlugin(t)
	p.initialize()
	read := make(chan struct{}, 1)
	p.listNodePods = func() ([]*corev1.Pod, error) {
		select {
		case read <- struct{}{}:
		default:
		}
		return nil, errors.New("snapshot unavailable")
	}
	p.workers.Add(1)
	go func() { defer p.workers.Done(); p.runMigAnnotationReconciler(time.Millisecond) }()
	select {
	case <-read:
	case <-time.After(time.Second):
		t.Fatal("periodic reconciliation did not reach the Pod snapshot")
	}
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}
}
