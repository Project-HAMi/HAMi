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
	"errors"
	"os"
	"path/filepath"
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
	}, pods)
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
		_, err := New(config, list)
		require.Error(t, err)
	}
	valid := Config{Root: t.TempDir(), ScanInterval: time.Second}
	_, err := New(valid, nil)
	require.Error(t, err)
}
