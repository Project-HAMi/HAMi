// Copyright 2026 The HAMi Authors.
// SPDX-License-Identifier: Apache-2.0
package cdi

import (
	"testing"

	"github.com/stretchr/testify/require"
	specs "tags.cncf.io/container-device-interface/specs-go"
)

func TestValidateMemoryLimitsSpec(t *testing.T) {
	good := func() *specs.Spec {
		return &specs.Spec{Devices: []specs.Device{{Name: "GPU-a", ContainerEdits: specs.ContainerEdits{Hooks: []*specs.Hook{{HookName: "createRuntime", Path: "/usr/bin/nvidia-cdi-hook", Args: []string{"nvidia-cdi-hook", "apply-cuda-memory-limits", "--gpu-id", "GPU-a"}}}}}}}
	}
	require.NoError(t, validateMemoryLimitsSpec(good(), []string{"GPU-a"}))
	require.Error(t, validateMemoryLimitsSpec(good(), []string{"GPU-b"}))
	s := good()
	s.Devices[0].ContainerEdits.Hooks[0].HookName = "createContainer"
	require.Error(t, validateMemoryLimitsSpec(s, []string{"GPU-a"}))
	s = good()
	s.Devices[0].ContainerEdits.Hooks[0].Args = []string{"create-symlinks"}
	require.Error(t, validateMemoryLimitsSpec(s, []string{"GPU-a"}))
	s = good()
	s.Devices[0].ContainerEdits.Hooks[0].Path = ""
	require.Error(t, validateMemoryLimitsSpec(s, []string{"GPU-a"}))
}
