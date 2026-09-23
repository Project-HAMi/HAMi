/*
Copyright 2026 The HAMi Authors.

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

package vgpucache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// newTestManager constructs an isolated manager with a shared test Pod provider for cached and
// live reads.
func newTestManager(t *testing.T, root string, pods func() ([]*corev1.Pod, error)) *Manager {
	t.Helper()
	manager, err := New(Config{
		Root:         root,
		ScanInterval: time.Second,
		GracePeriod:  time.Minute,
	}, pods, pods)
	require.NoError(t, err)
	return manager
}

// TestPrepareReplacesDirectory checks that Prepare clears old data and creates a writable
// container cache directory.
func TestPrepareReplacesDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "containers")
	manager := newTestManager(t, root, func() ([]*corev1.Pod, error) { return nil, nil })
	target, err := manager.Prepare("pod-uid", "main")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "pod-uid_main"), target)
	require.NoError(t, os.WriteFile(filepath.Join(target, "old.cache"), []byte("old"), 0o600))
	_, err = manager.Prepare("pod-uid", "main")
	require.NoError(t, err)
	_, statErr := os.Stat(filepath.Join(target, "old.cache"))
	require.ErrorIs(t, statErr, os.ErrNotExist)
	info, err := os.Stat(target)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o777), info.Mode().Perm())
}

// TestPrepareRejectsSymlinkRoot checks that preparation refuses a symlink cache root.
func TestPrepareRejectsSymlinkRoot(t *testing.T) {
	target := t.TempDir()
	root := filepath.Join(t.TempDir(), "containers")
	require.NoError(t, os.Symlink(target, root))
	manager := newTestManager(t, root, func() ([]*corev1.Pod, error) { return nil, nil })
	_, err := manager.Prepare("pod-uid", "main")
	require.Error(t, err)
}

// TestScanFailsClosedWhenPodListFails checks that failed Pod reads preserve cache directories.
func TestScanFailsClosedWhenPodListFails(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "pod-uid_main")
	require.NoError(t, os.Mkdir(target, 0o755))
	old := time.Now().Add(-2 * time.Minute)
	require.NoError(t, os.Chtimes(target, old, old))
	manager := newTestManager(t, root, func() ([]*corev1.Pod, error) {
		return nil, errors.New("not synced")
	})
	require.Error(t, manager.scan())
	_, err := os.Stat(target)
	require.NoError(t, err)
}

// TestScanUsesPodExistenceRegardlessOfPhase checks that all existing Pods retain their cache
// directories.
func TestScanUsesPodExistenceRegardlessOfPhase(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "pod-uid_main")
	require.NoError(t, os.Mkdir(target, 0o755))
	old := time.Now().Add(-2 * time.Minute)
	require.NoError(t, os.Chtimes(target, old, old))
	manager := newTestManager(t, root, func() ([]*corev1.Pod, error) {
		return []*corev1.Pod{{
			ObjectMeta: metav1.ObjectMeta{UID: types.UID("pod-uid")},
			Status:     corev1.PodStatus{Phase: corev1.PodSucceeded},
		}}, nil
	})
	require.NoError(t, manager.scan())
	_, err := os.Stat(target)
	require.NoError(t, err)
}

// TestScanDeleteAndSafetyRules checks expired orphan deletion and preservation of live,
// malformed, or unsafe entries.
func TestScanDeleteAndSafetyRules(t *testing.T) {
	t.Run("delete stale valid directory", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "missing-uid_main")
		require.NoError(t, os.Mkdir(target, 0o755))
		old := time.Now().Add(-2 * time.Minute)
		require.NoError(t, os.Chtimes(target, old, old))
		manager := newTestManager(t, root, func() ([]*corev1.Pod, error) { return nil, nil })
		require.NoError(t, manager.scan())
		_, statErr := os.Stat(target)
		require.ErrorIs(t, statErr, os.ErrNotExist)
	})

	t.Run("skip malformed file and symlink", func(t *testing.T) {
		root := t.TempDir()
		malformed := filepath.Join(root, "malformed")
		require.NoError(t, os.Mkdir(malformed, 0o755))
		file := filepath.Join(root, "missing-uid_file")
		require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))
		targetDir := t.TempDir()
		link := filepath.Join(root, "missing-uid_link")
		require.NoError(t, os.Symlink(targetDir, link))
		manager := newTestManager(t, root, func() ([]*corev1.Pod, error) { return nil, nil })
		manager.gracePeriod = 0
		require.NoError(t, manager.scan())
		for _, path := range []string{malformed, file, link, targetDir} {
			_, err := os.Lstat(path)
			require.NoError(t, err)
		}
	})
}

// TestNewValidatesConfig checks cache root, timing, and Pod provider validation.
func TestNewValidatesConfig(t *testing.T) {
	list := func() ([]*corev1.Pod, error) { return nil, nil }
	for _, config := range []Config{
		{Root: "relative", ScanInterval: time.Second},
		{Root: string(filepath.Separator), ScanInterval: time.Second},
		{Root: t.TempDir(), ScanInterval: 0},
		{Root: t.TempDir(), ScanInterval: time.Second, GracePeriod: -1},
	} {
		_, err := New(config, list, list)
		require.Error(t, err)
	}
	valid := Config{Root: t.TempDir(), ScanInterval: time.Second}
	_, err := New(valid, nil, list)
	require.Error(t, err)
	_, err = New(valid, list, nil)
	require.Error(t, err)
}

// TestRunRetriesUnavailableSnapshotAndStops checks periodic retry after Pod lookup failure and
// cancellation of the GC loop.
func TestRunRetriesUnavailableSnapshotAndStops(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "missing_main")
	require.NoError(t, os.Mkdir(target, 0o755))
	var synced atomic.Bool
	scanned := make(chan struct{}, 1)
	manager, err := New(Config{Root: root, ScanInterval: 5 * time.Millisecond}, func() ([]*corev1.Pod, error) {
		available := synced.Load()
		select {
		case scanned <- struct{}{}:
		default:
		}
		if !available {
			return nil, errors.New("snapshot unavailable")
		}
		return nil, nil
	}, func() ([]*corev1.Pod, error) { return nil, nil })
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); manager.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	select {
	case <-scanned:
	case <-time.After(time.Second):
		t.Fatal("initial scan did not run")
	}
	require.DirExists(t, target)
	synced.Store(true)
	require.Eventually(t, func() bool { _, err := os.Stat(target); return os.IsNotExist(err) }, time.Second, time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("GC did not stop after cancellation")
	}
	// Shutdown must leave no background scans that can delete later directories.
	require.NoError(t, os.Mkdir(target, 0o755))
	require.DirExists(t, target)
}

// TestPrepareRejectsInvalidIdentity checks that invalid container identities cannot escape the
// cache root.
func TestPrepareRejectsInvalidIdentity(t *testing.T) {
	root := t.TempDir()
	manager := newTestManager(t, root, func() ([]*corev1.Pod, error) { return nil, nil })
	for _, tc := range []struct{ uid, container string }{
		{"", "main"}, {"pod", ""}, {"../pod", "main"}, {"pod", "../main"}, {"pod", `nested\main`},
	} {
		t.Run(tc.uid+":"+tc.container, func(t *testing.T) {
			path, err := manager.Prepare(tc.uid, tc.container)
			require.ErrorContains(t, err, "invalid vGPU cache directory identity")
			require.Empty(t, path)
		})
	}
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Empty(t, entries)
}

// TestScanRootSafety checks handling of missing, symlink, and non-directory cache roots.
func TestScanRootSafety(t *testing.T) {
	t.Run("missing root is harmless", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "missing")
		manager := newTestManager(t, root, func() ([]*corev1.Pod, error) { return nil, nil })
		require.NoError(t, manager.scan())
		require.NoDirExists(t, root)
	})
	t.Run("root symlink never cleans its target", func(t *testing.T) {
		target := t.TempDir()
		stale := filepath.Join(target, "missing_main")
		require.NoError(t, os.Mkdir(stale, 0o755))
		root := filepath.Join(t.TempDir(), "link")
		require.NoError(t, os.Symlink(target, root))
		manager := newTestManager(t, root, func() ([]*corev1.Pod, error) { return nil, nil })
		manager.gracePeriod = 0
		require.ErrorContains(t, manager.scan(), "not a real directory")
		require.DirExists(t, stale)
	})
	t.Run("file is not a cache root", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(root, []byte("preserve"), 0o600))
		manager := newTestManager(t, root, func() ([]*corev1.Pod, error) { return nil, nil })
		require.ErrorContains(t, manager.scan(), "not a real directory")
		_, err := manager.Prepare("pod", "main")
		require.ErrorContains(t, err, "not a real directory")
		data, err := os.ReadFile(root)
		require.NoError(t, err)
		require.Equal(t, "preserve", string(data))
	})
	t.Run("invalid parent propagates filesystem errors", func(t *testing.T) {
		parent := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(parent, nil, 0o600))
		manager := newTestManager(t, filepath.Join(parent, "containers"), func() ([]*corev1.Pod, error) { return nil, nil })
		require.ErrorContains(t, manager.scan(), "stat vGPU cache root")
		_, err := manager.Prepare("pod", "main")
		require.Error(t, err)
	})
}

// TestScanHonorsGracePeriodAndNilPods checks grace-period protection and tolerance of nil Pod
// entries.
func TestScanHonorsGracePeriodAndNilPods(t *testing.T) {
	root := t.TempDir()
	manager := newTestManager(t, root, func() ([]*corev1.Pod, error) { return []*corev1.Pod{nil}, nil })
	target, err := manager.Prepare("missing", "main")
	require.NoError(t, err)
	require.NoError(t, manager.scan())
	require.DirExists(t, target)
	old := time.Now().Add(-2 * time.Minute)
	require.NoError(t, os.Chtimes(target, old, old))
	require.NoError(t, manager.scan())
	require.NoDirExists(t, target)
}

// TestCacheFilesystemPermissionErrors checks propagation of filesystem access failures during
// preparation and GC.
func TestCacheFilesystemPermissionErrors(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission checks")
	}
	t.Run("unreadable root", func(t *testing.T) {
		root := t.TempDir()
		manager := newTestManager(t, root, func() ([]*corev1.Pod, error) { return nil, nil })
		require.NoError(t, os.Chmod(root, 0))
		t.Cleanup(func() { require.NoError(t, os.Chmod(root, 0o700)) })
		require.ErrorIs(t, manager.scan(), os.ErrPermission)
	})
	t.Run("cannot create directory", func(t *testing.T) {
		root := t.TempDir()
		manager := newTestManager(t, root, func() ([]*corev1.Pod, error) { return nil, nil })
		require.NoError(t, os.Chmod(root, 0o500))
		t.Cleanup(func() { require.NoError(t, os.Chmod(root, 0o700)) })
		_, err := manager.Prepare("pod", "main")
		require.ErrorContains(t, err, "create vGPU cache directory")
		require.ErrorIs(t, err, os.ErrPermission)
	})
	t.Run("failed removal does not prevent other GC entries", func(t *testing.T) {
		root := t.TempDir()
		manager := newTestManager(t, root, func() ([]*corev1.Pod, error) { return nil, nil })
		manager.gracePeriod = 0
		blocked, err := manager.Prepare("a", "main")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(blocked, "cache"), nil, 0o600))
		require.NoError(t, os.Chmod(blocked, 0o500))
		t.Cleanup(func() { require.NoError(t, os.Chmod(blocked, 0o700)) })
		other, err := manager.Prepare("b", "main")
		require.NoError(t, err)
		_, err = manager.Prepare("a", "main")
		require.ErrorContains(t, err, "remove previous vGPU cache directory")
		require.ErrorIs(t, err, os.ErrPermission)
		require.ErrorIs(t, manager.scan(), os.ErrPermission)
		require.FileExists(t, filepath.Join(blocked, "cache"))
		require.NoDirExists(t, other)
	})
}

// TestScanConfirmsStaleSnapshotBeforeDeleting checks that live Pods in any phase survive stale
// cached absence while true orphans are deleted.
func TestScanConfirmsStaleSnapshotBeforeDeleting(t *testing.T) {
	for _, phase := range []corev1.PodPhase{corev1.PodPending, corev1.PodRunning, corev1.PodSucceeded} {
		t.Run(string(phase), func(t *testing.T) {
			root := t.TempDir()
			manager := newTestManager(t, root, func() ([]*corev1.Pod, error) { return nil, nil })
			manager.gracePeriod = 0
			live, err := manager.Prepare("live", "gpu")
			require.NoError(t, err)
			stale, err := manager.Prepare("stale", "gpu")
			require.NoError(t, err)
			calls := 0
			manager.listLiveNodePods = func() ([]*corev1.Pod, error) {
				calls++
				return []*corev1.Pod{nil, {ObjectMeta: metav1.ObjectMeta{UID: "live"}, Status: corev1.PodStatus{Phase: phase}}}, nil
			}
			require.NoError(t, manager.scan())
			require.DirExists(t, live)
			require.NoDirExists(t, stale)
			require.Equal(t, 1, calls, "batch confirmation once per scan")
		})
	}
}

// TestScanLiveConfirmationFailurePreservesAllCandidates checks that API errors preserve all
// candidates until confirmation succeeds.
func TestScanLiveConfirmationFailurePreservesAllCandidates(t *testing.T) {
	root := t.TempDir()
	manager := newTestManager(t, root, func() ([]*corev1.Pod, error) { return nil, nil })
	manager.gracePeriod = 0
	now := time.Now().Add(time.Hour)
	manager.now = func() time.Time { return now }
	first, err := manager.Prepare("a", "gpu")
	require.NoError(t, err)
	second, err := manager.Prepare("b", "gpu")
	require.NoError(t, err)
	for _, failure := range []error{errors.New("API unavailable"), context.DeadlineExceeded, context.Canceled} {
		manager.listLiveNodePods = func() ([]*corev1.Pod, error) { return nil, failure }
		require.ErrorIs(t, manager.scan(), failure)
		require.Empty(t, manager.deletionFailures, "API errors are not deletion attempts")
		require.DirExists(t, first)
		require.DirExists(t, second)
		now = now.Add(time.Minute)
	}
	manager.listLiveNodePods = func() ([]*corev1.Pod, error) { return nil, nil }
	require.NoError(t, manager.scan())
	require.NoDirExists(t, first)
	require.NoDirExists(t, second)
}

// TestScanAvoidsLiveQueryWithoutExpiredCandidates checks that live API calls are skipped when no
// expired orphan candidate exists.
func TestScanAvoidsLiveQueryWithoutExpiredCandidates(t *testing.T) {
	root := t.TempDir()
	manager := newTestManager(t, root, func() ([]*corev1.Pod, error) {
		return []*corev1.Pod{{ObjectMeta: metav1.ObjectMeta{UID: "known"}}}, nil
	})
	_, err := manager.Prepare("known", "gpu")
	require.NoError(t, err)
	_, err = manager.Prepare("recent", "gpu")
	require.NoError(t, err)
	manager.listLiveNodePods = func() ([]*corev1.Pod, error) {
		t.Fatal("no deletion candidate should trigger an API request")
		return nil, nil
	}
	require.NoError(t, manager.scan())
}

// TestScanRechecksDirectoryAfterLiveConfirmation checks that a directory replaced during API
// confirmation is preserved.
func TestScanRechecksDirectoryAfterLiveConfirmation(t *testing.T) {
	root := t.TempDir()
	manager := newTestManager(t, root, func() ([]*corev1.Pod, error) { return nil, nil })
	manager.gracePeriod = 0
	target, err := manager.Prepare("pod", "gpu")
	require.NoError(t, err)
	manager.listLiveNodePods = func() ([]*corev1.Pod, error) {
		// Simulate an external replacement while the API request is in flight.
		require.NoError(t, os.Rename(target, target+".old"))
		require.NoError(t, os.Mkdir(target, 0o777))
		return nil, nil
	}
	require.NoError(t, manager.scan())
	require.DirExists(t, target)
}

// TestScanAllowsPrepareDuringConfirmationAndEventuallyCleans verifies a slow LIST
// neither blocks Prepare nor authorizes deletion of its newly prepared cache.
func TestScanAllowsPrepareDuringConfirmationAndEventuallyCleans(t *testing.T) {
	manager := newTestManager(t, t.TempDir(), func() ([]*corev1.Pod, error) { return nil, nil })
	manager.gracePeriod = 0
	old, err := manager.Prepare("old", "gpu")
	require.NoError(t, err)
	replaced, err := manager.Prepare("replaced", "gpu")
	require.NoError(t, err)
	entered, release := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var calls atomic.Int32
	manager.listLiveNodePods = func() ([]*corev1.Pod, error) {
		if calls.Add(1) == 1 {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return nil, nil
	}
	done := make(chan error, 1)
	go func() { done <- manager.scan() }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("GC did not start confirmation")
	}
	prepared := make(chan error, 1)
	go func() { _, err := manager.Prepare("replaced", "gpu"); prepared <- err }()
	select {
	case err := <-prepared:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Prepare blocked behind live LIST")
	}
	// Another GC scan must not queue a second request or block behind this one.
	overlapped := make(chan error, 1)
	go func() { overlapped <- manager.scan() }()
	select {
	case err := <-overlapped:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("overlapping scan blocked")
	}
	require.Equal(t, int32(1), calls.Load())
	close(release)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("GC did not finish")
	}
	require.DirExists(t, old, "concurrent Prepare invalidates the entire batch")
	require.DirExists(t, replaced)
	// On a subsequent stable round both orphaned directories are collectible.
	manager.now = func() time.Time { return time.Now().Add(time.Minute) }
	require.NoError(t, manager.scan())
	require.NoDirExists(t, old)
	require.NoDirExists(t, replaced)
	require.Equal(t, int32(2), calls.Load(), "one LIST confirms the whole batch")
}

// TestScanConfirmationBackoffIsBoundedAndResets verifies suppressed retries retain
// files, retries continue after failures, and success restores the normal cadence.
func TestScanConfirmationBackoffIsBoundedAndResets(t *testing.T) {
	manager := newTestManager(t, t.TempDir(), func() ([]*corev1.Pod, error) { return nil, nil })
	manager.gracePeriod = 0
	now := time.Now().Add(time.Hour)
	manager.now = func() time.Time { return now }
	target, err := manager.Prepare("orphan", "gpu")
	require.NoError(t, err)
	failure := errors.New("API unavailable")
	calls := 0
	manager.listLiveNodePods = func() ([]*corev1.Pod, error) { calls++; return nil, failure }
	for _, delay := range []time.Duration{1, 2, 4, 8, 16, 32, 60, 60} {
		require.ErrorIs(t, manager.scan(), failure)
		previousCalls := calls
		now = now.Add(delay*time.Second - time.Nanosecond)
		require.NoError(t, manager.scan())
		require.Equal(t, previousCalls, calls, "LIST must be suppressed before retry deadline")
		require.DirExists(t, target)
		now = now.Add(time.Nanosecond)
	}
	manager.listLiveNodePods = func() ([]*corev1.Pod, error) { calls++; return nil, nil }
	require.NoError(t, manager.scan())
	require.NoDirExists(t, target, "API recovery must eventually permit cleanup")
	// A new orphan can be collected immediately; the successful round reset backoff.
	target, err = manager.Prepare("another", "gpu")
	require.NoError(t, err)
	previousCalls := calls
	require.NoError(t, manager.scan())
	require.Equal(t, previousCalls+1, calls)
	require.NoDirExists(t, target)
	// A later outage must restart at the initial one-second retry delay.
	_, err = manager.Prepare("last", "gpu")
	require.NoError(t, err)
	manager.listLiveNodePods = func() ([]*corev1.Pod, error) { return nil, failure }
	require.ErrorIs(t, manager.scan(), failure)
	manager.listLiveNodePods = func() ([]*corev1.Pod, error) { calls++; return nil, nil }
	now = now.Add(time.Second)
	require.NoError(t, manager.scan())
	require.NoDirExists(t, filepath.Join(manager.root, "last_gpu"))
}

// TestScanRechecksRootAndCandidateAfterConfirmation verifies external filesystem
// changes during the unlocked API request cannot redirect or accelerate deletion.
func TestScanRechecksRootAndCandidateAfterConfirmation(t *testing.T) {
	for _, change := range []string{"root replaced", "root symlink", "mtime changed", "candidate removed"} {
		t.Run(change, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "containers")
			manager := newTestManager(t, root, func() ([]*corev1.Pod, error) { return nil, nil })
			manager.gracePeriod = 0
			target, err := manager.Prepare("orphan", "gpu")
			require.NoError(t, err)
			manager.listLiveNodePods = func() ([]*corev1.Pod, error) {
				switch change {
				case "root replaced":
					require.NoError(t, os.Rename(root, root+".old"))
					require.NoError(t, os.MkdirAll(target, 0o777))
				case "root symlink":
					require.NoError(t, os.Rename(root, root+".old"))
					require.NoError(t, os.Symlink(root+".old", root))
				case "mtime changed":
					future := time.Now().Add(time.Hour)
					require.NoError(t, os.Chtimes(target, future, future))
				case "candidate removed":
					require.NoError(t, os.RemoveAll(target))
				}
				return nil, nil
			}
			require.NoError(t, manager.scan())
			if change == "candidate removed" {
				require.NoDirExists(t, target)
			} else {
				require.DirExists(t, target)
			}
		})
	}
}

// TestRunEventuallyCleansAfterConfirmationFailure verifies the periodic worker
// retries a failed live read and converges without a manual scan or restart.
func TestRunEventuallyCleansAfterConfirmationFailure(t *testing.T) {
	root := t.TempDir()
	var recovered atomic.Bool
	failed := make(chan struct{}, 1)
	manager, err := New(Config{Root: root, ScanInterval: 10 * time.Millisecond},
		func() ([]*corev1.Pod, error) { return nil, nil },
		func() ([]*corev1.Pod, error) {
			if !recovered.Load() {
				select {
				case failed <- struct{}{}:
				default:
				}
				return nil, errors.New("API unavailable")
			}
			return nil, nil
		})
	require.NoError(t, err)
	target, err := manager.Prepare("orphan", "gpu")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); manager.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	select {
	case <-failed:
	case <-time.After(time.Second):
		t.Fatal("GC did not attempt confirmation")
	}
	require.DirExists(t, target)
	recovered.Store(true)
	require.Eventually(t, func() bool { _, err := os.Stat(target); return os.IsNotExist(err) }, time.Second, time.Millisecond)
}

// TestScanIncompleteRoundsBackOff covers successful LISTs that cannot complete
// cleanup, including partial progress, and measures retry delay after slow work.
func TestScanIncompleteRoundsBackOff(t *testing.T) {
	for _, reason := range []string{"live Pod missing from cache", "concurrent Prepare", "changed directory"} {
		t.Run(reason, func(t *testing.T) {
			manager := newTestManager(t, t.TempDir(), func() ([]*corev1.Pod, error) { return nil, nil })
			manager.gracePeriod = 0
			now := time.Now().Add(time.Hour)
			manager.now = func() time.Time { return now }
			target, err := manager.Prepare("blocked", "gpu")
			require.NoError(t, err)
			_, err = manager.Prepare("collectible", "gpu")
			require.NoError(t, err)
			calls := 0
			manager.listLiveNodePods = func() ([]*corev1.Pod, error) {
				calls++
				now = now.Add(2 * time.Second) // Slow confirmation must not consume retry delay.
				switch reason {
				case "live Pod missing from cache":
					return []*corev1.Pod{{ObjectMeta: metav1.ObjectMeta{UID: "blocked"}}}, nil
				case "concurrent Prepare":
					_, err := manager.Prepare("blocked", "gpu")
					require.NoError(t, err)
				case "changed directory":
					require.NoError(t, os.Chtimes(target, now, now))
				}
				return nil, nil
			}
			for index, delay := range []time.Duration{1, 2, 4, 8, 16, 32, 60, 60} {
				err := manager.scan()
				require.NoError(t, err)
				require.Equal(t, index+1, calls)
				require.Empty(t, manager.deletionFailures, "discarded confirmations are not deletion attempts")
				require.DirExists(t, target)
				now = now.Add(delay*time.Second - time.Nanosecond)
				for range 3 {
					require.NoError(t, manager.scan())
				}
				require.Equal(t, index+1, calls)
				now = now.Add(time.Nanosecond)
			}
			require.NoError(t, os.Chmod(target, 0o700))
			manager.listLiveNodePods = func() ([]*corev1.Pod, error) { calls++; return nil, nil }
			require.NoError(t, manager.scan())
			require.Equal(t, 9, calls)
			require.NoDirExists(t, target)
		})
	}
}

// TestScanCachedRecoveryClearsRetry confirms an informer update removes the
// candidate without a new LIST, permitting unrelated new work immediately.
func TestScanCachedRecoveryClearsRetry(t *testing.T) {
	var cached []*corev1.Pod
	manager := newTestManager(t, t.TempDir(), func() ([]*corev1.Pod, error) { return cached, nil })
	manager.gracePeriod = 0
	_, err := manager.Prepare("live", "gpu")
	require.NoError(t, err)
	calls := 0
	manager.listLiveNodePods = func() ([]*corev1.Pod, error) {
		calls++
		return []*corev1.Pod{{ObjectMeta: metav1.ObjectMeta{UID: "live"}}}, nil
	}
	require.NoError(t, manager.scan())
	cached = []*corev1.Pod{{ObjectMeta: metav1.ObjectMeta{UID: "live"}}}
	require.NoError(t, manager.scan())
	require.Equal(t, 1, calls)
	orphan, err := manager.Prepare("orphan", "gpu")
	require.NoError(t, err)
	require.NoError(t, manager.scan())
	require.Equal(t, 2, calls)
	require.NoDirExists(t, orphan)
}

// TestScanAbandonsUndeletableDirectory limits actual removal attempts, reports
// once and skips abandoned entries even when other directories require LISTs.
func TestScanAbandonsUndeletableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires filesystem permission enforcement")
	}
	m := newTestManager(t, t.TempDir(), func() ([]*corev1.Pod, error) { return nil, nil })
	m.gracePeriod = 0
	now := time.Now().Add(time.Hour)
	m.now = func() time.Time { return now }
	target, err := m.Prepare("blocked", "gpu")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(target, "cache"), nil, 0o600))
	require.NoError(t, os.Chmod(target, 0o500))
	t.Cleanup(func() { _ = os.Chmod(target, 0o700) })
	calls, reports := 0, 0
	m.listLiveNodePods = func() ([]*corev1.Pod, error) { calls++; return nil, nil }
	m.onAbandoned = func(path string, attempts int, err error) {
		reports++
		require.Equal(t, target, path)
		require.Equal(t, 5, attempts)
		require.ErrorIs(t, err, os.ErrPermission)
	}
	for attempt, delay := range []time.Duration{1, 2, 4, 8, 16} {
		require.ErrorIs(t, m.scan(), os.ErrPermission)
		require.Equal(t, attempt+1, calls)
		require.Equal(t, attempt+1, m.deletionFailures[target].attempts)
		if attempt < 4 {
			require.Zero(t, reports)
		}
		require.NoError(t, m.scan())
		require.Equal(t, attempt+1, calls)
		now = now.Add(delay * time.Second)
	}
	require.Equal(t, 1, reports)
	// Fixing permission alone must not resume an abandoned cleanup.
	require.NoError(t, os.Chmod(target, 0o700))
	for range 10 {
		now = now.Add(time.Minute)
		require.NoError(t, m.scan())
	}
	require.Equal(t, 5, calls)
	require.DirExists(t, target)
	other, err := m.Prepare("other", "gpu")
	require.NoError(t, err)
	require.NoError(t, m.scan())
	require.Equal(t, 6, calls)
	require.NoDirExists(t, other)
	require.DirExists(t, target)
	require.Equal(t, 1, reports)
	// Keep the old inode alive to ensure a replacement has a distinct identity.
	old := filepath.Join(t.TempDir(), "old")
	require.NoError(t, os.Rename(target, old))
	require.NoError(t, os.Mkdir(target, 0o700))
	require.NoError(t, m.scan())
	require.Equal(t, 7, calls)
	require.NoDirExists(t, target)
	require.Empty(t, m.deletionFailures)
}

// TestScanDeleteFailureRecovery checks counters survive partial deletion but
// clear on success or disappearance; a new Manager retries after restart.
func TestScanDeleteFailureRecovery(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires filesystem permission enforcement")
	}
	for _, recovery := range []string{"before limit", "manual removal", "restart", "Prepare"} {
		t.Run(recovery, func(t *testing.T) {
			m := newTestManager(t, t.TempDir(), func() ([]*corev1.Pod, error) { return nil, nil })
			m.gracePeriod = 0
			now := time.Now().Add(time.Hour)
			m.now = func() time.Time { return now }
			target, err := m.Prepare("blocked", "gpu")
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(target, "cache"), nil, 0o600))
			require.NoError(t, os.Chmod(target, 0o500))
			t.Cleanup(func() { _ = os.Chmod(target, 0o700) })
			other, err := m.Prepare("other", "gpu")
			require.NoError(t, err)
			attempts := 5
			if recovery == "before limit" {
				attempts = 2
			}
			for range attempts {
				require.ErrorIs(t, m.scan(), os.ErrPermission)
				now = now.Add(time.Minute)
			}
			require.NoDirExists(t, other, "one bad entry must not prevent other cleanup")
			require.Equal(t, attempts, m.deletionFailures[target].attempts)
			require.NoError(t, os.Chmod(target, 0o700))
			switch recovery {
			case "manual removal":
				require.NoError(t, os.RemoveAll(target))
			case "Prepare":
				_, err := m.Prepare("blocked", "gpu")
				require.NoError(t, err)
				require.Empty(t, m.deletionFailures, "Prepare resets identity even if the filesystem reuses an inode")
			case "restart":
				m = newTestManager(t, m.root, func() ([]*corev1.Pod, error) { return nil, nil })
				m.gracePeriod = 0
			}
			require.NoError(t, m.scan())
			require.NoDirExists(t, target)
			require.Empty(t, m.deletionFailures)
		})
	}
}
