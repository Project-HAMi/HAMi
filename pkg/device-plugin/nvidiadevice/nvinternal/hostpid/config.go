/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * Copyright (c) 2026 The HAMi Authors.
 */

package hostpid

const (
	EnvironmentVariable = "LIBVGPU_HOSTPID_BROKER"

	ServerDirectory  = "/var/run/hami/hostpid"
	ServerSocketPath = ServerDirectory + "/broker.sock"

	ContainerDirectory  = "/tmp/vgpulock/hostpid"
	ContainerSocketPath = ContainerDirectory + "/broker.sock"
)

func Enabled(value string) bool {
	return value == "1"
}
