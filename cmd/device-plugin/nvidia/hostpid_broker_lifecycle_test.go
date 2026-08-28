/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * Copyright (c) 2026 The HAMi Authors.
 */

package main

import (
	"errors"
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/plugin"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/rm"
)

type fakeDevicePlugin struct {
	devices    rm.Devices
	start      func(string) error
	stopErr    error
	startCalls int
	stopCalls  int
}

func (p *fakeDevicePlugin) Devices() rm.Devices {
	return p.devices
}

func (p *fakeDevicePlugin) Start(socket string) error {
	p.startCalls++
	if p.start != nil {
		return p.start(socket)
	}
	return nil
}

func (p *fakeDevicePlugin) Stop() error {
	p.stopCalls++
	return p.stopErr
}

func devicePluginWithDevice() *fakeDevicePlugin {
	return &fakeDevicePlugin{
		devices: rm.Devices{"GPU-0": &rm.Device{}},
	}
}

func TestStartPluginServersDetectsBrokerFailureBeforeStart(t *testing.T) {
	wantErr := errors.New("broker failed")
	done := make(chan struct{})
	close(done)
	running := &runningHostPIDBroker{
		done:     done,
		serveErr: wantErr,
	}
	p := devicePluginWithDevice()

	started, restart, err := startPluginServers(
		[]plugin.Interface{p}, "/tmp/kubelet.sock", running)
	if started != 0 || restart || !errors.Is(err, wantErr) {
		t.Fatalf("started=%d restart=%v err=%v, want broker failure",
			started, restart, err)
	}
	if p.startCalls != 0 || p.stopCalls != 0 {
		t.Fatalf("start calls=%d stop calls=%d, want 0 and 0",
			p.startCalls, p.stopCalls)
	}
}

func TestStartPluginServersCleansUpAfterBrokerFailure(t *testing.T) {
	wantServeErr := errors.New("broker failed")
	wantStopErr := errors.New("plugin stop failed")
	done := make(chan struct{})
	running := &runningHostPIDBroker{done: done}
	p := devicePluginWithDevice()
	p.stopErr = wantStopErr
	p.start = func(string) error {
		running.serveErr = wantServeErr
		close(done)
		return nil
	}

	started, restart, err := startPluginServers(
		[]plugin.Interface{p}, "/tmp/kubelet.sock", running)
	if started != 0 || restart || !errors.Is(err, wantServeErr) ||
		!errors.Is(err, wantStopErr) {
		t.Fatalf("started=%d restart=%v err=%v, want joined failures",
			started, restart, err)
	}
	if p.startCalls != 1 || p.stopCalls != 1 {
		t.Fatalf("start calls=%d stop calls=%d, want 1 and 1",
			p.startCalls, p.stopCalls)
	}
}

func TestStartPluginServersRequestsRestartAfterStartFailure(t *testing.T) {
	wantErr := errors.New("plugin start failed")
	p := devicePluginWithDevice()
	p.start = func(socket string) error {
		if socket != "/tmp/kubelet.sock" {
			t.Fatalf("socket=%q", socket)
		}
		return wantErr
	}

	started, restart, err := startPluginServers(
		[]plugin.Interface{p}, "/tmp/kubelet.sock", nil)
	if started != 0 || !restart || err != nil {
		t.Fatalf("started=%d restart=%v err=%v", started, restart, err)
	}
	if p.startCalls != 1 || p.stopCalls != 0 {
		t.Fatalf("start calls=%d stop calls=%d, want 1 and 0",
			p.startCalls, p.stopCalls)
	}
}

func TestStartPluginServersSkipsEmptyPlugins(t *testing.T) {
	empty := &fakeDevicePlugin{}
	ready := devicePluginWithDevice()

	started, restart, err := startPluginServers(
		[]plugin.Interface{empty, ready}, "/tmp/kubelet.sock", nil)
	if started != 1 || restart || err != nil {
		t.Fatalf("started=%d restart=%v err=%v", started, restart, err)
	}
	if empty.startCalls != 0 || ready.startCalls != 1 {
		t.Fatalf("empty starts=%d ready starts=%d, want 0 and 1",
			empty.startCalls, ready.startCalls)
	}
}
