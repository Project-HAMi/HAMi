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
package watch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFiles_valid(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "test.txt")
	if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	w, err := Files(f)
	if err != nil {
		t.Fatalf("Files() error: %v", err)
	}
	defer w.Close()
}

func TestFiles_nonexistent(t *testing.T) {
	_, err := Files("/tmp/hami-test-nonexistent-file-12345")
	if err == nil {
		t.Fatal("Files() expected error for nonexistent path, got nil")
	}
}

func TestFiles_empty(t *testing.T) {
	w, err := Files()
	if err != nil {
		t.Fatalf("Files() error on empty input: %v", err)
	}
	defer w.Close()
}

func TestSignals(t *testing.T) {
	ch := Signals(os.Interrupt)
	if ch == nil {
		t.Fatal("Signals() returned nil channel")
	}
}
