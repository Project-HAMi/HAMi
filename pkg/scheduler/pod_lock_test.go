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

package scheduler

import (
	"sync"
	"testing"
	"time"

	k8stypes "k8s.io/apimachinery/pkg/types"
)

func TestPodLockManager_LockUnlock(t *testing.T) {
	mgr := newPodLockManager()
	uid := k8stypes.UID("test-uid")

	// Verify locking does not block a single goroutine
	mgr.Lock(uid)
	mgr.Unlock(uid)

	// Verify concurrent access blocks and executes sequentially
	var wg sync.WaitGroup
	var order []int
	var mu sync.Mutex

	wg.Add(2)

	mgr.Lock(uid)
	go func() {
		defer wg.Done()
		mgr.Lock(uid)
		mu.Lock()
		order = append(order, 2)
		mu.Unlock()
		mgr.Unlock(uid)
	}()

	time.Sleep(50 * time.Millisecond) // Give the goroutine time to start and block
	mu.Lock()
	order = append(order, 1)
	mu.Unlock()
	mgr.Unlock(uid)

	go func() {
		defer wg.Done()
		mgr.Lock(uid)
		// Should succeed immediately because main locked and unlocked
		mgr.Unlock(uid)
	}()

	wg.Wait()

	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Errorf("Expected order [1, 2], got %v", order)
	}
}

func TestPodLockManager_MemoryLeakClean(t *testing.T) {
	mgr := newPodLockManager()
	uid := k8stypes.UID("test-uid")

	mgr.Lock(uid)
	if len(mgr.locks) != 1 {
		t.Errorf("Expected 1 lock in map, got %d", len(mgr.locks))
	}
	mgr.Unlock(uid)

	if len(mgr.locks) != 0 {
		t.Errorf("Expected 0 locks in map after unlock, got %d", len(mgr.locks))
	}
}
