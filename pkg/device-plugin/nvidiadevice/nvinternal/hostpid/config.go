/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * Copyright (c) 2026 The HAMi Authors.
 */

package hostpid

import (
	"fmt"
	"strconv"
)

const (
	EnvironmentVariable               = "LIBVGPU_HOSTPID_BROKER"
	DriverAdmissionEnvironment        = "LIBVGPU_DRIVER_ADMISSION"
	DriverAdmissionPermitsEnvironment = "HAMI_HOSTPID_DRIVER_ADMISSION_PERMITS"
	MaximumDriverAdmissionPermits     = 64

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
