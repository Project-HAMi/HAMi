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

package e2e

import "testing"

func TestParseAllocation(t *testing.T) {
	got, err := parseAllocation("u1,AMDGPU,1000,25:u2,AMDGPU,2048,50:;u3,AMDGPU,1,1:;")
	if err != nil {
		t.Fatal(err)
	}
	want := []allocated{{"u1", 1000, 25}, {"u2", 2048, 50}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v, want %v (first container only)", got, want)
	}
	if _, err := parseAllocation("u1,AMDGPU,1000"); err == nil {
		t.Fatal("a device with fewer than four fields must be an error")
	}
	if got, err := parseAllocation(""); err != nil || len(got) != 0 {
		t.Fatalf("empty annotation: %v, %v", got, err)
	}
}
