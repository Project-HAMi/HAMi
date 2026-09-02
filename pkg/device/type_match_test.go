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

package device

import "testing"

func TestMatchesRequestType(t *testing.T) {
	tests := []struct {
		name        string
		deviceType  string
		requestType string
		want        bool
	}{
		{
			name:        "NVIDIA model name matches the vendor common word",
			deviceType:  "NVIDIA A100-SXM4-40GB",
			requestType: "NVIDIA",
			want:        true,
		},
		{
			name:        "Cambricon model name matches the vendor common word",
			deviceType:  "MLU370-X8",
			requestType: "MLU",
			want:        true,
		},
		{
			name:        "identical types still match",
			deviceType:  "Ascend910B",
			requestType: "Ascend910B",
			want:        true,
		},
		{
			name:        "match is case insensitive",
			deviceType:  "nvidia a100-sxm4-40gb",
			requestType: "NVIDIA",
			want:        true,
		},
		{
			name:        "unrelated vendor does not match",
			deviceType:  "NVIDIA A100-SXM4-40GB",
			requestType: "MLU",
			want:        false,
		},
		{
			name:        "a more specific request does not match a less specific device",
			deviceType:  "Ascend910B",
			requestType: "Ascend910B4",
			want:        false,
		},
		{
			name:        "empty request type matches nothing",
			deviceType:  "NVIDIA A100-SXM4-40GB",
			requestType: "",
			want:        false,
		},
		{
			name:        "blank request type matches nothing",
			deviceType:  "NVIDIA A100-SXM4-40GB",
			requestType: "   ",
			want:        false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchesRequestType(tt.deviceType, tt.requestType); got != tt.want {
				t.Errorf("MatchesRequestType(%q, %q) = %v, want %v", tt.deviceType, tt.requestType, got, tt.want)
			}
		})
	}
}
