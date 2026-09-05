/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * The HAMi Contributors require contributions made to
 * this file be licensed under the Apache-2.0 license or a
 * compatible open source license.
 */

/*
 * Licensed to NVIDIA CORPORATION under one or more contributor
 * license agreements. See the NOTICE file distributed with
 * this work for additional information regarding copyright
 * ownership. NVIDIA CORPORATION licenses this file to you under
 * the Apache License, Version 2.0 (the "License"); you may
 * not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/*
 * Modifications Copyright The HAMi Authors. See
 * GitHub history for details.
 */

package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"

	v1 "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/cdi"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/hostpid"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/imex"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/rm"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

func ptr[T any](value T) *T {
	return &value
}

func TestCDIAllocateResponse(t *testing.T) {
	testCases := []struct {
		description          string
		deviceIds            []string
		deviceListStrategies []string
		CDIPrefix            string
		AdditionalCDIDevices []string
		GDSEnabled           bool
		MOFEDEnabled         bool
		imexChannels         []*imex.Channel
		expectedResponse     kubeletdevicepluginv1beta1.ContainerAllocateResponse
	}{
		{
			description:          "empty device list has empty response",
			deviceListStrategies: []string{"cdi-annotations"},
			CDIPrefix:            "cdi.k8s.io/",
		},
		{
			description:          "single device is added to annotations",
			deviceIds:            []string{"gpu0"},
			deviceListStrategies: []string{"cdi-annotations"},
			CDIPrefix:            "cdi.k8s.io/",
			expectedResponse: kubeletdevicepluginv1beta1.ContainerAllocateResponse{
				Annotations: map[string]string{
					"cdi.k8s.io/nvidia-device-plugin_uuid": "nvidia.com/gpu=gpu0",
				},
			},
		},
		{
			description:          "single device is added to annotations with custom prefix",
			deviceIds:            []string{"gpu0"},
			deviceListStrategies: []string{"cdi-annotations"},
			CDIPrefix:            "custom.cdi.k8s.io/",
			expectedResponse: kubeletdevicepluginv1beta1.ContainerAllocateResponse{
				Annotations: map[string]string{
					"custom.cdi.k8s.io/nvidia-device-plugin_uuid": "nvidia.com/gpu=gpu0",
				},
			},
		},
		{
			description:          "multiple devices are added to annotations",
			deviceIds:            []string{"gpu0", "gpu1"},
			deviceListStrategies: []string{"cdi-annotations"},
			CDIPrefix:            "cdi.k8s.io/",
			expectedResponse: kubeletdevicepluginv1beta1.ContainerAllocateResponse{
				Annotations: map[string]string{
					"cdi.k8s.io/nvidia-device-plugin_uuid": "nvidia.com/gpu=gpu0,nvidia.com/gpu=gpu1",
				},
			},
		},
		{
			description:          "multiple devices are added to annotations with custom prefix",
			deviceIds:            []string{"gpu0", "gpu1"},
			deviceListStrategies: []string{"cdi-annotations"},
			CDIPrefix:            "custom.cdi.k8s.io/",
			expectedResponse: kubeletdevicepluginv1beta1.ContainerAllocateResponse{
				Annotations: map[string]string{
					"custom.cdi.k8s.io/nvidia-device-plugin_uuid": "nvidia.com/gpu=gpu0,nvidia.com/gpu=gpu1",
				},
			},
		},
		{
			description:          "mofed devices are selected if configured",
			deviceListStrategies: []string{"cdi-annotations"},
			CDIPrefix:            "cdi.k8s.io/",
			AdditionalCDIDevices: []string{"nvidia.com/mofed=all"},
			expectedResponse: kubeletdevicepluginv1beta1.ContainerAllocateResponse{
				Annotations: map[string]string{
					"cdi.k8s.io/nvidia-device-plugin_uuid": "nvidia.com/mofed=all",
				},
			},
		},
		{
			description:          "gds devices are selected if configured",
			deviceListStrategies: []string{"cdi-annotations"},
			CDIPrefix:            "cdi.k8s.io/",
			AdditionalCDIDevices: []string{"nvidia.com/gds=all"},
			expectedResponse: kubeletdevicepluginv1beta1.ContainerAllocateResponse{
				Annotations: map[string]string{
					"cdi.k8s.io/nvidia-device-plugin_uuid": "nvidia.com/gds=all",
				},
			},
		},
		{
			description:          "gds and mofed devices are included with device ids",
			deviceIds:            []string{"gpu0"},
			deviceListStrategies: []string{"cdi-annotations"},
			CDIPrefix:            "cdi.k8s.io/",
			AdditionalCDIDevices: []string{"nvidia.com/gds=all", "nvidia.com/mofed=all"},
			expectedResponse: kubeletdevicepluginv1beta1.ContainerAllocateResponse{
				Annotations: map[string]string{
					"cdi.k8s.io/nvidia-device-plugin_uuid": "nvidia.com/gpu=gpu0,nvidia.com/gds=all,nvidia.com/mofed=all",
				},
			},
		},
		{
			description:          "imex channel is included with devices",
			deviceListStrategies: []string{"cdi-annotations"},
			CDIPrefix:            "cdi.k8s.io/",
			imexChannels:         []*imex.Channel{{ID: "0"}},
			expectedResponse: kubeletdevicepluginv1beta1.ContainerAllocateResponse{
				Annotations: map[string]string{
					"cdi.k8s.io/nvidia-device-plugin_uuid": "nvidia.com/imex-channel=0",
				},
			},
		},
	}

	for i := range testCases {
		tc := &testCases[i]
		t.Run(tc.description, func(t *testing.T) {
			deviceListStrategies, _ := v1.NewDeviceListStrategies(tc.deviceListStrategies)
			plugin := NvidiaDevicePlugin{
				config: &nvidia.DeviceConfig{
					Config: &v1.Config{
						Flags: v1.Flags{
							CommandLineFlags: v1.CommandLineFlags{
								GDSEnabled:   &tc.GDSEnabled,
								MOFEDEnabled: &tc.MOFEDEnabled,
							},
						},
					},
				},
				cdiHandler: &cdi.InterfaceMock{
					QualifiedNameFunc: func(c string, s string) string {
						return "nvidia.com/" + c + "=" + s
					},
					AdditionalDevicesFunc: func() []string {
						return tc.AdditionalCDIDevices
					},
				},
				deviceListStrategies: deviceListStrategies,
				cdiAnnotationPrefix:  tc.CDIPrefix,
				imexChannels:         tc.imexChannels,
			}

			response := kubeletdevicepluginv1beta1.ContainerAllocateResponse{}
			err := plugin.updateResponseForCDI(&response, "uuid", tc.deviceIds...)

			require.Nil(t, err)
			require.EqualValues(t, &tc.expectedResponse, &response)
		})
	}
}

// TestNewNvidiaDevicePluginPropagatesImexChannels guards the wiring from options
// into the plugin: WithImexChannels stores the channels on options, and the
// plugin the constructor builds must carry them, otherwise updateResponseForCDI,
// updateResponseForImexChannelsEnvVar, updateResponseForDeviceMounts, and
// apiDeviceSpecs all see an empty list and IMEX channels are never exposed to the
// container.
func TestNewNvidiaDevicePluginPropagatesImexChannels(t *testing.T) {
	channels := imex.Channels{{ID: "0"}, {ID: "1"}}
	o := &options{
		imexChannels: channels,
		config: &nvidia.DeviceConfig{
			Config: &v1.Config{
				Flags: v1.Flags{
					CommandLineFlags: v1.CommandLineFlags{
						Plugin: &v1.PluginCommandLineFlags{
							CDIAnnotationPrefix: ptr("cdi.k8s.io/"),
						},
					},
				},
			},
		},
	}
	resourceManager := &rm.ResourceManagerMock{
		ResourceFunc: func() v1.ResourceName { return "nvidia.com/gpu" },
	}
	deviceListStrategies, err := v1.NewDeviceListStrategies([]string{"envvar"})
	require.NoError(t, err)

	plugin := o.newNvidiaDevicePlugin(
		context.Background(),
		resourceManager,
		deviceListStrategies,
		nvidia.NvidiaConfig{},
		"hami-core",
		nil,
	)

	require.Equal(t, channels, plugin.imexChannels,
		"newNvidiaDevicePlugin must copy imexChannels from options into the plugin")
}

// TestUpdateResponseForImexChannelsEnvVarExposesChannels covers the container-facing
// half of the IMEX wiring: once the channels are on the plugin, the allocate response
// must expose them to the container through the IMEX channel env var. With
// TestNewNvidiaDevicePluginPropagatesImexChannels (options into the plugin) this pins
// the full path the #2892 regression broke.
func TestUpdateResponseForImexChannelsEnvVarExposesChannels(t *testing.T) {
	plugin := NvidiaDevicePlugin{
		imexChannels: imex.Channels{{ID: "0"}, {ID: "3"}},
	}

	response := kubeletdevicepluginv1beta1.ContainerAllocateResponse{
		Envs: map[string]string{},
	}
	plugin.updateResponseForImexChannelsEnvVar(&response)

	require.Equal(t, "0,3", response.Envs[v1.ImexChannelEnvVar],
		"updateResponseForImexChannelsEnvVar must expose the plugin's IMEX channels to the container")
}

func TestSelectPreferredDeviceIDsFromAnnotatedDevices(t *testing.T) {
	plugin := &NvidiaDevicePlugin{}
	// Use real NVIDIA GPU UUID format: GPU-xxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
	available := []string{
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67a-0", "GPU-03f69c50-207a-2038-9b45-23cac89cb67a-1",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67b-0",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67c-0",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67d-0", "GPU-03f69c50-207a-2038-9b45-23cac89cb67d-1",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67e-0",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67f-0",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb680-0",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb681-0",
	}
	required := []string{"GPU-03f69c50-207a-2038-9b45-23cac89cb67d-1"}
	desired := device.ContainerDevices{
		{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67a"},
		{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67b"},
		{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67c"},
		{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67d"},
		{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67e"},
		{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67f"},
		{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb680"},
		{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb681"},
	}

	got, err := plugin.selectPreferredDeviceIDsFromAnnotatedDevices(available, required, desired, len(desired))
	require.NoError(t, err)
	require.Len(t, got, len(desired))
	require.Contains(t, got, "GPU-03f69c50-207a-2038-9b45-23cac89cb67d-1")
	require.ElementsMatch(t, []string{
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67a-0",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67b-0",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67c-0",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67d-1",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67e-0",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67f-0",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb680-0",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb681-0",
	}, got)
}

func TestSelectPreferredDeviceIDsFromAnnotatedDevicesErrorsWhenAnnotatedUUIDMissing(t *testing.T) {
	plugin := &NvidiaDevicePlugin{}
	available := []string{
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67a-0",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67b-0",
	}
	desired := device.ContainerDevices{
		{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67a"},
		{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67c"}, // Missing from available
	}

	_, err := plugin.selectPreferredDeviceIDsFromAnnotatedDevices(available, nil, desired, len(desired))
	require.Error(t, err)
	require.Contains(t, err.Error(), "GPU-03f69c50-207a-2038-9b45-23cac89cb67c")
}

// TestGetDevicePluginOptionsHonorsEnableGetPreferredAllocation is a regression
// test for issue #2844: GetDevicePluginOptions must reflect the configured
// enableGetPreferredAllocation value, not unconditionally report true.
func TestGetDevicePluginOptionsHonorsEnableGetPreferredAllocation(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
	}{
		{name: "enabled=true reports true", enabled: true},
		{name: "enabled=false reports false", enabled: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Save and restore the package-level variable used by both
			// Register() and GetDevicePluginOptions().
			original := enableGetPreferredAllocation
			enableGetPreferredAllocation = tc.enabled
			defer func() { enableGetPreferredAllocation = original }()

			plugin := &NvidiaDevicePlugin{}
			options, err := plugin.GetDevicePluginOptions(context.Background(), &kubeletdevicepluginv1beta1.Empty{})
			require.NoError(t, err)
			require.Equal(t, tc.enabled, options.GetPreferredAllocationAvailable,
				"GetDevicePluginOptions must reflect enableGetPreferredAllocation=%v", tc.enabled)
		})
	}
}

func TestGetPreferredAllocationAlignsWithAnnotatedDevices(t *testing.T) {
	previousInRequestDevice := device.InRequestDevices[nvidia.NvidiaGPUDevice]
	device.InRequestDevices[nvidia.NvidiaGPUDevice] = "hami.io/vgpu-devices-to-allocate"
	defer func() {
		device.InRequestDevices[nvidia.NvidiaGPUDevice] = previousInRequestDevice
	}()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "test-pod",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-to-allocate": device.EncodePodSingleDevice(device.PodSingleDevice{
					{
						{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67a", Type: nvidia.NvidiaGPUDevice},
						{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67b", Type: nvidia.NvidiaGPUDevice},
						{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67c", Type: nvidia.NvidiaGPUDevice},
						{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67d", Type: nvidia.NvidiaGPUDevice},
						{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67e", Type: nvidia.NvidiaGPUDevice},
						{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67f", Type: nvidia.NvidiaGPUDevice},
						{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb680", Type: nvidia.NvidiaGPUDevice},
						{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb681", Type: nvidia.NvidiaGPUDevice},
					},
				}),
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "main"}},
		},
	}
	plugin := &NvidiaDevicePlugin{}
	t.Setenv(util.NodeNameEnvName, "node-a")
	previousGetPendingPod := getPendingPod
	getPendingPod = func(context.Context, string) (*corev1.Pod, error) {
		return pod, nil
	}
	defer func() {
		getPendingPod = previousGetPendingPod
	}()

	request := &kubeletdevicepluginv1beta1.PreferredAllocationRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerPreferredAllocationRequest{
			{
				AvailableDeviceIDs: []string{
					"GPU-03f69c50-207a-2038-9b45-23cac89cb67a-0", "GPU-03f69c50-207a-2038-9b45-23cac89cb67a-1",
					"GPU-03f69c50-207a-2038-9b45-23cac89cb67b-0",
					"GPU-03f69c50-207a-2038-9b45-23cac89cb67c-0",
					"GPU-03f69c50-207a-2038-9b45-23cac89cb67d-0", "GPU-03f69c50-207a-2038-9b45-23cac89cb67d-1",
					"GPU-03f69c50-207a-2038-9b45-23cac89cb67e-0",
					"GPU-03f69c50-207a-2038-9b45-23cac89cb67f-0",
					"GPU-03f69c50-207a-2038-9b45-23cac89cb680-0",
					"GPU-03f69c50-207a-2038-9b45-23cac89cb681-0",
				},
				MustIncludeDeviceIDs: []string{"GPU-03f69c50-207a-2038-9b45-23cac89cb67d-1"},
				AllocationSize:       8,
			},
		},
	}

	response, err := plugin.GetPreferredAllocation(context.Background(), request)
	require.NoError(t, err)
	require.Len(t, response.ContainerResponses, 1)
	require.ElementsMatch(t, []string{
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67a-0",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67b-0",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67c-0",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67d-1",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67e-0",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67f-0",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb680-0",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb681-0",
	}, response.ContainerResponses[0].DeviceIDs)
}

func Test_pathGeneration(t *testing.T) {
	hostHookPath := "/usr/local/vgpu"
	uid := "testuid"
	cname := "testcname"
	expected := "/usr/local/vgpu/containers/testuid_testcname"
	result := fmt.Sprintf("%s/containers/%s_%s", hostHookPath, uid, cname)

	if expected != result {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}

func Test_configOverride(t *testing.T) {
	t.Setenv("NODE_NAME", "testnode")
	logLevel1 := nvidia.Debugs
	logLevel2 := nvidia.Infos
	split1 := uint(2)
	memScale1 := 1.5
	coreScale1 := 1.2

	split2 := uint(3)
	memScale2 := 0.8
	coreScale2 := 1.4

	config := nvidia.DevicePluginConfigs{
		Nodeconfig: []struct {
			nvidia.NodeDefaultConfig     `json:",inline"`
			Name                         string               `json:"name"`
			OperatingMode                string               `json:"operatingmode"`
			Migstrategy                  string               `json:"migstrategy"`
			FilterDevice                 *nvidia.FilterDevice `json:"filterdevices"`
			EnableGetPreferredAllocation bool                 `json:"enablegetpreferredallocation"`
		}{
			{
				NodeDefaultConfig: nvidia.NodeDefaultConfig{
					DeviceSplitCount:    &split1,
					DeviceMemoryScaling: &memScale1,
					DeviceCoreScaling:   &coreScale1,
					LogLevel:            &logLevel1,
				},
				Name:                         "node-1",
				OperatingMode:                "default",
				Migstrategy:                  "single",
				FilterDevice:                 nil,
				EnableGetPreferredAllocation: true,
			},
			{
				NodeDefaultConfig: nvidia.NodeDefaultConfig{
					DeviceSplitCount:    &split2,
					DeviceMemoryScaling: &memScale2,
					DeviceCoreScaling:   &coreScale2,
					LogLevel:            &logLevel2,
				},
				Name:                         "testnode",
				OperatingMode:                "custom",
				Migstrategy:                  "mixed",
				FilterDevice:                 nil,
				EnableGetPreferredAllocation: true,
			},
		},
	}

	bytes, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		panic(err)
	}
	path := t.TempDir()
	os.WriteFile(path+"/config.json", bytes, 0644)
	nvconfig := nvidia.NvidiaConfig{
		NodeDefaultConfig: nvidia.NodeDefaultConfig{
			DeviceSplitCount:    func() *uint { v := uint(1); return &v }(),
			DeviceMemoryScaling: func() *float64 { v := 1.0; return &v }(),
			DeviceCoreScaling:   func() *float64 { v := 1.0; return &v }(),
			LogLevel:            func() *nvidia.LibCudaLogLevel { v := nvidia.Error; return &v }(),
		},
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
		ResourceCoreName:             "nvidia.com/gpucores",
		DefaultGPUNum:                int32(2),
	}
	_, err = readFromConfigFile(&nvconfig, path+"/config.json")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	expected := nvidia.NvidiaConfig{
		NodeDefaultConfig: nvidia.NodeDefaultConfig{
			DeviceSplitCount:    func() *uint { v := uint(3); return &v }(),
			DeviceMemoryScaling: func() *float64 { v := 0.8; return &v }(),
			DeviceCoreScaling:   func() *float64 { v := 1.4; return &v }(),
			LogLevel:            func() *nvidia.LibCudaLogLevel { v := nvidia.Infos; return &v }(),
		},
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
		ResourceCoreName:             "nvidia.com/gpucores",
		DefaultGPUNum:                int32(2),
	}
	if !reflect.DeepEqual(nvconfig, expected) {
		t.Errorf("Expected %v, got %v", expected, nvconfig)
	}
}

func TestGetPreferredAllocationSkipsEmptyAnnotations(t *testing.T) {
	previousInRequestDevice := device.InRequestDevices[nvidia.NvidiaGPUDevice]
	device.InRequestDevices[nvidia.NvidiaGPUDevice] = "hami.io/vgpu-devices-to-allocate"
	defer func() {
		device.InRequestDevices[nvidia.NvidiaGPUDevice] = previousInRequestDevice
	}()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "test-pod",
			Annotations: map[string]string{
				// Annotation includes init container (empty) + regular container (with GPU)
				"hami.io/vgpu-devices-to-allocate": device.EncodePodSingleDevice(device.PodSingleDevice{
					{}, // init container - empty
					{ // regular container - 2 GPUs
						{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67a", Type: nvidia.NvidiaGPUDevice},
						{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67b", Type: nvidia.NvidiaGPUDevice},
					},
				}),
			},
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{Name: "init"}},
			Containers:     []corev1.Container{{Name: "main"}},
		},
	}

	plugin := &NvidiaDevicePlugin{}
	t.Setenv(util.NodeNameEnvName, "node-a")
	previousGetPendingPod := getPendingPod
	getPendingPod = func(context.Context, string) (*corev1.Pod, error) {
		return pod, nil
	}
	defer func() {
		getPendingPod = previousGetPendingPod
	}()

	// Kubelet only sends one request (for the main container), not two
	request := &kubeletdevicepluginv1beta1.PreferredAllocationRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerPreferredAllocationRequest{
			{
				AvailableDeviceIDs: []string{"GPU-03f69c50-207a-2038-9b45-23cac89cb67a-0", "GPU-03f69c50-207a-2038-9b45-23cac89cb67a-1", "GPU-03f69c50-207a-2038-9b45-23cac89cb67b-0", "GPU-03f69c50-207a-2038-9b45-23cac89cb67b-1"},
				AllocationSize:     2,
			},
		},
	}

	response, err := plugin.GetPreferredAllocation(context.Background(), request)
	require.NoError(t, err)
	require.Len(t, response.ContainerResponses, 1)
	// Should match GPU-a and GPU-b, not fail due to empty init container annotation
	require.ElementsMatch(t, []string{"GPU-03f69c50-207a-2038-9b45-23cac89cb67a-0", "GPU-03f69c50-207a-2038-9b45-23cac89cb67b-0"}, response.ContainerResponses[0].DeviceIDs)
}

func TestPhysicalDeviceIDHandlesVirtualFormats(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Virtual device format (6 dashes)
		{"GPU-03f69c50-207a-2038-9b45-23cac89cb67a-0", "GPU-03f69c50-207a-2038-9b45-23cac89cb67a"},
		{"GPU-03f69c50-207a-2038-9b45-23cac89cb67a-10", "GPU-03f69c50-207a-2038-9b45-23cac89cb67a"},
		{"GPU-03f69c50-207a-2038-9b45-23cac89cb67a::replica-1", "GPU-03f69c50-207a-2038-9b45-23cac89cb67a"},
		// Plain UUID (5 dashes, should not be modified)
		{"GPU-03f69c50-207a-2038-9b45-23cac89cb67a", "GPU-03f69c50-207a-2038-9b45-23cac89cb67a"},
		// UUID ending with -123 (5 dashes total, should NOT be treated as virtual device)
		{"GPU-03f69c50-207a-2038-9b45-23cac89cb123", "GPU-03f69c50-207a-2038-9b45-23cac89cb123"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := physicalDeviceID(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestSelectPreferredDeviceIDsWithPhysicalMIGReservations(t *testing.T) {
	plugin := &NvidiaDevicePlugin{}
	available := []string{
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67a-0", "GPU-03f69c50-207a-2038-9b45-23cac89cb67a-1",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67b-0",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67c-0",
	}
	desired := device.ContainerDevices{
		{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67a"},
		{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67b"},
		{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67c"},
	}

	got, err := plugin.selectPreferredDeviceIDsFromAnnotatedDevices(available, nil, desired, 3)
	require.NoError(t, err)
	require.Len(t, got, 3)
	// Should select one slice from each physical GPU
	require.Contains(t, got, "GPU-03f69c50-207a-2038-9b45-23cac89cb67a-0")
	require.Contains(t, got, "GPU-03f69c50-207a-2038-9b45-23cac89cb67b-0")
	require.Contains(t, got, "GPU-03f69c50-207a-2038-9b45-23cac89cb67c-0")
}

func TestGetPreferredAllocationFallbackOnAnnotatedDeviceMappingFailure(t *testing.T) {
	previousInRequestDevice := device.InRequestDevices[nvidia.NvidiaGPUDevice]
	device.InRequestDevices[nvidia.NvidiaGPUDevice] = "hami.io/vgpu-devices-to-allocate"
	defer func() {
		device.InRequestDevices[nvidia.NvidiaGPUDevice] = previousInRequestDevice
	}()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "test-pod",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-to-allocate": device.EncodePodSingleDevice(device.PodSingleDevice{
					{
						{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67a", Type: nvidia.NvidiaGPUDevice},
						{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67z", Type: nvidia.NvidiaGPUDevice},
					},
				}),
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "main"}},
		},
	}

	rmCallCount := 0
	mockRM := &rm.ResourceManagerMock{
		GetPreferredAllocationFunc: func(available []string, required []string, size int) ([]string, error) {
			rmCallCount++
			return []string{"GPU-03f69c50-207a-2038-9b45-23cac89cb67a-0", "GPU-03f69c50-207a-2038-9b45-23cac89cb67b-0"}, nil
		},
	}

	plugin := &NvidiaDevicePlugin{
		rm: mockRM,
	}
	t.Setenv(util.NodeNameEnvName, "node-a")
	previousGetPendingPod := getPendingPod
	getPendingPod = func(context.Context, string) (*corev1.Pod, error) {
		return pod, nil
	}
	defer func() {
		getPendingPod = previousGetPendingPod
	}()

	request := &kubeletdevicepluginv1beta1.PreferredAllocationRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerPreferredAllocationRequest{
			{
				AvailableDeviceIDs: []string{
					"GPU-03f69c50-207a-2038-9b45-23cac89cb67a-0",
					"GPU-03f69c50-207a-2038-9b45-23cac89cb67b-0",
				},
				AllocationSize: 2,
			},
		},
	}

	response, err := plugin.GetPreferredAllocation(context.Background(), request)
	require.NoError(t, err)
	require.Len(t, response.ContainerResponses, 0)
}

func TestGetPreferredAllocationFallbackOnInsufficientAnnotatedDevices(t *testing.T) {
	previousInRequestDevice := device.InRequestDevices[nvidia.NvidiaGPUDevice]
	device.InRequestDevices[nvidia.NvidiaGPUDevice] = "hami.io/vgpu-devices-to-allocate"
	defer func() {
		device.InRequestDevices[nvidia.NvidiaGPUDevice] = previousInRequestDevice
	}()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "test-pod",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-to-allocate": device.EncodePodSingleDevice(device.PodSingleDevice{
					{
						{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67a", Type: nvidia.NvidiaGPUDevice},
					},
				}),
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "main"}},
		},
	}

	rmCallCount := 0
	mockRM := &rm.ResourceManagerMock{
		GetPreferredAllocationFunc: func(available []string, required []string, size int) ([]string, error) {
			rmCallCount++
			return []string{"GPU-03f69c50-207a-2038-9b45-23cac89cb67a-0", "GPU-03f69c50-207a-2038-9b45-23cac89cb67b-0"}, nil
		},
	}

	plugin := &NvidiaDevicePlugin{
		rm: mockRM,
	}
	t.Setenv(util.NodeNameEnvName, "node-a")
	previousGetPendingPod := getPendingPod
	getPendingPod = func(context.Context, string) (*corev1.Pod, error) {
		return pod, nil
	}
	defer func() {
		getPendingPod = previousGetPendingPod
	}()

	request := &kubeletdevicepluginv1beta1.PreferredAllocationRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerPreferredAllocationRequest{
			{
				AvailableDeviceIDs: []string{
					"GPU-03f69c50-207a-2038-9b45-23cac89cb67a-0",
					"GPU-03f69c50-207a-2038-9b45-23cac89cb67b-0",
				},
				AllocationSize: 2,
			},
		},
	}

	response, err := plugin.GetPreferredAllocation(context.Background(), request)
	require.NoError(t, err)
	require.Len(t, response.ContainerResponses, 0)
}

func TestAlignContainerDevicesWithAllocatedIDsPreservesMetadata(t *testing.T) {
	plugin := &NvidiaDevicePlugin{}
	devreq := device.ContainerDevices{
		{UUID: "GPU-annotated-a", Type: nvidia.NvidiaGPUDevice, Usedmem: 3000, Usedcores: 50},
		{UUID: "GPU-annotated-b", Type: nvidia.NvidiaGPUDevice, Usedmem: 3000, Usedcores: 50},
	}

	aligned, err := plugin.alignContainerDevicesWithAllocatedIDs(devreq, []string{
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67a-0",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67b-1",
	})
	require.NoError(t, err)
	require.Equal(t, int32(3000), aligned[0].Usedmem)
	require.Equal(t, int32(50), aligned[0].Usedcores)
	require.Equal(t, "GPU-03f69c50-207a-2038-9b45-23cac89cb67a", aligned[0].UUID)
	require.Equal(t, "GPU-03f69c50-207a-2038-9b45-23cac89cb67b", aligned[1].UUID)
}

func TestAlignContainerDevicesWithAllocatedIDsRejectsLengthMismatch(t *testing.T) {
	plugin := &NvidiaDevicePlugin{}
	devreq := device.ContainerDevices{
		{UUID: "GPU-annotated-a", Type: nvidia.NvidiaGPUDevice, Usedmem: 3000, Usedcores: 50},
	}

	_, err := plugin.alignContainerDevicesWithAllocatedIDs(devreq, []string{
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67a-0",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67b-1",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "device number not matched")
}

func TestAllocateUsesSelectedUUIDsAndHostPIDBroker(t *testing.T) {
	t.Setenv(hostpid.EnvironmentVariable, "1")
	prepareCalls := 0
	previousPrepareHostPIDLockParent := prepareHostPIDLockParentForAllocation
	prepareHostPIDLockParentForAllocation = func() error {
		prepareCalls++
		return nil
	}
	defer func() {
		prepareHostPIDLockParentForAllocation =
			previousPrepareHostPIDLockParent
	}()
	previousEnableGetPreferredAllocation := enableGetPreferredAllocation
	enableGetPreferredAllocation = true
	defer func() {
		enableGetPreferredAllocation = previousEnableGetPreferredAllocation
	}()
	deviceListStrategies, _ := v1.NewDeviceListStrategies([]string{"envvar"})
	deviceIDStrategy := v1.DeviceIDStrategyUUID
	memScale := 1.0
	logLevel := nvidia.Error

	plugin := &NvidiaDevicePlugin{
		config: &nvidia.DeviceConfig{
			Config: &v1.Config{
				Flags: v1.Flags{
					CommandLineFlags: v1.CommandLineFlags{
						Plugin: &v1.PluginCommandLineFlags{
							DeviceIDStrategy: &deviceIDStrategy,
						},
					},
				},
			},
		},
		deviceListStrategies: deviceListStrategies,
		schedulerConfig: nvidia.NvidiaConfig{
			NodeDefaultConfig: nvidia.NodeDefaultConfig{
				DeviceMemoryScaling: &memScale,
				LogLevel:            &logLevel,
			},
		},
	}

	previousInRequestDevice := device.InRequestDevices[nvidia.NvidiaGPUDevice]
	device.InRequestDevices[nvidia.NvidiaGPUDevice] = "hami.io/vgpu-devices-to-allocate"
	defer func() { device.InRequestDevices[nvidia.NvidiaGPUDevice] = previousInRequestDevice }()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       "pod-uid",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-to-allocate": "GPU-annotated-a,NVIDIA,3000,50:;",
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
	}

	previousGetPendingPod := getPendingPod
	getPendingPod = func(context.Context, string) (*corev1.Pod, error) { return pod, nil }
	defer func() { getPendingPod = previousGetPendingPod }()

	previousPodAllocationFailed := podAllocationFailed
	podAllocationFailed = func(string, *corev1.Pod, string) {}
	defer func() { podAllocationFailed = previousPodAllocationFailed }()

	previousPodAllocationTrySuccess := podAllocationTrySuccess
	podAllocationTrySuccess = func(string, string, string, *corev1.Pod) {}
	defer func() { podAllocationTrySuccess = previousPodAllocationTrySuccess }()

	// Provide a fake K8s client so the real patchErasedAnnotation can patch
	previousKubeClient := client.KubeClient
	client.KubeClient = fake.NewSimpleClientset(pod)
	defer func() { client.KubeClient = previousKubeClient }()

	request := &kubeletdevicepluginv1beta1.AllocateRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerAllocateRequest{{
			DevicesIds: []string{"GPU-03f69c50-207a-2038-9b45-23cac89cb67a-0"},
		}},
	}

	response, err := plugin.Allocate(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, 1, prepareCalls)
	require.Equal(t, "GPU-03f69c50-207a-2038-9b45-23cac89cb67a", response.ContainerResponses[0].Envs[deviceListEnvVar])
	require.Equal(t, "3000m", response.ContainerResponses[0].Envs["CUDA_DEVICE_MEMORY_LIMIT_0"])
	require.Equal(t, "50", response.ContainerResponses[0].Envs["CUDA_DEVICE_SM_LIMIT"])
	require.Equal(t, "1", response.ContainerResponses[0].Envs[hostpid.EnvironmentVariable])
	brokerMountCount := 0
	brokerMountIndex := -1
	fallbackParentMountCount := 0
	fallbackParentMountIndex := -1
	for mountIndex, mount := range response.ContainerResponses[0].Mounts {
		if mount.ContainerPath == hostpid.ContainerDirectory {
			require.Equal(t, hostpid.ServerDirectory, mount.HostPath)
			require.True(t, mount.ReadOnly)
			brokerMountIndex = mountIndex
			brokerMountCount++
		}
		if mount.ContainerPath == hostPIDLockParentDirectory {
			require.Equal(t, hostPIDLockParentDirectory, mount.HostPath)
			require.False(t, mount.ReadOnly)
			fallbackParentMountIndex = mountIndex
			fallbackParentMountCount++
		}
	}
	require.Equal(t, 1, brokerMountCount)
	require.Equal(t, 1, fallbackParentMountCount)
	require.Less(t, fallbackParentMountIndex, brokerMountIndex)

	t.Setenv(hostpid.EnvironmentVariable, "")
	pod.Annotations["hami.io/vgpu-devices-to-allocate"] =
		"GPU-annotated-a,NVIDIA,3000,50:;"
	client.KubeClient = fake.NewSimpleClientset(pod)
	disabledResponse, err := plugin.Allocate(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, 2, prepareCalls)
	require.NotContains(t, disabledResponse.ContainerResponses[0].Envs,
		hostpid.EnvironmentVariable)
	fallbackMountCount := 0
	for _, mount := range disabledResponse.ContainerResponses[0].Mounts {
		require.NotEqual(t, hostpid.ContainerDirectory, mount.ContainerPath)
		if mount.ContainerPath == hostPIDLockParentDirectory {
			require.Equal(t, hostPIDLockParentDirectory, mount.HostPath)
			require.False(t, mount.ReadOnly)
			fallbackMountCount++
		}
	}
	require.Equal(t, 1, fallbackMountCount)

	prepareHostPIDLockParentForAllocation = func() error {
		return errors.New("parent preparation fixture")
	}
	pod.Annotations["hami.io/vgpu-devices-to-allocate"] =
		"GPU-annotated-a,NVIDIA,3000,50:;"
	client.KubeClient = fake.NewSimpleClientset(pod)
	failedResponse, err := plugin.Allocate(context.Background(), request)
	require.Nil(t, failedResponse)
	require.ErrorContains(t, err, "failed to prepare host PID lock parent")
}

func TestAllocatePreservesContainerOrderWhenOneContainerFallsBack(t *testing.T) {
	deviceListStrategies, _ := v1.NewDeviceListStrategies([]string{"envvar"})
	deviceIDStrategy := v1.DeviceIDStrategyUUID
	memScale := 1.0
	logLevel := nvidia.Error

	plugin := &NvidiaDevicePlugin{
		config: &nvidia.DeviceConfig{
			Config: &v1.Config{
				Flags: v1.Flags{
					CommandLineFlags: v1.CommandLineFlags{
						Plugin: &v1.PluginCommandLineFlags{
							DeviceIDStrategy: &deviceIDStrategy,
						},
					},
				},
			},
		},
		deviceListStrategies: deviceListStrategies,
		schedulerConfig: nvidia.NvidiaConfig{
			NodeDefaultConfig: nvidia.NodeDefaultConfig{
				DeviceMemoryScaling: &memScale,
				LogLevel:            &logLevel,
			},
		},
	}

	previousInRequestDevice := device.InRequestDevices[nvidia.NvidiaGPUDevice]
	device.InRequestDevices[nvidia.NvidiaGPUDevice] = "hami.io/vgpu-devices-to-allocate"
	defer func() { device.InRequestDevices[nvidia.NvidiaGPUDevice] = previousInRequestDevice }()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       "pod-uid",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-to-allocate": "GPU-annotated-a,NVIDIA,3000,50:;GPU-annotated-b,NVIDIA,4000,60:;",
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "c0"}, {Name: "c1"}}},
	}
	fakeClient := fake.NewSimpleClientset(pod.DeepCopy())
	previousKubeClient := client.KubeClient
	client.KubeClient = fakeClient
	defer func() { client.KubeClient = previousKubeClient }()

	previousGetPendingPod := getPendingPod
	getPendingPod = func(context.Context, string) (*corev1.Pod, error) { return pod, nil }
	defer func() { getPendingPod = previousGetPendingPod }()

	previousPodAllocationFailed := podAllocationFailed
	podAllocationFailed = func(string, *corev1.Pod, string) {}
	defer func() { podAllocationFailed = previousPodAllocationFailed }()

	previousPodAllocationTrySuccess := podAllocationTrySuccess
	podAllocationTrySuccess = func(string, string, string, *corev1.Pod) {}
	defer func() { podAllocationTrySuccess = previousPodAllocationTrySuccess }()

	request := &kubeletdevicepluginv1beta1.AllocateRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerAllocateRequest{
			{DevicesIds: []string{"GPU-03f69c50-207a-2038-9b45-23cac89cb67a-0"}},
			{DevicesIds: []string{"GPU-03f69c50-207a-2038-9b45-23cac89cb67b-1"}},
		},
	}

	response, err := plugin.Allocate(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, "GPU-03f69c50-207a-2038-9b45-23cac89cb67a", response.ContainerResponses[0].Envs[deviceListEnvVar])
	require.Equal(t, "GPU-03f69c50-207a-2038-9b45-23cac89cb67b", response.ContainerResponses[1].Envs[deviceListEnvVar])
	require.Equal(t, "3000m", response.ContainerResponses[0].Envs["CUDA_DEVICE_MEMORY_LIMIT_0"])
	require.Equal(t, "4000m", response.ContainerResponses[1].Envs["CUDA_DEVICE_MEMORY_LIMIT_0"])
}

type mockListAndWatchServer struct {
	grpc.ServerStream
	sendErrs []error
	sent     []*kubeletdevicepluginv1beta1.ListAndWatchResponse
}

func (m *mockListAndWatchServer) Send(response *kubeletdevicepluginv1beta1.ListAndWatchResponse) error {
	m.sent = append(m.sent, response)
	if len(m.sendErrs) == 0 {
		return nil
	}
	err := m.sendErrs[0]
	m.sendErrs = m.sendErrs[1:]
	return err
}

func TestListAndWatchSendError(t *testing.T) {
	mockRM := &rm.ResourceManagerMock{
		DevicesFunc:  func() rm.Devices { return rm.Devices{} },
		ResourceFunc: func() v1.ResourceName { return v1.ResourceName("nvidia.com/gpu") },
	}

	t.Run("initial send fails", func(t *testing.T) {
		expectedErr := fmt.Errorf("initial send failed")
		server := &mockListAndWatchServer{sendErrs: []error{expectedErr}}
		plugin := &NvidiaDevicePlugin{
			rm: mockRM, stop: make(chan any), health: make(chan *rm.Device, 1),
			schedulerConfig: nvidia.NvidiaConfig{NodeDefaultConfig: nvidia.NodeDefaultConfig{DeviceSplitCount: ptr[uint](1)}},
		}
		err := plugin.ListAndWatch(&kubeletdevicepluginv1beta1.Empty{}, server)
		require.ErrorIs(t, err, expectedErr)
		require.Len(t, server.sent, 1)
	})

	t.Run("update send fails", func(t *testing.T) {
		expectedErr := fmt.Errorf("update send failed")
		server := &mockListAndWatchServer{sendErrs: []error{nil, expectedErr}}
		plugin := &NvidiaDevicePlugin{
			rm: mockRM, stop: make(chan any), health: make(chan *rm.Device, 1),
			schedulerConfig: nvidia.NvidiaConfig{NodeDefaultConfig: nvidia.NodeDefaultConfig{DeviceSplitCount: ptr[uint](1)}},
		}
		plugin.health <- &rm.Device{Device: kubeletdevicepluginv1beta1.Device{ID: "gpu-1"}}
		err := plugin.ListAndWatch(&kubeletdevicepluginv1beta1.Empty{}, server)
		require.NoError(t, err)
		require.Len(t, server.sent, 2)
	})
}

// TestAllocateRejectsEmptyDeviceIDs guards against a regression of the
// index-out-of-range panic in Allocate when kubelet sends a container
// request with an empty DevicesIds list (upstream k8s-device-plugin has
// the same guard).
func TestAllocateRejectsEmptyDeviceIDs(t *testing.T) {
	plugin := &NvidiaDevicePlugin{
		config: &nvidia.DeviceConfig{
			Config: &v1.Config{
				Flags: v1.Flags{
					CommandLineFlags: v1.CommandLineFlags{
						Plugin: &v1.PluginCommandLineFlags{
							DeviceIDStrategy: ptr(v1.DeviceIDStrategyUUID),
						},
					},
				},
			},
		},
		deviceListStrategies: func() v1.DeviceListStrategies {
			s, _ := v1.NewDeviceListStrategies([]string{"envvar"})
			return s
		}(),
	}

	previousInRequestDevice := device.InRequestDevices[nvidia.NvidiaGPUDevice]
	device.InRequestDevices[nvidia.NvidiaGPUDevice] = "hami.io/vgpu-devices-to-allocate"
	defer func() { device.InRequestDevices[nvidia.NvidiaGPUDevice] = previousInRequestDevice }()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       "pod-uid",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-to-allocate": "GPU-annotated-a,NVIDIA,3000,50:;",
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
	}

	previousGetPendingPod := getPendingPod
	getPendingPod = func(context.Context, string) (*corev1.Pod, error) { return pod, nil }
	defer func() { getPendingPod = previousGetPendingPod }()

	previousPodAllocationFailed := podAllocationFailed
	podAllocationFailed = func(string, *corev1.Pod, string) {}
	defer func() { podAllocationFailed = previousPodAllocationFailed }()

	request := &kubeletdevicepluginv1beta1.AllocateRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerAllocateRequest{{
			DevicesIds: []string{},
		}},
	}

	response, err := plugin.Allocate(context.Background(), request)
	require.Nil(t, response)
	require.ErrorContains(t, err, "invalid allocation request with no devices requested")
}
