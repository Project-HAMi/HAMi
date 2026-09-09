/*
 * Copyright (c) 2026, HAMi. All rights reserved.
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
 */

package plugin

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	nvmlmock "github.com/NVIDIA/go-nvml/pkg/nvml/mock"
	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"google.golang.org/grpc"
	kubelet "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

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
	lib := &nvmlmock.Interface{
		InitFunc:           func() nvml.Return { return nvml.SUCCESS },
		ShutdownFunc:       func() nvml.Return { return nvml.SUCCESS },
		DeviceGetCountFunc: func() (int, nvml.Return) { return 0, nvml.SUCCESS },
	}
	p := &NvidiaDevicePlugin{
		ctx: context.Background(), operatingMode: nvidia.MigMode,
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
				kubelet.RegisterRegistrationServer(server, &kubelet.UnimplementedRegistrationServer{})
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
