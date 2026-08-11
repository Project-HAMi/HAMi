/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * Copyright (c) 2026 The HAMi Authors.
 */

package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/hostpid"
)

const hostPIDLockParentDirectory = "/tmp/vgpulock"

const hostPIDLockParentMode = os.FileMode(0o777) | os.ModeSticky

const hostPIDLockParentCreateMode = uint32(0o1777)

const hostPIDLockTrustedOwner = uint32(0)

var prepareHostPIDLockParentForAllocation = prepareDefaultHostPIDLockParent

func prepareDefaultHostPIDLockParent() error {
	return prepareHostPIDLockParent(hostPIDLockParentDirectory,
		hostPIDLockTrustedOwner)
}

func createHostPIDLockParent(parentFD int, baseName string) error {
	return createHostPIDLockParentWith(unix.Mkdirat, parentFD, baseName)
}

func createHostPIDLockParentWith(
	mkdirat func(int, string, uint32) error,
	parentFD int, baseName string) error {
	err := mkdirat(parentFD, baseName, hostPIDLockParentCreateMode)
	if err != nil && err != unix.EEXIST {
		return err
	}
	return nil
}

func prepareHostPIDLockParent(directory string, trustedOwner uint32) error {
	cleanDirectory := filepath.Clean(directory)
	if !filepath.IsAbs(cleanDirectory) ||
		cleanDirectory == string(filepath.Separator) {
		return fmt.Errorf("directory must be an absolute non-root path")
	}
	parentDirectory := filepath.Dir(cleanDirectory)
	baseName := filepath.Base(cleanDirectory)
	parentFD, err := unix.Open(parentDirectory,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open parent directory without symlinks: %w", err)
	}
	parentFile := os.NewFile(uintptr(parentFD), parentDirectory)
	if parentFile == nil {
		_ = unix.Close(parentFD)
		return fmt.Errorf("wrap parent directory descriptor")
	}
	defer parentFile.Close()

	parentInfo, err := parentFile.Stat()
	if err != nil {
		return fmt.Errorf("inspect parent directory: %w", err)
	}
	parentStat, ok := parentInfo.Sys().(*syscall.Stat_t)
	parentMode := parentInfo.Mode()
	if !ok || !parentInfo.IsDir() || parentStat.Uid != trustedOwner {
		return fmt.Errorf("parent directory is not owned by trusted UID %d",
			trustedOwner)
	}
	if parentMode.Perm()&0o022 != 0 && parentMode&os.ModeSticky == 0 {
		return fmt.Errorf("writable parent directory is not sticky")
	}

	if err := createHostPIDLockParent(parentFD, baseName); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	fd, err := unix.Openat(parentFD, baseName,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open directory without symlinks: %w", err)
	}
	file := os.NewFile(uintptr(fd), cleanDirectory)
	if file == nil {
		_ = unix.Close(fd)
		return fmt.Errorf("wrap directory descriptor")
	}
	defer file.Close()

	openedInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect opened directory: %w", err)
	}
	openedStat, ok := openedInfo.Sys().(*syscall.Stat_t)
	if !ok || !openedInfo.IsDir() || openedStat.Uid != trustedOwner {
		return fmt.Errorf("directory is not owned by trusted UID %d",
			trustedOwner)
	}
	if err := file.Chmod(hostPIDLockParentMode); err != nil {
		return fmt.Errorf("set sticky directory mode: %w", err)
	}

	verifiedInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("verify opened directory: %w", err)
	}
	verifiedStat, ok := verifiedInfo.Sys().(*syscall.Stat_t)
	var currentStat unix.Stat_t
	if err := unix.Fstatat(parentFD, baseName, &currentStat,
		unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fmt.Errorf("reinspect directory entry: %w", err)
	}
	if !ok || currentStat.Mode&unix.S_IFMT != unix.S_IFDIR ||
		currentStat.Dev != verifiedStat.Dev ||
		currentStat.Ino != verifiedStat.Ino ||
		currentStat.Uid != trustedOwner ||
		verifiedStat.Uid != trustedOwner ||
		currentStat.Mode&0o7777 != 0o1777 ||
		verifiedInfo.Mode()&(os.ModePerm|os.ModeSetuid|os.ModeSetgid|
			os.ModeSticky) !=
			hostPIDLockParentMode {
		return fmt.Errorf("directory changed while it was prepared")
	}
	return nil
}

func configureHostPIDBroker(
	response *kubeletdevicepluginv1beta1.ContainerAllocateResponse) {
	if response.Envs != nil {
		delete(response.Envs, hostpid.EnvironmentVariable)
	}
	if !hostpid.Enabled(os.Getenv(hostpid.EnvironmentVariable)) {
		removeHostPIDMounts(response, hostpid.ContainerDirectory)
		return
	}
	if response.Envs == nil {
		response.Envs = make(map[string]string)
	}
	response.Envs[hostpid.EnvironmentVariable] = "1"
	configureCanonicalHostPIDMount(response, hostpid.ContainerDirectory,
		hostpid.ServerDirectory, true)
	orderHostPIDMountPair(response)
}

func removeHostPIDMounts(
	response *kubeletdevicepluginv1beta1.ContainerAllocateResponse,
	containerPath string) {
	configuredMounts := make([]*kubeletdevicepluginv1beta1.Mount, 0,
		len(response.Mounts))
	for _, mount := range response.Mounts {
		if mountTargetsContainerPath(mount, containerPath) {
			continue
		}
		configuredMounts = append(configuredMounts, mount)
	}
	response.Mounts = configuredMounts
}

func configureHostPIDLockParentMount(
	response *kubeletdevicepluginv1beta1.ContainerAllocateResponse) {
	configureCanonicalHostPIDMount(response, hostPIDLockParentDirectory,
		hostPIDLockParentDirectory, false)
}

func orderHostPIDMountPair(
	response *kubeletdevicepluginv1beta1.ContainerAllocateResponse) {
	parentIndex := -1
	brokerIndex := -1
	for index, mount := range response.Mounts {
		if mount == nil {
			continue
		}
		if parentIndex < 0 &&
			mountTargetsContainerPath(mount, hostPIDLockParentDirectory) {
			parentIndex = index
		}
		if brokerIndex < 0 &&
			mountTargetsContainerPath(mount, hostpid.ContainerDirectory) {
			brokerIndex = index
		}
	}
	if parentIndex < 0 || brokerIndex < 0 || parentIndex < brokerIndex {
		return
	}

	parentMount := response.Mounts[parentIndex]
	brokerMount := response.Mounts[brokerIndex]
	orderedMounts := make([]*kubeletdevicepluginv1beta1.Mount, 0,
		len(response.Mounts))
	for index, mount := range response.Mounts {
		switch index {
		case brokerIndex:
			orderedMounts = append(orderedMounts, parentMount, brokerMount)
		case parentIndex:
			continue
		default:
			orderedMounts = append(orderedMounts, mount)
		}
	}
	response.Mounts = orderedMounts
}

func configureCanonicalHostPIDMount(
	response *kubeletdevicepluginv1beta1.ContainerAllocateResponse,
	containerPath string, hostPath string, readOnly bool) {
	canonicalMount := &kubeletdevicepluginv1beta1.Mount{
		ContainerPath: containerPath,
		HostPath:      hostPath,
		ReadOnly:      readOnly,
	}
	canonicalMountAdded := false
	configuredMounts := make([]*kubeletdevicepluginv1beta1.Mount, 0,
		len(response.Mounts)+1)
	for _, mount := range response.Mounts {
		if mountTargetsContainerPath(mount, containerPath) {
			if !canonicalMountAdded {
				configuredMounts = append(configuredMounts, canonicalMount)
				canonicalMountAdded = true
			}
			continue
		}
		configuredMounts = append(configuredMounts, mount)
	}
	if !canonicalMountAdded {
		configuredMounts = append(configuredMounts, canonicalMount)
	}
	response.Mounts = configuredMounts
}

func mountTargetsContainerPath(
	mount *kubeletdevicepluginv1beta1.Mount, containerPath string) bool {
	return mount != nil && filepath.Clean(filepath.Join(
		string(filepath.Separator), mount.ContainerPath)) == containerPath
}
