/*
Copyright 2024 The HAMi Authors.

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
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/rm"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

func u(v uint) *uint         { return &v }
func f64(v float64) *float64 { return &v }
func i64(v int64) *int64     { return &v }
func s(v string) *string     { return &v }

func cdiDevices(ids ...string) rm.Devices {
	d := rm.Devices{}
	for i, id := range ids {
		dev := &rm.Device{Index: intToStr(i)}
		dev.ID = id
		d[id] = dev
	}
	return d
}

func intToStr(i int) string {
	return string(rune('0' + i))
}

func newCDIPlugin(devs rm.Devices, cfg nvidia.NodeDefaultConfig) *NvidiaDevicePlugin {
	return &NvidiaDevicePlugin{
		cdiDiscovery:    true,
		operatingMode:   "hami-core",
		rm:              &rm.ResourceManagerMock{DevicesFunc: func() rm.Devices { return devs }},
		schedulerConfig: nvidia.NvidiaConfig{NodeDefaultConfig: cfg},
	}
}

func baseCfg() nvidia.NodeDefaultConfig {
	return nvidia.NodeDefaultConfig{
		PreConfiguredDeviceMemory: i64(122566),
		DeviceMemoryScaling:       f64(1),
		DeviceCoreScaling:         f64(1),
		DeviceSplitCount:          u(10),
		PreConfiguredDeviceType:   s("NVIDIA-GB10"),
	}
}

func TestGetCDIAPIDevices_Basic(t *testing.T) {
	p := newCDIPlugin(cdiDevices("GPU-abc"), baseCfg())
	res := *p.getCDIAPIDevices()
	require.Len(t, res, 1)
	d := res[0]
	require.Equal(t, "GPU-abc", d.ID)
	require.Equal(t, uint(0), d.Index)
	require.Equal(t, int32(122566), d.Devmem)
	require.Equal(t, int32(100), d.Devcore)
	require.Equal(t, "NVIDIA-GB10", d.Type)
	require.Equal(t, int32(10), d.Count)
	require.True(t, d.Health)
	require.Equal(t, "hami-core", d.Mode)
}

func TestGetCDIAPIDevices_MemoryScaling(t *testing.T) {
	cfg := baseCfg()
	cfg.DeviceMemoryScaling = f64(2)
	p := newCDIPlugin(cdiDevices("GPU-abc"), cfg)
	res := *p.getCDIAPIDevices()
	require.Len(t, res, 1)
	require.Equal(t, int32(122566*2), res[0].Devmem)
}

func TestGetCDIAPIDevices_DefaultTypeAndPrefix(t *testing.T) {
	// nil type -> built-in default.
	cfg := baseCfg()
	cfg.PreConfiguredDeviceType = nil
	p := newCDIPlugin(cdiDevices("GPU-abc"), cfg)
	require.Equal(t, cdiDefaultDeviceType, (*p.getCDIAPIDevices())[0].Type)

	// type without an NVIDIA prefix gets one added.
	cfg2 := baseCfg()
	cfg2.PreConfiguredDeviceType = s("GB10")
	p2 := newCDIPlugin(cdiDevices("GPU-abc"), cfg2)
	require.Equal(t, "NVIDIA-GB10", (*p2.getCDIAPIDevices())[0].Type)
}

func TestGetCDIAPIDevices_NoPreConfiguredMemory(t *testing.T) {
	cfg := baseCfg()
	cfg.PreConfiguredDeviceMemory = nil
	p := newCDIPlugin(cdiDevices("GPU-abc"), cfg)
	require.Empty(t, *p.getCDIAPIDevices())

	cfg.PreConfiguredDeviceMemory = i64(0)
	p = newCDIPlugin(cdiDevices("GPU-abc"), cfg)
	require.Empty(t, *p.getCDIAPIDevices())
}

func TestGetCDIAPIDevices_InvalidIndexSkipped(t *testing.T) {
	devs := rm.Devices{}
	bad := &rm.Device{Index: "not-a-number"}
	bad.ID = "GPU-bad"
	devs["GPU-bad"] = bad
	p := newCDIPlugin(devs, baseCfg())
	require.Empty(t, *p.getCDIAPIDevices())
}

// getAPIDevices dispatches to the CDI path when cdiDiscovery is set.
func TestGetAPIDevices_DispatchesToCDI(t *testing.T) {
	p := newCDIPlugin(cdiDevices("GPU-abc"), baseCfg())
	res := *p.getAPIDevices()
	require.Len(t, res, 1)
	require.Equal(t, "GPU-abc", res[0].ID)
}
