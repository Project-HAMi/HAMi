/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * The HAMi Contributors require contributions made to
 * this file be licensed under the Apache-2.0 license or a
 * compatible open source license.
 */

package containerconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteConfig_HappyPath(t *testing.T) {
	dir := t.TempDir()

	cfg := ContainerConfig{
		Version:       Version,
		PodUID:        "pod-uid-abc",
		ContainerName: "my-container",
		Devices: []DeviceLimitConfig{
			{Index: 0, UUID: "GPU-1234", MemoryLimitMB: 3000, SMLimit: 50},
			{Index: 1, UUID: "GPU-5678", MemoryLimitMB: 4000, SMLimit: 80},
		},
		SharedCachePath:  "/usr/local/vgpu/abc.cache",
		Oversubscribe:    false,
		DisableCoreLimit: false,
		LogLevel:         "info",
	}

	if err := WriteConfig(dir, cfg); err != nil {
		t.Fatalf("WriteConfig returned unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, Filename))
	if err != nil {
		t.Fatalf("config.json not found after write: %v", err)
	}

	var got ContainerConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("config.json is not valid JSON: %v", err)
	}

	if got.Version != Version {
		t.Errorf("version: got %d, want %d", got.Version, Version)
	}
	if got.PodUID != cfg.PodUID {
		t.Errorf("pod_uid: got %q, want %q", got.PodUID, cfg.PodUID)
	}
	if got.ContainerName != cfg.ContainerName {
		t.Errorf("container_name: got %q, want %q", got.ContainerName, cfg.ContainerName)
	}
	if len(got.Devices) != 2 {
		t.Fatalf("devices length: got %d, want 2", len(got.Devices))
	}
	if got.Devices[0].MemoryLimitMB != 3000 {
		t.Errorf("devices[0].memory_limit_mb: got %d, want 3000", got.Devices[0].MemoryLimitMB)
	}
	if got.Devices[1].MemoryLimitMB != 4000 {
		t.Errorf("devices[1].memory_limit_mb: got %d, want 4000", got.Devices[1].MemoryLimitMB)
	}
	if got.SharedCachePath != cfg.SharedCachePath {
		t.Errorf("shared_cache_path: got %q, want %q", got.SharedCachePath, cfg.SharedCachePath)
	}
}

// TestWriteConfig_FilePermissions verifies config.json is written 0644 so
// non-root users (SSH logins) can read it.
func TestWriteConfig_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	cfg := ContainerConfig{
		Version:       Version,
		PodUID:        "uid-perm-test",
		ContainerName: "ctr",
		Devices:       []DeviceLimitConfig{{Index: 0, UUID: "GPU-PERM", MemoryLimitMB: 1000, SMLimit: 100}},
		SharedCachePath: "/tmp/x.cache",
	}

	if err := WriteConfig(dir, cfg); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, Filename))
	if err != nil {
		t.Fatalf("stat config.json: %v", err)
	}

	if runtime.GOOS == "windows" {
		t.Skip("skipping POSIX file permission bit check on Windows")
	}

	// 0644: world-readable so SSH-spawned non-root shells can open the file.
	const wantMode = 0644
	if got := info.Mode().Perm(); got != wantMode {
		t.Errorf("file permissions: got %o, want %o", got, wantMode)
	}
}

// TestWriteConfig_Idempotent verifies that calling WriteConfig twice overwrites
// the first file cleanly (no stale partial state).
func TestWriteConfig_Idempotent(t *testing.T) {
	dir := t.TempDir()

	write := func(mem int32) {
		cfg := ContainerConfig{
			Version:       Version,
			PodUID:        "uid-idem",
			ContainerName: "ctr",
			Devices:       []DeviceLimitConfig{{Index: 0, UUID: "GPU-X", MemoryLimitMB: mem, SMLimit: 50}},
			SharedCachePath: "/tmp/y.cache",
		}
		if err := WriteConfig(dir, cfg); err != nil {
			t.Fatalf("WriteConfig(%d): %v", mem, err)
		}
	}

	write(1000)
	write(2000)

	data, _ := os.ReadFile(filepath.Join(dir, Filename))
	var got ContainerConfig
	_ = json.Unmarshal(data, &got)
	if got.Devices[0].MemoryLimitMB != 2000 {
		t.Errorf("expected second write (2000m) to win; got %dm", got.Devices[0].MemoryLimitMB)
	}
}

// TestWriteConfig_NonExistentDir verifies WriteConfig returns an error for a
// missing directory rather than panicking.
func TestWriteConfig_NonExistentDir(t *testing.T) {
	if err := WriteConfig("/this/path/does/not/exist", ContainerConfig{}); err == nil {
		t.Error("expected error for non-existent directory, got nil")
	}
}

// TestWriteConfig_OversubscribeFlag verifies the oversubscribe field is
// faithfully preserved (controls libvgpu.so behaviour when scaling > 1).
func TestWriteConfig_OversubscribeFlag(t *testing.T) {
	dir := t.TempDir()
	cfg := ContainerConfig{
		Version:         Version,
		PodUID:          "uid-over",
		ContainerName:   "ctr-over",
		Devices:         []DeviceLimitConfig{{Index: 0, UUID: "GPU-O", MemoryLimitMB: 5000, SMLimit: 100}},
		SharedCachePath: "/tmp/o.cache",
		Oversubscribe:   true,
	}
	if err := WriteConfig(dir, cfg); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, Filename))
	var got ContainerConfig
	_ = json.Unmarshal(data, &got)
	if !got.Oversubscribe {
		t.Error("oversubscribe: expected true, got false")
	}
}
