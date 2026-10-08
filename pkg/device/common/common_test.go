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

package common

import (
	"reflect"
	"testing"
)

func TestParseReason(t *testing.T) {
	for _, ts := range []struct {
		name   string
		reason string

		expectedReasonMap map[string]int
	}{
		{
			name:   "base test",
			reason: "3/8 CardInsufficientMemory, 2/8 CardInsufficientCore, 3/8 CardNotHealth",

			expectedReasonMap: map[string]int{
				"CardInsufficientMemory": 3,
				"CardInsufficientCore":   2,
				"CardNotHealth":          3,
			},
		},
		{
			name:   "node-level rejection reported before any device backend runs",
			reason: GenReason(map[string]int{NodeInsufficientDevice: 1}, 4),

			expectedReasonMap: map[string]int{
				NodeInsufficientDevice: 1,
			},
		},
		{
			// fitInDevices reports internal failures as plain error text on the same
			// field. Parsing it would turn the UUID it carries into an event reason.
			name:   "free-form internal error is not a reason type",
			reason: "AddResourceUsage failed for device GPU-0fc3eda5: device is full",

			expectedReasonMap: map[string]int{},
		},
		{
			name:   "empty reason",
			reason: "",

			expectedReasonMap: map[string]int{},
		},
	} {
		t.Run(ts.name, func(t *testing.T) {
			result := ParseReason(ts.reason)
			if !reflect.DeepEqual(result, ts.expectedReasonMap) {
				t.Errorf("ParseReason failed: result %v, expected %v",
					result, ts.expectedReasonMap)
			}
		})
	}
}

func TestCordonedDevices(t *testing.T) {
	got := CordonedDevices(map[string]string{DeviceCordonAnnotation: " a, b ,,c"})
	if len(got) != 3 {
		t.Fatalf("cordoned = %v, want a, b, c", got)
	}
	for _, id := range []string{"a", "b", "c"} {
		if _, ok := got[id]; !ok {
			t.Fatalf("%s missing from %v", id, got)
		}
	}
	if len(CordonedDevices(nil)) != 0 || len(CordonedDevices(map[string]string{DeviceCordonAnnotation: ""})) != 0 {
		t.Fatal("no annotation or an empty one cordons nothing")
	}
}
