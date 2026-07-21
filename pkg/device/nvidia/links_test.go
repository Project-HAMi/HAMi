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
	"testing"
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
		got := nvlinkCountToType(tt.n)
		if got != tt.want {
			t.Errorf("nvlinkCountToType(%d) = %v, want %v", tt.n, got, tt.want)
		}
	}
}

// mockPciInfo creates a PciInfo whose BusID() method returns busID.
func mockPciInfo(busIDStr string) PciInfo {
	var raw [32]uint8
	copy(raw[:], busIDStr)
	return PciInfo{BusId: raw}
}
