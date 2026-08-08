/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * Copyright (c) 2026 The HAMi Authors.
 */

package main

import (
	"errors"
	"sync"
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/hostpid"
)

type fakeHostPIDBroker struct {
	serveStarted chan struct{}
	serveRelease chan struct{}
	closeOnce    sync.Once
	serveErr     error
	closeErr     error
}

func newFakeHostPIDBroker() *fakeHostPIDBroker {
	return &fakeHostPIDBroker{
		serveStarted: make(chan struct{}),
		serveRelease: make(chan struct{}),
	}
}

func (broker *fakeHostPIDBroker) Serve() error {
	close(broker.serveStarted)
	<-broker.serveRelease
	return broker.serveErr
}

func (broker *fakeHostPIDBroker) Close() error {
	broker.closeOnce.Do(func() {
		close(broker.serveRelease)
	})
	return broker.closeErr
}

func TestStartHostPIDBrokerDisabled(t *testing.T) {
	for _, value := range []string{"", "0", "true", "false", "01", " 1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv(hostpid.EnvironmentVariable, value)
			listenerCalled := false
			running, err := startHostPIDBrokerWithListener(
				func() (hostPIDBroker, error) {
					listenerCalled = true
					return nil, nil
				})
			if err != nil || running != nil {
				t.Fatalf("running=%v err=%v", running, err)
			}
			if listenerCalled {
				t.Fatal("listener was called while broker was disabled")
			}
		})
	}
}

func TestStartHostPIDBrokerDefaultDisabled(t *testing.T) {
	t.Setenv(hostpid.EnvironmentVariable, "")
	running, err := startHostPIDBroker()
	if err != nil || running != nil {
		t.Fatalf("running=%v err=%v", running, err)
	}
}

func TestStartHostPIDBrokerListenFailure(t *testing.T) {
	t.Setenv(hostpid.EnvironmentVariable, "1")
	wantErr := errors.New("listen failed")
	running, err := startHostPIDBrokerWithListener(
		func() (hostPIDBroker, error) {
			return nil, wantErr
		})
	if running != nil || !errors.Is(err, wantErr) {
		t.Fatalf("running=%v err=%v, want %v", running, err, wantErr)
	}
}

func TestStartAndStopHostPIDBroker(t *testing.T) {
	t.Setenv(hostpid.EnvironmentVariable, "1")
	broker := newFakeHostPIDBroker()
	running, err := startHostPIDBrokerWithListener(
		func() (hostPIDBroker, error) {
			return broker, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	<-broker.serveStarted
	if err := running.failure(); err != nil {
		t.Fatalf("running broker failure=%v", err)
	}
	if err := running.stop(); err != nil {
		t.Fatalf("stop broker: %v", err)
	}
	if err := running.failure(); err == nil {
		t.Fatal("stopped broker was not reported")
	}
}

func TestHostPIDBrokerServeFailure(t *testing.T) {
	t.Setenv(hostpid.EnvironmentVariable, "1")
	wantErr := errors.New("serve failed")
	broker := newFakeHostPIDBroker()
	broker.serveErr = wantErr
	close(broker.serveRelease)
	running, err := startHostPIDBrokerWithListener(
		func() (hostPIDBroker, error) {
			return broker, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	<-running.done
	if err := running.failure(); !errors.Is(err, wantErr) {
		t.Fatalf("failure=%v, want %v", err, wantErr)
	}
}

func TestHostPIDBrokerCloseFailure(t *testing.T) {
	t.Setenv(hostpid.EnvironmentVariable, "1")
	wantErr := errors.New("close failed")
	broker := newFakeHostPIDBroker()
	broker.closeErr = wantErr
	running, err := startHostPIDBrokerWithListener(
		func() (hostPIDBroker, error) {
			return broker, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	<-broker.serveStarted
	if err := running.stop(); !errors.Is(err, wantErr) {
		t.Fatalf("stop=%v, want %v", err, wantErr)
	}
}

func TestRunningHostPIDBrokerFailure(t *testing.T) {
	var disabled *runningHostPIDBroker
	if err := disabled.failure(); err != nil {
		t.Fatalf("disabled broker failure=%v", err)
	}

	running := &runningHostPIDBroker{done: make(chan struct{})}
	if err := running.failure(); err != nil {
		t.Fatalf("running broker failure=%v", err)
	}

	serveErr := errors.New("accept failed")
	running.serveErr = serveErr
	close(running.done)
	if err := running.failure(); !errors.Is(err, serveErr) {
		t.Fatalf("stopped broker failure=%v, want %v", err, serveErr)
	}

	stopped := &runningHostPIDBroker{done: make(chan struct{})}
	close(stopped.done)
	if err := stopped.failure(); err == nil {
		t.Fatal("clean broker stop was not reported")
	}
}
