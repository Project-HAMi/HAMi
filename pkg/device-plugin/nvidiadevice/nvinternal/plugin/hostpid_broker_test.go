/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * Copyright (c) 2026 The HAMi Authors.
 */

package plugin

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/hostpid"
)

func TestMain(m *testing.M) {
	prepareHostPIDLockParentForAllocation = func() error { return nil }
	os.Exit(m.Run())
}

func TestPrepareHostPIDLockParent(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "vgpulock")

	require.Equal(t, uint32(0), hostPIDLockTrustedOwner)
	require.NoError(t, os.MkdirAll(directory, 0o700))
	require.NoError(t, prepareHostPIDLockParent(directory,
		uint32(os.Geteuid())))

	info, err := os.Stat(directory)
	require.NoError(t, err)
	require.True(t, info.IsDir())
	require.Equal(t, hostPIDLockParentMode,
		info.Mode()&(os.ModePerm|os.ModeSticky))
}

func TestPrepareHostPIDLockParentRequestsStickyCreateMode(t *testing.T) {
	called := false
	var requestedFD int
	var requestedName string
	var requestedMode uint32
	require.NoError(t, createHostPIDLockParentWith(
		func(parentFD int, baseName string, mode uint32) error {
			called = true
			requestedFD = parentFD
			requestedName = baseName
			requestedMode = mode
			return nil
		}, 17, "vgpulock"))
	require.True(t, called)
	require.Equal(t, 17, requestedFD)
	require.Equal(t, "vgpulock", requestedName)
	require.Equal(t, uint32(0o1777), requestedMode)
}

func TestPrepareHostPIDLockParentCreatesStickyModeOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux mkdirat mode behavior is required")
	}

	parent := t.TempDir()
	parentFD, err := unix.Open(parent,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, unix.Close(parentFD))
	})

	var createErr error
	func() {
		oldUmask := unix.Umask(0)
		defer unix.Umask(oldUmask)
		createErr = createHostPIDLockParent(parentFD, "vgpulock")
	}()
	require.NoError(t, createErr)

	var createdStat unix.Stat_t
	require.NoError(t, unix.Fstatat(parentFD, "vgpulock", &createdStat,
		unix.AT_SYMLINK_NOFOLLOW))
	require.Equal(t, hostPIDLockParentCreateMode,
		uint32(createdStat.Mode)&0o7777)
}

func TestPrepareDefaultHostPIDLockParentAsRoot(t *testing.T) {
	if os.Geteuid() != 0 ||
		os.Getenv("HAMI_TEST_PRODUCTION_PARENT") != "1" {
		t.Skip("an isolated root mount namespace is required")
	}

	require.NoError(t, prepareDefaultHostPIDLockParent())
	info, err := os.Stat(hostPIDLockParentDirectory)
	require.NoError(t, err)
	stat, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok)
	require.Equal(t, hostPIDLockTrustedOwner, stat.Uid)
	require.Equal(t, hostPIDLockParentMode,
		info.Mode()&(os.ModePerm|os.ModeSticky))
}

func TestPrepareHostPIDLockParentRejectsUntrustedObjects(t *testing.T) {
	t.Run("parent owner", func(t *testing.T) {
		directory := filepath.Join(t.TempDir(), "vgpulock")
		require.NoError(t, os.Mkdir(directory, 0o700))

		err := prepareHostPIDLockParent(directory, uint32(os.Geteuid()+1))
		require.ErrorContains(t, err,
			"parent directory is not owned by trusted UID")
	})

	t.Run("regular file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "vgpulock")
		require.NoError(t, os.WriteFile(path, nil, 0o600))

		require.Error(t, prepareHostPIDLockParent(path,
			uint32(os.Geteuid())))
	})

	t.Run("symlink", func(t *testing.T) {
		parent := t.TempDir()
		target := filepath.Join(parent, "target")
		link := filepath.Join(parent, "vgpulock")
		require.NoError(t, os.Mkdir(target, 0o700))
		require.NoError(t, os.Symlink(target, link))

		require.Error(t, prepareHostPIDLockParent(link,
			uint32(os.Geteuid())))
	})
}

func TestPrepareHostPIDLockParentRejectsUntrustedParent(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		fixture := t.TempDir()
		realParent := filepath.Join(fixture, "real-parent")
		linkParent := filepath.Join(fixture, "link-parent")
		directory := filepath.Join(linkParent, "vgpulock")
		realDirectory := filepath.Join(realParent, "vgpulock")
		require.NoError(t, os.Mkdir(realParent, 0o700))
		require.NoError(t, os.Symlink(realParent, linkParent))

		require.Error(t, prepareHostPIDLockParent(directory,
			uint32(os.Geteuid())))
		_, err := os.Lstat(realDirectory)
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("writable without sticky bit", func(t *testing.T) {
		parent := filepath.Join(t.TempDir(), "parent")
		directory := filepath.Join(parent, "vgpulock")
		require.NoError(t, os.Mkdir(parent, 0o700))
		require.NoError(t, os.Chmod(parent, 0o777))

		require.Error(t, prepareHostPIDLockParent(directory,
			uint32(os.Geteuid())))
	})
}

func TestPrepareHostPIDLockParentAllowsStickyWritableParent(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "parent")
	directory := filepath.Join(parent, "vgpulock")
	require.NoError(t, os.Mkdir(parent, 0o700))
	require.NoError(t, os.Chmod(parent,
		os.FileMode(0o777)|os.ModeSticky))

	require.NoError(t, prepareHostPIDLockParent(directory,
		uint32(os.Geteuid())))
}

func TestConfigureHostPIDBrokerDisabled(t *testing.T) {
	for _, value := range []string{"", "0", "true", "false", "01", " 1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv(hostpid.EnvironmentVariable, value)
			response := &kubeletdevicepluginv1beta1.ContainerAllocateResponse{}

			configureHostPIDBroker(response)

			require.Empty(t, response.Envs)
			require.Empty(t, response.Mounts)
		})
	}
}

func TestConfigureHostPIDBrokerDisabledRemovesStaleConfiguration(t *testing.T) {
	t.Setenv(hostpid.EnvironmentVariable, "0")
	before := &kubeletdevicepluginv1beta1.Mount{
		ContainerPath: "/before",
		HostPath:      "/before",
	}
	lockParent := &kubeletdevicepluginv1beta1.Mount{
		ContainerPath: hostPIDLockParentDirectory,
		HostPath:      hostPIDLockParentDirectory,
	}
	after := &kubeletdevicepluginv1beta1.Mount{
		ContainerPath: "/after",
		HostPath:      "/after",
	}
	response := &kubeletdevicepluginv1beta1.ContainerAllocateResponse{
		Envs: map[string]string{
			"KEEP":                      "yes",
			hostpid.EnvironmentVariable: "1",
		},
		Mounts: []*kubeletdevicepluginv1beta1.Mount{
			before,
			{
				ContainerPath: hostpid.ContainerDirectory + "/",
				HostPath:      "/first-stale",
			},
			lockParent,
			{
				ContainerPath: "tmp/vgpulock/hostpid",
				HostPath:      "/second-stale",
			},
			after,
		},
	}

	configureHostPIDBroker(response)

	require.Equal(t, map[string]string{"KEEP": "yes"}, response.Envs)
	require.Equal(t, []*kubeletdevicepluginv1beta1.Mount{
		before,
		lockParent,
		after,
	}, response.Mounts)
}

func TestConfigureHostPIDBrokerEnabled(t *testing.T) {
	t.Setenv(hostpid.EnvironmentVariable, "1")
	existingMount := &kubeletdevicepluginv1beta1.Mount{
		ContainerPath: "/tmp/vgpulock",
		HostPath:      "/tmp/vgpulock",
	}
	response := &kubeletdevicepluginv1beta1.ContainerAllocateResponse{
		Envs:   map[string]string{"KEEP": "yes"},
		Mounts: []*kubeletdevicepluginv1beta1.Mount{existingMount},
	}

	configureHostPIDBroker(response)

	require.Equal(t, "yes", response.Envs["KEEP"])
	require.Equal(t, "1", response.Envs[hostpid.EnvironmentVariable])
	require.Len(t, response.Mounts, 2)
	require.Same(t, existingMount, response.Mounts[0])
	require.Equal(t, &kubeletdevicepluginv1beta1.Mount{
		ContainerPath: hostpid.ContainerDirectory,
		HostPath:      hostpid.ServerDirectory,
		ReadOnly:      true,
	}, response.Mounts[1])
}

func TestConfigureHostPIDBrokerIsIdempotent(t *testing.T) {
	t.Setenv(hostpid.EnvironmentVariable, "1")
	response := &kubeletdevicepluginv1beta1.ContainerAllocateResponse{}

	configureHostPIDBroker(response)
	configureHostPIDBroker(response)

	require.Equal(t, "1", response.Envs[hostpid.EnvironmentVariable])
	require.Len(t, response.Mounts, 1)
	require.Equal(t, &kubeletdevicepluginv1beta1.Mount{
		ContainerPath: hostpid.ContainerDirectory,
		HostPath:      hostpid.ServerDirectory,
		ReadOnly:      true,
	}, response.Mounts[0])
}

func TestConfigureHostPIDBrokerReplacesConflictingMount(t *testing.T) {
	t.Setenv(hostpid.EnvironmentVariable, "1")
	response := &kubeletdevicepluginv1beta1.ContainerAllocateResponse{
		Mounts: []*kubeletdevicepluginv1beta1.Mount{{
			ContainerPath: hostpid.ContainerDirectory,
			HostPath:      "/untrusted",
			ReadOnly:      false,
		}},
	}

	configureHostPIDBroker(response)

	require.Len(t, response.Mounts, 1)
	require.Equal(t, hostpid.ServerDirectory,
		response.Mounts[0].HostPath)
	require.True(t, response.Mounts[0].ReadOnly)
}

func TestConfigureHostPIDBrokerCanonicalizesDuplicateMounts(t *testing.T) {
	t.Setenv(hostpid.EnvironmentVariable, "1")
	before := &kubeletdevicepluginv1beta1.Mount{
		ContainerPath: "/before",
		HostPath:      "/before",
	}
	after := &kubeletdevicepluginv1beta1.Mount{
		ContainerPath: "/after",
		HostPath:      "/after",
	}
	response := &kubeletdevicepluginv1beta1.ContainerAllocateResponse{
		Mounts: []*kubeletdevicepluginv1beta1.Mount{
			before,
			{
				ContainerPath: hostpid.ContainerDirectory + "/",
				HostPath:      "/first-untrusted",
				ReadOnly:      false,
			},
			after,
			{
				ContainerPath: hostPIDLockParentDirectory +
					"/hostpid/../hostpid",
				HostPath: "/second-untrusted",
				ReadOnly: false,
			},
		},
	}

	configureHostPIDBroker(response)

	require.Len(t, response.Mounts, 3)
	require.Same(t, before, response.Mounts[0])
	require.Equal(t, &kubeletdevicepluginv1beta1.Mount{
		ContainerPath: hostpid.ContainerDirectory,
		HostPath:      hostpid.ServerDirectory,
		ReadOnly:      true,
	}, response.Mounts[1])
	require.Same(t, after, response.Mounts[2])
}

func TestConfigureHostPIDBrokerOrdersParentBeforeNestedMount(t *testing.T) {
	t.Setenv(hostpid.EnvironmentVariable, "1")
	before := &kubeletdevicepluginv1beta1.Mount{
		ContainerPath: "/before",
		HostPath:      "/before",
	}
	middle := &kubeletdevicepluginv1beta1.Mount{
		ContainerPath: "/middle",
		HostPath:      "/middle",
	}
	after := &kubeletdevicepluginv1beta1.Mount{
		ContainerPath: "/after",
		HostPath:      "/after",
	}
	response := &kubeletdevicepluginv1beta1.ContainerAllocateResponse{
		Mounts: []*kubeletdevicepluginv1beta1.Mount{
			before,
			{
				ContainerPath: "tmp/vgpulock/hostpid",
				HostPath:      "/untrusted-broker",
				ReadOnly:      false,
			},
			middle,
			{
				ContainerPath: "/tmp/./vgpulock",
				HostPath:      "/untrusted-parent",
				ReadOnly:      true,
			},
			after,
		},
	}

	configureHostPIDLockParentMount(response)
	configureHostPIDBroker(response)

	require.Equal(t, []*kubeletdevicepluginv1beta1.Mount{
		before,
		{
			ContainerPath: hostPIDLockParentDirectory,
			HostPath:      hostPIDLockParentDirectory,
			ReadOnly:      false,
		},
		{
			ContainerPath: hostpid.ContainerDirectory,
			HostPath:      hostpid.ServerDirectory,
			ReadOnly:      true,
		},
		middle,
		after,
	}, response.Mounts)
}

func TestConfigureHostPIDLockParentMountCanonicalizesDuplicateMounts(
	t *testing.T) {
	before := &kubeletdevicepluginv1beta1.Mount{
		ContainerPath: "/before",
		HostPath:      "/before",
	}
	after := &kubeletdevicepluginv1beta1.Mount{
		ContainerPath: "/after",
		HostPath:      "/after",
	}
	response := &kubeletdevicepluginv1beta1.ContainerAllocateResponse{
		Mounts: []*kubeletdevicepluginv1beta1.Mount{
			before,
			{
				ContainerPath: hostPIDLockParentDirectory + "/",
				HostPath:      "/first-untrusted",
				ReadOnly:      true,
			},
			after,
			{
				ContainerPath: "tmp/./vgpulock",
				HostPath:      "/second-untrusted",
				ReadOnly:      false,
			},
		},
	}

	configureHostPIDLockParentMount(response)

	require.Len(t, response.Mounts, 3)
	require.Same(t, before, response.Mounts[0])
	require.Equal(t, &kubeletdevicepluginv1beta1.Mount{
		ContainerPath: hostPIDLockParentDirectory,
		HostPath:      hostPIDLockParentDirectory,
		ReadOnly:      false,
	}, response.Mounts[1])
	require.Same(t, after, response.Mounts[2])
}
