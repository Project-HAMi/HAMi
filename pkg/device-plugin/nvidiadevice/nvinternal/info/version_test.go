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

package info

import (
	"strings"
	"testing"
)

func TestGetVersion(t *testing.T) {
	original := version
	defer func() { version = original }()

	version = "v2.5.0"
	if got := GetVersion(); got != "v2.5.0" {
		t.Errorf("GetVersion() = %q, want %q", got, "v2.5.0")
	}
}

func TestGetVersion_default(t *testing.T) {
	original := version
	defer func() { version = original }()

	version = "unknown"
	if got := GetVersion(); got != "unknown" {
		t.Errorf("GetVersion() = %q, want %q", got, "unknown")
	}
}

func TestGetVersionParts_withCommit(t *testing.T) {
	origVersion := version
	origCommit := gitCommit
	defer func() {
		version = origVersion
		gitCommit = origCommit
	}()

	version = "v2.5.0"
	gitCommit = "abc123"

	parts := GetVersionParts()
	if len(parts) != 2 {
		t.Fatalf("GetVersionParts() returned %d parts, want 2", len(parts))
	}
	if parts[0] != "v2.5.0" {
		t.Errorf("parts[0] = %q, want %q", parts[0], "v2.5.0")
	}
	if parts[1] != "commit: abc123" {
		t.Errorf("parts[1] = %q, want %q", parts[1], "commit: abc123")
	}
}

func TestGetVersionParts_noCommit(t *testing.T) {
	origVersion := version
	origCommit := gitCommit
	defer func() {
		version = origVersion
		gitCommit = origCommit
	}()

	version = "v2.5.0"
	gitCommit = ""

	parts := GetVersionParts()
	if len(parts) != 1 {
		t.Fatalf("GetVersionParts() returned %d parts, want 1", len(parts))
	}
	if parts[0] != "v2.5.0" {
		t.Errorf("parts[0] = %q, want %q", parts[0], "v2.5.0")
	}
}

func TestGetVersionString_basic(t *testing.T) {
	origVersion := version
	origCommit := gitCommit
	defer func() {
		version = origVersion
		gitCommit = origCommit
	}()

	version = "v2.5.0"
	gitCommit = "abc123"

	got := GetVersionString()
	if !strings.Contains(got, "v2.5.0") {
		t.Errorf("GetVersionString() missing version: %q", got)
	}
	if !strings.Contains(got, "abc123") {
		t.Errorf("GetVersionString() missing commit: %q", got)
	}
}

func TestGetVersionString_withExtra(t *testing.T) {
	origVersion := version
	origCommit := gitCommit
	defer func() {
		version = origVersion
		gitCommit = origCommit
	}()

	version = "v2.5.0"
	gitCommit = ""

	got := GetVersionString("go1.25", "linux/amd64")
	parts := strings.Split(got, "\n")
	if len(parts) != 3 {
		t.Fatalf("GetVersionString() returned %d lines, want 3, got: %q", len(parts), got)
	}
	if parts[0] != "v2.5.0" {
		t.Errorf("parts[0] = %q, want %q", parts[0], "v2.5.0")
	}
	if parts[1] != "go1.25" {
		t.Errorf("parts[1] = %q, want %q", parts[1], "go1.25")
	}
	if parts[2] != "linux/amd64" {
		t.Errorf("parts[2] = %q, want %q", parts[2], "linux/amd64")
	}
}
