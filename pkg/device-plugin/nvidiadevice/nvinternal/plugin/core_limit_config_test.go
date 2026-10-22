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
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"testing"

	v1 "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/rm"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/stretchr/testify/require"
)

// Exercise file loading and the real construction path, so the test catches
// merging into a temporary config that never reaches the plugin's Allocate path.
func TestDevicePluginForResourceDisableCoreLimit(t *testing.T) {
	previousDevices, previousTypes := device.DevicesMap, device.DevicesToHandle
	previousRequests, previousSupport := maps.Clone(device.InRequestDevices), maps.Clone(device.SupportDevices)
	previousHandshake := maps.Clone(util.HandshakeAnnos)
	previousMemoryFactor := nvidia.MemoryFactor
	previousConfigFile := ConfigFile
	t.Cleanup(func() {
		device.DevicesMap, device.DevicesToHandle = previousDevices, previousTypes
		device.InRequestDevices, device.SupportDevices = previousRequests, previousSupport
		util.HandshakeAnnos = previousHandshake
		nvidia.MemoryFactor = previousMemoryFactor
		ConfigFile = previousConfigFile
	})

	for _, tt := range []struct {
		name string
		file string
		cli  bool
		want bool
	}{
		{name: "both unset"},
		{name: "file unset CLI true", cli: true, want: true},
		{name: "both false", file: "  disableCoreLimit: false\n"},
		{name: "file false CLI true", file: "  disableCoreLimit: false\n", cli: true, want: true},
		{name: "file true chart default false", file: "  disableCoreLimit: true\n", want: true},
		{name: "both true", file: "  disableCoreLimit: true\n", cli: true, want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "device-config.yaml")
			ConfigFile = &path
			require.NoError(t, os.WriteFile(path, []byte("nvidia:\n  resourceCountName: nvidia.com/gpu\n"+tt.file), 0600))
			sConfig, err := config.LoadConfig(path)
			require.NoError(t, err)
			upstream := &v1.Config{}
			require.NoError(t, json.Unmarshal([]byte(`{"flags":{"plugin":{"deviceListStrategy":["envvar"],"cdiAnnotationPrefix":"cdi.k8s.io/"}}}`), upstream))
			nvconfig := &nvidia.DeviceConfig{
				Config:           upstream,
				DisableCoreLimit: tt.cli,
				ResourceName:     ptr("nvidia.com/gpu"),
			}
			o := &options{config: nvconfig}
			resourceManager := &rm.ResourceManagerMock{
				ResourceFunc: func() v1.ResourceName { return "nvidia.com/gpu" },
			}
			got, err := o.devicePluginForResource(context.Background(), nvconfig, resourceManager, sConfig, "hami-core")
			require.NoError(t, err)
			plugin, ok := got.(*NvidiaDevicePlugin)
			require.True(t, ok)
			require.Equal(t, tt.want, plugin.schedulerConfig.DisableCoreLimit)
		})
	}
}
