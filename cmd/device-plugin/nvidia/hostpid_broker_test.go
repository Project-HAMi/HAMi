/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * Copyright (c) 2026 The HAMi Authors.
 */

package main

import (
	"errors"
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/hostpid"
)

func TestStartHostPIDBrokerDisabled(t *testing.T) {
	for _, value := range []string{"", "0", "true", "false", "01", " 1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv(hostpid.EnvironmentVariable, value)
			running, err := startHostPIDBroker()
			if err != nil || running != nil {
				t.Fatalf("running=%v err=%v", running, err)
			}
		})
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
