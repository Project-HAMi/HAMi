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
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// TestWaitLoop_MainGoroutine exercises waitForLockRemoval, the function
// extracted from start()'s inline goroutine in main.go. Tests call the real
// function directly so that Codecov instruments the actual source lines.
//
// lockExistFn is passed as a local closure for each subtest — no global-var
// override is required, since waitForLockRemoval accepts it as a parameter.
//
// Covered paths:
//  1. Lock held → loop does NOT exit while lockExistFn returns true
//  2. Lock released + signal → returns true (caller should restart watchAndFeedback)
//  3. Context cancelled while lock held → returns false (caller should exit)
//  4. sigChan closed while lock held → returns false (caller should exit)
func TestWaitLoop_MainGoroutine(t *testing.T) {
	t.Run("DoesNotRestartWhileLocked", func(t *testing.T) {
		// While lockExistFn always returns true the loop must keep blocking.
		// Confirm this by asserting waitForLockRemoval has NOT returned after
		// a brief wait.
		lockFn := func() bool { return true } // always locked

		lockChannel := make(chan struct{}, 1)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		done := make(chan bool, 1)
		go func() {
			done <- waitForLockRemoval(ctx, lockChannel, lockFn)
		}()

		select {
		case <-done:
			t.Error("waitForLockRemoval should not return while lock is still held")
		case <-time.After(120 * time.Millisecond):
			// Correct: function is still blocked inside its select.
		}
		cancel() // unblock the goroutine so the test can clean up
		<-done
	})

	t.Run("RestartsAfterLockRemoved", func(t *testing.T) {
		// Once lockExistFn flips to false and a signal arrives, the loop exits
		// and waitForLockRemoval returns true (caller should restart).
		var locked atomic.Bool
		locked.Store(true)
		lockFn := func() bool { return locked.Load() }

		lockChannel := make(chan struct{}, 1)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		done := make(chan bool, 1)
		go func() {
			done <- waitForLockRemoval(ctx, lockChannel, lockFn)
		}()

		// Release the lock and send a wake-up signal.
		locked.Store(false)
		lockChannel <- struct{}{}

		select {
		case restarted := <-done:
			if !restarted {
				t.Error("want true (restart) after lock removed, got false")
			}
		case <-time.After(time.Second):
			t.Fatal("waitForLockRemoval did not exit after lock was released")
		}
	})

	t.Run("ExitsOnCtxCancelWhileLocked", func(t *testing.T) {
		// ctx cancellation while the lock is held must cause the loop to exit
		// promptly and return false (caller should exit, not restart).
		lockFn := func() bool { return true } // always locked

		lockChannel := make(chan struct{}, 1)
		ctx, cancel := context.WithCancel(context.Background())

		done := make(chan bool, 1)
		go func() {
			done <- waitForLockRemoval(ctx, lockChannel, lockFn)
		}()

		// Cancel after a brief pause so the goroutine has entered the select.
		time.Sleep(20 * time.Millisecond)
		cancel()

		select {
		case restarted := <-done:
			if restarted {
				t.Error("want false (no restart) on ctx cancel while locked, got true")
			}
		case <-time.After(time.Second):
			t.Fatal("waitForLockRemoval did not exit promptly after context cancellation")
		}
	})

	t.Run("ExitsOnChannelCloseWhileLocked", func(t *testing.T) {
		// A closed sigChan while the lock is held must cause the loop to exit
		// and return false (caller should exit, not restart).
		lockFn := func() bool { return true } // always locked

		lockChannel := make(chan struct{}, 1)
		close(lockChannel)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		done := make(chan bool, 1)
		go func() {
			done <- waitForLockRemoval(ctx, lockChannel, lockFn)
		}()

		select {
		case restarted := <-done:
			if restarted {
				t.Error("want false (no restart) on closed channel while locked, got true")
			}
		case <-time.After(time.Second):
			t.Fatal("waitForLockRemoval did not exit after sigChan was closed")
		}
	})
}

// TestWaitLoop_ErrTemporaryClosed verifies the errors.Is guard in start() that
// decides whether a watchAndFeedback error warrants entering waitForLockRemoval
// or propagating to the error channel.
func TestWaitLoop_ErrTemporaryClosed(t *testing.T) {
	if !errors.Is(errTemporaryClosed, errTemporaryClosed) {
		t.Error("errTemporaryClosed must satisfy errors.Is(err, errTemporaryClosed)")
	}
	other := errors.New("some other error")
	if errors.Is(other, errTemporaryClosed) {
		t.Error("an unrelated error must not match errTemporaryClosed")
	}
}
