/*
 * Copyright (c) 2024, HAMi.  All rights reserved.
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
	"errors"
	"os"
	"testing"

	v1 "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/rm"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

// ---------------------------------------------------------------------------
// test helpers
// ---------------------------------------------------------------------------

func cd(uuid, typ string, usedmem, usedcores int32) device.ContainerDevice {
	return device.ContainerDevice{
		UUID:      uuid,
		Type:      typ,
		Usedmem:   usedmem,
		Usedcores: usedcores,
	}
}

func setupInRequestDevices(t *testing.T) {
	t.Helper()
	old := device.InRequestDevices[nvidia.NvidiaGPUDevice]
	device.InRequestDevices[nvidia.NvidiaGPUDevice] = "hami.io/vgpu-devices-to-allocate"
	t.Cleanup(func() {
		if old != "" {
			device.InRequestDevices[nvidia.NvidiaGPUDevice] = old
		} else {
			delete(device.InRequestDevices, nvidia.NvidiaGPUDevice)
		}
	})
}

func setupFakeClient(t *testing.T, objs ...runtime.Object) *fake.Clientset {
	t.Helper()
	orig := client.KubeClient
	fc := fake.NewSimpleClientset(objs...)
	client.KubeClient = fc
	t.Cleanup(func() { client.KubeClient = orig })
	return fc
}

func mustStrategies(t *testing.T, strategies ...string) v1.DeviceListStrategies {
	t.Helper()
	dls, err := v1.NewDeviceListStrategies(strategies)
	require.NoError(t, err)
	return dls
}

func newTestPlugin(t *testing.T) *NvidiaDevicePlugin {
	t.Helper()
	prevHookPath := os.Getenv("HOOK_PATH")
	prevHostHookPath := hostHookPath
	os.Setenv("HOOK_PATH", "/tmp/hami-test-hookpath")
	hostHookPath = "/tmp/hami-test-hookpath"
	t.Cleanup(func() {
		if prevHookPath != "" {
			os.Setenv("HOOK_PATH", prevHookPath)
		} else {
			os.Unsetenv("HOOK_PATH")
		}
		hostHookPath = prevHostHookPath
	})
	logLevel := nvidia.Error
	memScale := 1.0
	return &NvidiaDevicePlugin{
		operatingMode: "vgpu",
		config: &nvidia.DeviceConfig{
			Config: &v1.Config{
				Flags: v1.Flags{
					CommandLineFlags: v1.CommandLineFlags{
						Plugin: &v1.PluginCommandLineFlags{
							DeviceIDStrategy: ptr("UUID"),
						},
					},
				},
			},
		},
		deviceListStrategies: mustStrategies(t, "envvar"),
		schedulerConfig: nvidia.NvidiaConfig{
			NodeDefaultConfig: nvidia.NodeDefaultConfig{
				DeviceMemoryScaling: &memScale,
				LogLevel:            &logLevel,
			},
		},
	}
}

func hasLdSoPreloadMount(mounts []*kubeletdevicepluginv1beta1.Mount) bool {
	for _, m := range mounts {
		if m.ContainerPath == "/etc/ld.so.preload" {
			return true
		}
	}
	return false
}

func mockAllocateGlobals(t *testing.T, pod *corev1.Pod) {
	t.Helper()
	previousGetPendingPod := getPendingPod
	getPendingPod = func(context.Context, string) (*corev1.Pod, error) { return pod, nil }
	t.Cleanup(func() { getPendingPod = previousGetPendingPod })

	previousPodAllocationFailed := podAllocationFailed
	podAllocationFailed = func(string, *corev1.Pod, string) {}
	t.Cleanup(func() { podAllocationFailed = previousPodAllocationFailed })

	previousPodAllocationTrySuccess := podAllocationTrySuccess
	podAllocationTrySuccess = func(string, string, string, *corev1.Pod) {}
	t.Cleanup(func() { podAllocationTrySuccess = previousPodAllocationTrySuccess })
}

// ---------------------------------------------------------------------------
// popNextContainerDevices — unit tests
// ---------------------------------------------------------------------------

func TestPopNextContainerDevices(t *testing.T) {
	tests := []struct {
		name          string
		containers    []corev1.Container
		podSingleDev  device.PodSingleDevice
		wantName      string
		wantUUID      string
		wantErr       string
		wantRemaining int
	}{
		{
			name:         "EmptyPodSingleDevice",
			containers:   []corev1.Container{{Name: "c0"}},
			podSingleDev: device.PodSingleDevice{},
			wantErr:      "no pending device allocation found",
		},
		{
			name:         "AllContainersEmpty",
			containers:   []corev1.Container{{Name: "c0"}, {Name: "c1"}, {Name: "c2"}},
			podSingleDev: device.PodSingleDevice{{}, {}, {}},
			wantErr:      "no pending device allocation found",
		},
		{
			name:       "FirstContainerHasDevices",
			containers: []corev1.Container{{Name: "c0"}, {Name: "c1"}},
			podSingleDev: device.PodSingleDevice{
				{cd("uuid1", "NVIDIA", 1024, 4)},
				{cd("uuid2", "NVIDIA", 2048, 8)},
			},
			wantName:      "c0",
			wantUUID:      "uuid1",
			wantRemaining: 1,
		},
		{
			name:       "SecondContainerHasDevices",
			containers: []corev1.Container{{Name: "c0"}, {Name: "c1"}, {Name: "c2"}},
			podSingleDev: device.PodSingleDevice{
				{},
				{cd("uuid2", "NVIDIA", 2048, 8)},
				{cd("uuid3", "NVIDIA", 512, 2)},
			},
			wantName:      "c1",
			wantUUID:      "uuid2",
			wantRemaining: 1,
		},
		{
			name:       "MutationErasesFirstNonEmpty",
			containers: []corev1.Container{{Name: "c0"}, {Name: "c1"}},
			podSingleDev: device.PodSingleDevice{
				{cd("uuid1", "NVIDIA", 1024, 4)},
				{cd("uuid2", "NVIDIA", 2048, 8)},
			},
			wantName:      "c0",
			wantUUID:      "uuid1",
			wantRemaining: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{Containers: tc.containers},
			}
			ctr, got, err := popNextContainerDevices(pod, tc.podSingleDev)
			if tc.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantName, ctr.Name)
			require.NotEmpty(t, got)
			require.Equal(t, tc.wantUUID, got[0].UUID)
			// mutation: the popped container should now be empty — find it by UUID
			for i, d := range tc.podSingleDev {
				if len(d) > 0 && d[0].UUID == tc.wantUUID {
					t.Fatalf("popped container at index %d should be erased in place, found %d devices", i, len(d))
				}
			}
			if tc.wantRemaining > 0 {
				nonEmpty := 0
				for _, d := range tc.podSingleDev {
					nonEmpty += len(d)
				}
				require.Equal(t, tc.wantRemaining, nonEmpty)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// popNextContainerDevices_AfterDecode — integration test
// ---------------------------------------------------------------------------

func TestPopNextContainerDevices_AfterDecode(t *testing.T) {
	setupInRequestDevices(t)

	input := device.EncodePodSingleDevice(device.PodSingleDevice{
		{},
		{cd("uuid1", "NVIDIA", 1024, 4)},
		{cd("uuid2", "NVIDIA", 2048, 8)},
	})
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"hami.io/vgpu-devices-to-allocate": input,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "c0"},
				{Name: "c1"},
				{Name: "c2"},
			},
		},
	}

	podSingleDev, err := decodePodSingleDevice(nvidia.NvidiaGPUDevice, pod)
	require.NoError(t, err)

	// First pop → c1 / uuid1 (index 0 is empty, so first non-empty is index 1)
	ctr1, got1, err := popNextContainerDevices(pod, podSingleDev)
	require.NoError(t, err)
	require.Equal(t, "c1", ctr1.Name)
	require.Equal(t, "uuid1", got1[0].UUID)

	// Second pop → c2 / uuid2
	ctr2, got2, err := popNextContainerDevices(pod, podSingleDev)
	require.NoError(t, err)
	require.Equal(t, "c2", ctr2.Name)
	require.Equal(t, "uuid2", got2[0].UUID)

	// Third pop → error
	_, _, err = popNextContainerDevices(pod, podSingleDev)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// patchErasedAnnotation — unit tests
// ---------------------------------------------------------------------------

func TestPatchErasedAnnotation(t *testing.T) {
	setupInRequestDevices(t)

	toAllocAnno := "hami.io/vgpu-devices-to-allocate"
	input := device.EncodePodSingleDevice(device.PodSingleDevice{
		{cd("uuid1", "NVIDIA", 1024, 4)},
		{cd("uuid2", "NVIDIA", 2048, 8)},
	})
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Annotations: map[string]string{
				toAllocAnno: input,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "c0"}, {Name: "c1"}},
		},
	}
	setupFakeClient(t, pod)

	// Pop one container, then patch
	podSingleDev, err := decodePodSingleDevice(nvidia.NvidiaGPUDevice, pod)
	require.NoError(t, err)
	_, _, err = popNextContainerDevices(pod, podSingleDev)
	require.NoError(t, err)

	origValue := pod.Annotations[toAllocAnno]
	err = patchErasedAnnotation(pod, nvidia.NvidiaGPUDevice, podSingleDev)
	require.NoError(t, err)

	// In-memory annotation updated
	require.NotEqual(t, origValue, pod.Annotations[toAllocAnno],
		"pod.Annotations should have been updated in place")

	// API server also updated
	refreshed, err := client.KubeClient.CoreV1().Pods("default").Get(context.Background(), "test-pod", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, pod.Annotations[toAllocAnno], refreshed.Annotations[toAllocAnno],
		"API server annotation should match in-memory annotation")

	// Re-decode: only uuid2 remains
	remaining, err := decodePodSingleDevice(nvidia.NvidiaGPUDevice, pod)
	require.NoError(t, err)
	nonEmpty := 0
	for _, d := range remaining {
		if len(d) > 0 {
			nonEmpty++
		}
	}
	require.Equal(t, 1, nonEmpty, "one container should remain after pop")
}

// ---------------------------------------------------------------------------
// Allocate — end-to-end tests
// ---------------------------------------------------------------------------

func TestAllocate_MultiContainer_EachGetsOwnDevice(t *testing.T) {
	setupInRequestDevices(t)
	plugin := newTestPlugin(t)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       "pod-uid",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-to-allocate": "GPU-aaa,NVIDIA,3000,50:;GPU-bbb,NVIDIA,4000,60:;",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "c0"},
				{Name: "c1"},
			},
		},
	}
	setupFakeClient(t, pod)
	mockAllocateGlobals(t, pod)

	request := &kubeletdevicepluginv1beta1.AllocateRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerAllocateRequest{
			{DevicesIds: []string{"GPU-aaa-0"}},
			{DevicesIds: []string{"GPU-bbb-0"}},
		},
	}

	response, err := plugin.Allocate(context.Background(), request)
	require.NoError(t, err)
	require.Len(t, response.ContainerResponses, 2)

	// Each container gets its own memory limit and SM limit
	require.Equal(t, "3000m", response.ContainerResponses[0].Envs["CUDA_DEVICE_MEMORY_LIMIT_0"])
	require.Equal(t, "50", response.ContainerResponses[0].Envs["CUDA_DEVICE_SM_LIMIT"])
	require.Equal(t, "4000m", response.ContainerResponses[1].Envs["CUDA_DEVICE_MEMORY_LIMIT_0"])
	require.Equal(t, "60", response.ContainerResponses[1].Envs["CUDA_DEVICE_SM_LIMIT"])
}

func TestAllocate_MultiContainer_CUDA_DISABLE_CONTROL_SecondContainer(t *testing.T) {
	setupInRequestDevices(t)
	plugin := newTestPlugin(t)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       "pod-uid",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-to-allocate": "GPU-aaa,NVIDIA,3000,50:;GPU-bbb,NVIDIA,4000,60:;",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "c0"},
				{Name: "c1", Env: []corev1.EnvVar{
					{Name: "CUDA_DISABLE_CONTROL", Value: "true"},
				}},
			},
		},
	}
	setupFakeClient(t, pod)
	mockAllocateGlobals(t, pod)

	request := &kubeletdevicepluginv1beta1.AllocateRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerAllocateRequest{
			{DevicesIds: []string{"GPU-aaa-0"}},
			{DevicesIds: []string{"GPU-bbb-0"}},
		},
	}

	response, err := plugin.Allocate(context.Background(), request)
	require.NoError(t, err)
	require.Len(t, response.ContainerResponses, 2)

	require.True(t, hasLdSoPreloadMount(response.ContainerResponses[0].Mounts),
		"container 0 should have ld.so.preload mounted")
	require.False(t, hasLdSoPreloadMount(response.ContainerResponses[1].Mounts),
		"container 1 (CUDA_DISABLE_CONTROL=true) should NOT have ld.so.preload mounted")
}

func TestAllocate_MultiContainer_CUDA_DISABLE_CONTROL_FirstContainer(t *testing.T) {
	setupInRequestDevices(t)
	plugin := newTestPlugin(t)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       "pod-uid",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-to-allocate": "GPU-aaa,NVIDIA,3000,50:;GPU-bbb,NVIDIA,4000,60:;",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "c0", Env: []corev1.EnvVar{
					{Name: "CUDA_DISABLE_CONTROL", Value: "true"},
				}},
				{Name: "c1"},
			},
		},
	}
	setupFakeClient(t, pod)
	mockAllocateGlobals(t, pod)

	request := &kubeletdevicepluginv1beta1.AllocateRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerAllocateRequest{
			{DevicesIds: []string{"GPU-aaa-0"}},
			{DevicesIds: []string{"GPU-bbb-0"}},
		},
	}

	response, err := plugin.Allocate(context.Background(), request)
	require.NoError(t, err)
	require.Len(t, response.ContainerResponses, 2)

	require.False(t, hasLdSoPreloadMount(response.ContainerResponses[0].Mounts),
		"container 0 (CUDA_DISABLE_CONTROL=true) should NOT have ld.so.preload mounted")
	require.True(t, hasLdSoPreloadMount(response.ContainerResponses[1].Mounts),
		"container 1 should have ld.so.preload mounted")
}

func TestAllocate_DeviceNumberMismatch(t *testing.T) {
	setupInRequestDevices(t)
	plugin := newTestPlugin(t)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       "pod-uid",
			Annotations: map[string]string{
				// annotation says 1 device, but request asks for 2
				"hami.io/vgpu-devices-to-allocate": "GPU-aaa,NVIDIA,3000,50:;",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "c0"}},
		},
	}
	setupFakeClient(t, pod)
	mockAllocateGlobals(t, pod)

	request := &kubeletdevicepluginv1beta1.AllocateRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerAllocateRequest{
			{DevicesIds: []string{"GPU-aaa-0", "GPU-aaa-1"}},
		},
	}

	_, err := plugin.Allocate(context.Background(), request)
	require.Error(t, err)
	require.Contains(t, err.Error(), "device number not matched")
}

func TestAllocate_PatchesAnnotationOnlyOnce(t *testing.T) {
	setupInRequestDevices(t)
	plugin := newTestPlugin(t)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       "pod-uid",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-to-allocate": "GPU-aaa,NVIDIA,3000,50:;GPU-bbb,NVIDIA,4000,60:;",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "c0"},
				{Name: "c1"},
			},
		},
	}
	fc := setupFakeClient(t, pod)
	mockAllocateGlobals(t, pod)

	// Count Patch calls on the fake clientset
	patchCount := 0
	fc.PrependReactor("patch", "pods", func(action clienttesting.Action) (bool, runtime.Object, error) {
		patchCount++
		return false, nil, nil // fall through to default
	})

	request := &kubeletdevicepluginv1beta1.AllocateRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerAllocateRequest{
			{DevicesIds: []string{"GPU-aaa-0"}},
			{DevicesIds: []string{"GPU-bbb-0"}},
		},
	}

	_, err := plugin.Allocate(context.Background(), request)
	require.NoError(t, err)

	require.Equal(t, 1, patchCount,
		"Allocate should patch the API server exactly once for multi-container pods")
}

func TestAllocate_SingleContainer(t *testing.T) {
	setupInRequestDevices(t)
	plugin := newTestPlugin(t)

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
			Containers: []corev1.Container{{Name: "c0"}},
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
	require.Equal(t, "3000m", response.ContainerResponses[0].Envs["CUDA_DEVICE_MEMORY_LIMIT_0"])
	require.Equal(t, "50", response.ContainerResponses[0].Envs["CUDA_DEVICE_SM_LIMIT"])
	require.True(t, hasLdSoPreloadMount(response.ContainerResponses[0].Mounts))
}

// ---------------------------------------------------------------------------
// Allocate — MIG branch coverage
// ---------------------------------------------------------------------------

// newTestPluginWithRM returns a test plugin with a mocked ResourceManager
// so that MIG device lookups (plugin.rm.Devices().Contains) can be controlled.
func newTestPluginWithRM(t *testing.T, devices map[string]*rm.Device) *NvidiaDevicePlugin {
	t.Helper()
	plugin := newTestPlugin(t)
	plugin.rm = &rm.ResourceManagerMock{
		ResourceFunc: func() v1.ResourceName { return "nvidia.com/gpu" },
		DevicesFunc:  func() rm.Devices { return rm.Devices(devices) },
	}
	return plugin
}

func TestAllocate_MIG_Success(t *testing.T) {
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

	request := &kubeletdevicepluginv1beta1.AllocateRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerAllocateRequest{
			{DevicesIds: []string{"MIG-aaa-0"}},
		},
	}

	response, err := plugin.Allocate(context.Background(), request)
	require.NoError(t, err)
	require.Len(t, response.ContainerResponses, 1)
}

func TestAllocate_MIG_FailRequestsGreaterThanOne(t *testing.T) {
	setupInRequestDevices(t)

	// Both device IDs must be in the device map so we pass the Contains check.
	// The IDs contain "::" so AnyHasAnnotations() returns true (shared replicas).
	dev1 := &rm.Device{}
	dev1.ID = "MIG-gpu-uuid::0"
	dev2 := &rm.Device{}
	dev2.ID = "MIG-gpu-uuid::1"
	plugin := newTestPluginWithRM(t, map[string]*rm.Device{
		"MIG-gpu-uuid::0": dev1,
		"MIG-gpu-uuid::1": dev2,
	})
	// Enable FailRequestsGreaterThanOne — requesting >1 annotated shared device errors.
	plugin.config.Sharing.TimeSlicing.FailRequestsGreaterThanOne = true

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

	request := &kubeletdevicepluginv1beta1.AllocateRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerAllocateRequest{
			{DevicesIds: []string{"MIG-gpu-uuid::0", "MIG-gpu-uuid::1"}},
		},
	}

	_, err := plugin.Allocate(context.Background(), request)
	require.Error(t, err)
	require.Contains(t, err.Error(), "too large")
}

func TestAllocate_MIG_UnknownDevice(t *testing.T) {
	setupInRequestDevices(t)

	// Device map does NOT contain the requested ID
	plugin := newTestPluginWithRM(t, map[string]*rm.Device{})

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

	request := &kubeletdevicepluginv1beta1.AllocateRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerAllocateRequest{
			{DevicesIds: []string{"MIG-unknown-0"}},
		},
	}

	_, err := plugin.Allocate(context.Background(), request)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown device")
}

// ---------------------------------------------------------------------------
// Allocate — error path coverage
// ---------------------------------------------------------------------------

func TestAllocate_GetPendingPodError(t *testing.T) {
	setupInRequestDevices(t)
	plugin := newTestPlugin(t)

	// Override getPendingPod to return an error
	prevGetPendingPod := getPendingPod
	getPendingPod = func(context.Context, string) (*corev1.Pod, error) {
		return nil, errors.New("pod not found")
	}
	t.Cleanup(func() { getPendingPod = prevGetPendingPod })

	request := &kubeletdevicepluginv1beta1.AllocateRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerAllocateRequest{
			{DevicesIds: []string{"GPU-aaa-0"}},
		},
	}

	_, err := plugin.Allocate(context.Background(), request)
	require.Error(t, err)
	require.Contains(t, err.Error(), "pod not found")
}

func TestAllocate_DecodePodSingleDeviceError(t *testing.T) {
	setupInRequestDevices(t)
	plugin := newTestPlugin(t)

	// Annotation is empty → decodePodSingleDevice returns error
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "bad-pod", Namespace: "default", UID: "bad-uid",
			Annotations: map[string]string{},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "c0"}}},
	}
	setupFakeClient(t, pod)
	mockAllocateGlobals(t, pod)

	request := &kubeletdevicepluginv1beta1.AllocateRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerAllocateRequest{
			{DevicesIds: []string{"GPU-aaa-0"}},
		},
	}

	_, err := plugin.Allocate(context.Background(), request)
	require.Error(t, err)
	require.Contains(t, err.Error(), "device request not found")
}

func TestAllocate_PopNextContainerDevicesError_MoreContainersThanAnnotation(t *testing.T) {
	setupInRequestDevices(t)
	plugin := newTestPlugin(t)

	// Annotation has 1 container, but Allocate receives 2 ContainerRequests.
	// After popping the first container, the second pop finds an empty
	// (all-erased) podSingleDev → error.
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pop-err-pod", Namespace: "default", UID: "pop-err-uid",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-to-allocate": "GPU-aaa,NVIDIA,3000,50:;",
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "c0"}, {Name: "c1"}}},
	}
	setupFakeClient(t, pod)
	mockAllocateGlobals(t, pod)

	request := &kubeletdevicepluginv1beta1.AllocateRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerAllocateRequest{
			{DevicesIds: []string{"GPU-aaa-0"}},
			{DevicesIds: []string{"GPU-bbb-0"}},
		},
	}

	_, err := plugin.Allocate(context.Background(), request)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no pending device allocation found")
}

func TestAllocate_PatchErasedAnnotationError(t *testing.T) {
	setupInRequestDevices(t)
	plugin := newTestPlugin(t)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "patch-err-pod", Namespace: "default", UID: "patch-err-uid",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-to-allocate": "GPU-aaa,NVIDIA,3000,50:;",
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "c0"}}},
	}

	// Use a fake client that fails Patch on pods
	fc := setupFakeClient(t, pod)
	fc.PrependReactor("patch", "pods", func(action clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("patch not allowed")
	})

	mockAllocateGlobals(t, pod)

	request := &kubeletdevicepluginv1beta1.AllocateRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerAllocateRequest{
			{DevicesIds: []string{"GPU-aaa-0"}},
		},
	}

	_, err := plugin.Allocate(context.Background(), request)
	require.Error(t, err)
	require.Contains(t, err.Error(), "patch not allowed")
}

func exclusiveControlPod(env []corev1.EnvVar, annotation string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       "pod-uid",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-to-allocate": annotation,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "c0", Env: env}},
		},
	}
}

func allocateSingle(t *testing.T, pod *corev1.Pod) *kubeletdevicepluginv1beta1.ContainerAllocateResponse {
	t.Helper()
	setupInRequestDevices(t)
	plugin := newTestPlugin(t)
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

func TestAllocate_ExclusiveGPU_AutoDisablesControl(t *testing.T) {
	ctr := allocateSingle(t, exclusiveControlPod(nil, "GPU-aaa,NVIDIA,0,100:;"))
	require.Equal(t, "true", ctr.Envs["CUDA_DISABLE_CONTROL"])
	require.False(t, hasLdSoPreloadMount(ctr.Mounts))
}

func TestAllocate_ExclusiveGPU_ExplicitFalseKeepsControl(t *testing.T) {
	env := []corev1.EnvVar{{Name: "CUDA_DISABLE_CONTROL", Value: "false"}}
	ctr := allocateSingle(t, exclusiveControlPod(env, "GPU-aaa,NVIDIA,0,100:;"))
	_, set := ctr.Envs["CUDA_DISABLE_CONTROL"]
	require.False(t, set)
	require.True(t, hasLdSoPreloadMount(ctr.Mounts))
}

func TestAllocate_LimitlessSharedGPU_KeepsControl(t *testing.T) {
	ctr := allocateSingle(t, exclusiveControlPod(nil, "GPU-aaa,NVIDIA,0,0:;"))
	_, set := ctr.Envs["CUDA_DISABLE_CONTROL"]
	require.False(t, set)
	require.True(t, hasLdSoPreloadMount(ctr.Mounts))
}

func TestAllocate_ExclusiveCoresWithMemoryCap_KeepsControl(t *testing.T) {
	ctr := allocateSingle(t, exclusiveControlPod(nil, "GPU-aaa,NVIDIA,3000,100:;"))
	_, set := ctr.Envs["CUDA_DISABLE_CONTROL"]
	require.False(t, set)
	require.True(t, hasLdSoPreloadMount(ctr.Mounts))
}
