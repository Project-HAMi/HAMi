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
)

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
	})
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
