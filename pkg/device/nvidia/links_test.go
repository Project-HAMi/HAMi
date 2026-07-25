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
	"fmt"
	"testing"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	nvmlmock "github.com/NVIDIA/go-nvml/pkg/nvml/mock"
)

func Test_countMatchingLinks(t *testing.T) {
	tests := []struct {
		name    string
		pciList []PciInfo
		busID   string
		want    int
	}{
		{
			name:    "empty",
			pciList: nil,
			busID:   "01:00.0",
			want:    0,
		},
		{
			name: "single match",
			pciList: []PciInfo{
				mockPciInfo("00000000:01:00.0"),
			},
			busID: "0000:01:00.0",
			want:  1,
		},
		{
			name: "no match",
			pciList: []PciInfo{
				mockPciInfo("00000000:01:00.0"),
			},
			busID: "0000:02:00.0",
			want:  0,
		},
		{
			name: "multiple matches",
			pciList: []PciInfo{
				mockPciInfo("00000000:01:00.0"),
				mockPciInfo("00000000:02:00.0"),
				mockPciInfo("00000000:01:00.0"),
			},
			busID: "0000:01:00.0",
			want:  2,
		},
		{
			name: "all match",
			pciList: []PciInfo{
				mockPciInfo("00000000:0a:00.0"),
				mockPciInfo("00000000:0a:00.0"),
				mockPciInfo("00000000:0a:00.0"),
			},
			busID: "0000:0a:00.0",
			want:  3,
		},
		{
			name: "NVSwitch remote PCI vs GPU PCI — no match",
			pciList: []PciInfo{
				mockPciInfo("00000000:05:00.0"),
				mockPciInfo("00000000:06:00.0"),
				mockPciInfo("00000000:07:00.0"),
				mockPciInfo("00000000:08:00.0"),
			},
			busID: "0000:18:00.0",
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countMatchingLinks(tt.pciList, tt.busID)
			if got != tt.want {
				t.Errorf("countMatchingLinks() = %d, want %d", got, tt.want)
			}
		})
	}
}

func Test_nvlinkCountToType(t *testing.T) {
	tests := []struct {
		n    int
		want P2PLinkType
	}{
		{-1, P2PLinkUnknown},
		{0, P2PLinkUnknown},
		{1, SingleNVLINKLink},
		{2, TwoNVLINKLinks},
		{3, ThreeNVLINKLinks},
		{4, FourNVLINKLinks},
		{5, FiveNVLINKLinks},
		{6, SixNVLINKLinks},
		{7, SevenNVLINKLinks},
		{8, EightNVLINKLinks},
		{9, NineNVLINKLinks},
		{10, TenNVLINKLinks},
		{11, ElevenNVLINKLinks},
		{12, TwelveNVLINKLinks},
		{13, ThirteenNVLINKLinks},
		{14, FourteenNVLINKLinks},
		{15, FifteenNVLINKLinks},
		{16, SixteenNVLINKLinks},
		{17, SeventeenNVLINKLinks},
		{18, EighteenNVLINKLinks},
		{19, P2PLinkUnknown}, // beyond max
		{100, P2PLinkUnknown},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("n=%d", tt.n), func(t *testing.T) {
			got := nvlinkCountToType(tt.n)
			if got != tt.want {
				t.Errorf("nvlinkCountToType(%d) = %v, want %v", tt.n, got, tt.want)
			}
		})
	}
}

// mockPciInfo creates a PciInfo whose BusID() method returns busID.
func mockPciInfo(busIDStr string) PciInfo {
	var raw [32]int8
	for i := 0; i < len(busIDStr) && i < len(raw); i++ {
		raw[i] = int8(busIDStr[i])
	}
	return PciInfo{BusId: raw}
}

// newMockDevice wraps a go-nvml mock.Device into a go-nvlib device.Device,
// following the same pattern used by go-nvlib's own internal tests.
func newMockDevice(mockDev *nvmlmock.Device) device.Device {
	lib := device.New(nil, device.WithVerifySymbols(false))
	dev, _ := lib.NewDevice(mockDev)
	return dev
}

// makeNvLinkStateFunc returns a GetNvLinkStateFunc that reports `enabledCount`
// links as FEATURE_ENABLED and the rest as FEATURE_DISABLED.
func makeNvLinkStateFunc(enabledCount int) func(int) (nvml.EnableState, nvml.Return) {
	return func(i int) (nvml.EnableState, nvml.Return) {
		if i < enabledCount {
			return nvml.FEATURE_ENABLED, nvml.SUCCESS
		}
		return nvml.FEATURE_DISABLED, nvml.SUCCESS
	}
}

// makeNvLinkStateErrorFunc returns a GetNvLinkStateFunc where link `errLink`
// returns `errRet` and all other links return SUCCESS+ENABLED.
func makeNvLinkStateErrorFunc(errLink int, errRet nvml.Return) func(int) (nvml.EnableState, nvml.Return) {
	return func(i int) (nvml.EnableState, nvml.Return) {
		if i == errLink {
			return nvml.FEATURE_DISABLED, errRet
		}
		return nvml.FEATURE_ENABLED, nvml.SUCCESS
	}
}

// makeRemoteDeviceTypeFunc returns a GetNvLinkRemoteDeviceTypeFunc where the
// first `switchCount` enabled links report NVLINK_DEVICE_TYPE_SWITCH and the
// rest report NVLINK_DEVICE_TYPE_GPU.
func makeRemoteDeviceTypeFunc(switchCount int) func(int) (nvml.IntNvLinkDeviceType, nvml.Return) {
	return func(i int) (nvml.IntNvLinkDeviceType, nvml.Return) {
		if i < switchCount {
			return nvml.NVLINK_DEVICE_TYPE_SWITCH, nvml.SUCCESS
		}
		return nvml.NVLINK_DEVICE_TYPE_GPU, nvml.SUCCESS
	}
}

// makeRemoteDeviceTypeErrorFunc returns a GetNvLinkRemoteDeviceTypeFunc where
// link `errLink` returns `errRet` and all other links return SUCCESS+SWITCH.
func makeRemoteDeviceTypeErrorFunc(errLink int, errRet nvml.Return) func(int) (nvml.IntNvLinkDeviceType, nvml.Return) {
	return func(i int) (nvml.IntNvLinkDeviceType, nvml.Return) {
		if i == errLink {
			return nvml.NVLINK_DEVICE_TYPE_UNKNOWN, errRet
		}
		return nvml.NVLINK_DEVICE_TYPE_SWITCH, nvml.SUCCESS
	}
}

// makeRemotePciInfoFunc returns a GetNvLinkRemotePciInfoFunc where all enabled
// links report the given busID as the remote PCI address.
func makeRemotePciInfoFunc(busID string) func(int) (nvml.PciInfo, nvml.Return) {
	return func(int) (nvml.PciInfo, nvml.Return) {
		return nvml.PciInfo(mockPciInfo(busID)), nvml.SUCCESS
	}
}

func Test_countNvSwitchLinks(t *testing.T) {
	tests := []struct {
		name      string
		stateFunc func(int) (nvml.EnableState, nvml.Return)
		typeFunc  func(int) (nvml.IntNvLinkDeviceType, nvml.Return)
		wantCount int
		wantVia   bool
		wantErr   bool
	}{
		{
			name:      "all_links_via_nvswitch",
			stateFunc: makeNvLinkStateFunc(int(nvml.NVLINK_MAX_LINKS)),
			typeFunc:  makeRemoteDeviceTypeFunc(int(nvml.NVLINK_MAX_LINKS)),
			wantCount: int(nvml.NVLINK_MAX_LINKS),
			wantVia:   true,
		},
		{
			name:      "no_links_enabled",
			stateFunc: makeNvLinkStateFunc(0),
			typeFunc:  makeRemoteDeviceTypeFunc(0),
			wantCount: 0,
			wantVia:   false,
		},
		{
			name:      "links_enabled_but_not_switch",
			stateFunc: makeNvLinkStateFunc(int(nvml.NVLINK_MAX_LINKS)),
			typeFunc:  makeRemoteDeviceTypeFunc(0), // all GPU
			wantCount: 0,
			wantVia:   false,
		},
		{
			name: "mixed_switch_and_gpu",
			// Tests counting accuracy when link types are mixed, not a real
			// topology. countNvSwitchLinks should only count SWITCH links.
			stateFunc: makeNvLinkStateFunc(int(nvml.NVLINK_MAX_LINKS)),
			typeFunc:  makeRemoteDeviceTypeFunc(10), // 10 SWITCH, rest GPU
			wantCount: 10,
			wantVia:   true,
		},
		{
			name:      "nvlink_state_not_supported_skipped",
			stateFunc: makeNvLinkStateErrorFunc(0, nvml.ERROR_NOT_SUPPORTED),
			typeFunc:  makeRemoteDeviceTypeFunc(int(nvml.NVLINK_MAX_LINKS)),
			wantCount: int(nvml.NVLINK_MAX_LINKS) - 1, // 1 skipped
			wantVia:   true,
		},
		{
			name:      "nvlink_state_unexpected_error",
			stateFunc: makeNvLinkStateErrorFunc(0, nvml.ERROR_UNKNOWN),
			typeFunc:  makeRemoteDeviceTypeFunc(int(nvml.NVLINK_MAX_LINKS)),
			wantErr:   true,
		},
		{
			name:      "remote_type_not_supported_skipped",
			stateFunc: makeNvLinkStateFunc(int(nvml.NVLINK_MAX_LINKS)),
			typeFunc:  makeRemoteDeviceTypeErrorFunc(0, nvml.ERROR_NOT_SUPPORTED),
			wantCount: int(nvml.NVLINK_MAX_LINKS) - 1, // 1 skipped
			wantVia:   true,
		},
		{
			name:      "remote_type_unexpected_error",
			stateFunc: makeNvLinkStateFunc(int(nvml.NVLINK_MAX_LINKS)),
			typeFunc:  makeRemoteDeviceTypeErrorFunc(0, nvml.ERROR_UNKNOWN),
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dev := newMockDevice(&nvmlmock.Device{
				GetNvLinkStateFunc:            tt.stateFunc,
				GetNvLinkRemoteDeviceTypeFunc: tt.typeFunc,
			})
			gotCount, gotVia, err := countNvSwitchLinks(dev)
			if (err != nil) != tt.wantErr {
				t.Errorf("countNvSwitchLinks() err = %v, wantErr = %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if gotCount != tt.wantCount {
				t.Errorf("countNvSwitchLinks() count = %d, want %d", gotCount, tt.wantCount)
			}
			if gotVia != tt.wantVia {
				t.Errorf("countNvSwitchLinks() viaSwitch = %v, want %v", gotVia, tt.wantVia)
			}
		})
	}
}

func Test_GetNVLink(t *testing.T) {
	const dev2BusID = "00000000:2a:00.0"
	const switchBusID = "00000000:07:00.0"

	dev2PciFunc := func() (nvml.PciInfo, nvml.Return) {
		return nvml.PciInfo(mockPciInfo(dev2BusID)), nvml.SUCCESS
	}

	tests := []struct {
		name     string
		dev1Mock *nvmlmock.Device
		dev2Mock *nvmlmock.Device
		want     P2PLinkType
		wantErr  bool
	}{
		{
			name: "direct_connect_18_links",
			dev1Mock: &nvmlmock.Device{
				GetNvLinkStateFunc:         makeNvLinkStateFunc(18),
				GetNvLinkRemotePciInfoFunc: makeRemotePciInfoFunc(dev2BusID),
			},
			dev2Mock: &nvmlmock.Device{
				GetPciInfoFunc: dev2PciFunc,
			},
			want: EighteenNVLINKLinks,
		},
		{
			name: "direct_connect_partial",
			dev1Mock: &nvmlmock.Device{
				GetNvLinkStateFunc:         makeNvLinkStateFunc(4),
				GetNvLinkRemotePciInfoFunc: makeRemotePciInfoFunc(dev2BusID),
			},
			dev2Mock: &nvmlmock.Device{
				GetPciInfoFunc: dev2PciFunc,
			},
			want: FourNVLINKLinks,
		},
		{
			name: "nvswitch_full_mesh_18",
			dev1Mock: &nvmlmock.Device{
				GetNvLinkStateFunc:            makeNvLinkStateFunc(18),
				GetNvLinkRemotePciInfoFunc:    makeRemotePciInfoFunc(switchBusID), // remote is NVSwitch, not dev2
				GetNvLinkRemoteDeviceTypeFunc: makeRemoteDeviceTypeFunc(18),
			},
			dev2Mock: &nvmlmock.Device{
				GetPciInfoFunc:                dev2PciFunc, // does not match switchBusID
				GetNvLinkStateFunc:            makeNvLinkStateFunc(18),
				GetNvLinkRemoteDeviceTypeFunc: makeRemoteDeviceTypeFunc(18),
			},
			want: EighteenNVLINKLinks,
		},
		{
			name: "nvswitch_asymmetric",
			dev1Mock: &nvmlmock.Device{
				GetNvLinkStateFunc:            makeNvLinkStateFunc(18),
				GetNvLinkRemotePciInfoFunc:    makeRemotePciInfoFunc(switchBusID),
				GetNvLinkRemoteDeviceTypeFunc: makeRemoteDeviceTypeFunc(18),
			},
			dev2Mock: &nvmlmock.Device{
				GetPciInfoFunc:                dev2PciFunc,
				GetNvLinkStateFunc:            makeNvLinkStateFunc(4),
				GetNvLinkRemoteDeviceTypeFunc: makeRemoteDeviceTypeFunc(4),
			},
			want: FourNVLINKLinks, // min(18, 4)
		},
		{
			name: "one_side_no_nvswitch",
			dev1Mock: &nvmlmock.Device{
				GetNvLinkStateFunc:            makeNvLinkStateFunc(18),
				GetNvLinkRemotePciInfoFunc:    makeRemotePciInfoFunc(switchBusID),
				GetNvLinkRemoteDeviceTypeFunc: makeRemoteDeviceTypeFunc(18),
			},
			dev2Mock: &nvmlmock.Device{
				GetPciInfoFunc:                dev2PciFunc,
				GetNvLinkStateFunc:            makeNvLinkStateFunc(0), // no links
				GetNvLinkRemoteDeviceTypeFunc: makeRemoteDeviceTypeFunc(0),
			},
			want: P2PLinkUnknown,
		},
		{
			name: "no_nvlink_anywhere",
			dev1Mock: &nvmlmock.Device{
				GetNvLinkStateFunc:         makeNvLinkStateFunc(0),
				GetNvLinkRemotePciInfoFunc: makeRemotePciInfoFunc(switchBusID),
			},
			dev2Mock: &nvmlmock.Device{
				GetPciInfoFunc:     dev2PciFunc,
				GetNvLinkStateFunc: makeNvLinkStateFunc(0),
			},
			want: P2PLinkUnknown,
		},
		{
			name: "dev1_nvlink_state_error",
			dev1Mock: &nvmlmock.Device{
				GetNvLinkStateFunc:         makeNvLinkStateErrorFunc(0, nvml.ERROR_UNKNOWN),
				GetNvLinkRemotePciInfoFunc: makeRemotePciInfoFunc(switchBusID),
			},
			dev2Mock: &nvmlmock.Device{
				GetPciInfoFunc: dev2PciFunc,
			},
			wantErr: true,
		},
		{
			name: "dev1_nvswitch_count_error",
			dev1Mock: &nvmlmock.Device{
				GetNvLinkStateFunc:            makeNvLinkStateFunc(18),
				GetNvLinkRemotePciInfoFunc:    makeRemotePciInfoFunc(switchBusID), // won't match dev2
				GetNvLinkRemoteDeviceTypeFunc: makeRemoteDeviceTypeErrorFunc(0, nvml.ERROR_UNKNOWN),
			},
			dev2Mock: &nvmlmock.Device{
				GetPciInfoFunc: dev2PciFunc,
			},
			wantErr: true,
		},
		// Swapped dev1/dev2 variants — GetNVLink is asymmetric:
		// getAllNvLinkRemotePciInfo(dev1) then countNvSwitchLinks(dev1) then (dev2).
		{
			name: "nvswitch_asymmetric_swapped",
			dev1Mock: &nvmlmock.Device{
				GetNvLinkStateFunc:            makeNvLinkStateFunc(4),
				GetNvLinkRemotePciInfoFunc:    makeRemotePciInfoFunc(switchBusID),
				GetNvLinkRemoteDeviceTypeFunc: makeRemoteDeviceTypeFunc(4),
			},
			dev2Mock: &nvmlmock.Device{
				GetPciInfoFunc:                dev2PciFunc,
				GetNvLinkStateFunc:            makeNvLinkStateFunc(18),
				GetNvLinkRemoteDeviceTypeFunc: makeRemoteDeviceTypeFunc(18),
			},
			want: FourNVLINKLinks, // min(4, 18)
		},
		{
			name: "one_side_no_nvswitch_swapped",
			dev1Mock: &nvmlmock.Device{
				GetNvLinkStateFunc:            makeNvLinkStateFunc(0), // no NVSwitch links
				GetNvLinkRemotePciInfoFunc:    makeRemotePciInfoFunc(switchBusID),
				GetNvLinkRemoteDeviceTypeFunc: makeRemoteDeviceTypeFunc(0),
			},
			dev2Mock: &nvmlmock.Device{
				GetPciInfoFunc:                dev2PciFunc,
				GetNvLinkStateFunc:            makeNvLinkStateFunc(18),
				GetNvLinkRemoteDeviceTypeFunc: makeRemoteDeviceTypeFunc(18),
			},
			want: P2PLinkUnknown, // viaSwitch1=false
		},
		{
			name: "dev2_nvswitch_state_error",
			dev1Mock: &nvmlmock.Device{
				GetNvLinkStateFunc:            makeNvLinkStateFunc(18),
				GetNvLinkRemotePciInfoFunc:    makeRemotePciInfoFunc(switchBusID),
				GetNvLinkRemoteDeviceTypeFunc: makeRemoteDeviceTypeFunc(18),
			},
			dev2Mock: &nvmlmock.Device{
				GetPciInfoFunc:     dev2PciFunc,
				GetNvLinkStateFunc: makeNvLinkStateErrorFunc(0, nvml.ERROR_UNKNOWN),
			},
			wantErr: true,
		},
		{
			name: "dev2_nvswitch_count_error",
			dev1Mock: &nvmlmock.Device{
				GetNvLinkStateFunc:            makeNvLinkStateFunc(18),
				GetNvLinkRemotePciInfoFunc:    makeRemotePciInfoFunc(switchBusID),
				GetNvLinkRemoteDeviceTypeFunc: makeRemoteDeviceTypeFunc(18),
			},
			dev2Mock: &nvmlmock.Device{
				GetPciInfoFunc:                dev2PciFunc,
				GetNvLinkStateFunc:            makeNvLinkStateFunc(18),
				GetNvLinkRemoteDeviceTypeFunc: makeRemoteDeviceTypeErrorFunc(0, nvml.ERROR_UNKNOWN),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dev1 := newMockDevice(tt.dev1Mock)
			dev2 := newMockDevice(tt.dev2Mock)
			got, err := GetNVLink(dev1, dev2)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetNVLink() err = %v, wantErr = %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("GetNVLink() = %v, want %v", got, tt.want)
			}
		})
	}
}
