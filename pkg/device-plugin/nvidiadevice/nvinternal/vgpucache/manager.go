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

// Manager serializes cache mutations with Prepare, while live Pod confirmation
// runs outside the mutation lock. Concurrent preparation invalidates a GC batch.
type Manager struct {
	root             string
	scanInterval     time.Duration
	gracePeriod      time.Duration
	listNodePods     func() ([]*corev1.Pod, error)
	listLiveNodePods func() ([]*corev1.Pod, error)
	mutex            sync.Mutex
	// generation is protected by mutex and advances before any Prepare mutation.
	generation uint64
	// scanMutex protects scan/retry state without blocking Prepare.
	scanMutex        sync.Mutex
	now              func() time.Time
	retryDelay       time.Duration
	nextConfirmation time.Time
}

// New constructs a cache manager around the device-plugin process's shared Pod
// informer snapshot and an authoritative API-server confirmation function.
func New(config Config, listNodePods, listLiveNodePods func() ([]*corev1.Pod, error)) (*Manager, error) {
	root := filepath.Clean(config.Root)
	if !filepath.IsAbs(root) || root == string(filepath.Separator) {
		return nil, fmt.Errorf("vGPU cache root must be an absolute non-root path: %q", config.Root)
	}
	if listNodePods == nil {
		return nil, errors.New("node Pod list function is nil")
	}
	if listLiveNodePods == nil {
		return nil, errors.New("live node Pod list function is nil")
	}
	if config.ScanInterval <= 0 {
		return nil, fmt.Errorf("vGPU cache scan interval must be positive: %v", config.ScanInterval)
	}
	if config.GracePeriod < 0 {
		return nil, fmt.Errorf("vGPU cache grace period must not be negative: %v", config.GracePeriod)
	}
	return &Manager{
		root:             root,
		scanInterval:     config.ScanInterval,
		gracePeriod:      config.GracePeriod,
		listNodePods:     listNodePods,
		listLiveNodePods: listLiveNodePods,
		now:              time.Now,
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
	m.generation++
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

// scanAndLog runs one GC pass and logs skipped or incomplete cleanup.
func (m *Manager) scanAndLog() {
	if err := m.scan(); err != nil {
		klog.InfoS("vGPU cache GC skipped or incomplete", "err", err)
	}
}

// scan collects expired candidates under mutex, confirms them without blocking
// Prepare, then rechecks identities before deletion. A concurrent Prepare skips
// the whole batch; the next scan retries from a new snapshot.
func (m *Manager) scan() error {
	// Skip overlapping scans instead of queuing redundant API requests.
	if !m.scanMutex.TryLock() {
		return nil
	}
	defer m.scanMutex.Unlock()
	if m.now().Before(m.nextConfirmation) {
		return nil
	}
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
	type candidate struct {
		podUID string
		path   string
		info   os.FileInfo
	}
	var candidates []candidate
	now := m.now()
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

		candidates = append(candidates, candidate{podUID: podUID, path: target, info: before})
	}
	if len(candidates) == 0 {
		return scanErr
	}
	generation := m.generation
	m.mutex.Unlock()
	livePods, confirmErr := m.listLiveNodePods()
	m.mutex.Lock()
	if confirmErr != nil {
		// Back off only failed API confirmations. The normal scan interval is
		// the initial delay, doubled on each failure and capped at one minute.
		if m.retryDelay == 0 {
			m.retryDelay = min(m.scanInterval, time.Minute)
		} else {
			m.retryDelay = min(2*m.retryDelay, time.Minute)
		}
		m.nextConfirmation = m.now().Add(m.retryDelay)
		return errors.Join(scanErr, fmt.Errorf("confirm node Pods before cache GC: %w", confirmErr))
	}
	m.retryDelay = 0
	m.nextConfirmation = time.Time{}
	if generation != m.generation {
		return scanErr
	}
	// The root may have been replaced externally while the mutex was released.
	currentRoot, err := os.Lstat(m.root)
	if err != nil {
		if os.IsNotExist(err) {
			return scanErr
		}
		return errors.Join(scanErr, fmt.Errorf("recheck vGPU cache root %s: %w", m.root, err))
	}
	if !currentRoot.IsDir() || !os.SameFile(rootInfo, currentRoot) {
		return scanErr
	}
	confirmedPodUIDs := make(map[string]struct{}, len(livePods))
	for _, pod := range livePods {
		if pod != nil {
			confirmedPodUIDs[string(pod.UID)] = struct{}{}
		}
	}
	for _, entry := range candidates {
		if _, exists := confirmedPodUIDs[entry.podUID]; exists {
			continue
		}
		target, before := entry.path, entry.info

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
		klog.InfoS("Removed stale vGPU cache directory", "directory", target, "podUID", entry.podUID)
	}
	return scanErr
}

// ensureRoot creates the cache root when absent and rejects symlinks and non-directories.
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

// parseCacheDirectoryName extracts the Pod UID and container name, rejecting malformed names and
// path separators.
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
