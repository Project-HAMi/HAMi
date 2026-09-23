//go:build !linux

/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * Copyright (c) 2026 The HAMi Authors.
 */

package hostpid

import "errors"

var errUnsupported = errors.New("the host PID broker requires Linux")

type Broker struct{}

func ListenDefault() (*Broker, error) {
	return nil, errUnsupported
}

func (broker *Broker) Serve() error {
	return errUnsupported
}

func (broker *Broker) Close() error {
	return nil
}
