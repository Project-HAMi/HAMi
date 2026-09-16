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

package nvidia

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/util"
)

func TestNewContainerListerCacheRoot(t *testing.T) {
	for _, tc := range []struct {
		name, root string
		invalid    bool
	}{
		{name: "fallback"}, {name: "custom", root: t.TempDir()},
		{name: "relative", root: "relative", invalid: true}, {name: "filesystem root", root: "/", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Query().Get("watch") == "true" {
					// Support the initial-events bookmark used by watch-list clients.
					bookmark := map[string]any{"type": "BOOKMARK", "object": &corev1.Pod{
						TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
						ObjectMeta: metav1.ObjectMeta{ResourceVersion: "1", Annotations: map[string]string{"k8s.io/initial-events-end": "true"}},
					}}
					if err := json.NewEncoder(w).Encode(bookmark); err != nil {
						t.Errorf("encode bookmark: %v", err)
						return
					}
					if err := http.NewResponseController(w).Flush(); err != nil {
						t.Errorf("flush bookmark: %v", err)
						return
					}
					<-r.Context().Done()
					return
				}
				err := json.NewEncoder(w).Encode(&corev1.PodList{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PodList"}, ListMeta: metav1.ListMeta{ResourceVersion: "1"}, Items: []corev1.Pod{}})
				if err != nil {
					t.Errorf("encode Pod list: %v", err)
				}
			}))
			defer server.Close()
			hook := t.TempDir()
			t.Setenv("HOOK_PATH", hook)
			t.Setenv("HAMI_VGPU_CACHE_ROOT", tc.root)
			t.Setenv("KUBECONFIG", writeKubeconfig(t, server.URL))
			t.Setenv(util.NodeNameEnvName, "test-node")
			l, err := NewContainerLister()
			if tc.invalid {
				require.ErrorContains(t, err, "absolute non-root path")
				require.Nil(t, l)
				return
			}
			require.NoError(t, err)
			defer l.Close()
			expected := tc.root
			if expected == "" {
				expected = filepath.Join(hook, "containers")
			}
			require.Equal(t, expected, l.containerPath)
			require.True(t, l.podListerSynced())
			l.Close()
			require.Eventually(t, func() bool { return l.podInformer.IsStopped() }, time.Second, time.Millisecond)
		})
	}
}

func TestContainerListerIndependentMappings(t *testing.T) {
	root := t.TempDir()
	pods := &fakePodLister{pods: []*corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{UID: "pod-a"}}, {ObjectMeta: metav1.ObjectMeta{UID: "pod-b"}},
	}}
	l := &ContainerLister{containerPath: root, containers: map[string]*ContainerUsage{}, podLister: pods, stopCh: make(chan struct{})}
	t.Cleanup(l.Close)
	paths := map[string]string{}
	for name, limit := range map[string]uint64{"pod-a_small": 1024, "pod-a_large": 2048, "pod-b_main": 4096} {
		dir := filepath.Join(root, name)
		require.NoError(t, os.Mkdir(dir, 0o755))
		paths[name] = writeCacheFile(t, dir, "x.cache", makeV0CacheBytes(1, []uint64{limit}))
	}
	require.NoError(t, l.Update())
	require.Len(t, l.containers, 3)
	small, large, other := l.containers["pod-a_small"], l.containers["pod-a_large"], l.containers["pod-b_main"]
	require.Equal(t, uint64(1024), small.Info.DeviceMemoryLimit(0))
	require.Equal(t, uint64(2048), large.Info.DeviceMemoryLimit(0))
	require.Equal(t, uint64(4096), other.Info.DeviceMemoryLimit(0))
	require.Equal(t, "pod-a", small.PodUID)
	require.Equal(t, "small", small.ContainerName)
	require.Equal(t, "large", large.ContainerName)

	// A missing cache releases only its mapping, even while its Pod still exists.
	require.NoError(t, os.Remove(paths["pod-a_small"]))
	require.NoError(t, l.Update())
	require.NotContains(t, l.containers, "pod-a_small")
	require.Same(t, large, l.containers["pod-a_large"])
	require.Same(t, other, l.containers["pod-b_main"])
	writeCacheFile(t, filepath.Dir(paths["pod-a_small"]), "x.cache", makeV0CacheBytes(1, []uint64{512}))
	require.NoError(t, l.Update())
	require.NotSame(t, small, l.containers["pod-a_small"])
	require.Equal(t, uint64(512), l.containers["pod-a_small"].Info.DeviceMemoryLimit(0))

	// An inode-preserving size change must also invalidate the old mapping.
	require.NoError(t, os.Truncate(paths["pod-a_small"], int64(v0CacheFileSize+4096)))
	before := l.containers["pod-a_small"]
	require.NoError(t, l.Update())
	require.NotSame(t, before, l.containers["pod-a_small"])
	require.Len(t, l.containers["pod-a_small"].data, v0CacheFileSize+4096)
	require.Same(t, large, l.containers["pod-a_large"])

	// Deleting one Pod retains recent mappings, then releases both of its
	// containers without deleting directories or touching the other Pod.
	pods.pods = pods.pods[1:]
	recent := time.Now()
	for _, name := range []string{"pod-a_small", "pod-a_large"} {
		require.NoError(t, os.Chtimes(filepath.Join(root, name), recent, recent))
	}
	require.NoError(t, l.Update())
	require.Len(t, l.containers, 3)
	old := time.Now().Add(-2 * resyncInterval)
	for _, name := range []string{"pod-a_small", "pod-a_large"} {
		require.NoError(t, os.Chtimes(filepath.Join(root, name), old, old))
	}
	require.NoError(t, l.Update())
	require.Len(t, l.containers, 1)
	require.Same(t, other, l.containers["pod-b_main"])
	require.DirExists(t, filepath.Join(root, "pod-a_small"))
	require.DirExists(t, filepath.Join(root, "pod-a_large"))

	// Removing the whole shared root releases all remaining mappings as well.
	require.NoError(t, os.RemoveAll(root))
	require.ErrorIs(t, l.Update(), os.ErrNotExist)
	require.Empty(t, l.containers)
}
