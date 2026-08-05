/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * Copyright (c) 2026 The HAMi Authors.
 */

package plugin

import (
	"os"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/hostpid"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

func configureHostPIDBroker(
	response *kubeletdevicepluginv1beta1.ContainerAllocateResponse) {
	if !hostpid.Enabled(os.Getenv(hostpid.EnvironmentVariable)) {
		return
	}
	if response.Envs == nil {
		response.Envs = make(map[string]string)
	}
	response.Envs[hostpid.EnvironmentVariable] = "1"
	for _, mount := range response.Mounts {
		if mount.ContainerPath == hostpid.ContainerDirectory {
			mount.HostPath = hostpid.ServerDirectory
			mount.ReadOnly = true
			return
		}
	}
	response.Mounts = append(response.Mounts,
		&kubeletdevicepluginv1beta1.Mount{
			ContainerPath: hostpid.ContainerDirectory,
			HostPath:      hostpid.ServerDirectory,
			ReadOnly:      true,
		})
}
