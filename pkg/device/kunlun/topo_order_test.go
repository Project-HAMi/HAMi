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

package kunlun

import (
	"reflect"
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

// The scheduler sorts the device slice by score before Fit runs, so
// graghSelect cannot assume devices arrive in index order. With XPU 7 used
// and the slice rotated to score order, position based wing math previously
// picked {3,4,5,6}, a set that crosses both wings and is not a valid
// interconnect group.
func TestGraghSelect_ScoreSortedInputMatchesIndexOrder(t *testing.T) {
	ordered := newDevices(7)
	want := graghSelect(ordered, req(4), fitAvailable)

	scrambled := []*device.DeviceUsage{
		ordered[7], ordered[0], ordered[1], ordered[2],
		ordered[3], ordered[4], ordered[5], ordered[6],
	}
	got := graghSelect(scrambled, req(4), fitAvailable)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selection depends on slice order: got %v from score sorted input, want %v", got, want)
	}
	if !reflect.DeepEqual(want, []int{0, 1, 2, 3}) {
		t.Fatalf("expected the free left wing {0,1,2,3}, got %v", want)
	}
}

// The caller's slice must not be reordered as a side effect.
func TestGraghSelect_DoesNotMutateCallerSlice(t *testing.T) {
	ordered := newDevices()
	scrambled := []*device.DeviceUsage{
		ordered[4], ordered[5], ordered[6], ordered[7],
		ordered[0], ordered[1], ordered[2], ordered[3],
	}
	snapshot := make([]*device.DeviceUsage, len(scrambled))
	copy(snapshot, scrambled)

	graghSelect(scrambled, req(2), fitAvailable)

	if !reflect.DeepEqual(scrambled, snapshot) {
		t.Fatal("graghSelect reordered the caller's slice")
	}
}
