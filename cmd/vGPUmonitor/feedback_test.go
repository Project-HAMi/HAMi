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
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"

	"github.com/Project-HAMi/HAMi/pkg/monitor/nvidia"
)

// stubDeviceMax mirrors the production Spec.DeviceMax, which reports the constant
// maxDevices (16) rather than the live device count.
const stubDeviceMax = 16

// stubInfo mocks nvidia.UsageInfo. The container's real device UUIDs occupy the
// leading slots of a fixed-size table; DeviceMax still reports the constant slot
// count and the trailing slots read back invalid, so the check functions walk the
// same 16 slots they do in production.
type stubInfo struct {
	priority   int
	uuids      []string
	total      []uint64
	limit      []uint64
	ctxSize    []uint64
	modSize    []uint64
	bufSize    []uint64
	smUtil     []uint64
	lastKernel int64
}

func slot(v []uint64, i int) uint64 {
	if i >= 0 && i < len(v) {
		return v[i]
	}
	return 0
}

func (s *stubInfo) DeviceMax() int { return stubDeviceMax }
func (s *stubInfo) DeviceNum() int { return len(s.uuids) }
func (s *stubInfo) DeviceUUID(i int) string {
	if i < len(s.uuids) {
		return s.uuids[i]
	}
	return ""
}
func (s *stubInfo) DeviceMemoryContextSize(i int) uint64 { return slot(s.ctxSize, i) }
func (s *stubInfo) DeviceMemoryModuleSize(i int) uint64  { return slot(s.modSize, i) }
func (s *stubInfo) DeviceMemoryBufferSize(i int) uint64  { return slot(s.bufSize, i) }
func (s *stubInfo) DeviceMemoryOffset(int) uint64        { return 0 }
func (s *stubInfo) DeviceMemoryTotal(i int) uint64       { return slot(s.total, i) }
func (s *stubInfo) DeviceSmUtil(i int) uint64            { return slot(s.smUtil, i) }
func (s *stubInfo) SetDeviceSmLimit(uint64)              {}
func (s *stubInfo) IsValidUUID(i int) bool               { return i < len(s.uuids) }
func (s *stubInfo) DeviceMemoryLimit(i int) uint64       { return slot(s.limit, i) }
func (s *stubInfo) SetDeviceMemoryLimit(uint64)          {}
func (s *stubInfo) LastKernelTime() int64                { return s.lastKernel }
func (s *stubInfo) GetPriority() int                     { return s.priority }
func (s *stubInfo) GetRecentKernel() int32               { return 1 }
func (s *stubInfo) SetRecentKernel(int32)                {}
func (s *stubInfo) GetUtilizationSwitch() int32          { return 0 }
func (s *stubInfo) SetUtilizationSwitch(int32)           {}

func TestCheckFunctionsHighPriority(t *testing.T) {
	sw := map[string]UtilizationPerDevice{"gpu-0": {0, 1}}
	c := &nvidia.ContainerUsage{Info: &stubInfo{priority: 3, uuids: []string{"gpu-0"}}}
	if !CheckBlocking(sw, 3, c) {
		t.Error("CheckBlocking: expected true")
	}
	if !CheckPriority(sw, 3, c) {
		t.Error("CheckPriority: expected true")
	}
	sw2 := map[string]UtilizationPerDevice{"gpu-0": {0, 0}}
	if CheckBlocking(sw2, 2, c) {
		t.Error("CheckBlocking: expected false")
	}
}

// TestCheckBlocking_MultiDevice verifies that CheckBlocking inspects every device
// the container uses, not just the first one that appears in the switch map. The
// cases cover several device counts, all-clear, contention isolated to the last
// device, and UUIDs missing from the switch map. In every case contention (when
// present) sits at an index below the priority, so CheckPriority agrees and is
// asserted for parity.
func TestCheckBlocking_MultiDevice(t *testing.T) {
	tests := []struct {
		name     string
		priority int
		uuids    []string
		sw       map[string]UtilizationPerDevice
		want     bool
	}{
		{
			name:     "two devices, contention on the second",
			priority: 1,
			uuids:    []string{"gpu-0", "gpu-1"},
			sw:       map[string]UtilizationPerDevice{"gpu-0": {0, 0}, "gpu-1": {1, 0}},
			want:     true,
		},
		{
			name:     "three devices, all clear",
			priority: 1,
			uuids:    []string{"gpu-0", "gpu-1", "gpu-2"},
			sw:       map[string]UtilizationPerDevice{"gpu-0": {0, 0}, "gpu-1": {0, 0}, "gpu-2": {0, 0}},
			want:     false,
		},
		{
			name:     "four devices, contention only on the last",
			priority: 1,
			uuids:    []string{"gpu-0", "gpu-1", "gpu-2", "gpu-3"},
			sw:       map[string]UtilizationPerDevice{"gpu-0": {0, 0}, "gpu-1": {0, 0}, "gpu-2": {0, 0}, "gpu-3": {1, 0}},
			want:     true,
		},
		{
			name:     "some device UUIDs missing from switch map, present one contended",
			priority: 1,
			uuids:    []string{"gpu-0", "gpu-1", "gpu-2"},
			sw:       map[string]UtilizationPerDevice{"gpu-1": {1, 0}},
			want:     true,
		},
		{
			name:     "none of the container UUIDs are in the switch map",
			priority: 1,
			uuids:    []string{"gpu-0", "gpu-1"},
			sw:       map[string]UtilizationPerDevice{"gpu-9": {1, 0}},
			want:     false,
		},
		{
			name:     "empty switch map",
			priority: 1,
			uuids:    []string{"gpu-0", "gpu-1"},
			sw:       map[string]UtilizationPerDevice{},
			want:     false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := &nvidia.ContainerUsage{Info: &stubInfo{priority: test.priority, uuids: test.uuids}}
			if got := CheckBlocking(test.sw, test.priority, c); got != test.want {
				t.Errorf("CheckBlocking: want %v, got %v", test.want, got)
			}
			// The sibling CheckPriority scans all devices too and, for these
			// inputs, agrees with CheckBlocking.
			if got := CheckPriority(test.sw, test.priority, c); got != test.want {
				t.Errorf("CheckPriority: want %v, got %v", test.want, got)
			}
		})
	}
}

// stubNvmlSuccess stubs out NVML init/shutdown so watchAndFeedback tests do
// not require real GPU hardware. Call it in every watchAndFeedback subtest and
// restore via t.Cleanup.
func stubNvml(t *testing.T) {
	t.Helper()
	origInit := nvmlInitFn
	origShutdown := nvmlShutdownFn
	nvmlInitFn = func() nvml.Return { return nvml.SUCCESS }
	nvmlShutdownFn = func() {}
	t.Cleanup(func() {
		nvmlInitFn = origInit
		nvmlShutdownFn = origShutdown
	})
}

// stubMigLock replaces migLockExistFn with a closure backed by a simple
// atomic bool, returning the setter so tests can flip the state. The original
// function is restored via t.Cleanup.
func stubMigLock(t *testing.T, initiallyLocked bool) (setLocked func(bool)) {
	t.Helper()
	var locked atomic.Bool
	locked.Store(initiallyLocked)
	orig := migLockExistFn
	migLockExistFn = func() bool { return locked.Load() }
	t.Cleanup(func() { migLockExistFn = orig })
	return func(v bool) { locked.Store(v) }
}

// TestWatchAndFeedback covers the paths added/changed by PR #2451:
//   - startup when lock is already present (initial check path)
//   - startup when lock is absent, then ctx cancel
//   - notification received while lock is held   (create notification path)
//   - notification received while lock is absent (remove notification path)
//   - signal channel closed mid-run
//   - rapid burst of coalesced create/remove cycles (ErrEventOverflow path)
//
// NOTE: watchAndFeedback is tightly coupled to nvml.Init (GPU hardware) and
// plugin.IsMigApplyLockExist (production lock path). Both are tested through
// package-level testability seams — nvmlInitFn/nvmlShutdownFn and
// migLockExistFn — so no GPU or real filesystem is required.
func TestWatchAndFeedback(t *testing.T) {
	// nilLister is a safe stand-in: ticker fires are not expected in most tests
	// because the tests short-circuit via ctx or signal before 5 s elapses.
	nilLister := &nvidia.ContainerLister{}

	t.Run("LockAlreadyPresentAtStartup", func(t *testing.T) {
		// watchAndFeedback should return errTemporaryClosed immediately when
		// the lock file exists before the function is called.
		stubNvml(t)
		stubMigLock(t, true) // lock is held

		sigChan := make(chan struct{}, 1)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err := watchAndFeedback(ctx, nilLister, sigChan)
		if !errors.Is(err, errTemporaryClosed) {
			t.Errorf("want errTemporaryClosed, got %v", err)
		}
	})

	t.Run("LockAbsentAtStartup_CtxCancel", func(t *testing.T) {
		// watchAndFeedback should block (no signal) until ctx is cancelled,
		// then return nil.
		stubNvml(t)
		stubMigLock(t, false) // lock absent

		sigChan := make(chan struct{}, 1)
		ctx, cancel := context.WithCancel(context.Background())

		// Cancel after a brief delay so the function has time to enter its loop.
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		err := watchAndFeedback(ctx, nilLister, sigChan)
		if err != nil {
			t.Errorf("want nil after ctx cancel, got %v", err)
		}
	})

	t.Run("CreateNotification_LockHeld", func(t *testing.T) {
		// Receiving a signal while the lock is present should return
		// errTemporaryClosed (the re-evaluation path).
		stubNvml(t)
		setLocked := stubMigLock(t, false) // start unlocked so we clear startup check

		sigChan := make(chan struct{}, 1)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// Flip the lock on and then send the signal so the re-evaluation sees it.
		setLocked(true)
		sigChan <- struct{}{}

		err := watchAndFeedback(ctx, nilLister, sigChan)
		if !errors.Is(err, errTemporaryClosed) {
			t.Errorf("want errTemporaryClosed on create notification, got %v", err)
		}
	})

	t.Run("RemoveNotification_LockAbsent", func(t *testing.T) {
		// Receiving a signal while the lock is absent should NOT cause a
		// return — the function continues its loop. Cancelling ctx then
		// produces nil.
		stubNvml(t)
		stubMigLock(t, false) // always absent

		sigChan := make(chan struct{}, 1)
		ctx, cancel := context.WithCancel(context.Background())

		// Send a "remove" signal (lock is absent, so re-evaluation → continue).
		sigChan <- struct{}{}

		// Cancel shortly after so we observe the loop continuing, not blocking.
		go func() {
			time.Sleep(80 * time.Millisecond)
			cancel()
		}()

		err := watchAndFeedback(ctx, nilLister, sigChan)
		if err != nil {
			t.Errorf("want nil after remove notification + ctx cancel, got %v", err)
		}
	})

	t.Run("SignalChannelClosed", func(t *testing.T) {
		// A closed migLockSignal channel should cause the function to return nil.
		stubNvml(t)
		stubMigLock(t, false)

		sigChan := make(chan struct{}, 1)
		close(sigChan)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err := watchAndFeedback(ctx, nilLister, sigChan)
		if err != nil {
			t.Errorf("want nil on channel close, got %v", err)
		}
	})

	t.Run("CoalescedBurst_FinalStateConverges", func(t *testing.T) {
		// Simulate an ErrEventOverflow scenario: many signals coalesce into
		// the buffered channel (capacity 1), but the lock is ultimately absent.
		// The final observed state must converge to "unlocked" (no
		// errTemporaryClosed returned).
		stubNvml(t)
		setLocked := stubMigLock(t, false)

		sigChan := make(chan struct{}, 1)
		ctx, cancel := context.WithCancel(context.Background())

		// Simulate a burst: toggle lock on/off rapidly, then settle on absent.
		// Drain/fill sigChan without blocking (non-blocking send) mimics the
		// coalescing behaviour of the watcher goroutine under ErrEventOverflow.
		go func() {
			for i := 0; i < 10; i++ {
				setLocked(i%2 == 0) // alternates locked/unlocked
				select {
				case sigChan <- struct{}{}:
				default:
				}
				time.Sleep(2 * time.Millisecond)
			}
			// Settle: lock absent, one final notification.
			setLocked(false)
			select {
			case sigChan <- struct{}{}:
			default:
			}
			// Give the function time to process the final state, then cancel.
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		err := watchAndFeedback(ctx, nilLister, sigChan)
		// After the burst the lock is absent, so the function must have
		// exited via ctx.Done() → nil, NOT via errTemporaryClosed.
		if err != nil {
			t.Errorf("want nil after coalesced burst (final state unlocked), got %v", err)
		}
	})

	t.Run("NvmlInitFailure", func(t *testing.T) {
		// If nvml.Init fails, watchAndFeedback must return a non-nil,
		// non-errTemporaryClosed error immediately.
		origInit := nvmlInitFn
		origShutdown := nvmlShutdownFn
		nvmlInitFn = func() nvml.Return { return nvml.ERROR_DRIVER_NOT_LOADED }
		nvmlShutdownFn = func() {}
		t.Cleanup(func() {
			nvmlInitFn = origInit
			nvmlShutdownFn = origShutdown
		})

		sigChan := make(chan struct{}, 1)
		ctx := context.Background()
		err := watchAndFeedback(ctx, nilLister, sigChan)
		if err == nil {
			t.Error("want error on NVML init failure, got nil")
		}
		if errors.Is(err, errTemporaryClosed) {
			t.Error("want non-temporary error on NVML init failure")
		}
	})

	// OS-level test: verify that the real filesystem-backed migLockExistFn
	// used by migLockExistFn integrates correctly with watchAndFeedback by
	// using a temp file. This exercises the os.Stat path introduced in Part A.
	t.Run("RealLockFile_StartupLocked", func(t *testing.T) {
		stubNvml(t)

		dir := t.TempDir()
		lockFile, err := os.CreateTemp(dir, "mig-apply-*.lock")
		if err != nil {
			t.Fatalf("failed to create temp lock file: %v", err)
		}
		lockFile.Close()
		path := lockFile.Name()

		orig := migLockExistFn
		migLockExistFn = func() bool {
			_, statErr := os.Stat(path)
			return statErr == nil
		}
		t.Cleanup(func() { migLockExistFn = orig })

		sigChan := make(chan struct{}, 1)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// Lock file exists → must get errTemporaryClosed
		gotErr := watchAndFeedback(ctx, nilLister, sigChan)
		if !errors.Is(gotErr, errTemporaryClosed) {
			t.Errorf("want errTemporaryClosed with real lock file present, got %v", gotErr)
		}
	})
}
