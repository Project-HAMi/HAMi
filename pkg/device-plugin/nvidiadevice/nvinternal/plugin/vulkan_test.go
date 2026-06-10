/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * The HAMi Contributors require contributions made to
 * this file be licensed under the Apache-2.0 license or a
 * compatible open source license.
 */

package plugin

import (
	"os"
	"path/filepath"
	"testing"

	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

// appendVulkanManifestMount must return the input slice untouched when the
// Vulkan implicit-layer manifest is absent on the host. This is the path
// taken on nodes where vgpu-init.sh has not (or cannot) place the manifest,
// and Pod startup must not block on a missing optional file.
func TestAppendVulkanManifestMount_Absent(t *testing.T) {
	dir := t.TempDir() // no vgpu/vulkan/implicit_layer.d/hami.json under here
	in := []*kubeletdevicepluginv1beta1.Mount{
		{ContainerPath: "/already/there", HostPath: "/already/there"},
	}
	out := appendVulkanManifestMount(in, dir)
	if len(out) != len(in) {
		t.Fatalf("expected mounts unchanged when manifest absent, got %d mounts (want %d)", len(out), len(in))
	}
	for i := range in {
		if out[i] != in[i] {
			t.Fatalf("mount[%d] mutated: got %+v, want %+v", i, out[i], in[i])
		}
	}
}

// When the Vulkan implicit-layer manifest is present on the host, the helper
// must append a single bind-mount at the well-known container path so the
// Vulkan loader picks the layer up via the enable_environment guard.
func TestAppendVulkanManifestMount_Present(t *testing.T) {
	dir := t.TempDir()
	manifestRel := "vgpu/vulkan/implicit_layer.d/hami.json"
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, manifestRel)), 0o755); err != nil {
		t.Fatalf("setup mkdir: %v", err)
	}
	manifestHost := filepath.Join(dir, manifestRel)
	if err := os.WriteFile(manifestHost, []byte("{}"), 0o644); err != nil {
		t.Fatalf("setup writefile: %v", err)
	}

	in := []*kubeletdevicepluginv1beta1.Mount{}
	out := appendVulkanManifestMount(in, dir)
	if len(out) != 1 {
		t.Fatalf("expected exactly one mount appended, got %d", len(out))
	}
	m := out[0]
	if m.ContainerPath != "/etc/vulkan/implicit_layer.d/hami.json" {
		t.Errorf("ContainerPath = %q, want /etc/vulkan/implicit_layer.d/hami.json", m.ContainerPath)
	}
	if m.HostPath != manifestHost {
		t.Errorf("HostPath = %q, want %q", m.HostPath, manifestHost)
	}
	if !m.ReadOnly {
		t.Errorf("ReadOnly = false, want true (manifest must not be writable from container)")
	}
}

// Helper must preserve the order and identity of preceding mounts when it
// appends. Regression guard for the MIG / non-MIG callers in server.go that
// rely on positional ordering.
func TestAppendVulkanManifestMount_PreservesPriorMounts(t *testing.T) {
	dir := t.TempDir()
	manifestRel := "vgpu/vulkan/implicit_layer.d/hami.json"
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, manifestRel)), 0o755); err != nil {
		t.Fatalf("setup mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifestRel), []byte("{}"), 0o644); err != nil {
		t.Fatalf("setup writefile: %v", err)
	}

	first := &kubeletdevicepluginv1beta1.Mount{ContainerPath: "/a", HostPath: "/a"}
	second := &kubeletdevicepluginv1beta1.Mount{ContainerPath: "/b", HostPath: "/b"}
	out := appendVulkanManifestMount([]*kubeletdevicepluginv1beta1.Mount{first, second}, dir)
	if len(out) != 3 {
		t.Fatalf("expected 3 mounts, got %d", len(out))
	}
	if out[0] != first || out[1] != second {
		t.Fatalf("prior mounts reordered or replaced")
	}
	if out[2].ContainerPath != "/etc/vulkan/implicit_layer.d/hami.json" {
		t.Errorf("appended mount has wrong ContainerPath: %q", out[2].ContainerPath)
	}
}
