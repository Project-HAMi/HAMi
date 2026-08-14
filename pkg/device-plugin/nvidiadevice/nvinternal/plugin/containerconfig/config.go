/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * The HAMi Contributors require contributions made to
 * this file be licensed under the Apache-2.0 license or a
 * compatible open source license.
 */

// Package containerconfig defines the per-container GPU allocation config that
// the NVIDIA device plugin writes to disk during Allocate so that libvgpu.so
// can recover GPU limits even when process environment variables have been
// scrubbed by SSH, su, sudo, or login shells (issue #2125).
package containerconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Version is the schema version written into every config.json.
// Increment when making backward-incompatible changes to the struct layout
// so that libvgpu.so can detect and handle old vs new files correctly.
const Version = 1

// Filename is the name of the per-container config file written into the
// container's cache directory. It is a deliberately short, fixed name so that
// libvgpu.so can locate it without any additional coordination.
const Filename = "config.json"

// DeviceLimitConfig holds the resource constraints for a single GPU device
// allocated to a container.
//
// WHY a struct per device: a container can request multiple physical GPUs.
// Each GPU gets its own memory and core limits, so they must be stored
// individually rather than as a single aggregate.
type DeviceLimitConfig struct {
	// Index is the 0-based device slot index within this container's allocation
	// (matches the suffix of CUDA_DEVICE_MEMORY_LIMIT_<index>).
	Index int `json:"index"`

	// UUID is the physical GPU UUID string (e.g. "GPU-xxxxxxxx-...").
	// libvgpu.so can cross-check this against the device it detects at runtime
	// to ensure it is applying limits to the correct GPU.
	UUID string `json:"uuid"`

	// MemoryLimitMB is the GPU memory quota for this device in mebibytes
	// (matches the numeric part of CUDA_DEVICE_MEMORY_LIMIT_<index>=NNNm).
	MemoryLimitMB int32 `json:"memory_limit_mb"`

	// SMLimit is the GPU compute utilisation limit, expressed as a percentage
	// of streaming-multiprocessor throughput (0 = unlimited).
	// Matches CUDA_DEVICE_SM_LIMIT.
	SMLimit int32 `json:"sm_limit_percent"`
}

// ContainerConfig is the complete allocation record written to config.json.
// It mirrors every environment variable that the device plugin injects so that
// processes with a clean environment (SSH logins, su/sudo shells, child
// processes started with an explicit empty env) can still recover the full
// isolation policy directly from the filesystem.
//
// Backward compatibility: all env-var injection in Allocate is kept unchanged.
// This file is additive — it provides a second, env-independent path to the
// same information.
type ContainerConfig struct {
	// Version is the schema version (currently 1).
	// libvgpu.so should reject or warn on versions it does not understand.
	Version int `json:"version"`

	// PodUID is the Kubernetes pod UID for operator diagnostics.
	PodUID string `json:"pod_uid"`

	// ContainerName is the Kubernetes container name within the pod.
	ContainerName string `json:"container_name"`

	// Devices lists the per-GPU constraints in ascending index order.
	Devices []DeviceLimitConfig `json:"devices"`

	// SharedCachePath is the path of the shared-memory tracker file
	// (the value of CUDA_DEVICE_MEMORY_SHARED_CACHE).
	// All processes within the container must attach to the same cache file so
	// that their aggregate allocations are correctly accounted.
	SharedCachePath string `json:"shared_cache_path"`

	// Oversubscribe indicates whether memory over-subscription is enabled
	// (true when DeviceMemoryScaling > 1; matches CUDA_OVERSUBSCRIBE=true).
	Oversubscribe bool `json:"oversubscribe"`

	// DisableCoreLimit, when true, means compute-utilisation enforcement is
	// intentionally disabled (GPU_CORE_UTILIZATION_POLICY=disable).
	DisableCoreLimit bool `json:"disable_core_limit"`

	// LogLevel is the optional logging verbosity forwarded from the scheduler
	// config (LIBCUDA_LOG_LEVEL). Empty string means default.
	LogLevel string `json:"log_level,omitempty"`
}

// WriteConfig serialises cfg as JSON and writes it atomically to
// directory/config.json with permissions 0644.
//
// WHY 0644 and not 0600 or 0777:
//   - 0644 allows any UID inside the container (including non-root users and
//     SSH-spawned shells) to read the file.
//   - The file contains no secrets — only resource limits that the container
//     is already entitled to know (they are also in its environment).
//   - Write permission is root-only (the device plugin runs as root) to
//     prevent container processes from tampering with their own limits.
//
// WHY atomic write (temp file + rename):
//   - libvgpu.so may open this file at any point after container start.
//   - A direct os.WriteFile leaves the file half-written during the syscall.
//   - A rename from a temp file on the same filesystem is atomic on Linux,
//     so the reader always sees either the complete old file or the complete
//     new file, never a partial write.
func WriteConfig(directory string, cfg ContainerConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal container config: %w", err)
	}

	// Write to a sibling temp file first so the rename is intra-directory
	// (same filesystem mount) and therefore atomic on Linux.
	tmpPath := filepath.Join(directory, ".config.json.tmp")
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write container config temp file: %w", err)
	}

	finalPath := filepath.Join(directory, Filename)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath) // best-effort cleanup of the orphaned temp file
		return fmt.Errorf("rename container config into place: %w", err)
	}

	return nil
}
