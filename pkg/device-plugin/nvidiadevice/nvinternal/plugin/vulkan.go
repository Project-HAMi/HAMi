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

	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

// appendVulkanManifestMount appends a bind-mount for the HAMi Vulkan implicit
// layer manifest when present on the host. The manifest is placed under
// hostHookPath/vgpu/vulkan/implicit_layer.d/hami.json by vgpu-init.sh as part
// of the standard lib distribution.
//
// The manifest's enable_environment guard means the Vulkan layer activates
// only when the pod sets HAMI_VULKAN_ENABLE=1 (injected by the admission
// webhook for pods that carry the hami.io/vulkan="true" annotation), so the
// mount is safe to append unconditionally for both vGPU and MIG paths.
//
// Returns the input slice unchanged when the host file is absent, so nodes
// without the Vulkan manifest do not block pod startup.
func appendVulkanManifestMount(mounts []*kubeletdevicepluginv1beta1.Mount, hostHookPath string) []*kubeletdevicepluginv1beta1.Mount {
	manifestHost := hostHookPath + "/vgpu/vulkan/implicit_layer.d/hami.json"
	if _, err := os.Stat(manifestHost); err != nil {
		return mounts
	}
	return append(mounts, &kubeletdevicepluginv1beta1.Mount{
		ContainerPath: "/etc/vulkan/implicit_layer.d/hami.json",
		HostPath:      manifestHost,
		ReadOnly:      true,
	})
}
