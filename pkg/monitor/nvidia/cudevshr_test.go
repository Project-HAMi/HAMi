/*
Copyright 2025 The HAMi Authors.

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
	"encoding/binary"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	v1 "github.com/Project-HAMi/HAMi/pkg/monitor/nvidia/v1"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

// v1CacheFileSize is large enough for v1.CastSpec to safely dereference
// every field of the real v1 sharedRegionT; using a smaller fixture would
// leave usage.Info backed by an out-of-bounds pointer.
var v1CacheFileSize = v1.MinSize()

// fakePodLister is a minimal corelisters.PodLister used to drive Update()'s
// error path without touching a real (or fake) Kubernetes API server.
type fakePodLister struct {
	pods    []*corev1.Pod
	listErr error
}

func (f *fakePodLister) List(labels.Selector) ([]*corev1.Pod, error) {
	return f.pods, f.listErr
}

func (f *fakePodLister) Pods(string) corelisters.PodNamespaceLister {
	return nil
}

func newTestPodLister(pods ...*corev1.Pod) corelisters.PodLister {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	for _, p := range pods {
		_ = indexer.Add(p)
	}
	return corelisters.NewPodLister(indexer)
}

func headerBytes(size int, magic, major, minor int32) []byte {
	buf := make([]byte, size)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(magic))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(major))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(minor))
	return buf
}

func writeCacheFile(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	assert.NilError(t, os.WriteFile(p, content, 0666))
	return p
}

func Test_ContainerLister_Accessors(t *testing.T) {
	l := &ContainerLister{
		containers: map[string]*ContainerUsage{"a": {}},
	}
	assert.Equal(t, len(l.ListContainers()), 1)
	assert.Assert(t, l.Clientset() == nil)

	l.Lock()
	l.UnLock()
}

func Test_NewContainerLister_errors(t *testing.T) {
	t.Run("HOOK_PATH not set", func(t *testing.T) {
		t.Setenv("HOOK_PATH", "")
		os.Unsetenv("HOOK_PATH")
		_, err := NewContainerLister()
		assert.ErrorContains(t, err, "HOOK_PATH not set")
	})

	t.Run("invalid KUBECONFIG", func(t *testing.T) {
		t.Setenv("HOOK_PATH", t.TempDir())
		t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "does-not-exist.yaml"))
		_, err := NewContainerLister()
		assert.Assert(t, err != nil)
	})

	t.Run("clientset construction fails", func(t *testing.T) {
		t.Setenv("HOOK_PATH", t.TempDir())
		t.Setenv("KUBECONFIG", writeKubeconfig(t, "http://[::1]:namedport"))
		_, err := NewContainerLister()
		assert.ErrorContains(t, err, "host must be a URL or a host:port pair")
	})

	t.Run("NODE_NAME not set", func(t *testing.T) {
		t.Setenv("HOOK_PATH", t.TempDir())
		t.Setenv("KUBECONFIG", writeKubeconfig(t, "http://localhost:8080"))
		t.Setenv(util.NodeNameEnvName, "")
		os.Unsetenv(util.NodeNameEnvName)
		_, err := NewContainerLister()
		assert.ErrorContains(t, err, "env "+util.NodeNameEnvName+" not set")
	})
}

func writeKubeconfig(t *testing.T, server string) string {
	t.Helper()
	kubeconfig := filepath.Join(t.TempDir(), "kubeconfig.yaml")
	content := `apiVersion: v1
clusters:
- cluster:
    server: ` + server + `
  name: local-server
contexts:
- context:
    cluster: local-server
    namespace: the-right-prefix
    user: myself
  name: default-context
current-context: default-context
kind: Config
preferences: {}
users:
- name: myself
  user:
    password: secret
    username: admin
`
	assert.NilError(t, os.WriteFile(kubeconfig, []byte(content), 0644))
	return kubeconfig
}

// unreachableClientset builds a real *kubernetes.Clientset pointed at a port
// nothing is listening on, so any request fails fast with connection refused
// instead of hanging.
func unreachableClientset(t *testing.T) *kubernetes.Clientset {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NilError(t, err)
	addr := ln.Addr().String()
	assert.NilError(t, ln.Close())

	cs, err := kubernetes.NewForConfig(&rest.Config{Host: "http://" + addr})
	assert.NilError(t, err)
	return cs
}

func Test_initInformerWithConfig_syncTimeout(t *testing.T) {
	l := &ContainerLister{
		clientset: unreachableClientset(t),
		nodeName:  "test-node",
		stopCh:    make(chan struct{}),
	}
	time.AfterFunc(200*time.Millisecond, func() { close(l.stopCh) })

	err := l.initInformerWithConfig(50 * time.Millisecond)
	assert.ErrorContains(t, err, "failed to sync pod informer cache")
}

func Test_onPodDelete(t *testing.T) {
	l := &ContainerLister{}

	t.Run("plain pod", func(t *testing.T) {
		l.onPodDelete(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"}})
	})

	t.Run("tombstone wrapping a pod", func(t *testing.T) {
		l.onPodDelete(cache.DeletedFinalStateUnknown{
			Key: "default/p2",
			Obj: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p2", Namespace: "default"}},
		})
	})

	t.Run("tombstone wrapping a non-pod", func(t *testing.T) {
		l.onPodDelete(cache.DeletedFinalStateUnknown{Key: "x", Obj: "not-a-pod"})
	})

	t.Run("neither pod nor tombstone", func(t *testing.T) {
		l.onPodDelete("garbage")
	})
}

func Test_loadCache(t *testing.T) {
	t.Run("directory missing", func(t *testing.T) {
		_, err := loadCache(filepath.Join(t.TempDir(), "missing"))
		assert.Assert(t, err != nil)
	})

	t.Run("too many files", func(t *testing.T) {
		dir := t.TempDir()
		writeCacheFile(t, dir, "a.cache", []byte("x"))
		writeCacheFile(t, dir, "b.cache", []byte("x"))
		writeCacheFile(t, dir, "c.cache", []byte("x"))
		_, err := loadCache(dir)
		assert.ErrorContains(t, err, "cache num not matched")
	})

	t.Run("empty directory", func(t *testing.T) {
		usage, err := loadCache(t.TempDir())
		assert.NilError(t, err)
		assert.Assert(t, usage == nil)
	})

	t.Run("no matching cache file", func(t *testing.T) {
		dir := t.TempDir()
		writeCacheFile(t, dir, "libvgpu.so", []byte("x"))
		writeCacheFile(t, dir, "notes.txt", []byte("x"))
		usage, err := loadCache(dir)
		assert.NilError(t, err)
		assert.Assert(t, usage == nil)
	})

	t.Run("cache file too small", func(t *testing.T) {
		dir := t.TempDir()
		writeCacheFile(t, dir, "x.cache", []byte("tiny"))
		_, err := loadCache(dir)
		assert.ErrorContains(t, err, "too small")
	})

	t.Run("magic flag mismatch", func(t *testing.T) {
		dir := t.TempDir()
		writeCacheFile(t, dir, "x.cache", headerBytes(64, 0, 1, 0))
		_, err := loadCache(dir)
		assert.ErrorContains(t, err, "magic flag not matched")
	})

	t.Run("unknown version and size", func(t *testing.T) {
		dir := t.TempDir()
		writeCacheFile(t, dir, "x.cache", headerBytes(64, SharedRegionMagicFlag, 99, 0))
		_, err := loadCache(dir)
		assert.ErrorContains(t, err, "unknown cache file size")
	})

	t.Run("v1 header but file too small for the v1 spec", func(t *testing.T) {
		dir := t.TempDir()
		writeCacheFile(t, dir, "x.cache", headerBytes(64, SharedRegionMagicFlag, 1, 0))
		_, err := loadCache(dir)
		assert.ErrorContains(t, err, "unknown cache file size")
	})

	t.Run("v1 cache file", func(t *testing.T) {
		dir := t.TempDir()
		writeCacheFile(t, dir, "x.cache", headerBytes(v1CacheFileSize, SharedRegionMagicFlag, 1, 0))
		usage, err := loadCache(dir)
		assert.NilError(t, err)
		assert.Assert(t, usage != nil)
		assert.Assert(t, usage.Info != nil)
		defer func() { _ = usage.Unmap() }()
	})

	t.Run("v0 cache file (exact legacy size)", func(t *testing.T) {
		dir := t.TempDir()
		writeCacheFile(t, dir, "x.cache", headerBytes(1197897, SharedRegionMagicFlag, 0, 0))
		usage, err := loadCache(dir)
		assert.NilError(t, err)
		assert.Assert(t, usage != nil)
		assert.Assert(t, usage.Info != nil)
		defer func() { _ = usage.Unmap() }()
	})

	t.Run("open file fails", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("running as root bypasses file permission checks")
		}
		dir := t.TempDir()
		path := writeCacheFile(t, dir, "x.cache", headerBytes(64, SharedRegionMagicFlag, 1, 0))
		assert.NilError(t, os.Chmod(path, 0000))
		defer os.Chmod(path, 0666)
		_, err := loadCache(dir)
		assert.Assert(t, err != nil)
	})
}

func Test_ContainerLister_Update(t *testing.T) {
	t.Run("container path missing", func(t *testing.T) {
		l := &ContainerLister{
			containerPath: filepath.Join(t.TempDir(), "missing"),
			containers:    map[string]*ContainerUsage{},
			podLister:     newTestPodLister(),
		}
		err := l.Update()
		assert.Assert(t, err != nil)
	})

	t.Run("podLister error propagates", func(t *testing.T) {
		l := &ContainerLister{
			containerPath: t.TempDir(),
			containers:    map[string]*ContainerUsage{},
			podLister:     &fakePodLister{listErr: errors.New("boom")},
		}
		err := l.Update()
		assert.ErrorContains(t, err, "failed to list pods")
	})

	t.Run("skips non-directory entries", func(t *testing.T) {
		dir := t.TempDir()
		assert.NilError(t, os.WriteFile(filepath.Join(dir, "stray-file"), []byte("x"), 0644))
		l := &ContainerLister{
			containerPath: dir,
			containers:    map[string]*ContainerUsage{},
			podLister:     newTestPodLister(),
		}
		assert.NilError(t, l.Update())
		assert.Equal(t, len(l.containers), 0)
	})

	t.Run("recently modified stale dir is kept", func(t *testing.T) {
		dir := t.TempDir()
		ctrDir := filepath.Join(dir, "missing-pod-uid_ctr")
		assert.NilError(t, os.Mkdir(ctrDir, 0755))
		l := &ContainerLister{
			containerPath: dir,
			containers:    map[string]*ContainerUsage{},
			podLister:     newTestPodLister(),
		}
		assert.NilError(t, l.Update())
		_, err := os.Stat(ctrDir)
		assert.NilError(t, err)
	})

	t.Run("old stale dir with tracked mapping is removed and unmapped", func(t *testing.T) {
		dir := t.TempDir()
		entryName := "missing-pod-uid_ctr"
		ctrDir := filepath.Join(dir, entryName)
		assert.NilError(t, os.Mkdir(ctrDir, 0755))
		old := time.Now().Add(-2 * resyncInterval)
		assert.NilError(t, os.Chtimes(ctrDir, old, old))

		cacheDir := t.TempDir()
		writeCacheFile(t, cacheDir, "x.cache", headerBytes(v1CacheFileSize, SharedRegionMagicFlag, 1, 0))
		usage, err := loadCache(cacheDir)
		assert.NilError(t, err)

		l := &ContainerLister{
			containerPath: dir,
			containers:    map[string]*ContainerUsage{entryName: usage},
			podLister:     newTestPodLister(),
		}
		assert.NilError(t, l.Update())
		_, ok := l.containers[entryName]
		assert.Equal(t, ok, false)
		assert.Assert(t, usage.data == nil)
		assert.Assert(t, usage.Info == nil)
		_, err = os.Stat(ctrDir)
		assert.Assert(t, os.IsNotExist(err))
	})

	t.Run("already tracked entry is left untouched", func(t *testing.T) {
		dir := t.TempDir()
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default", UID: "uid1"}}
		entryName := "uid1_ctr"
		assert.NilError(t, os.Mkdir(filepath.Join(dir, entryName), 0755))
		existing := &ContainerUsage{PodUID: "uid1", ContainerName: "ctr"}
		l := &ContainerLister{
			containerPath: dir,
			containers:    map[string]*ContainerUsage{entryName: existing},
			podLister:     newTestPodLister(pod),
		}
		assert.NilError(t, l.Update())
		assert.Assert(t, l.containers[entryName] == existing)
	})

	t.Run("loadCache error is skipped", func(t *testing.T) {
		dir := t.TempDir()
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default", UID: "uid2"}}
		entryName := "uid2_ctr"
		ctrDir := filepath.Join(dir, entryName)
		assert.NilError(t, os.Mkdir(ctrDir, 0755))
		writeCacheFile(t, ctrDir, "a.cache", []byte("x"))
		writeCacheFile(t, ctrDir, "b.cache", []byte("x"))
		writeCacheFile(t, ctrDir, "c.cache", []byte("x"))
		l := &ContainerLister{
			containerPath: dir,
			containers:    map[string]*ContainerUsage{},
			podLister:     newTestPodLister(pod),
		}
		assert.NilError(t, l.Update())
		assert.Equal(t, len(l.containers), 0)
	})

	t.Run("no cuInit yet is skipped", func(t *testing.T) {
		dir := t.TempDir()
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default", UID: "uid3"}}
		entryName := "uid3_ctr"
		ctrDir := filepath.Join(dir, entryName)
		assert.NilError(t, os.Mkdir(ctrDir, 0755))
		l := &ContainerLister{
			containerPath: dir,
			containers:    map[string]*ContainerUsage{},
			podLister:     newTestPodLister(pod),
		}
		assert.NilError(t, l.Update())
		assert.Equal(t, len(l.containers), 0)
	})

	t.Run("new container is added with PodUID and ContainerName set", func(t *testing.T) {
		dir := t.TempDir()
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default", UID: "uid4"}}
		entryName := "uid4_mycontainer"
		ctrDir := filepath.Join(dir, entryName)
		assert.NilError(t, os.Mkdir(ctrDir, 0755))
		writeCacheFile(t, ctrDir, "x.cache", headerBytes(v1CacheFileSize, SharedRegionMagicFlag, 1, 0))
		l := &ContainerLister{
			containerPath: dir,
			containers:    map[string]*ContainerUsage{},
			podLister:     newTestPodLister(pod),
		}
		assert.NilError(t, l.Update())
		got, ok := l.containers[entryName]
		assert.Equal(t, ok, true)
		assert.Equal(t, got.PodUID, "uid4")
		assert.Equal(t, got.ContainerName, "mycontainer")
		defer func() { _ = got.Unmap() }()
	})

	t.Run("dir without underscore in name is skipped", func(t *testing.T) {
		dir := t.TempDir()
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default", UID: "nodashes"}}
		ctrDir := filepath.Join(dir, "nodashes")
		assert.NilError(t, os.Mkdir(ctrDir, 0755))
		writeCacheFile(t, ctrDir, "x.cache", headerBytes(v1CacheFileSize, SharedRegionMagicFlag, 1, 0))
		l := &ContainerLister{
			containerPath: dir,
			containers:    map[string]*ContainerUsage{},
			podLister:     newTestPodLister(pod),
		}
		assert.NilError(t, l.Update())
		assert.Equal(t, len(l.containers), 0)
	})

	t.Run("dir with trailing underscore is skipped", func(t *testing.T) {
		dir := t.TempDir()
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default", UID: "uid"}}
		ctrDir := filepath.Join(dir, "uid_")
		assert.NilError(t, os.Mkdir(ctrDir, 0755))
		writeCacheFile(t, ctrDir, "x.cache", headerBytes(v1CacheFileSize, SharedRegionMagicFlag, 1, 0))
		l := &ContainerLister{
			containerPath: dir,
			containers:    map[string]*ContainerUsage{},
			podLister:     newTestPodLister(pod),
		}
		assert.NilError(t, l.Update())
		assert.Equal(t, len(l.containers), 0)
	})
}

func Test_ContainerUsage_Unmap(t *testing.T) {
	t.Run("nil ContainerUsage", func(t *testing.T) {
		var c *ContainerUsage
		assert.NilError(t, c.Unmap())
	})

	t.Run("empty data", func(t *testing.T) {
		c := &ContainerUsage{}
		assert.NilError(t, c.Unmap())
		assert.Assert(t, c.data == nil)
		assert.Assert(t, c.Info == nil)
	})

	t.Run("valid mapped data", func(t *testing.T) {
		dir := t.TempDir()
		writeCacheFile(t, dir, "x.cache", headerBytes(v1CacheFileSize, SharedRegionMagicFlag, 1, 0))
		usage, err := loadCache(dir)
		assert.NilError(t, err)
		assert.Assert(t, usage != nil)
		assert.Assert(t, usage.data != nil)
		assert.Assert(t, usage.Info != nil)

		assert.NilError(t, usage.Unmap())
		assert.Assert(t, usage.data == nil)
		assert.Assert(t, usage.Info == nil)

		// Second call is safe and no-op
		assert.NilError(t, usage.Unmap())
	})
}
