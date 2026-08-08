/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * Copyright (c) 2026 The HAMi Authors.
 */

package main

import (
	"errors"
	"fmt"
	"os"

	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/hostpid"
)

type runningHostPIDBroker struct {
	broker   hostPIDBroker
	done     chan struct{}
	serveErr error
}

type hostPIDBroker interface {
	Serve() error
	Close() error
}

type hostPIDBrokerListener func() (hostPIDBroker, error)

func startHostPIDBroker() (*runningHostPIDBroker, error) {
	return startHostPIDBrokerWithListener(func() (hostPIDBroker, error) {
		return hostpid.ListenDefault()
	})
}

func startHostPIDBrokerWithListener(
	listen hostPIDBrokerListener) (*runningHostPIDBroker, error) {
	if !hostpid.Enabled(os.Getenv(hostpid.EnvironmentVariable)) {
		return nil, nil
	}
	broker, err := listen()
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

func (running *runningHostPIDBroker) failure() error {
	if running == nil {
		return nil
	}
	select {
	case <-running.done:
		if running.serveErr != nil {
			return fmt.Errorf("host PID broker stopped: %w", running.serveErr)
		}
		return errors.New("host PID broker stopped unexpectedly")
	default:
		return nil
	}
}
