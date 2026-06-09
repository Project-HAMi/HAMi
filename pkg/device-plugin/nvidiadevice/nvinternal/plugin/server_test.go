package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	v1 "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/cdi"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/imex"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/rm"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

func ptr[T any](x T) *T {
	return &x
}

// runFallbackInit mirrors the fallback block from Start.
func runFallbackInit(plugin *NvidiaDevicePlugin, deviceNumbers int) {
	plugin.migCurrent.MigConfigs = make(map[string]nvidia.MigConfigSpecSlice)
	configSlice := nvidia.MigConfigSpecSlice{}
	for i := 0; i < deviceNumbers; i++ {
		conf := nvidia.MigConfigSpec{MigEnabled: false, Devices: []int32{int32(i)}}
		configSlice = append(configSlice, conf)
	}
	plugin.migCurrent.MigConfigs["current"] = configSlice
}

type MigDeviceConfigs struct {
	Configs []map[string]int32
}

func TestMigConfigFilePermissions(t *testing.T) {
	testCases := []struct {
		name             string
		expectedMode     os.FileMode
		shouldBeReadable bool
		shouldBeWritable bool
		otherCanWrite    bool
	}{
		{
			name:             "0644 permissions - owner read/write, others read-only",
			expectedMode:     0644,
			shouldBeReadable: true,
			shouldBeWritable: true,
			otherCanWrite:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			testFile := tmpDir + "/migconfig.yaml"
			testData := []byte("test MIG configuration data")

			err := os.WriteFile(testFile, testData, tc.expectedMode)
			require.NoError(t, err, "file write should succeed")

			info, err := os.Stat(testFile)
			require.NoError(t, err, "file should exist after write")

			mode := info.Mode().Perm()
			require.Equal(t, tc.expectedMode, mode,
				"file permissions should match: expected %#o, got %#o", tc.expectedMode, mode)

			data, err := os.ReadFile(testFile)
			require.NoError(t, err, "file should be readable")
			require.Equal(t, testData, data, "file content should match")

			permString := mode.String()
			if tc.expectedMode == 0644 {
				require.Equal(t, "-rw-r--r--", permString, "0644 should display as -rw-r--r--")
			}
		})
	}
}

func TestMigConfigFilePermissionSecurityImprovement(t *testing.T) {
	tmpDir := t.TempDir()

	correctFile := tmpDir + "/config_0644.yaml"
	testData := []byte("GPU MIG configuration")

	err := os.WriteFile(correctFile, testData, 0644)
	require.NoError(t, err)

	info644, err := os.Stat(correctFile)
	require.NoError(t, err)
	mode644 := info644.Mode().Perm()

	ownerCanWrite := (mode644 & 0200) != 0
	groupCanWrite := (mode644 & 0020) != 0
	otherCanWrite := (mode644 & 0002) != 0

	require.True(t, ownerCanWrite, "0644: Owner should be able to write")
	require.False(t, groupCanWrite, "0644: Group should NOT write")
	require.False(t, otherCanWrite, "0644: Others should NOT write (secure!)")

	otherCanRead := (mode644 & 0004) != 0
	require.True(t, otherCanRead, "0644: Others CAN read (for debugging)")
}

// TestGetMigCapabilityMapHybridGPUs verifies that getMigCapabilityMap correctly
// assigns MIG capability per-device, not all-or-nothing.
//
// THE BUG IT GUARDS AGAINST (upstream HAMi):
// The original upstream Start() loop resets deviceSupportMig=false on each
// iteration and calls `break` the moment any device is non-MIG. This means a
// single T4 anywhere in the list silently disables MIG for every A100 on the
// node. getMigCapabilityMap fixes this by producing an independent true/false
// per device index.
func TestGetMigCapabilityMapHybridGPUs(t *testing.T) {
	testCases := []struct {
		name                  string
		deviceNames           []string
		migGeometries         []string // model substrings that indicate MIG support
		expectedCapabilityMap map[int]bool
		expectedNodeHasMigGPU bool
	}{
		{
			name:                  "all MIG-capable (A100, A100, A100)",
			deviceNames:           []string{"A100", "A100", "A100"},
			migGeometries:         []string{"A100"},
			expectedCapabilityMap: map[int]bool{0: true, 1: true, 2: true},
			expectedNodeHasMigGPU: true,
		},
		{
			name:                  "all non-MIG (T4, T4, T4)",
			deviceNames:           []string{"T4", "T4", "T4"},
			migGeometries:         []string{"A100"}, // T4 not in list
			expectedCapabilityMap: map[int]bool{0: false, 1: false, 2: false},
			expectedNodeHasMigGPU: false,
		},
		{
			name:                  "hybrid - A100, T4, A100 (THE FIX!)",
			deviceNames:           []string{"A100", "T4", "A100"},
			migGeometries:         []string{"A100"},
			expectedCapabilityMap: map[int]bool{0: true, 1: false, 2: true},
			expectedNodeHasMigGPU: true,
		},
		{
			name:                  "mixed - T4, A100, T4",
			deviceNames:           []string{"T4", "A100", "T4"},
			migGeometries:         []string{"A100"},
			expectedCapabilityMap: map[int]bool{0: false, 1: true, 2: false},
			expectedNodeHasMigGPU: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Build the MIG geometries list using the real type that
			// getMigCapabilityMap reads from: []device.AllowedMigGeometries.
			// Each entry declares which GPU model substrings qualify as MIG-capable.
			// containsModel(deviceName, entry.Models) uses strings.Contains, so
			// "A100" in Models will match any device name that contains "A100".
			migGeomsList := make([]device.AllowedMigGeometries, 0, len(tc.migGeometries))
			for _, model := range tc.migGeometries {
				migGeomsList = append(migGeomsList, device.AllowedMigGeometries{
					Models: []string{model},
				})
			}

			plugin := &NvidiaDevicePlugin{
				config: &nvidia.DeviceConfig{
					Config: &v1.Config{
						Flags: v1.Flags{
							CommandLineFlags: v1.CommandLineFlags{},
						},
					},
				},
				schedulerConfig: nvidia.NvidiaConfig{
					MigGeometriesList: migGeomsList,
				},
			}

			capabilityMap, nodeHasMig := plugin.getMigCapabilityMap(tc.deviceNames)

			require.Equal(t, tc.expectedCapabilityMap, capabilityMap,
				"capability map should match expected per-device detection")
			require.Equal(t, tc.expectedNodeHasMigGPU, nodeHasMig,
				"nodeHasMigCapableGPU flag should be true if ANY GPU supports MIG")
		})
	}
}

// TestGetMigCapabilityMapFixesBrokenLogic directly tests the core correctness
// property: A100 is MIG-capable and T4 is not, even when both are on the same node.
//
// This test would FAIL against the upstream HAMi logic because [A100, T4] would
// cause the T4 to short-circuit the loop and mark the whole node as non-MIG.
func TestGetMigCapabilityMapFixesBrokenLogic(t *testing.T) {
	deviceNames := []string{"A100", "T4"}

	// Populate MigGeometriesList so the function has data to match against.
	// Without this, getMigCapabilityMap has nothing to iterate and every
	// device silently gets false — exactly what caused these test failures.
	plugin := &NvidiaDevicePlugin{
		config: &nvidia.DeviceConfig{
			Config: &v1.Config{
				Flags: v1.Flags{
					CommandLineFlags: v1.CommandLineFlags{},
				},
			},
		},
		schedulerConfig: nvidia.NvidiaConfig{
			MigGeometriesList: []device.AllowedMigGeometries{
				{Models: []string{"A100"}},
			},
		},
	}

	capabilityMap, nodeHasMig := plugin.getMigCapabilityMap(deviceNames)

	require.True(t, capabilityMap[0], "GPU[0] (A100) should support MIG")
	require.False(t, capabilityMap[1], "GPU[1] (T4) should not support MIG")
	require.True(t, nodeHasMig, "Node has MIG-capable GPU (GPU[0])")
}

// TestBuildHybridMigConfigMixedDevices verifies hybrid MIG configuration
func TestBuildHybridMigConfigMixedDevices(t *testing.T) {
	testCases := []struct {
		name                string
		deviceNumbers       int
		deviceNames         []string
		deviceMigSupportMap map[int]bool
		expectedMigEnabled  map[int]bool
		expectedDeviceList  map[int][]int32
	}{
		{
			name:                "hybrid - GPU[0] MIG, GPU[1] whole",
			deviceNumbers:       2,
			deviceNames:         []string{"A100", "T4"},
			deviceMigSupportMap: map[int]bool{0: true, 1: false},
			expectedMigEnabled:  map[int]bool{},
			expectedDeviceList: map[int][]int32{
				1: {1},
			},
		},
		{
			name:                "all MIG - GPU[0], GPU[1], GPU[2] all MIG",
			deviceNumbers:       3,
			deviceNames:         []string{"A100", "A100", "A100"},
			deviceMigSupportMap: map[int]bool{0: true, 1: true, 2: true},
			expectedDeviceList:  map[int][]int32{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			plugin := &NvidiaDevicePlugin{
				config: &nvidia.DeviceConfig{
					Config: &v1.Config{
						Flags: v1.Flags{
							CommandLineFlags: v1.CommandLineFlags{},
						},
					},
				},
				migCurrent: nvidia.MigPartedSpec{},
			}

			migConfigs := make(map[string]nvidia.MigConfigSpecSlice)

			for i := 0; i < tc.deviceNumbers; i++ {
				if !tc.deviceMigSupportMap[i] {
					nonMigConf := nvidia.MigConfigSpec{
						MigEnabled: false,
						Devices:    []int32{int32(i)},
					}
					migConfigs["current"] = append(migConfigs["current"], nonMigConf)
				}
			}

			plugin.migCurrent.MigConfigs = migConfigs

			if tc.name == "hybrid - GPU[0] MIG, GPU[1] whole" {
				require.Len(t, plugin.migCurrent.MigConfigs["current"], 1,
					"should have 1 config for non-MIG GPU")
				require.Equal(t, int32(1), plugin.migCurrent.MigConfigs["current"][0].Devices[0],
					"GPU[1] should be in non-MIG config")
				require.False(t, plugin.migCurrent.MigConfigs["current"][0].MigEnabled,
					"GPU[1] should have MigEnabled=false")
			}
		})
	}
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
		tc := testCases[i]
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

func TestSelectPreferredDeviceIDsFromAnnotatedDevices(t *testing.T) {
	plugin := &NvidiaDevicePlugin{}
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
		{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67c"},
	}

	_, err := plugin.selectPreferredDeviceIDsFromAnnotatedDevices(available, nil, desired, len(desired))
	require.Error(t, err)
	require.Contains(t, err.Error(), "GPU-03f69c50-207a-2038-9b45-23cac89cb67c")
}

func TestGetDevicePluginOptionsEnablesPreferredAllocation(t *testing.T) {
	plugin := &NvidiaDevicePlugin{}

	options, err := plugin.GetDevicePluginOptions(context.Background(), &kubeletdevicepluginv1beta1.Empty{})
	require.NoError(t, err)
	require.True(t, options.GetPreferredAllocationAvailable)
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
				"hami.io/vgpu-devices-to-allocate": device.EncodePodSingleDevice(device.PodSingleDevice{
					{}, // init container – empty
					{ // regular container – 2 GPUs
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

	request := &kubeletdevicepluginv1beta1.PreferredAllocationRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerPreferredAllocationRequest{
			{
				AvailableDeviceIDs: []string{
					"GPU-03f69c50-207a-2038-9b45-23cac89cb67a-0", "GPU-03f69c50-207a-2038-9b45-23cac89cb67a-1",
					"GPU-03f69c50-207a-2038-9b45-23cac89cb67b-0", "GPU-03f69c50-207a-2038-9b45-23cac89cb67b-1",
				},
				AllocationSize: 2,
			},
		},
	}

	response, err := plugin.GetPreferredAllocation(context.Background(), request)
	require.NoError(t, err)
	require.Len(t, response.ContainerResponses, 1)
	require.ElementsMatch(t, []string{
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67a-0",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67b-0",
	}, response.ContainerResponses[0].DeviceIDs)
}

func TestPhysicalDeviceIDHandlesMIGFormat(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"GPU-03f69c50-207a-2038-9b45-23cac89cb67a-0", "GPU-03f69c50-207a-2038-9b45-23cac89cb67a"},
		{"GPU-03f69c50-207a-2038-9b45-23cac89cb67a-10", "GPU-03f69c50-207a-2038-9b45-23cac89cb67a"},
		{"GPU-03f69c50-207a-2038-9b45-23cac89cb67a[0-1]", "GPU-03f69c50-207a-2038-9b45-23cac89cb67a"},
		{"GPU-03f69c50-207a-2038-9b45-23cac89cb67a[1-2]", "GPU-03f69c50-207a-2038-9b45-23cac89cb67a"},
		{"GPU-03f69c50-207a-2038-9b45-23cac89cb67a::replica-1", "GPU-03f69c50-207a-2038-9b45-23cac89cb67a"},
		{"GPU-03f69c50-207a-2038-9b45-23cac89cb67a", "GPU-03f69c50-207a-2038-9b45-23cac89cb67a"},
		{"GPU-03f69c50-207a-2038-9b45-23cac89cb123", "GPU-03f69c50-207a-2038-9b45-23cac89cb123"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := physicalDeviceID(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestSelectPreferredDeviceIDsWithMIGUUIDs(t *testing.T) {
	plugin := &NvidiaDevicePlugin{}
	available := []string{
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67a-0", "GPU-03f69c50-207a-2038-9b45-23cac89cb67a-1",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67b-0",
		"GPU-03f69c50-207a-2038-9b45-23cac89cb67c-0",
	}
	desired := device.ContainerDevices{
		{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67a[0-1]"},
		{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67b"},
		{UUID: "GPU-03f69c50-207a-2038-9b45-23cac89cb67c[1-2]"},
	}

	got, err := plugin.selectPreferredDeviceIDsFromAnnotatedDevices(available, nil, desired, 3)
	require.NoError(t, err)
	require.Len(t, got, 3)
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

	plugin := &NvidiaDevicePlugin{rm: mockRM}
	t.Setenv(util.NodeNameEnvName, "node-a")
	previousGetPendingPod := getPendingPod
	getPendingPod = func(context.Context, string) (*corev1.Pod, error) { return pod, nil }
	defer func() { getPendingPod = previousGetPendingPod }()

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

	plugin := &NvidiaDevicePlugin{rm: mockRM}
	t.Setenv(util.NodeNameEnvName, "node-a")
	previousGetPendingPod := getPendingPod
	getPendingPod = func(context.Context, string) (*corev1.Pod, error) { return pod, nil }
	defer func() { getPendingPod = previousGetPendingPod }()

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

func TestAllocateUsesKubeletSelectedUUIDsForVGPUResponse(t *testing.T) {
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

	previousEraseNextDeviceTypeFromAnnotation := eraseNextDeviceTypeFromAnnotation
	eraseNextDeviceTypeFromAnnotation = func(string, corev1.Pod) error { return nil }
	defer func() { eraseNextDeviceTypeFromAnnotation = previousEraseNextDeviceTypeFromAnnotation }()

	previousPodAllocationFailed := podAllocationFailed
	podAllocationFailed = func(string, *corev1.Pod, string) {}
	defer func() { podAllocationFailed = previousPodAllocationFailed }()

	previousPodAllocationTrySuccess := podAllocationTrySuccess
	podAllocationTrySuccess = func(string, string, string, *corev1.Pod) {}
	defer func() { podAllocationTrySuccess = previousPodAllocationTrySuccess }()

	request := &kubeletdevicepluginv1beta1.AllocateRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerAllocateRequest{{
			DevicesIds: []string{"GPU-03f69c50-207a-2038-9b45-23cac89cb67a-0"},
		}},
	}

	response, err := plugin.Allocate(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, "GPU-03f69c50-207a-2038-9b45-23cac89cb67a", response.ContainerResponses[0].Envs[deviceListEnvVar])
	require.Equal(t, "3000m", response.ContainerResponses[0].Envs["CUDA_DEVICE_MEMORY_LIMIT_0"])
	require.Equal(t, "50", response.ContainerResponses[0].Envs["CUDA_DEVICE_SM_LIMIT"])
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

	previousGetPendingPod := getPendingPod
	getPendingPod = func(context.Context, string) (*corev1.Pod, error) { return pod, nil }
	defer func() { getPendingPod = previousGetPendingPod }()

	previousEraseNextDeviceTypeFromAnnotation := eraseNextDeviceTypeFromAnnotation
	eraseNextDeviceTypeFromAnnotation = func(dtype string, p corev1.Pod) error {
		pod.Annotations["hami.io/vgpu-devices-to-allocate"] = ";GPU-annotated-b,NVIDIA,4000,60:;"
		return nil
	}
	defer func() { eraseNextDeviceTypeFromAnnotation = previousEraseNextDeviceTypeFromAnnotation }()

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

func TestMigFallbackInitialization(t *testing.T) {
	testCases := []struct {
		name          string
		deviceNumbers int
	}{
		{name: "zero devices", deviceNumbers: 0},
		{name: "single device", deviceNumbers: 1},
		{name: "three devices", deviceNumbers: 3},
		{name: "eight devices", deviceNumbers: 8},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			plugin := &NvidiaDevicePlugin{
				config: &nvidia.DeviceConfig{
					Config: &v1.Config{
						Flags: v1.Flags{
							CommandLineFlags: v1.CommandLineFlags{},
						},
					},
				},
				migCurrent: nvidia.MigPartedSpec{},
			}

			runFallbackInit(plugin, tc.deviceNumbers)

			require.NotNil(t, plugin.migCurrent.MigConfigs,
				"MigConfigs must not be nil after fallback init")
			require.NotNil(t, plugin.migCurrent.MigConfigs["current"],
				"current key must always exist after fallback init")
			require.Len(t, plugin.migCurrent.MigConfigs["current"], tc.deviceNumbers,
				"one config entry per device is required")

			for i := 0; i < tc.deviceNumbers; i++ {
				cfg := plugin.migCurrent.MigConfigs["current"][i]
				require.False(t, cfg.MigEnabled,
					"fallback must set MigEnabled=false for device %d", i)
				require.Len(t, cfg.Devices, 1,
					"each fallback entry must reference exactly one device (device %d)", i)
				require.Equal(t, int32(i), cfg.Devices[0],
					"device index must match loop counter for device %d", i)
			}
		})
	}
}

func TestMigFallbackInit_ReplacesExistingConfigs(t *testing.T) {
	plugin := &NvidiaDevicePlugin{
		config: &nvidia.DeviceConfig{
			Config: &v1.Config{
				Flags: v1.Flags{
					CommandLineFlags: v1.CommandLineFlags{},
				},
			},
		},
		migCurrent: nvidia.MigPartedSpec{
			MigConfigs: map[string]nvidia.MigConfigSpecSlice{
				"current": {
					nvidia.MigConfigSpec{MigEnabled: true, Devices: []int32{0}},
					nvidia.MigConfigSpec{MigEnabled: true, Devices: []int32{1}},
				},
				"stale-key": {},
			},
		},
	}

	deviceNumbers := 2
	runFallbackInit(plugin, deviceNumbers)

	require.Len(t, plugin.migCurrent.MigConfigs, 1,
		"fallback must produce a map with only the current key")
	require.Len(t, plugin.migCurrent.MigConfigs["current"], deviceNumbers)
	for i := 0; i < deviceNumbers; i++ {
		require.False(t, plugin.migCurrent.MigConfigs["current"][i].MigEnabled,
			"stale MigEnabled=true must be overwritten for device %d", i)
	}
}

func TestMigSuccessFlag_GatesApplyMigTemplate(t *testing.T) {
	testCases := []struct {
		name                       string
		migSuccessfullyInitialized bool
		expectApplyTemplateCalled  bool
	}{
		{
			name:                       "template applied when MIG succeeded",
			migSuccessfullyInitialized: true,
			expectApplyTemplateCalled:  true,
		},
		{
			name:                       "template skipped when MIG failed or was disabled",
			migSuccessfullyInitialized: false,
			expectApplyTemplateCalled:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			applyTemplateCalled := false
			mockApplyMigTemplate := func() { applyTemplateCalled = true }

			if tc.migSuccessfullyInitialized {
				mockApplyMigTemplate()
			}

			require.Equal(t, tc.expectApplyTemplateCalled, applyTemplateCalled,
				"ApplyMigTemplate call decision must be controlled by migSuccessfullyInitialized")
		})
	}
}

func TestMigSuccessFlag_FallbackRunsExactlyWhenNotInitialized(t *testing.T) {
	testCases := []struct {
		name                       string
		migSuccessfullyInitialized bool
		expectFallbackRan          bool
	}{
		{
			name:                       "fallback skipped when MIG succeeded",
			migSuccessfullyInitialized: true,
			expectFallbackRan:          false,
		},
		{
			name:                       "fallback runs when MIG was not initialised",
			migSuccessfullyInitialized: false,
			expectFallbackRan:          true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			plugin := &NvidiaDevicePlugin{
				config: &nvidia.DeviceConfig{
					Config: &v1.Config{
						Flags: v1.Flags{
							CommandLineFlags: v1.CommandLineFlags{},
						},
					},
				},
				migCurrent: nvidia.MigPartedSpec{},
			}
			fallbackRan := false

			if !tc.migSuccessfullyInitialized {
				fallbackRan = true
				runFallbackInit(plugin, 1)
			}

			require.Equal(t, tc.expectFallbackRan, fallbackRan,
				"fallback execution must be gated by !migSuccessfullyInitialized")

			if tc.expectFallbackRan {
				require.NotNil(t, plugin.migCurrent.MigConfigs)
				require.NotNil(t, plugin.migCurrent.MigConfigs["current"])
			}
		})
	}
}

func TestMigInitFlow_EndToEnd(t *testing.T) {
	testCases := []struct {
		name                string
		operatingMode       string
		deviceSupportMig    bool
		migPartsedSucceeds  bool
		expectFallback      bool
		expectApplyTemplate bool
	}{
		{
			name:                "non-mig mode (default) – MIG disabled",
			operatingMode:       "default",
			deviceSupportMig:    false,
			migPartsedSucceeds:  false,
			expectFallback:      true,
			expectApplyTemplate: false,
		},
		{
			name:                "mig mode but device unsupported – graceful degradation",
			operatingMode:       "mig",
			deviceSupportMig:    false,
			migPartsedSucceeds:  false,
			expectFallback:      true,
			expectApplyTemplate: false,
		},
		{
			name:                "mig mode, supported, but nvidia-mig-parted fails",
			operatingMode:       "mig",
			deviceSupportMig:    true,
			migPartsedSucceeds:  false,
			expectFallback:      true,
			expectApplyTemplate: false,
		},
		{
			name:                "mig mode, supported, nvidia-mig-parted succeeds",
			operatingMode:       "mig",
			deviceSupportMig:    true,
			migPartsedSucceeds:  true,
			expectFallback:      false,
			expectApplyTemplate: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			plugin := &NvidiaDevicePlugin{
				config: &nvidia.DeviceConfig{
					Config: &v1.Config{
						Flags: v1.Flags{
							CommandLineFlags: v1.CommandLineFlags{},
						},
					},
				},
				operatingMode: tc.operatingMode,
				migCurrent:    nvidia.MigPartedSpec{},
			}

			shouldUseMig := plugin.operatingMode == "mig"
			if shouldUseMig && !tc.deviceSupportMig {
				shouldUseMig = false
			}

			migSuccessfullyInitialized := false
			if tc.deviceSupportMig && shouldUseMig && tc.migPartsedSucceeds {
				migSuccessfullyInitialized = true
				plugin.migCurrent.MigConfigs = map[string]nvidia.MigConfigSpecSlice{
					"current": {{MigEnabled: true, Devices: []int32{0}}},
				}
			}

			fallbackRan := false
			if !migSuccessfullyInitialized {
				fallbackRan = true
				runFallbackInit(plugin, 1)
			}

			applyTemplateCalled := false
			if migSuccessfullyInitialized {
				applyTemplateCalled = true
			}

			require.Equal(t, tc.expectFallback, fallbackRan, "fallback execution mismatch")
			require.Equal(t, tc.expectApplyTemplate, applyTemplateCalled, "ApplyMigTemplate call mismatch")

			require.NotNil(t, plugin.migCurrent.MigConfigs,
				"MigConfigs must never be nil after Start() completes")
			require.NotNil(t, plugin.migCurrent.MigConfigs["current"],
				"current key must always exist after Start() completes")
		})
	}
}

func TestMigCurrentConfigsNeverNil(t *testing.T) {
	plugin := &NvidiaDevicePlugin{
		config: &nvidia.DeviceConfig{
			Config: &v1.Config{
				Flags: v1.Flags{
					CommandLineFlags: v1.CommandLineFlags{},
				},
			},
		},
		operatingMode: "mig",
		migCurrent:    nvidia.MigPartedSpec{},
	}

	shouldUseMig := plugin.operatingMode == "mig"
	deviceSupportMig := false
	if shouldUseMig && !deviceSupportMig {
		shouldUseMig = false
	}

	migSuccessfullyInitialized := false
	if !migSuccessfullyInitialized {
		runFallbackInit(plugin, 2)
	}

	require.NotNil(t, plugin.migCurrent.MigConfigs)
	require.NotNil(t, plugin.migCurrent.MigConfigs["current"])
	require.Len(t, plugin.migCurrent.MigConfigs["current"], 2)
	for _, cfg := range plugin.migCurrent.MigConfigs["current"] {
		require.False(t, cfg.MigEnabled)
	}
}
