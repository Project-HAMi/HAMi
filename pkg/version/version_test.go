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

package version

import (
	"bytes"
	"fmt"
	"runtime"
	"testing"
)

func TestVersion(t *testing.T) {
	tests := []struct {
		name string
		info Info
		want string
	}{
		{
			name: "version string",
			info: Info{
				Version:   "2.8.0",
				Revision:  "5125fd664",
				BuildDate: "2026-01-11T13:09:22Z",
				GoVersion: "go1.25.3",
				Compiler:  "gc",
				Platform:  "linux/amd64",
			},
			want: `version.Info{Version:"2.8.0", Revision:"5125fd664", BuildDate:"2026-01-11T13:09:22Z", GoVersion:"go1.25.3", Compiler:"gc", Platform:"linux/amd64"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.info.String(); got != tt.want {
				t.Errorf("Info.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVersionPrint(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{
			name: "version print",
			want: `version:          v0.0.0-master
revision:         unknown
build date:       unknown
go version:       ` + runtime.Version() + `
compiler:         ` + runtime.Compiler + `
platform:         ` + fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Print(); got != tt.want {
				t.Errorf("Print() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVersionInfo(t *testing.T) {
	got := Version()

	if got.Version != version {
		t.Errorf("Version().Version = %q, want %q", got.Version, version)
	}
	if got.Revision != revision {
		t.Errorf("Version().Revision = %q, want %q", got.Revision, revision)
	}
	if got.BuildDate != buildDate {
		t.Errorf("Version().BuildDate = %q, want %q", got.BuildDate, buildDate)
	}
	if got.GoVersion != runtime.Version() {
		t.Errorf("Version().GoVersion = %q, want %q", got.GoVersion, runtime.Version())
	}
	if got.Compiler != runtime.Compiler {
		t.Errorf("Version().Compiler = %q, want %q", got.Compiler, runtime.Compiler)
	}

	wantPlatform := fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
	if got.Platform != wantPlatform {
		t.Errorf("Version().Platform = %q, want %q", got.Platform, wantPlatform)
	}
}

func TestVersionCmd(t *testing.T) {
	var buf bytes.Buffer
	VersionCmd.SetOut(&buf)

	VersionCmd.Run(VersionCmd, nil)

	want := Print() + "\n"
	if buf.String() != want {
		t.Errorf("VersionCmd output = %q, want %q", buf.String(), want)
	}
}
