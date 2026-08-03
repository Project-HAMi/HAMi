/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * Copyright (c) 2026 The HAMi Authors.
 */

package main

import (
	"os"

	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/hostpid"
)

type runningHostPIDBroker struct {
	broker   *hostpid.Broker
	done     chan struct{}
	serveErr error
}

func startHostPIDBroker() (*runningHostPIDBroker, error) {
	if !hostpid.Enabled(os.Getenv(hostpid.EnvironmentVariable)) {
		return nil, nil
	}
	broker, err := hostpid.ListenDefault()
	if err != nil {
		return nil, err
	}
	running := &runningHostPIDBroker{
		broker: broker,
		done:   make(chan struct{}),
	}
	go func() {
		running.serveErr = broker.Serve()
		close(running.done)
	}()
	klog.Infof("Host PID broker is listening on %s", hostpid.ServerSocketPath)
	return running, nil
}

func (running *runningHostPIDBroker) stop() error {
	closeErr := running.broker.Close()
	<-running.done
	return closeErr
}
