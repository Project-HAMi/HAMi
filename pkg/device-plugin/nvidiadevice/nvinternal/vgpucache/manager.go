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

// Package vgpucache owns preparation and garbage collection of the libvgpu
// per-container cache directories used by the NVIDIA device plugin.
package vgpucache

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

// Config configures a Manager.
type Config struct {
	Root         string
	ScanInterval time.Duration
	GracePeriod  time.Duration
}

// Manager serializes Allocate-time preparation and GC so a scan cannot remove
// a directory while it is being prepared for a new container.
type Manager struct {
	root         string
	scanInterval time.Duration
	gracePeriod  time.Duration
	listNodePods func() ([]*corev1.Pod, error)
	mutex        sync.Mutex
}

// New constructs a cache manager around the device-plugin process's shared Pod
// informer snapshot function.
func New(config Config, listNodePods func() ([]*corev1.Pod, error)) (*Manager, error) {
	root := filepath.Clean(config.Root)
	if !filepath.IsAbs(root) || root == string(filepath.Separator) {
		return nil, fmt.Errorf("vGPU cache root must be an absolute non-root path: %q", config.Root)
	}
	if listNodePods == nil {
		return nil, errors.New("node Pod list function is nil")
	}
	if config.ScanInterval <= 0 {
		return nil, fmt.Errorf("vGPU cache scan interval must be positive: %v", config.ScanInterval)
	}
	if config.GracePeriod < 0 {
		return nil, fmt.Errorf("vGPU cache grace period must not be negative: %v", config.GracePeriod)
	}
	return &Manager{
		root:         root,
		scanInterval: config.ScanInterval,
		gracePeriod:  config.GracePeriod,
		listNodePods: listNodePods,
	}, nil
}

// Prepare replaces any old cache directory for a Pod container and returns its
// host path for the Allocate response.
func (m *Manager) Prepare(podUID, containerName string) (string, error) {
	name := podUID + "_" + containerName
	if _, _, ok := parseCacheDirectoryName(name); !ok {
		return "", fmt.Errorf("invalid vGPU cache directory identity %q", name)
	}
	target := filepath.Join(m.root, name)
	if filepath.Dir(target) != m.root {
		return "", fmt.Errorf("vGPU cache directory escapes root: %q", target)
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()
	if err := m.ensureRoot(); err != nil {
		return "", err
	}
	if err := os.RemoveAll(target); err != nil {
		return "", fmt.Errorf("remove previous vGPU cache directory %s: %w", target, err)
	}
	if err := os.Mkdir(target, 0o777); err != nil {
		return "", fmt.Errorf("create vGPU cache directory %s: %w", target, err)
	}
	if err := os.Chmod(target, 0o777); err != nil {
		return "", fmt.Errorf("set vGPU cache directory permissions %s: %w", target, err)
	}
	return target, nil
}

// Run scans once at startup and then periodically until the process stops.
func (m *Manager) Run(ctx context.Context) {
	m.scanAndLog()
	ticker := time.NewTicker(m.scanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.scanAndLog()
		}
	}
}

func (m *Manager) scanAndLog() {
	if err := m.scan(); err != nil {
		klog.InfoS("vGPU cache GC skipped or incomplete", "err", err)
	}
}

func (m *Manager) scan() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	pods, err := m.listNodePods()
	if err != nil {
		return fmt.Errorf("list node Pods: %w", err)
	}
	livePodUIDs := make(map[string]struct{}, len(pods))
	for _, pod := range pods {
		if pod != nil {
			livePodUIDs[string(pod.UID)] = struct{}{}
		}
	}

	rootInfo, err := os.Lstat(m.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat vGPU cache root %s: %w", m.root, err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return fmt.Errorf("vGPU cache root is not a real directory: %s", m.root)
	}
	entries, err := os.ReadDir(m.root)
	if err != nil {
		return fmt.Errorf("read vGPU cache root %s: %w", m.root, err)
	}

	var scanErr error
	now := time.Now()
	for _, entry := range entries {
		podUID, _, ok := parseCacheDirectoryName(entry.Name())
		if !ok {
			klog.InfoS("Skipping malformed vGPU cache entry", "entry", entry.Name())
			continue
		}
		if _, live := livePodUIDs[podUID]; live {
			continue
		}

		target := filepath.Join(m.root, entry.Name())
		if filepath.Dir(target) != m.root {
			continue
		}
		before, err := os.Lstat(target)
		if err != nil {
			if !os.IsNotExist(err) {
				scanErr = errors.Join(scanErr, fmt.Errorf("stat vGPU cache entry %s: %w", target, err))
			}
			continue
		}
		if before.Mode()&os.ModeSymlink != 0 || !before.IsDir() {
			klog.InfoS("Skipping non-directory vGPU cache entry", "entry", target)
			continue
		}
		if before.ModTime().Add(m.gracePeriod).After(now) {
			continue
		}

		current, err := os.Lstat(target)
		if err != nil {
			if !os.IsNotExist(err) {
				scanErr = errors.Join(scanErr, fmt.Errorf("recheck vGPU cache entry %s: %w", target, err))
			}
			continue
		}
		if !os.SameFile(before, current) || !before.ModTime().Equal(current.ModTime()) {
			klog.InfoS("Skipping vGPU cache entry changed during GC", "directory", target)
			continue
		}
		if err := os.RemoveAll(target); err != nil {
			scanErr = errors.Join(scanErr, fmt.Errorf("remove vGPU cache directory %s: %w", target, err))
			continue
		}
		klog.InfoS("Removed stale vGPU cache directory", "directory", target, "podUID", podUID)
	}
	return scanErr
}

func (m *Manager) ensureRoot() error {
	info, err := os.Lstat(m.root)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(m.root, 0o755); err != nil {
			return fmt.Errorf("create vGPU cache root %s: %w", m.root, err)
		}
		info, err = os.Lstat(m.root)
	}
	if err != nil {
		return fmt.Errorf("stat vGPU cache root %s: %w", m.root, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("vGPU cache root is not a real directory: %s", m.root)
	}
	return nil
}

func parseCacheDirectoryName(name string) (string, string, bool) {
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
		return "", "", false
	}
	parts := strings.SplitN(name, "_", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
