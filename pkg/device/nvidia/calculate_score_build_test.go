/*
Copyright 2025 The HAMi Authors.

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

package nvidia

import (
	"errors"
	"testing"

	nvlibdevice "github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/NVIDIA/go-nvml/pkg/nvml/mock"
	"gotest.tools/v3/assert"
)

// fakeDevice wraps a moq-generated nvml.Device mock to additionally satisfy
// the nvlibdevice.Device interface, which extends nvml.Device with a handful
// of higher-level helper methods that calculate_score.go/links.go never call.
type fakeDevice struct {
	*mock.Device
}

func (f *fakeDevice) GetArchitectureAsString() (string, error)          { return "", nil }
func (f *fakeDevice) GetBrandAsString() (string, error)                 { return "", nil }
func (f *fakeDevice) GetCudaComputeCapabilityAsString() (string, error) { return "", nil }
func (f *fakeDevice) GetAddressingModeAsString() (string, error)        { return "", nil }
func (f *fakeDevice) GetMigDevices() ([]nvlibdevice.MigDevice, error)   { return nil, nil }
func (f *fakeDevice) GetMigProfiles() ([]nvlibdevice.MigProfile, error) { return nil, nil }
func (f *fakeDevice) GetPCIBusID() (string, error)                      { return "", nil }
func (f *fakeDevice) IsCoherent() (bool, error)                         { return false, nil }
func (f *fakeDevice) IsFabricAttached() (bool, error)                   { return false, nil }
func (f *fakeDevice) IsMigCapable() (bool, error)                       { return false, nil }
func (f *fakeDevice) IsMigEnabled() (bool, error)                       { return false, nil }
func (f *fakeDevice) VisitMigDevices(func(int, nvlibdevice.MigDevice) error) error {
	return nil
}
func (f *fakeDevice) VisitMigProfiles(func(nvlibdevice.MigProfile) error) error {
	return nil
}

var _ nvlibdevice.Device = &fakeDevice{}

// fakeDeviceLib is a minimal nvlibdevice.Interface stand-in; only GetDevices
// is exercised by build(), the rest are unused stubs required by the interface.
type fakeDeviceLib struct {
	getDevices func() ([]nvlibdevice.Device, error)
}

func (f *fakeDeviceLib) AssertValidMigProfileFormat(string) error { return nil }
func (f *fakeDeviceLib) GetDevices() ([]nvlibdevice.Device, error) {
	return f.getDevices()
}
func (f *fakeDeviceLib) GetMigDevices() ([]nvlibdevice.MigDevice, error) { return nil, nil }
func (f *fakeDeviceLib) GetMigProfiles() ([]nvlibdevice.MigProfile, error) {
	return nil, nil
}
func (f *fakeDeviceLib) NewDevice(nvml.Device) (nvlibdevice.Device, error) { return nil, nil }
func (f *fakeDeviceLib) NewDeviceByUUID(string) (nvlibdevice.Device, error) {
	return nil, nil
}
func (f *fakeDeviceLib) NewMigDevice(nvml.Device) (nvlibdevice.MigDevice, error) {
	return nil, nil
}
func (f *fakeDeviceLib) NewMigDeviceByUUID(string) (nvlibdevice.MigDevice, error) {
	return nil, nil
}
func (f *fakeDeviceLib) NewMigProfile(int, int, int, uint64, uint64) (nvlibdevice.MigProfile, error) {
	return nil, nil
}
func (f *fakeDeviceLib) ParseMigProfile(string) (nvlibdevice.MigProfile, error) {
	return nil, nil
}
func (f *fakeDeviceLib) VisitDevices(func(int, nvlibdevice.Device) error) error { return nil }
func (f *fakeDeviceLib) VisitMigDevices(func(int, nvlibdevice.Device, int, nvlibdevice.MigDevice) error) error {
	return nil
}
func (f *fakeDeviceLib) VisitMigProfiles(func(nvlibdevice.MigProfile) error) error { return nil }

var _ nvlibdevice.Interface = &fakeDeviceLib{}

// busIDArray converts a bus ID string into the fixed-size int8 array nvml.PciInfo uses.
func busIDArray(id string) [32]int8 {
	var arr [32]int8
	for i := 0; i < len(id) && i < len(arr); i++ {
		arr[i] = int8(id[i])
	}
	return arr
}

func newFakeDevice(uuid string, busID string, topologyLevel nvml.GpuTopologyLevel) *fakeDevice {
	return &fakeDevice{
		Device: &mock.Device{
			GetUUIDFunc: func() (string, nvml.Return) {
				return uuid, nvml.SUCCESS
			},
			GetPciInfoFunc: func() (nvml.PciInfo, nvml.Return) {
				return nvml.PciInfo{BusId: busIDArray(busID)}, nvml.SUCCESS
			},
			GetTopologyCommonAncestorFunc: func(nvml.Device) (nvml.GpuTopologyLevel, nvml.Return) {
				return topologyLevel, nvml.SUCCESS
			},
			GetNvLinkStateFunc: func(int) (nvml.EnableState, nvml.Return) {
				return nvml.FEATURE_DISABLED, nvml.SUCCESS
			},
		},
	}
}

func Test_newDevice(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		d := newFakeDevice("gpu-uuid-0", "0000:00:00.0", nvml.TOPOLOGY_INTERNAL)
		got, err := newDevice(0, d)
		assert.NilError(t, err)
		assert.Equal(t, got.Index, 0)
		assert.Equal(t, got.UUID, "gpu-uuid-0")
		// BusID() strips a leading "0000" prefix (see links.go PciInfo.BusID).
		assert.Equal(t, got.PCI.BusID, ":00:00.0")
	})

	t.Run("GetUUID fails", func(t *testing.T) {
		d := &fakeDevice{Device: &mock.Device{
			GetUUIDFunc: func() (string, nvml.Return) { return "", nvml.ERROR_UNKNOWN },
		}}
		_, err := newDevice(0, d)
		assert.ErrorContains(t, err, "failed to get device uuid")
	})

	t.Run("GetPciInfo fails", func(t *testing.T) {
		d := &fakeDevice{Device: &mock.Device{
			GetUUIDFunc: func() (string, nvml.Return) { return "gpu-uuid-0", nvml.SUCCESS },
			GetPciInfoFunc: func() (nvml.PciInfo, nvml.Return) {
				return nvml.PciInfo{}, nvml.ERROR_UNKNOWN
			},
		}}
		_, err := newDevice(0, d)
		assert.ErrorContains(t, err, "failed to get device pci info")
	})
}

func Test_deviceListBuilder_build(t *testing.T) {
	t.Run("nvml Init fails", func(t *testing.T) {
		o := &deviceListBuilder{
			nvmllib: &mock.Interface{
				InitFunc: func() nvml.Return { return nvml.ERROR_UNKNOWN },
			},
		}
		_, err := o.build()
		assert.ErrorContains(t, err, "error calling nvml.Init")
	})

	t.Run("GetDevices fails", func(t *testing.T) {
		o := &deviceListBuilder{
			nvmllib: &mock.Interface{
				InitFunc:     func() nvml.Return { return nvml.SUCCESS },
				ShutdownFunc: func() nvml.Return { return nvml.SUCCESS },
			},
			devicelib: &fakeDeviceLib{
				getDevices: func() ([]nvlibdevice.Device, error) {
					return nil, errors.New("boom")
				},
			},
		}
		_, err := o.build()
		assert.ErrorContains(t, err, "failed to get devices")
	})

	t.Run("newDevice construction fails", func(t *testing.T) {
		badDevice := &fakeDevice{Device: &mock.Device{
			GetUUIDFunc: func() (string, nvml.Return) { return "", nvml.ERROR_UNKNOWN },
		}}
		o := &deviceListBuilder{
			nvmllib: &mock.Interface{
				InitFunc:     func() nvml.Return { return nvml.SUCCESS },
				ShutdownFunc: func() nvml.Return { return nvml.SUCCESS },
			},
			devicelib: &fakeDeviceLib{
				getDevices: func() ([]nvlibdevice.Device, error) {
					return []nvlibdevice.Device{badDevice}, nil
				},
			},
		}
		_, err := o.build()
		assert.ErrorContains(t, err, "failed to construct linked device")
	})

	t.Run("success with same-board link", func(t *testing.T) {
		d0 := newFakeDevice("gpu-uuid-0", "0000:00:00.0", nvml.TOPOLOGY_INTERNAL)
		d1 := newFakeDevice("gpu-uuid-1", "0000:00:01.0", nvml.TOPOLOGY_INTERNAL)
		o := &deviceListBuilder{
			nvmllib: &mock.Interface{
				InitFunc:     func() nvml.Return { return nvml.SUCCESS },
				ShutdownFunc: func() nvml.Return { return nvml.SUCCESS },
			},
			devicelib: &fakeDeviceLib{
				getDevices: func() ([]nvlibdevice.Device, error) {
					return []nvlibdevice.Device{d0, d1}, nil
				},
			},
		}
		devices, err := o.build()
		assert.NilError(t, err)
		assert.Equal(t, len(devices), 2)
		assert.Equal(t, devices[0].Links[1][0].Type, P2PLinkSameBoard)
		assert.Equal(t, devices[1].Links[0][0].Type, P2PLinkSameBoard)
	})

	t.Run("GetP2PLink fails", func(t *testing.T) {
		d0 := newFakeDevice("gpu-uuid-0", "0000:00:00.0", nvml.TOPOLOGY_INTERNAL)
		d0.GetTopologyCommonAncestorFunc = func(nvml.Device) (nvml.GpuTopologyLevel, nvml.Return) {
			return 0, nvml.ERROR_UNKNOWN
		}
		d1 := newFakeDevice("gpu-uuid-1", "0000:00:01.0", nvml.TOPOLOGY_INTERNAL)
		o := &deviceListBuilder{
			nvmllib: &mock.Interface{
				InitFunc:     func() nvml.Return { return nvml.SUCCESS },
				ShutdownFunc: func() nvml.Return { return nvml.SUCCESS },
			},
			devicelib: &fakeDeviceLib{
				getDevices: func() ([]nvlibdevice.Device, error) {
					return []nvlibdevice.Device{d0, d1}, nil
				},
			},
		}
		_, err := o.build()
		assert.ErrorContains(t, err, "error getting P2PLink")
	})

	t.Run("GetNVLink fails", func(t *testing.T) {
		d0 := newFakeDevice("gpu-uuid-0", "0000:00:00.0", nvml.TOPOLOGY_INTERNAL)
		d0.GetNvLinkStateFunc = func(int) (nvml.EnableState, nvml.Return) {
			return 0, nvml.ERROR_UNKNOWN
		}
		d1 := newFakeDevice("gpu-uuid-1", "0000:00:01.0", nvml.TOPOLOGY_INTERNAL)
		o := &deviceListBuilder{
			nvmllib: &mock.Interface{
				InitFunc:     func() nvml.Return { return nvml.SUCCESS },
				ShutdownFunc: func() nvml.Return { return nvml.SUCCESS },
			},
			devicelib: &fakeDeviceLib{
				getDevices: func() ([]nvlibdevice.Device, error) {
					return []nvlibdevice.Device{d0, d1}, nil
				},
			},
		}
		_, err := o.build()
		assert.ErrorContains(t, err, "error getting NVLink")
	})
}
