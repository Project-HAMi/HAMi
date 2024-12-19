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
	"io"
	"os"
	"testing"

	"gotest.tools/v3/assert"
)

func TestVersion(t *testing.T) {
	version = "v1.0.0.1234567890"
	versionWant := "v1.0.0.1234567890\n"

	var out bytes.Buffer
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() failed: %v", err)
	}
	defer r.Close()
	originalStdout := os.Stdout
	defer func() {
		os.Stdout = originalStdout
		w.Close()
	}()
	os.Stdout = w

	VersionCmd.Run(nil, nil)
	w.Close()

	io.Copy(&out, r)

	versionGet := out.String()
	assert.Equal(t, versionWant, versionGet)
}
