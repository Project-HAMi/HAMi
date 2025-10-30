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
