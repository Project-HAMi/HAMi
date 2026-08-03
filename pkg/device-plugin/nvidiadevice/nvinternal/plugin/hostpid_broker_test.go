/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * Copyright (c) 2026 The HAMi Authors.
 */

package plugin

import (
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/hostpid"
	"github.com/stretchr/testify/require"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

func TestConfigureHostPIDBrokerDisabled(t *testing.T) {
	for _, value := range []string{"", "0"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv(hostpid.EnvironmentVariable, value)
			response := &kubeletdevicepluginv1beta1.ContainerAllocateResponse{}

			configureHostPIDBroker(response)

			require.Empty(t, response.Envs)
			require.Empty(t, response.Mounts)
		})
	}
}

func TestConfigureHostPIDBrokerEnabled(t *testing.T) {
	t.Setenv(hostpid.EnvironmentVariable, "1")
	existingMount := &kubeletdevicepluginv1beta1.Mount{
		ContainerPath: "/tmp/vgpulock",
		HostPath:      "/tmp/vgpulock",
	}
	response := &kubeletdevicepluginv1beta1.ContainerAllocateResponse{
		Envs:   map[string]string{"KEEP": "yes"},
		Mounts: []*kubeletdevicepluginv1beta1.Mount{existingMount},
	}

	configureHostPIDBroker(response)

	require.Equal(t, "yes", response.Envs["KEEP"])
	require.Equal(t, "1", response.Envs[hostpid.EnvironmentVariable])
	require.Len(t, response.Mounts, 2)
	require.Same(t, existingMount, response.Mounts[0])
	require.Equal(t, &kubeletdevicepluginv1beta1.Mount{
		ContainerPath: hostpid.ContainerDirectory,
		HostPath:      hostpid.ServerDirectory,
		ReadOnly:      true,
	}, response.Mounts[1])
}
