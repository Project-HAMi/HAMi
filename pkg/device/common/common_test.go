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

func TestGenReason(t *testing.T) {
	tests := []struct {
		name     string
		reasons  map[string]int
		cards    int
		expected string
	}{
		{
			name:     "empty reasons",
			reasons:  map[string]int{},
			cards:    8,
			expected: "",
		},
		{
			name: "single reason",
			reasons: map[string]int{
				CardInsufficientMemory: 3,
			},
			cards:    8,
			expected: "3/8 CardInsufficientMemory",
		},
		{
			name: "multiple reasons sorted alphabetically",
			reasons: map[string]int{
				CardInsufficientMemory: 3,
				CardInsufficientCore:   2,
				CardNotHealth:          3,
			},
			cards:    8,
			expected: "2/8 CardInsufficientCore, 3/8 CardInsufficientMemory, 3/8 CardNotHealth",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenReason(tt.reasons, tt.cards)
			if result != tt.expected {
				t.Errorf("GenReason() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

func TestParseReason(t *testing.T) {
	tests := []struct {
		name              string
		reason            string
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
			name:              "empty reason string",
			reason:            "",
			expectedReasonMap: map[string]int{},
		},
		{
			name:              "malformed reason string",
			reason:            "invalid_reason_format",
			expectedReasonMap: map[string]int{},
		},
		{
			name:   "single reason format",
			reason: "4/4 CardTypeMismatch",
			expectedReasonMap: map[string]int{
				CardTypeMismatch: 4,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseReason(tt.reason)
			if !reflect.DeepEqual(result, tt.expectedReasonMap) {
				t.Errorf("ParseReason() = %v, expected %v", result, tt.expectedReasonMap)
			}
		})
	}
}

func TestGenAndParseReasonRoundTrip(t *testing.T) {
	originalReasons := map[string]int{
		CardInsufficientMemory: 5,
		CardInsufficientCore:   2,
	}
	cards := 8

	formatted := GenReason(originalReasons, cards)
	parsed := ParseReason(formatted)

	if !reflect.DeepEqual(parsed, originalReasons) {
		t.Errorf("Round-trip failed: original %v, parsed %v", originalReasons, parsed)
	}
}
