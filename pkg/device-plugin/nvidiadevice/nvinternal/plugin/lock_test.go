/*
 * Copyright (c) 2024, HAMi. All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package plugin

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func clearFile(t *testing.T, path string) {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("Failed to remove file: %v", err)
	}
}

func TestWatchLockFile(t *testing.T) {
	tests := []struct {
		name            string
		initiallyExists bool
		action          string
	}{
		{name: "FileNotExist"},
		{name: "FileCreate", action: "create"},
		{name: "FileRemove", initiallyExists: true, action: "remove"},
		{name: "FileExistInitially", initiallyExists: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFile := filepath.Join(t.TempDir(), "test.lock")
			if tt.initiallyExists {
				if err := os.WriteFile(testFile, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			// Register existing files before testing removal. On kqueue,
			// receiving Create does not mean the file watch is registered yet.
			sigChan, err := watchLockFile(testFile)
			if err != nil {
				t.Fatalf("WatchLockFile failed: %v", err)
			}
			switch tt.action {
			case "create":
				err = os.WriteFile(testFile, nil, 0600)
			case "remove":
				err = os.Remove(testFile)
			}
			if err != nil {
				t.Fatalf("%s lock file: %v", tt.action, err)
			}
			if tt.action == "" {
				select {
				case <-sigChan:
					t.Fatal("Unexpected signal without a file change")
				case <-time.After(100 * time.Millisecond):
				}
				return
			}
			select {
			case _, ok := <-sigChan:
				if !ok {
					t.Fatal("Watcher stopped before reporting the file change")
				}
				if exists := isMigApplyLockExist(testFile); exists != (tt.action == "create") {
					t.Fatalf("Unexpected lock state after %s: exists=%t", tt.action, exists)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("Timeout waiting for %s signal", tt.action)
			}
		})
	}
}

func TestCreateAndRemoveMigApplyLock(t *testing.T) {

	t.Run("CreateLock", func(t *testing.T) {
		err := CreateMigApplyLockDir()
		if err != nil {
			t.Errorf("CreateMigApplyLockDir failed: %v", err)
		}
		err = CreateMigApplyLock()
		if err != nil {
			t.Errorf("CreateMigApplyLock failed: %v", err)
		}

		if _, err = os.Stat(MigApplyLockFile); os.IsNotExist(err) {
			t.Error("Lock file was not created")
		}
	})

	t.Run("RemoveLock", func(t *testing.T) {
		defer clearFile(t, filepath.Dir(MigApplyLockFile))
		err := CreateMigApplyLockDir()
		if err != nil {
			t.Errorf("CreateMigApplyLockDir failed: %v", err)
		}
		f, err := os.Create(MigApplyLockFile)
		if err != nil {
			t.Fatalf("Create file failed: %v", err)
		}
		f.Close()

		err = RemoveMigApplyLock()
		if err != nil {
			t.Errorf("RemoveMigApplyLock failed: %v", err)
		}

		if _, err := os.Stat(MigApplyLockFile); !os.IsNotExist(err) {
			t.Error("Lock file was not removed")
		}
	})
}
