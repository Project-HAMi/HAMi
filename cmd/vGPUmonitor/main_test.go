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

package main

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// initMetrics must fail when it cannot bind, rather than leaving the process
// running with nothing to scrape. It binds before touching the container
// lister, so a nil lister never gets dereferenced on this path.
func TestInitMetricsReturnsErrorWhenBindFails(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to occupy a port for the test: %v", err)
	}
	defer taken.Close()

	original := metricsBindAddress
	metricsBindAddress = taken.Addr().String()
	defer func() { metricsBindAddress = original }()

	if err := initMetrics(context.Background(), nil); err == nil {
		t.Fatalf("expected an error when %s is already in use", metricsBindAddress)
	}
}

// Must unblock on ctx cancel alone, even with the lock still held.
func TestWaitForLockRemoval_CtxCancelUnblocksWhileLockHeld(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sigChan := make(chan struct{})
	alwaysLocked := func() bool { return true }

	done := make(chan bool, 1)
	go func() {
		done <- waitForLockRemoval(ctx, sigChan, alwaysLocked)
	}()

	cancel()

	select {
	case restart := <-done:
		if restart {
			t.Fatal("waitForLockRemoval() = true after ctx cancellation, want false")
		}
	case <-time.After(time.Second):
		t.Fatal("waitForLockRemoval did not return after ctx cancellation")
	}
}

// Must unblock the same way on a closed sigChan.
func TestWaitForLockRemoval_ClosedChannelUnblocksWhileLockHeld(t *testing.T) {
	sigChan := make(chan struct{})
	close(sigChan)
	alwaysLocked := func() bool { return true }

	done := make(chan bool, 1)
	go func() {
		done <- waitForLockRemoval(context.Background(), sigChan, alwaysLocked)
	}()

	select {
	case restart := <-done:
		if restart {
			t.Fatal("waitForLockRemoval() = true after sigChan closed, want false")
		}
	case <-time.After(time.Second):
		t.Fatal("waitForLockRemoval did not return after sigChan closed")
	}
}

// Must return true once the lock clears.
func TestWaitForLockRemoval_ReturnsTrueOnceLockClears(t *testing.T) {
	var locked atomic.Bool
	locked.Store(true)
	lockExistFn := locked.Load
	sigChan := make(chan struct{}, 1)

	done := make(chan bool, 1)
	go func() {
		done <- waitForLockRemoval(context.Background(), sigChan, lockExistFn)
	}()

	locked.Store(false)
	sigChan <- struct{}{}

	select {
	case restart := <-done:
		if !restart {
			t.Fatal("waitForLockRemoval() = false once the lock cleared, want true")
		}
	case <-time.After(time.Second):
		t.Fatal("waitForLockRemoval did not return once the lock cleared")
	}
}
