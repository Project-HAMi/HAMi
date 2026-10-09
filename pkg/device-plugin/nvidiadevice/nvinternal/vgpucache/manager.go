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

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/nodepodinformer"
)

const maxDeleteAttempts = 5

// deletionFailure belongs to one directory identity, not merely its pathname.
// mutex protects these records, including resets when Prepare replaces a directory.
type deletionFailure struct {
	info     os.FileInfo
	attempts int
}

// Config configures a Manager.
type Config struct {
	Root         string
	ScanInterval time.Duration
	GracePeriod  time.Duration
	// OnAbandoned must enqueue notifications without waiting for network I/O.
	OnAbandoned func(path string, attempts int, err error)
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
	scanMutex         sync.Mutex
	now               func() time.Time
	confirmationRetry nodepodinformer.ConfirmationBackoff
	deletionFailures  map[string]deletionFailure
	onAbandoned       func(string, int, error)
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
		root:              root,
		scanInterval:      config.ScanInterval,
		gracePeriod:       config.GracePeriod,
		listNodePods:      listNodePods,
		listLiveNodePods:  listLiveNodePods,
		now:               time.Now,
		confirmationRetry: nodepodinformer.ConfirmationBackoff{Initial: config.ScanInterval},
		deletionFailures:  make(map[string]deletionFailure),
		onAbandoned:       config.OnAbandoned,
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
	delete(m.deletionFailures, target)
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
// the whole batch. Incomplete rounds back off before taking a new snapshot.
func (m *Manager) scan() error {
	// Skip overlapping scans instead of queuing redundant API requests.
	if !m.scanMutex.TryLock() {
		return nil
	}
	defer m.scanMutex.Unlock()
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
			m.confirmationRetry.Reset()
			clear(m.deletionFailures)
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
	present := make(map[string]struct{}, len(entries))
	now := m.now()
	for _, entry := range entries {
		present[filepath.Join(m.root, entry.Name())] = struct{}{}
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
		if failure, ok := m.deletionFailures[target]; ok {
			if !os.SameFile(failure.info, before) {
				delete(m.deletionFailures, target)
			} else if failure.attempts >= maxDeleteAttempts {
				continue
			}
		}
		if before.ModTime().Add(m.gracePeriod).After(now) {
			continue
		}

		candidates = append(candidates, candidate{podUID: podUID, path: target, info: before})
	}
	for path := range m.deletionFailures {
		if _, exists := present[path]; !exists {
			delete(m.deletionFailures, path)
		}
	}
	if len(candidates) == 0 {
		if scanErr == nil {
			m.confirmationRetry.Reset()
		}
		return scanErr
	}
	if !m.confirmationRetry.Ready(m.now()) {
		return scanErr
	}
	complete := false
	defer func() { m.confirmationRetry.Finish(m.now(), complete) }()
	generation := m.generation
	m.mutex.Unlock()
	livePods, confirmErr := m.listLiveNodePods()
	m.mutex.Lock()
	if confirmErr != nil {
		return errors.Join(scanErr, fmt.Errorf("confirm node Pods before cache GC: %w", confirmErr))
	}
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
	complete = scanErr == nil
	for _, entry := range candidates {
		if _, exists := confirmedPodUIDs[entry.podUID]; exists {
			complete = false
			continue
		}
		target, before := entry.path, entry.info

		current, err := os.Lstat(target)
		if err != nil {
			if !os.IsNotExist(err) {
				complete = false
				scanErr = errors.Join(scanErr, fmt.Errorf("recheck vGPU cache entry %s: %w", target, err))
			}
			continue
		}
		if !os.SameFile(before, current) || !before.ModTime().Equal(current.ModTime()) {
			complete = false
			klog.InfoS("Skipping vGPU cache entry changed during GC", "directory", target)
			continue
		}
		if err := os.RemoveAll(target); err != nil {
			failure := m.deletionFailures[target]
			failure.info = current
			failure.attempts++
			m.deletionFailures[target] = failure
			scanErr = errors.Join(scanErr, fmt.Errorf("remove vGPU cache directory %s: %w", target, err))
			if failure.attempts == maxDeleteAttempts {
				klog.ErrorS(err, "Abandoning vGPU cache cleanup; remove the directory manually or restart the device plugin after fixing the cause",
					"directory", target, "podUID", entry.podUID, "attempts", failure.attempts)
				if m.onAbandoned != nil {
					m.onAbandoned(target, failure.attempts, err)
				}
			} else {
				complete = false
			}
			continue
		}
		delete(m.deletionFailures, target)
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
