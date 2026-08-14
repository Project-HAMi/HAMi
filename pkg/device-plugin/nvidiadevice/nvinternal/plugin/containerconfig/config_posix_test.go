//go:build unix

/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * The HAMi Contributors require contributions made to
 * this file be licensed under the Apache-2.0 license or a
 * compatible open source license.
 */

package containerconfig

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestWriteConfig_RestrictiveUmask verifies that WriteConfig explicitly applies
// mode 0644 so config.json remains world-readable even when the process has a
// restrictive umask (e.g. 0077 or 0027).
func TestWriteConfig_RestrictiveUmask(t *testing.T) {
	// Set a very restrictive umask (0077 -> removes group and other permissions).
	oldMask := syscall.Umask(0077)
	defer syscall.Umask(oldMask)

	dir := t.TempDir()
	cfg := ContainerConfig{
		Version:       Version,
		PodUID:        "uid-umask-test",
		ContainerName: "ctr",
		Devices:       []DeviceLimitConfig{{Index: 0, UUID: "GPU-UMASK", MemoryLimitMB: 1000, SMLimit: 100}},
		SharedCachePath: "/tmp/u.cache",
	}

	if err := WriteConfig(dir, cfg); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, Filename))
	if err != nil {
		t.Fatalf("stat config.json: %v", err)
	}

	// Because of explicit os.Chmod(..., 0644), the file must be 0644 despite umask 0077.
	const wantMode = 0644
	if got := info.Mode().Perm(); got != wantMode {
		t.Errorf("file permissions with restrictive umask (0077): got %o, want %o", got, wantMode)
	}
}
