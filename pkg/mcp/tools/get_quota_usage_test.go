/*
Copyright 2024 The HAMi Authors.

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

package tools

import "testing"

func TestNvidiaMemResourceNameFor_Known(t *testing.T) {
	got := nvidiaMemResourceNameFor("NVIDIA")
	if got != "nvidia.com/gpumem" {
		t.Errorf("nvidiaMemResourceNameFor(NVIDIA) = %q, want nvidia.com/gpumem", got)
	}
}

func TestNvidiaCoreResourceNameFor_Known(t *testing.T) {
	got := nvidiaCoreResourceNameFor("NVIDIA")
	if got != "nvidia.com/gpucores" {
		t.Errorf("nvidiaCoreResourceNameFor(NVIDIA) = %q, want nvidia.com/gpucores", got)
	}
}

func TestNvidiaMemResourceNameFor_Unknown(t *testing.T) {
	got := nvidiaMemResourceNameFor("SOME-FUTURE-VENDOR")
	want := "SOME-FUTURE-VENDOR/unknown-memory"
	if got != want {
		t.Errorf("nvidiaMemResourceNameFor(unknown) = %q, want %q", got, want)
	}
}
