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
