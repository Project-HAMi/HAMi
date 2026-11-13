/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * Copyright (c) 2026 The HAMi Authors.
 */

package hostpid

import (
	"fmt"
	"strconv"
	"time"
)

const (
	EnvironmentVariable               = "LIBVGPU_HOSTPID_BROKER"
	DriverAdmissionEnvironment        = "LIBVGPU_DRIVER_ADMISSION"
	DriverAdmissionPermitsEnvironment = "HAMI_HOSTPID_DRIVER_ADMISSION_PERMITS"
	MaximumDriverAdmissionPermits     = 64

	DriverAdmissionMaxHoldEnvironment = "HAMI_HOSTPID_DRIVER_ADMISSION_MAX_HOLD_SECONDS"
	DefaultDriverAdmissionMaxHold     = 60 * time.Second
	MaximumDriverAdmissionMaxHold     = time.Hour

	ServerDirectory  = "/var/run/hami/hostpid"
	ServerSocketPath = ServerDirectory + "/broker.sock"

	ContainerDirectory  = "/tmp/vgpulock/hostpid"
	ContainerSocketPath = ContainerDirectory + "/broker.sock"
)

func Enabled(value string) bool {
	return value == "1"
}

func ParseDriverAdmissionPermits(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	permits, err := strconv.Atoi(value)
	if err != nil || permits < 0 || permits == 1 ||
		permits > MaximumDriverAdmissionPermits {
		return 0, fmt.Errorf("driver admission permits must be 0 or between 2 and %d",
			MaximumDriverAdmissionPermits)
	}
	return permits, nil
}

func ParseDriverAdmissionMaxHold(value string) (time.Duration, error) {
	if value == "" {
		return DefaultDriverAdmissionMaxHold, nil
	}
	maximum := int(MaximumDriverAdmissionMaxHold / time.Second)
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 1 || seconds > maximum {
		return 0, fmt.Errorf("driver admission max hold must be between 1 and %d seconds",
			maximum)
	}
	return time.Duration(seconds) * time.Second, nil
}
