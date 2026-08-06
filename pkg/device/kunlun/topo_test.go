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
	"sort"
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

// newDevices builds 8 fake XPU devices (index 0-7), all healthy, with
// Count=1 so a device is "available" while Used < Count.
func newDevices(usedMask ...int) []*device.DeviceUsage {
	used := map[int]bool{}
	for _, u := range usedMask {
		used[u] = true
	}
	devices := make([]*device.DeviceUsage, 0, 8)
	for i := range 8 {
		u := int32(0)
		if used[i] {
			u = 1
		}
		devices = append(devices, &device.DeviceUsage{
			Index:  uint(i),
			Used:   u,
			Count:  1,
			Health: true,
		})
	}
	return devices
}

// fitAvailable treats a device as fitting the request if it still has
// free capacity (Used < Count). Nums on the request is irrelevant here
// since fitFn is evaluated once per device.
func fitAvailable(d *device.DeviceUsage, _ device.ContainerDeviceRequest) bool {
	return d.Used < d.Count
}

func req(nums int32) device.ContainerDeviceRequest {
	return device.ContainerDeviceRequest{Nums: nums}
}

// sortedCopy returns a sorted copy of ints without mutating the input.
func sortedCopy(in []int) []int {
	out := append([]int{}, in...)
	sort.Ints(out)
	return out
}

// onlyFree returns a usedMask that leaves exactly the devices in keep
// free (Used < Count) and marks every other device (0-7) as used.
// This lets a test force interconnect/graghSelect down a specific
// selection path instead of leaving all 8 devices free, where a
// broken/empty implementation could otherwise slip through unnoticed.
func onlyFree(keep []int) []int {
	keepSet := map[int]bool{}
	for _, idx := range keep {
		keepSet[idx] = true
	}
	var usedMask []int
	for i := range 8 {
		if !keepSet[i] {
			usedMask = append(usedMask, i)
		}
	}
	return usedMask
}

func TestParseUsage(t *testing.T) {
	devices := newDevices(1, 3, 5) // 1,3,5 are used -> unavailable
	got := parseUsage(devices, req(1), fitAvailable)
	want := []int{0, 2, 4, 6, 7}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseUsage() = %v, want %v", got, want)
	}
}

func TestParseUsage_NoneAvailable(t *testing.T) {
	devices := newDevices(0, 1, 2, 3, 4, 5, 6, 7)
	got := parseUsage(devices, req(1), fitAvailable)
	if len(got) != 0 {
		t.Errorf("parseUsage() = %v, want empty", got)
	}
}

func TestAddidx(t *testing.T) {
	tests := []struct {
		name  string
		start []int
		value int
		want  []int
	}{
		{"append new value", []int{1, 2}, 3, []int{1, 2, 3}},
		{"duplicate value is ignored", []int{1, 2, 3}, 2, []int{1, 2, 3}},
		{"append to empty slice", []int{}, 5, []int{5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := addidx(tt.start, tt.value)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("addidx(%v, %d) = %v, want %v", tt.start, tt.value, got, tt.want)
			}
		})
	}
}

func TestGetvalue(t *testing.T) {
	tests := []struct {
		in   int
		want int
	}{
		{4, 0},
		{1, 2},
		{0, 1},
		{2, 1},
		{3, 1},
	}
	for _, tt := range tests {
		if got := getvalue(tt.in); got != tt.want {
			t.Errorf("getvalue(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestCountbubble(t *testing.T) {
	tests := []struct {
		name string
		in   []int
		want int
	}{
		{"empty", []int{}, 1},
		{"all left, full group of 4", []int{0, 1, 2, 3}, 1}, // left=4 -> getvalue(4)=0, right=0 -> getvalue(0)=1 => 0+1=1
		{"single left device", []int{0}, 3},                 // left=1 -> getvalue(1)=2, right=0 -> getvalue(0)=1 => 3
		{"single right device", []int{4}, 3},                // left=0 -> 1, right=1 -> 2 => 3
		{"two left devices", []int{0, 1}, 2},                // left=2 -> 1, right=0 -> 1 => 2
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countbubble(tt.in); got != tt.want {
				t.Errorf("countbubble(%v) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestCalcscore(t *testing.T) {
	// countbubble([]) = 1 (baseline "prev")
	// Use single-element slices to get predictable countbubble results:
	//   countbubble([0])  -> left=1,right=0 -> getvalue(1)+getvalue(0) = 2+1 = 3
	//   countbubble([4])  -> left=0,right=1 -> getvalue(0)+getvalue(1) = 1+2 = 3
	//   countbubble([0,1])-> left=2,right=0 -> getvalue(2)+getvalue(0) = 1+1 = 2
	//   countbubble([0,1,2,3]) -> left=4,right=0 -> getvalue(4)+getvalue(0) = 0+1 = 1
	tests := []struct {
		name string
		prev []int
		cur  []int
		want float32
	}{
		{"cur decreases fragmentation by 1", []int{0}, []int{0, 1}, 3000},    // 2-3 = -1
		{"cur unchanged", []int{0}, []int{4}, 2000},                          // 3-3 = 0
		{"cur increases fragmentation by 1", []int{0, 1}, []int{0}, 1000},    // 3-2 = 1
		{"cur increases fragmentation by 2", []int{0, 1, 2, 3}, []int{0}, 0}, // 3-1 = 2
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// calcscore sorts its inputs in place; pass copies.
			p := append([]int{}, tt.prev...)
			c := append([]int{}, tt.cur...)
			if got := calcscore(p, c); got != tt.want {
				t.Errorf("calcscore(%v, %v) = %v, want %v", tt.prev, tt.cur, got, tt.want)
			}
		})
	}
}

func TestCanMeet(t *testing.T) {
	tests := []struct {
		name string
		have []int
		want []int
		res  bool
	}{
		{"subset present", []int{0, 1, 2, 3}, []int{1, 2}, true},
		{"exact match", []int{0, 1}, []int{0, 1}, true},
		{"missing element", []int{0, 1, 2}, []int{2, 3}, false},
		{"empty want", []int{0, 1}, []int{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canMeet(tt.have, tt.want); got != tt.res {
				t.Errorf("canMeet(%v, %v) = %v, want %v", tt.have, tt.want, got, tt.res)
			}
		})
	}
}

func TestDelta(t *testing.T) {
	tests := []struct {
		name string
		have []int
		want []int
		res  []int
	}{
		{"removes matched elements", []int{0, 1, 2, 3}, []int{1, 2}, []int{0, 3}},
		{"no overlap keeps all", []int{0, 1}, []int{5, 6}, []int{0, 1}},
		{"full overlap returns nil", []int{0, 1}, []int{0, 1}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := delta(tt.have, tt.want)
			if !reflect.DeepEqual(got, tt.res) {
				t.Errorf("delta(%v, %v) = %v, want %v", tt.have, tt.want, got, tt.res)
			}
		})
	}
}

func TestDevicepick(t *testing.T) {
	devices := newDevices(1, 3) // indices 1 and 3 are unavailable

	got := devicepick(devices, 0, req(2), fitAvailable)
	want := []int{0, 2}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("devicepick(start=0, count=2) = %v, want %v", got, want)
	}

	got = devicepick(devices, 4, req(3), fitAvailable)
	want = []int{4, 5, 6}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("devicepick(start=4, count=3) = %v, want %v", got, want)
	}

	// Not enough devices left to satisfy count -> returns whatever it found.
	got = devicepick(devices, 6, req(5), fitAvailable)
	want = []int{6, 7}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("devicepick(start=6, count=5) = %v, want %v", got, want)
	}
}

// TestDevicepick_FewerThanEightDevices guards against a regression where the
// scan loop was hard-coded to `t < 8` and indexed devices[t] directly,
// panicking with index out of range on nodes that report fewer than 8 XPUs.
func TestDevicepick_FewerThanEightDevices(t *testing.T) {
	devices := newDevices(0, 1, 2, 3, 4, 5, 6, 7)[:4] // node with only 4 XPUs, all busy

	got := devicepick(devices, 0, req(2), fitAvailable)
	want := []int{}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("devicepick(start=0, count=2) = %v, want %v", got, want)
	}
}

func TestGraghSelect_AllEightAvailable(t *testing.T) {
	devices := newDevices() // none used
	got := graghSelect(devices, req(8), fitAvailable)
	want := []int{0, 1, 2, 3, 4, 5, 6, 7}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("graghSelect(count=8) = %v, want %v", got, want)
	}
}

func TestGraghSelect_EightNotFullyAvailable(t *testing.T) {
	devices := newDevices(0) // device 0 used, only 7 free
	got := graghSelect(devices, req(8), fitAvailable)
	if len(got) != 0 {
		t.Errorf("graghSelect(count=8) with insufficient devices = %v, want empty", got)
	}
}

func TestGraghSelect_SingleDevice(t *testing.T) {
	// Both wings fully free -> leftwing==4 is matched first, so devicepick
	// starts at index 0 and the result is deterministically [0].
	devices := newDevices()
	got := graghSelect(devices, req(1), fitAvailable)
	want := []int{0}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("graghSelect(count=1) = %v, want %v", got, want)
	}
}

func TestGraghSelect_FourFromLeftWing(t *testing.T) {
	// Left wing (indices 0-3) fully free, right wing fully used.
	devices := newDevices(4, 5, 6, 7)
	got := graghSelect(devices, req(4), fitAvailable)
	want := []int{0, 1, 2, 3}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("graghSelect(count=4) = %v, want %v", got, want)
	}
}

func TestInterconnect_TwoDevices(t *testing.T) {
	pairs := parseInterconnection()
	if len(pairs) == 0 {
		t.Fatal("parseInterconnection() returned no pairs to test against")
	}
	target := pairs[0]

	// Only the target pair is left free; every other device is marked
	// used so the count==2 path is actually forced to choose this pair.
	devices := newDevices(onlyFree(target)...)

	got := interconnect(devices, req(2), fitAvailable)
	if !reflect.DeepEqual(sortedCopy(got), sortedCopy(target)) {
		t.Errorf("interconnect(count=2) = %v, want %v (only this pair available)", got, target)
	}
}

func TestInterconnect_TwoDevices_ChoosesAmongMultipleCandidates(t *testing.T) {
	pairs := parseInterconnection()
	if len(pairs) < 2 {
		t.Skip("need at least 2 candidate pairs in parseInterconnection() for this test")
	}

	devices := newDevices() // all 8 free -> more than one valid pair available
	got := interconnect(devices, req(2), fitAvailable)
	if len(got) != 2 {
		t.Fatalf("interconnect(count=2) = %v, want length 2", got)
	}
	if got[0] == got[1] {
		t.Fatalf("interconnect(count=2) returned duplicate indices %v", got)
	}

	sortedGot := sortedCopy(got)
	found := false
	for _, pair := range pairs {
		if reflect.DeepEqual(sortedGot, sortedCopy(pair)) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("interconnect(count=2) = %v, not a recognized interconnected pair from parseInterconnection()", got)
	}
}

func TestInterconnect_FourDevices(t *testing.T) {
	groups := parseInterconnection2()
	if len(groups) == 0 {
		t.Fatal("parseInterconnection2() returned no groups to test against")
	}
	target := groups[0]

	// Only the target group is left free, so the count==4 path is forced
	// down a real selection instead of possibly short-circuiting to nil
	// (which happened with all 8 devices free, since len(have) was 8).
	devices := newDevices(onlyFree(target)...)

	got := interconnect(devices, req(4), fitAvailable)
	if !reflect.DeepEqual(sortedCopy(got), sortedCopy(target)) {
		t.Errorf("interconnect(count=4) = %v, want %v (only this group available)", got, target)
	}
}

func TestInterconnect_UnsupportedCount(t *testing.T) {
	devices := newDevices()
	got := interconnect(devices, req(3), fitAvailable)
	if len(got) != 0 {
		t.Errorf("interconnect(count=3) = %v, want empty (unsupported count)", got)
	}
}

func TestParseInterconnection_Structure(t *testing.T) {
	got := parseInterconnection()
	if len(got) == 0 {
		t.Fatal("parseInterconnection() returned no pairs")
	}
	for _, pair := range got {
		if len(pair) != 2 {
			t.Errorf("parseInterconnection() entry %v does not have exactly 2 elements", pair)
			continue
		}
		if pair[0] == pair[1] {
			t.Errorf("parseInterconnection() entry %v contains duplicate indices", pair)
		}
	}
}

func TestParseInterconnection2_Structure(t *testing.T) {
	got := parseInterconnection2()
	if len(got) == 0 {
		t.Fatal("parseInterconnection2() returned no groups")
	}
	for _, group := range got {
		if len(group) != 4 {
			t.Errorf("parseInterconnection2() entry %v does not have exactly 4 elements", group)
		}
		sorted := append([]int{}, group...)
		sort.Ints(sorted)
		for i := 1; i < len(sorted); i++ {
			if sorted[i] == sorted[i-1] {
				t.Errorf("parseInterconnection2() entry %v contains duplicate indices", group)
			}
		}
	}
}
