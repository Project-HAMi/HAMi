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

package kunlun

import (
	"reflect"
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

func TestParseGroup(t *testing.T) {
	tests := []struct {
		name  string
		group string
		want  int
		out   []int
		ok    bool
	}{
		{name: "valid pair", group: "0-4", want: 2, out: []int{0, 4}, ok: true},
		{name: "valid quad", group: "0-1-4-5", want: 4, out: []int{0, 1, 4, 5}, ok: true},
		{name: "missing dash", group: "3", want: 2, ok: false},
		{name: "empty string", group: "", want: 2, ok: false},
		{name: "non-numeric value", group: "0-x", want: 2, ok: false},
		{name: "too many values", group: "0-1-2", want: 2, ok: false},
		{name: "too few values", group: "0-1", want: 4, ok: false},
		{name: "trailing dash", group: "0-", want: 2, ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, ok := parseGroup(tt.group, tt.want)
			if ok != tt.ok {
				t.Fatalf("parseGroup(%q, %d) ok = %v, want %v", tt.group, tt.want, ok, tt.ok)
			}
			if tt.ok && !reflect.DeepEqual(out, tt.out) {
				t.Fatalf("parseGroup(%q, %d) = %v, want %v", tt.group, tt.want, out, tt.out)
			}
		})
	}
}

func TestParsePairsSkipsMalformed(t *testing.T) {
	// Malformed entries must be skipped without panicking.
	got := parsePairs("0-4,3,,1-x,1-5")
	want := [][]int{{0, 4}, {1, 5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsePairs = %v, want %v", got, want)
	}
}

func TestParseInterconnection(t *testing.T) {
	got := parseInterconnection()
	wantLen := 4 + 12 // InterGroupConnection pairs + GroupConnection pairs
	if len(got) != wantLen {
		t.Fatalf("parseInterconnection returned %d pairs, want %d", len(got), wantLen)
	}
	if !reflect.DeepEqual(got[0], []int{0, 4}) {
		t.Fatalf("first pair = %v, want [0 4]", got[0])
	}
}

func TestParseInterconnection2(t *testing.T) {
	got := parseInterconnection2()
	if len(got) != 6 {
		t.Fatalf("parseInterconnection2 returned %d groups, want 6", len(got))
	}
	if !reflect.DeepEqual(got[0], []int{0, 1, 4, 5}) {
		t.Fatalf("first group = %v, want [0 1 4 5]", got[0])
	}
}

func makeDevices(usedIdx ...int) []*device.DeviceUsage {
	used := map[int]bool{}
	for _, idx := range usedIdx {
		used[idx] = true
	}
	devices := make([]*device.DeviceUsage, 8)
	for i := range devices {
		devices[i] = &device.DeviceUsage{Index: uint(i)}
		if used[i] {
			devices[i].Used = 1
		}
	}
	return devices
}

func fitFree(d *device.DeviceUsage, _ device.ContainerDeviceRequest) bool {
	return d.Used == 0
}

func TestInterconnectPairSelection(t *testing.T) {
	// Only 1 and 5 free in opposite wings: the inter-group pair 1-5 must be picked.
	devices := makeDevices(0, 2, 3, 4, 6, 7)
	got := interconnect(devices, device.ContainerDeviceRequest{Nums: 2}, fitFree)
	if !reflect.DeepEqual(got, []int{1, 5}) {
		t.Fatalf("interconnect = %v, want [1 5]", got)
	}
}

func TestInterconnectNoFit(t *testing.T) {
	// All devices used: no pair available.
	devices := makeDevices(0, 1, 2, 3, 4, 5, 6, 7)
	got := interconnect(devices, device.ContainerDeviceRequest{Nums: 2}, fitFree)
	if len(got) != 0 {
		t.Fatalf("interconnect = %v, want empty", got)
	}
}
