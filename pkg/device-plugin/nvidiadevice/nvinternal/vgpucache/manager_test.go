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

func TestPrepareRejectsSymlinkRoot(t *testing.T) {
	target := t.TempDir()
	root := filepath.Join(t.TempDir(), "containers")
	require.NoError(t, os.Symlink(target, root))
	manager := newTestManager(t, root, func() ([]*corev1.Pod, error) { return nil, nil })
	_, err := manager.Prepare("pod-uid", "main")
	require.Error(t, err)
}

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

func TestScanLiveConfirmationFailurePreservesAllCandidates(t *testing.T) {
	root := t.TempDir()
	manager := newTestManager(t, root, func() ([]*corev1.Pod, error) { return nil, nil })
	manager.gracePeriod = 0
	first, err := manager.Prepare("a", "gpu")
	require.NoError(t, err)
	second, err := manager.Prepare("b", "gpu")
	require.NoError(t, err)
	for _, failure := range []error{errors.New("API unavailable"), context.DeadlineExceeded, context.Canceled} {
		manager.listLiveNodePods = func() ([]*corev1.Pod, error) { return nil, failure }
		require.ErrorIs(t, manager.scan(), failure)
		require.DirExists(t, first)
		require.DirExists(t, second)
	}
	manager.listLiveNodePods = func() ([]*corev1.Pod, error) { return nil, nil }
	require.NoError(t, manager.scan())
	require.NoDirExists(t, first)
	require.NoDirExists(t, second)
}

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
