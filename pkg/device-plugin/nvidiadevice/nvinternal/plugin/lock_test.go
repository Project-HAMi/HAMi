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
	"sync"
	"testing"
	"time"

	"k8s.io/klog/v2"
)

func clearFile(t *testing.T, path string) {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("Failed to remove file: %v", err)
	}
}

func TestWatchLockFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.lock")

	t.Run("FileNotExist", func(t *testing.T) {
		sigChan, err := watchLockFile(testFile)
		if err != nil {
			t.Fatalf("WatchLockFile failed: %v", err)
		}
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-sigChan:
					t.Error("Unexpected signal when file not exist")
					return
				case <-time.After(100 * time.Millisecond):
					return
				}
			}
		}()
		wg.Wait()
	})

	t.Run("FileCreate", func(t *testing.T) {
		defer clearFile(t, testFile)
		sigChan, err := watchLockFile(testFile)
		if err != nil {
			t.Fatalf("WatchLockFile failed: %v", err)
		}

		f, err := os.Create(testFile)
		if err != nil {
			t.Fatalf("Create file failed: %v", err)
		}
		f.Close()
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case status := <-sigChan:
					klog.Infof("Received signal %v", status)
					return
				case <-time.After(time.Second):
					t.Error("Timeout waiting for create signal")
					return
				}
			}
		}()
		wg.Wait()
	})

	t.Run("FileRemove", func(t *testing.T) {
		sigChan, err := watchLockFile(testFile)
		if err != nil {
			t.Fatalf("WatchLockFile failed: %v", err)
		}

		if _, err := os.Stat(testFile); os.IsNotExist(err) {
			f, err := os.Create(testFile)
			if err != nil {
				t.Fatalf("Create file failed: %v", err)
			}
			f.Close()
			<-sigChan
		}

		err = os.Remove(testFile)
		if err != nil {
			t.Fatalf("Remove file failed: %v", err)
		}
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case status := <-sigChan:
					klog.Infof("Received signal %v", status)
					return
				case <-time.After(time.Second):
					t.Error("Timeout waiting for remove signal")
					return
				}
			}
		}()
		wg.Wait()
	})

	t.Run("FileExistInitially", func(t *testing.T) {
		defer clearFile(t, testFile)
		f, err := os.Create(testFile)
		if err != nil {
			t.Fatalf("Create file failed: %v", err)
		}
		f.Close()

		sigChan, err := watchLockFile(testFile)
		if err != nil {
			t.Fatalf("WatchLockFile failed: %v", err)
		}
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-sigChan:
					t.Error("Unexpected signal when file exist")
					return
				case <-time.After(time.Second):
					return
				}
			}
		}()
		wg.Wait()
	})
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
