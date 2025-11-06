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
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	v1 "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/cdi"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/stretchr/testify/require"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

func TestCDIAllocateResponse(t *testing.T) {
	testCases := []struct {
		description          string
		deviceIds            []string
		deviceListStrategies []string
		CDIPrefix            string
		CDIEnabled           bool
		GDSEnabled           bool
		MOFEDEnabled         bool
		expectedResponse     kubeletdevicepluginv1beta1.ContainerAllocateResponse
	}{
		{
			description:          "empty device list has empty response",
			deviceListStrategies: []string{"cdi-annotations"},
			CDIPrefix:            "cdi.k8s.io/",
			CDIEnabled:           true,
		},
		{
			description:          "CDI disabled has empty response",
			deviceIds:            []string{"gpu0"},
			deviceListStrategies: []string{"cdi-annotations"},
			CDIPrefix:            "cdi.k8s.io/",
			CDIEnabled:           false,
		},
		{
			description:          "single device is added to annotations",
			deviceIds:            []string{"gpu0"},
			deviceListStrategies: []string{"cdi-annotations"},
			CDIPrefix:            "cdi.k8s.io/",
			CDIEnabled:           true,
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
			CDIEnabled:           true,
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
			CDIEnabled:           true,
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
			CDIEnabled:           true,
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
			CDIEnabled:           true,
			MOFEDEnabled:         true,
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
			CDIEnabled:           true,
			GDSEnabled:           true,
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
			CDIEnabled:           true,
			GDSEnabled:           true,
			MOFEDEnabled:         true,
			expectedResponse: kubeletdevicepluginv1beta1.ContainerAllocateResponse{
				Annotations: map[string]string{
					"cdi.k8s.io/nvidia-device-plugin_uuid": "nvidia.com/gpu=gpu0,nvidia.com/gds=all,nvidia.com/mofed=all",
				},
			},
		},
	}

	for _, tc := range testCases {
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
				},
				cdiEnabled:           tc.CDIEnabled,
				deviceListStrategies: deviceListStrategies,
				cdiAnnotationPrefix:  tc.CDIPrefix,
			}

			response, err := plugin.getAllocateResponseForCDI("uuid", tc.deviceIds)

			require.Nil(t, err)
			require.EqualValues(t, &tc.expectedResponse, &response)
		})
	}
}

type MigDeviceConfigs struct {
	Configs []map[string]int32
}

func Test_processMigConfigs(t *testing.T) {
	type testCase struct {
		name        string
		migConfigs  map[string]nvidia.MigConfigSpecSlice
		deviceCount int
		expectError bool
		validate    func(t *testing.T, result nvidia.MigConfigSpecSlice)
	}

	testConfigs := MigDeviceConfigs{
		Configs: []map[string]int32{
			{
				"1g.10gb": 4,
				"2g.20gb": 1,
			},
			{
				"3g.30gb": 2,
			},
			{},
		},
	}

	testCases := []testCase{
		{
			name: "SingleConfigForAllDevices",
			migConfigs: map[string]nvidia.MigConfigSpecSlice{
				"current": {
					nvidia.MigConfigSpec{
						Devices:    []int32{},
						MigEnabled: true,
						MigDevices: testConfigs.Configs[1],
					},
				},
			},
			deviceCount: 3,
			expectError: false,
			validate: func(t *testing.T, result nvidia.MigConfigSpecSlice) {
				if len(result) != 3 {
					t.Errorf("Expected 3 configs, got %d", len(result))
				}
				for i, config := range result {
					if len(config.Devices) != 1 || config.Devices[0] != int32(i) {
						t.Errorf("Config for device %d is incorrect: %v", i, config)
					}
					if !config.MigEnabled {
						t.Error("MigEnabled should be true")
					}
					if len(config.MigDevices) != 1 || config.MigDevices["3g.30gb"] != 2 {
						t.Error("MigDevices not preserved correctly")
					}
				}
			},
		},
		{
			name: "MultipleConfigsForSpecificDevicesWithNoEnabled",
			migConfigs: map[string]nvidia.MigConfigSpecSlice{
				"current": {
					nvidia.MigConfigSpec{
						Devices:    []int32{0, 1},
						MigEnabled: true,
						MigDevices: testConfigs.Configs[0],
					},
					nvidia.MigConfigSpec{
						Devices:    []int32{2},
						MigEnabled: false,
						MigDevices: testConfigs.Configs[1],
					},
				},
			},
			deviceCount: 3,
			expectError: false,
			validate: func(t *testing.T, result nvidia.MigConfigSpecSlice) {
				if len(result) != 3 {
					t.Errorf("Expected 3 configs, got %d", len(result))
				}
				for i := 0; i < 2; i++ {
					if len(result[i].Devices) != 1 || result[i].Devices[0] != int32(i) {
						t.Errorf("Config for device %d is incorrect: %v", i, result[i])
					}
					if !result[i].MigEnabled {
						t.Error("MigEnabled should be true for device", i)
					}
					if len(result[i].MigDevices) != 2 || (result[i].MigDevices["1g.10gb"] != 4 || result[i].MigDevices["2g.20gb"] != 1) {
						t.Error("MigDevices not preserved correctly for device", i)
					}
				}
				if len(result[2].Devices) != 1 || result[2].Devices[0] != 2 {
					t.Errorf("Config for device 2 is incorrect: %v", result[2])
				}
				if result[2].MigEnabled {
					t.Error("MigEnabled should be false for device 2")
				}
				if len(result[2].MigDevices) != 1 || result[2].MigDevices["3g.30gb"] != 2 {
					t.Error("MigDevices not preserved correctly for device 2")
				}
			},
		},
		{
			name: "MultipleConfigsForSpecificDevicesWithAllEnabled",
			migConfigs: map[string]nvidia.MigConfigSpecSlice{
				"current": {
					nvidia.MigConfigSpec{
						Devices:    []int32{0, 1},
						MigEnabled: true,
						MigDevices: testConfigs.Configs[0],
					},
					nvidia.MigConfigSpec{
						Devices:    []int32{2},
						MigEnabled: true,
						MigDevices: testConfigs.Configs[1],
					},
				},
			},
			deviceCount: 3,
			expectError: false,
			validate: func(t *testing.T, result nvidia.MigConfigSpecSlice) {
				if len(result) != 3 {
					t.Errorf("Expected 3 configs, got %d", len(result))
				}
				for i := 0; i < 2; i++ {
					if len(result[i].Devices) != 1 || result[i].Devices[0] != int32(i) {
						t.Errorf("Config for device %d is incorrect: %v", i, result[i])
					}
					if !result[i].MigEnabled {
						t.Error("MigEnabled should be true for device", i)
					}
					if len(result[i].MigDevices) != 2 || (result[i].MigDevices["1g.10gb"] != 4 || result[i].MigDevices["2g.20gb"] != 1) {
						t.Error("MigDevices not preserved correctly for device", i)
					}
				}
				if len(result[2].Devices) != 1 || result[2].Devices[0] != 2 {
					t.Errorf("Config for device 2 is incorrect: %v", result[2])
				}
				if !result[2].MigEnabled {
					t.Error("MigEnabled should be false for device 2")
				}
				if len(result[2].MigDevices) != 1 || result[2].MigDevices["3g.30gb"] != 2 {
					t.Error("MigDevices not preserved correctly for device 2")
				}
				t.Log(result)
			},
		},
		{
			name: "DeviceNotMatched",
			migConfigs: map[string]nvidia.MigConfigSpecSlice{
				"current": {
					nvidia.MigConfigSpec{
						Devices:    []int32{0, 1},
						MigEnabled: true,
					},
				},
			},
			deviceCount: 3,
			expectError: true,
			validate:    nil,
		},
	}

	plugin := NvidiaDevicePlugin{
		config: &nvidia.DeviceConfig{
			Config: &v1.Config{
				Flags: v1.Flags{
					CommandLineFlags: v1.CommandLineFlags{},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := plugin.processMigConfigs(tc.migConfigs, tc.deviceCount)

			if tc.expectError {
				if err == nil {
					t.Error("Expected error but got nil")
				}
				t.Log(err)
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if tc.validate != nil {
				tc.validate(t, result)
			}
		})
	}
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
			nvidia.NodeDefaultConfig `json:",inline"`
			Name                     string               `json:"name"`
			OperatingMode            string               `json:"operatingmode"`
			Migstrategy              string               `json:"migstrategy"`
			FilterDevice             *nvidia.FilterDevice `json:"filterdevices"`
		}{
			{
				NodeDefaultConfig: nvidia.NodeDefaultConfig{
					DeviceSplitCount:    &split1,
					DeviceMemoryScaling: &memScale1,
					DeviceCoreScaling:   &coreScale1,
					LogLevel:            &logLevel1,
				},
				Name:          "node-1",
				OperatingMode: "default",
				Migstrategy:   "single",
				FilterDevice:  nil,
			},
			{
				NodeDefaultConfig: nvidia.NodeDefaultConfig{
					DeviceSplitCount:    &split2,
					DeviceMemoryScaling: &memScale2,
					DeviceCoreScaling:   &coreScale2,
					LogLevel:            &logLevel2,
				},
				Name:          "testnode",
				OperatingMode: "custom",
				Migstrategy:   "mixed",
				FilterDevice:  nil,
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
