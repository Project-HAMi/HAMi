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

package metax

import (
	"strings"
	"testing"
)

func TestLinkDeviceScore(t *testing.T) {
	for _, ts := range []struct {
		name     string
		from     *LinkDevice
		to       *LinkDevice
		expected int
	}{
		{
			name:     "same uuid returns zero",
			from:     &LinkDevice{uuid: "GPU-0", linkZone: 1},
			to:       &LinkDevice{uuid: "GPU-0", linkZone: 1},
			expected: 0,
		},
		{
			name:     "from linkZone zero returns zero",
			from:     &LinkDevice{uuid: "GPU-0", linkZone: 0},
			to:       &LinkDevice{uuid: "GPU-1", linkZone: 1},
			expected: 0,
		},
		{
			name:     "to linkZone zero returns zero",
			from:     &LinkDevice{uuid: "GPU-0", linkZone: 1},
			to:       &LinkDevice{uuid: "GPU-1", linkZone: 0},
			expected: 0,
		},
		{
			name:     "both linkZone zero returns zero",
			from:     &LinkDevice{uuid: "GPU-0", linkZone: 0},
			to:       &LinkDevice{uuid: "GPU-1", linkZone: 0},
			expected: 0,
		},
		{
			name:     "same linkZone returns direct link score",
			from:     &LinkDevice{uuid: "GPU-0", linkZone: 1},
			to:       &LinkDevice{uuid: "GPU-1", linkZone: 1},
			expected: DirectLinkScore,
		},
		{
			name:     "different linkZone returns zero",
			from:     &LinkDevice{uuid: "GPU-0", linkZone: 1},
			to:       &LinkDevice{uuid: "GPU-1", linkZone: 2},
			expected: 0,
		},
	} {
		t.Run(ts.name, func(t *testing.T) {
			result := ts.from.score(ts.to)
			if result != ts.expected {
				t.Errorf("score() failed: result %v, expected %v", result, ts.expected)
			}
		})
	}
}

func TestLinkDevicesScore(t *testing.T) {
	for _, ts := range []struct {
		name     string
		devs     LinkDevices
		expected int
	}{
		{
			name:     "empty devices",
			devs:     LinkDevices{},
			expected: 0,
		},
		{
			name: "single device",
			devs: LinkDevices{
				{uuid: "GPU-0", linkZone: 1},
			},
			expected: 0,
		},
		{
			name: "two devices same zone",
			devs: LinkDevices{
				{uuid: "GPU-0", linkZone: 1},
				{uuid: "GPU-1", linkZone: 1},
			},
			expected: DirectLinkScore,
		},
		{
			name: "two devices different zone",
			devs: LinkDevices{
				{uuid: "GPU-0", linkZone: 1},
				{uuid: "GPU-1", linkZone: 2},
			},
			expected: 0,
		},
		{
			name: "three devices all same zone sums pairwise",
			devs: LinkDevices{
				{uuid: "GPU-0", linkZone: 1},
				{uuid: "GPU-1", linkZone: 1},
				{uuid: "GPU-2", linkZone: 1},
			},

			expected: 3 * DirectLinkScore,
		},
		{
			name: "mixed zones only matching pairs score",
			devs: LinkDevices{
				{uuid: "GPU-0", linkZone: 1},
				{uuid: "GPU-1", linkZone: 1},
				{uuid: "GPU-2", linkZone: 2},
			},

			expected: DirectLinkScore,
		},
		{
			name: "devices with zero linkZone never score",
			devs: LinkDevices{
				{uuid: "GPU-0", linkZone: 0},
				{uuid: "GPU-1", linkZone: 0},
			},
			expected: 0,
		},
	} {
		t.Run(ts.name, func(t *testing.T) {
			result := ts.devs.Score()
			if result != ts.expected {
				t.Errorf("Score() failed: result %v, expected %v", result, ts.expected)
			}
		})
	}
}

func TestLinkDevicesString(t *testing.T) {
	for _, ts := range []struct {
		name     string
		devs     LinkDevices
		expected string
	}{
		{
			name:     "empty devices",
			devs:     LinkDevices{},
			expected: "[]",
		},
		{
			name: "non-empty devices produce bracketed output",
			devs: LinkDevices{
				{uuid: "GPU-0", linkZone: 1},
				{uuid: "GPU-1", linkZone: 2},
			},
			expected: "",
		},
	} {
		t.Run(ts.name, func(t *testing.T) {
			result := ts.devs.String()
			if !strings.HasPrefix(result, "[") || !strings.HasSuffix(result, "]") {
				t.Errorf("String() failed: result %q is not bracketed", result)
			}
			if ts.name == "empty devices" && result != ts.expected {
				t.Errorf("String() failed: result %v, expected %v", result, ts.expected)
			}
			if ts.name == "non-empty devices produce bracketed output" {
				if !strings.Contains(result, "GPU-0") || !strings.Contains(result, "GPU-1") {
					t.Errorf("String() failed: result %q does not contain expected uuids", result)
				}
			}
		})
	}
}
