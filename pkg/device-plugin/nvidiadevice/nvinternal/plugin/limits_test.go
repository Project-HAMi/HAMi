/*
Copyright 2026 The HAMi Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package plugin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/hostpid"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/rm"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/vgpucache"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

// findMount returns the mount of containerPath, or nil.
func findMount(mounts []*kubeletdevicepluginv1beta1.Mount, containerPath string) *kubeletdevicepluginv1beta1.Mount {
	for _, mount := range mounts {
		if mount.ContainerPath == containerPath {
			return mount
		}
	}
	return nil
}

// newCacheDirectory creates a container cache directory in a fresh root, as Prepare does.
func newCacheDirectory(t *testing.T) string {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "pod-uid_main")
	require.NoError(t, os.Mkdir(directory, 0o777))
	return directory
}

// TestLimitsFileValues checks which variables the limits file carries and which source wins.
func TestLimitsFileValues(t *testing.T) {
	responseEnvs := map[string]string{
		"CUDA_DEVICE_MEMORY_LIMIT_0":      "3000m",
		"CUDA_DEVICE_MEMORY_LIMIT_1":      "2000m",
		"CUDA_DEVICE_SM_LIMIT":            "50",
		"CUDA_DEVICE_MEMORY_SHARED_CACHE": "/usr/local/vgpu/abc.cache",
		"CUDA_OVERSUBSCRIBE":              "true",
		"LIBCUDA_LOG_LEVEL":               "0",
		util.CoreLimitSwitch:              "disable",
		hostpid.EnvironmentVariable:       "1",
		"NVIDIA_VISIBLE_DEVICES":          "GPU-aaa",
	}
	ctr := &corev1.Container{Env: []corev1.EnvVar{
		{Name: util.CoreLimitSwitch, Value: "force"},
		{Name: util.TaskPriority, ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
		}},
		{Name: "LIBCUDA_LOG_LEVEL", Value: "4"},
		{Name: "UNRELATED", Value: "x"},
	}}

	require.Equal(t, map[string]string{
		"CUDA_DEVICE_MEMORY_LIMIT_0":      "3000m",
		"CUDA_DEVICE_MEMORY_LIMIT_1":      "2000m",
		"CUDA_DEVICE_SM_LIMIT":            "50",
		"CUDA_DEVICE_MEMORY_SHARED_CACHE": "/usr/local/vgpu/abc.cache",
		"CUDA_OVERSUBSCRIBE":              "true",
		"LIBCUDA_LOG_LEVEL":               "4",
		util.CoreLimitSwitch:              "force",
		hostpid.EnvironmentVariable:       "1",
		limitsFormatKey:                   limitsFormatVersion,
	}, limitsFileValues(responseEnvs, ctr))

	require.Equal(t, map[string]string{limitsFormatKey: limitsFormatVersion}, limitsFileValues(nil, nil))
}

// TestWriteLimitsFile checks the file layout and mode, and that a rewrite replaces it without
// leaving a temporary file behind.
func TestWriteLimitsFile(t *testing.T) {
	cacheDirectory := newCacheDirectory(t)
	path := vgpucache.LimitsFilePath(cacheDirectory)

	require.NoError(t, writeLimitsFile(path, cacheDirectory, map[string]string{
		"CUDA_DEVICE_SM_LIMIT":       "50",
		"CUDA_DEVICE_MEMORY_LIMIT_0": "3000m",
		limitsFormatKey:              limitsFormatVersion,
	}))
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "CUDA_DEVICE_MEMORY_LIMIT_0=3000m\nCUDA_DEVICE_SM_LIMIT=50\nHAMI_LIMITS_FORMAT=1\n", string(content))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(limitsFileMode), info.Mode().Perm())

	require.NoError(t, writeLimitsFile(path, cacheDirectory, map[string]string{"CUDA_DEVICE_SM_LIMIT": "30"}))
	content, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "CUDA_DEVICE_SM_LIMIT=30\n", string(content))

	entries, err := os.ReadDir(cacheDirectory)
	require.NoError(t, err)
	require.Empty(t, entries, "the temporary file must be renamed away")
}

// TestWriteLimitsFileRejectsInvalidEntries checks that a key or value cannot add a line of its
// own, and that nothing is written when one tries.
func TestWriteLimitsFileRejectsInvalidEntries(t *testing.T) {
	for name, values := range map[string]map[string]string{
		"newline in value":         {"CUDA_DEVICE_SM_LIMIT": "50\nCUDA_DEVICE_MEMORY_LIMIT_0=0"},
		"carriage return in value": {"CUDA_DEVICE_SM_LIMIT": "50\r"},
		"equals sign in key":       {"CUDA_DEVICE=SM_LIMIT": "50"},
		"empty key":                {"": "50"},
	} {
		t.Run(name, func(t *testing.T) {
			cacheDirectory := newCacheDirectory(t)
			path := vgpucache.LimitsFilePath(cacheDirectory)
			require.ErrorContains(t, writeLimitsFile(path, cacheDirectory, values), "invalid limits file entry")
			require.NoFileExists(t, path)
			entries, err := os.ReadDir(cacheDirectory)
			require.NoError(t, err)
			require.Empty(t, entries)
		})
	}
}

// TestWriteLimitsFileRemovesTemporaryFileOnFailure checks the cleanup when the final rename fails.
func TestWriteLimitsFileRemovesTemporaryFileOnFailure(t *testing.T) {
	cacheDirectory := newCacheDirectory(t)
	path := vgpucache.LimitsFilePath(cacheDirectory)
	// A directory where the file belongs makes the rename fail.
	require.NoError(t, os.Mkdir(path, 0o755))

	err := writeLimitsFile(path, cacheDirectory, map[string]string{"CUDA_DEVICE_SM_LIMIT": "50"})
	require.ErrorContains(t, err, "publish limits file")
	entries, err := os.ReadDir(cacheDirectory)
	require.NoError(t, err)
	require.Empty(t, entries)
}

// newLimitsTestPod returns a single-container vGPU Pod with a task priority on its spec.
func newLimitsTestPod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       "pod-uid",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-to-allocate": "GPU-aaa,NVIDIA,3000,50:;",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "c0",
				Env:  []corev1.EnvVar{{Name: util.TaskPriority, Value: "1"}},
			}},
		},
	}
}

// TestAllocate_WritesLimitsFileAndMountsOverrideEnv checks that a vGPU container gets its limits
// in a file beside its cache directory, mounted read-only at /overrideEnv.
func TestAllocate_WritesLimitsFileAndMountsOverrideEnv(t *testing.T) {
	// Keep the host PID broker off, so the expected file does not depend on the test environment.
	t.Setenv(hostpid.EnvironmentVariable, "")
	setupInRequestDevices(t)
	plugin := newTestPlugin(t)
	pod := newLimitsTestPod()
	setupFakeClient(t, pod)
	mockAllocateGlobals(t, pod)

	response, err := plugin.Allocate(context.Background(), &kubeletdevicepluginv1beta1.AllocateRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerAllocateRequest{
			{DevicesIds: []string{"GPU-aaa-0"}},
		},
	})
	require.NoError(t, err)
	require.Len(t, response.ContainerResponses, 1)
	containerResponse := response.ContainerResponses[0]
	// The environment keeps the limits too, for a HAMi-core that does not read the file early.
	require.Equal(t, "3000m", containerResponse.Envs["CUDA_DEVICE_MEMORY_LIMIT_0"])
	require.Equal(t, "50", containerResponse.Envs["CUDA_DEVICE_SM_LIMIT"])

	limitsMount := findMount(containerResponse.Mounts, overrideEnvContainerPath)
	require.NotNil(t, limitsMount)
	require.True(t, limitsMount.ReadOnly)
	cacheMount := findMount(containerResponse.Mounts, fmt.Sprintf("%s/vgpu", hostHookPath))
	require.NotNil(t, cacheMount)
	require.False(t, cacheMount.ReadOnly)
	// The file sits beside the read-write cache directory, never inside it.
	require.Equal(t, vgpucache.LimitsFilePath(cacheMount.HostPath), limitsMount.HostPath)

	content, err := os.ReadFile(limitsMount.HostPath)
	require.NoError(t, err)
	require.Equal(t, "CUDA_DEVICE_MEMORY_LIMIT_0=3000m\n"+
		"CUDA_DEVICE_MEMORY_SHARED_CACHE="+containerResponse.Envs["CUDA_DEVICE_MEMORY_SHARED_CACHE"]+"\n"+
		"CUDA_DEVICE_SM_LIMIT=50\n"+
		"CUDA_TASK_PRIORITY=1\n"+
		"HAMI_LIMITS_FORMAT=1\n"+
		"LIBCUDA_LOG_LEVEL=0\n", string(content))
}

// TestAllocate_FailsWhenLimitsFileCannotBeWritten checks that a container never starts with its
// limits silently missing.
func TestAllocate_FailsWhenLimitsFileCannotBeWritten(t *testing.T) {
	setupInRequestDevices(t)
	plugin := newTestPlugin(t)
	root := t.TempDir()
	plugin.prepareCache = func(podUID, containerName string) (string, error) {
		directory := filepath.Join(root, podUID+"_"+containerName)
		if err := os.Mkdir(directory, 0o777); err != nil {
			return "", err
		}
		// A directory where the file belongs makes the write fail.
		if err := os.Mkdir(vgpucache.LimitsFilePath(directory), 0o755); err != nil {
			return "", err
		}
		return directory, nil
	}
	pod := newLimitsTestPod()
	setupFakeClient(t, pod)
	mockAllocateGlobals(t, pod)

	response, err := plugin.Allocate(context.Background(), &kubeletdevicepluginv1beta1.AllocateRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerAllocateRequest{
			{DevicesIds: []string{"GPU-aaa-0"}},
		},
	})
	require.Nil(t, response)
	require.ErrorContains(t, err, "write GPU limits file")
}

// TestAllocate_MIGDeviceGetsNoLimitsFile checks that a MIG allocation, which carries no HAMi-core
// limits, gets no /overrideEnv mount.
func TestAllocate_MIGDeviceGetsNoLimitsFile(t *testing.T) {
	setupInRequestDevices(t)
	migDevice := &rm.Device{}
	migDevice.ID = "MIG-aaa-0"
	plugin := newTestPluginWithRM(t, map[string]*rm.Device{"MIG-aaa-0": migDevice})
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mig-pod", Namespace: "default", UID: "mig-uid",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-to-allocate": "MIG-aaa,NVIDIA,3000,50:;",
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "c0"}}},
	}
	setupFakeClient(t, pod)
	mockAllocateGlobals(t, pod)

	response, err := plugin.Allocate(context.Background(), &kubeletdevicepluginv1beta1.AllocateRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerAllocateRequest{
			{DevicesIds: []string{"MIG-aaa-0"}},
		},
	})
	require.NoError(t, err)
	require.Len(t, response.ContainerResponses, 1)
	require.Nil(t, findMount(response.ContainerResponses[0].Mounts, overrideEnvContainerPath))
}
