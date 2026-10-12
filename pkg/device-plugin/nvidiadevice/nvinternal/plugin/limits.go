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

package plugin

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/hostpid"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/vgpucache"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

const (
	// overrideEnvContainerPath is where HAMi-core reads the limits file
	// (ENV_OVERRIDE_FILE). The path is compiled into HAMi-core and never passed
	// through the environment, which a login session (SSH, su -, sudo, cron)
	// does not inherit.
	overrideEnvContainerPath = "/overrideEnv"

	// limitsFormatKey records the file layout for a future HAMi-core reader.
	limitsFormatKey     = "HAMI_LIMITS_FORMAT"
	limitsFormatVersion = "1"

	// limitsFileMode lets a non-root container user read the file.
	limitsFileMode = 0o644

	// memoryLimitEnvPrefix matches CUDA_DEVICE_MEMORY_LIMIT and the per-device
	// CUDA_DEVICE_MEMORY_LIMIT_<i> variables.
	memoryLimitEnvPrefix = "CUDA_DEVICE_MEMORY_LIMIT"
)

// limitsResponseEnvs are the other variables the allocate response sets for
// HAMi-core.
var limitsResponseEnvs = []string{
	"CUDA_DEVICE_SM_LIMIT",
	"CUDA_DEVICE_MEMORY_SHARED_CACHE",
	"CUDA_OVERSUBSCRIBE",
	"LIBCUDA_LOG_LEVEL",
	util.CoreLimitSwitch,
	hostpid.EnvironmentVariable,
}

// limitsContainerEnvs are the HAMi-core variables the container spec may set;
// its value wins over the allocate response, as it does in the environment.
var limitsContainerEnvs = []string{
	util.TaskPriority,
	util.CoreLimitSwitch,
	"LIBCUDA_LOG_LEVEL",
}

// writeContainerLimits writes the limits file of one container beside its
// cache directory and returns the file's host path.
func writeContainerLimits(cacheDirectory string, responseEnvs map[string]string, ctr *corev1.Container) (string, error) {
	path := vgpucache.LimitsFilePath(cacheDirectory)
	if err := writeLimitsFile(path, cacheDirectory, limitsFileValues(responseEnvs, ctr)); err != nil {
		return "", err
	}
	return path, nil
}

// limitsFileValues returns what the limits file holds for a container: the
// HAMi-core variables of the allocate response, then limitsContainerEnvs from
// the container spec. kubelet passes the spec's variables after the device
// plugin's, so the container sees the spec value when both set one. Only
// literal values are taken, because a valueFrom reference is not resolved here.
func limitsFileValues(responseEnvs map[string]string, ctr *corev1.Container) map[string]string {
	values := map[string]string{limitsFormatKey: limitsFormatVersion}
	for name, value := range responseEnvs {
		if strings.HasPrefix(name, memoryLimitEnvPrefix) || slices.Contains(limitsResponseEnvs, name) {
			values[name] = value
		}
	}
	if ctr == nil {
		return values
	}
	for _, env := range ctr.Env {
		if env.ValueFrom == nil && slices.Contains(limitsContainerEnvs, env.Name) {
			values[env.Name] = env.Value
		}
	}
	return values
}

// formatLimitsFile renders one KEY=VALUE line per value, sorted by key, which
// is the format HAMi-core's load_env_from_file() reads. A line break in a key
// or value would let one entry add another, so it is refused.
func formatLimitsFile(values map[string]string) ([]byte, error) {
	var b strings.Builder
	for _, key := range slices.Sorted(maps.Keys(values)) {
		value := values[key]
		if key == "" || strings.ContainsAny(key, "=\n\r\x00") || strings.ContainsAny(value, "\n\r\x00") {
			return nil, fmt.Errorf("invalid limits file entry %q", key)
		}
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(value)
		b.WriteByte('\n')
	}
	return []byte(b.String()), nil
}

// writeLimitsFile atomically replaces path with the rendered values. The
// temporary file is created in tmpDir, which must be on the same filesystem as
// path. If the plugin stops before the rename, the leftover is removed with
// tmpDir instead of staying beside path.
func writeLimitsFile(path, tmpDir string, values map[string]string) error {
	content, err := formatLimitsFile(values)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(tmpDir, ".limits-*")
	if err != nil {
		return fmt.Errorf("create temporary limits file: %w", err)
	}
	tmpPath := tmp.Name()
	if err := writeSyncClose(tmp, content); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("publish limits file %s: %w", path, err)
	}
	return nil
}

// writeSyncClose writes content to f, makes it readable for any container user,
// flushes it to disk and closes f, so the rename that follows never publishes a
// partial file.
func writeSyncClose(f *os.File, content []byte) error {
	_, err := f.Write(content)
	if err == nil {
		err = f.Chmod(limitsFileMode)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write limits file %s: %w", f.Name(), err)
	}
	return nil
}
