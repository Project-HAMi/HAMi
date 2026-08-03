/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * Copyright (c) 2026 The HAMi Authors.
 */

package main

import (
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/hostpid"
)

func TestStartHostPIDBrokerDisabled(t *testing.T) {
	t.Setenv(hostpid.EnvironmentVariable, "")
	running, err := startHostPIDBroker()
	if err != nil || running != nil {
		t.Fatalf("running=%v err=%v", running, err)
	}

	t.Setenv(hostpid.EnvironmentVariable, "0")
	running, err = startHostPIDBroker()
	if err != nil || running != nil {
		t.Fatalf("running=%v err=%v", running, err)
	}
}
