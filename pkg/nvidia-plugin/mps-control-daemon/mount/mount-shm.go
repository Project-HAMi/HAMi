/**
# Copyright 2024 NVIDIA CORPORATION
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
**/

package mount

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/urfave/cli/v2"
	"k8s.io/klog/v2"
	"k8s.io/mount-utils"
)

// NewCommand constructs a mount command.
func NewCommand() *cli.Command {
	c := cli.Command{
		Name:   "mount-shm",
		Usage:  "Set up the /dev/shm mount required by the MPS daemon",
		Action: mountShm,
	}

	return &c
}

// mountShm creates a tmpfs mount at /mps/shm to be used by the mps control daemon.
func mountShm(c *cli.Context) error {
	mountExecutable, err := exec.LookPath("mount")
	if err != nil {
		return fmt.Errorf("error finding 'mount' executable: %w", err)
	}
	mounter := mount.New(mountExecutable)

	// TODO: /mps should be configurable.
	shmDir := "/mps/shm"
	err = mount.CleanupMountPoint(shmDir, mounter, true)
	if err != nil {
		return fmt.Errorf("error unmounting %v: %w", shmDir, err)
	}

	if err := os.MkdirAll(shmDir, 0755); err != nil {
		return fmt.Errorf("error creating directory %v: %w", shmDir, err)
	}

	sizeArg := fmt.Sprintf("size=%v", getDefaultShmSize())
	mountOptions := []string{"rw", "nosuid", "nodev", "noexec", "relatime", sizeArg}
	if err := mounter.Mount("shm", shmDir, "tmpfs", mountOptions); err != nil {
		return fmt.Errorf("error mounting %v as tmpfs: %w", shmDir, err)
	}

	return nil
}

// getDefaultShmSize returns the default size for the tmpfs to be created.
// This reads /proc/meminfo to get the total memory to calculate this. If this
// fails a fallback size of 65536k is used.
func getDefaultShmSize() string {
	const fallbackSize = "65536k"

	meminfo, err := os.Open("/proc/meminfo")
	if err != nil {
		klog.ErrorS(err, "failed to open /proc/meminfo")
		return fallbackSize
	}
	defer func() {
		_ = meminfo.Close()
	}()

	scanner := bufio.NewScanner(meminfo)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}

		parts := strings.SplitN(strings.TrimSpace(strings.TrimPrefix(line, "MemTotal:")), " ", 2)
		memTotal, err := strconv.Atoi(parts[0])
		if err != nil {
			klog.ErrorS(err, "could not convert MemTotal to an integer")
			return fallbackSize
		}

		var unit string
		if len(parts) == 2 {
			unit = string(parts[1][0])
		}

		return fmt.Sprintf("%d%s", memTotal/2, unit)
	}
	return fallbackSize
}
