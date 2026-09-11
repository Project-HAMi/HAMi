/*
 * Copyright (c) 2026, HAMi.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package plugin

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

func hasLibvgpuMount(mounts []*kubeletdevicepluginv1beta1.Mount) bool {
	for _, m := range mounts {
		if m.ContainerPath == fmt.Sprintf("%s/vgpu/libvgpu.so", hostHookPath) {
			return true
		}
	}
	return false
}

func TestIsSandboxedRuntimeClass(t *testing.T) {
	tests := []struct {
		name         string
		runtimeClass *string
		configured   []string
		want         bool
	}{
		{
			name:         "NoRuntimeClass",
			runtimeClass: nil,
			want:         false,
		},
		{
			name:         "EmptyRuntimeClass",
			runtimeClass: ptr(""),
			want:         false,
		},
		{
			name:         "NvidiaRuntimeClass",
			runtimeClass: ptr("nvidia"),
			want:         false,
		},
		{
			name:         "DefaultGvisorName",
			runtimeClass: ptr("gvisor"),
			want:         true,
		},
		{
			name:         "DefaultRunscName",
			runtimeClass: ptr("runsc"),
			want:         true,
		},
		{
			name:         "NameMatchIsCaseInsensitiveAndTrimmed",
			runtimeClass: ptr(" gVisor "),
			want:         true,
		},
		{
			name:         "ConfiguredListReplacesDefaults",
			runtimeClass: ptr("gvisor"),
			configured:   []string{"sandboxed"},
			want:         false,
		},
		{
			name:         "ConfiguredListMatches",
			runtimeClass: ptr("sandboxed"),
			configured:   []string{"sandboxed", "gvisor"},
			want:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{Spec: corev1.PodSpec{RuntimeClassName: tt.runtimeClass}}
			require.Equal(t, tt.want, isSandboxedRuntimeClass(pod, tt.configured))
		})
	}
}

func TestIsSandboxedRuntimeClass_NilPod(t *testing.T) {
	require.False(t, isSandboxedRuntimeClass(nil, nil))
}

// allocateWithRuntimeClass runs a single container Allocate for a pod pinned to runtimeClass.
func allocateWithRuntimeClass(t *testing.T, runtimeClass string, configured []string) *kubeletdevicepluginv1beta1.ContainerAllocateResponse {
	t.Helper()
	setupInRequestDevices(t)
	plugin := newTestPlugin(t)
	plugin.schedulerConfig.SandboxRuntimeClassNames = configured

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       "pod-uid",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-to-allocate": "GPU-aaa,NVIDIA,3000,50:;",
			},
		},
		Spec: corev1.PodSpec{
			RuntimeClassName: &runtimeClass,
			Containers:       []corev1.Container{{Name: "c0"}},
		},
	}
	setupFakeClient(t, pod)
	mockAllocateGlobals(t, pod)

	request := &kubeletdevicepluginv1beta1.AllocateRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerAllocateRequest{
			{DevicesIds: []string{"GPU-aaa-0"}},
		},
	}

	response, err := plugin.Allocate(context.Background(), request)
	require.NoError(t, err)
	require.Len(t, response.ContainerResponses, 1)
	return response.ContainerResponses[0]
}

func TestAllocate_GvisorRuntimeClassSkipsLdSoPreload(t *testing.T) {
	containerResponse := allocateWithRuntimeClass(t, "gvisor", nil)

	require.False(t, hasLdSoPreloadMount(containerResponse.Mounts),
		"mounting the preload makes every binary fail on libvgpu.so's libcuda.so.1 dependency")
	// The allocation is otherwise untouched: the device is still handed to the container.
	require.True(t, hasLibvgpuMount(containerResponse.Mounts))
	require.Equal(t, "3000m", containerResponse.Envs["CUDA_DEVICE_MEMORY_LIMIT_0"])
	require.Equal(t, "50", containerResponse.Envs["CUDA_DEVICE_SM_LIMIT"])
}

func TestAllocate_NvidiaRuntimeClassKeepsLdSoPreload(t *testing.T) {
	containerResponse := allocateWithRuntimeClass(t, "nvidia", nil)

	require.True(t, hasLdSoPreloadMount(containerResponse.Mounts),
		"runc based runtime classes must keep the libvgpu preload injection")
}

func TestAllocate_ConfiguredSandboxRuntimeClassSkipsLdSoPreload(t *testing.T) {
	containerResponse := allocateWithRuntimeClass(t, "gvisor-nvidia", []string{"gvisor-nvidia"})

	require.False(t, hasLdSoPreloadMount(containerResponse.Mounts))
}
