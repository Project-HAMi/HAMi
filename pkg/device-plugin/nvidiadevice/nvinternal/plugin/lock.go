/*
 * Copyright (c) 2024, HAMi.  All rights reserved.
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

	"github.com/fsnotify/fsnotify"
	"k8s.io/klog/v2"
)

const (
	MigApplyLockFile = "/tmp/hami/hami-mig-apply.lock"
)

// CreateMigApplyLockDir creates the lock directory for MIG apply operation
func CreateMigApplyLockDir() error {
	return createMigApplyLockDir(MigApplyLockFile)
}
func createMigApplyLockDir(file string) error {
	dir := filepath.Dir(file)
	_, err := os.Stat(dir)
	if os.IsNotExist(err) {
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			klog.Errorf("Failed to create MIG apply lock directory: %v", err)
			return err
		}
		return nil
	}
	if err != nil {
		klog.Errorf("Failed to check MIG apply lock directory: %v", err)
		return err
	}
	klog.Info("MIG apply lock directory already exists")
	return nil
}

// CreateMigApplyLock creates the lock file for MIG apply operation
func CreateMigApplyLock() error {
	return createMigApplyLock(MigApplyLockFile)
}
func createMigApplyLock(file string) error {
	// Check if the lock file already exists
	if _, err := os.Stat(file); err == nil {
		klog.Infof("MIG apply lock file already exists: %s", MigApplyLockFile)
		return nil
	}
	f, err := os.Create(file)
	if err != nil {
		klog.Errorf("Failed to create MIG apply lock file: %v", err)
		return err
	}
	return f.Close()
}

// RemoveMigApplyLock removes the lock file for MIG apply operation
func RemoveMigApplyLock() error {
	return removeMigApplyLock(MigApplyLockFile)
}

func removeMigApplyLock(file string) error {
	err := os.Remove(file)
	if err != nil && !os.IsNotExist(err) {
		klog.Errorf("Failed to remove MIG apply lock file: %v", err)
		return err
	}
	return nil
}

// IsMigApplyLockExist reports whether the lock file is currently present.
// Callers must re-check this on every WatchLockFile wake-up.
func IsMigApplyLockExist() bool {
	return isMigApplyLockExist(MigApplyLockFile)
}

func isMigApplyLockExist(file string) bool {
	_, err := os.Stat(file)
	return err == nil
}

func WatchLockFile() (chan struct{}, error) {
	return watchLockFile(MigApplyLockFile)
}

// watchLockFile wakes on every create/remove of file and closes the channel
// when the watcher stops. It carries no payload; call IsMigApplyLockExist to
// learn the current state after each wake-up.
func watchLockFile(file string) (chan struct{}, error) {
	sigChan := make(chan struct{}, 1)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(file)
	if err := watcher.Add(dir); err != nil {
		watcher.Close()
		return nil, err
	}

	go func() {
		defer watcher.Close()
		defer close(sigChan)
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Name == file && (event.Has(fsnotify.Create) || event.Has(fsnotify.Remove)) {
					klog.V(4).Infof("MIG apply lock file state may have changed: %s", event.Name)
					select {
					case sigChan <- struct{}{}:
					default:
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				klog.Errorf("File watch error: %v", err)
			}
		}
	}()

	return sigChan, nil
}
