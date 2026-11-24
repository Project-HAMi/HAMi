---
name: hami-nvml-mock
description: Test the HAMi scheduler or HAMi's NVIDIA device plugin with kind and nvml-mock. Reproduce bugs or verify local fixes without physical GPUs.
---

# Test HAMi with nvml-mock

Run kind on a host without NVIDIA GPUs or in a VM without NVIDIA device passthrough. Containers share the kernel of the system that runs kind. If a mock character device uses the same major and minor numbers as a real NVIDIA device, opening it can reach the real driver. nvml-mock can reuse NVIDIA's major number `195`.

Use Lab 5 to set up the cluster, run test workloads, and clean up. [Lab 5 in English](https://project-hami.io/tutorials/labs/nvml-mock) · [中文实验](https://project-hami.io/zh/tutorials/labs/nvml-mock)

## Deploy the current checkout

Install `jq` for local deployment. Use a standalone HAMi clone with the `libvgpu` submodule initialized. The Docker build copies `.git/modules/libvgpu` from the checkout. In a linked Git worktree, `.git` is a file, so that path is unavailable.

After you install nvml-mock, use these steps in place of the HAMi install command in Lab 5.

1. Copy the lab's `devicePlugin.nvidiaDriverRoot` and `devicePlugin.nvidiaNodeSelector.gpu` overrides into a Helm values file as nested YAML. [Lab 5 Helm settings](https://project-hami.io/tutorials/labs/nvml-mock#31-install-via-helm)
2. Use a kubeconfig whose current context is the kind-generated context for the lab cluster, such as `kind-nvml-mock-test`.
3. Delete all Pods from earlier Lab 5 tests. A `Pending` Pod can fail the cluster-wide status check. Running test Pods keep their existing allocations.
4. Run `make local-deploy` with the kubeconfig and values file. The command replaces any existing `hami` release in `hami-system` on that cluster. For an ARM64 kind node, add `TARGET_PLATFORMS=linux/arm64`.

   ```bash
   make local-deploy KUBE_CONF=/path/to/kubeconfig HELM_VALUES_FILE=/path/to/values.yaml
   ```

5. Create the test Pods from Lab 5 to exercise `Allocate` on the updated device plugin.

### Missing `nvidia-modeset` on a GPU host

If a scheduled test Pod fails to start, run `kubectl describe pod <pod-name>` with the lab kubeconfig and look for this error in Events.

```text
lstat /var/lib/nvml-mock/driver/dev/nvidia-modeset: no such file or directory
```

If the device plugin sees `/dev/nvidia-modeset`, it can include a `DeviceSpec` that points to `dev/nvidia-modeset` under the mock driver root. Container creation fails when that path is missing.

Use the following override to check scheduling, the GPU UUID that `Allocate` sets in `NVIDIA_VISIBLE_DEVICES`, and Pod startup. The override omits the device plugin's `DeviceSpec` entries.

With the default `envvar` strategy, nvml-mock 0.7.0's NRI plugin skips the test container. The plugin looks for a numbered GPU device with its cgroup rule or a GPU CDI device. As a result, NRI omits the `/opt/nvml-mock` mount and `MOCK_NVML_CONFIG` variable. [How nvml-mock 0.7.0 recognizes GPU allocations](https://github.com/NVIDIA/k8s-test-infra/blob/v0.7.0/docs/components/nri-plugin.md#recognising-a-gpu-allocation)

Add the override to the Helm values file. Redeploy and recreate the test Pod.

```yaml
devicePlugin:
  passDeviceSpecsEnabled: false
```

To test NRI injection, keep `devicePlugin.passDeviceSpecsEnabled: true` in the isolated environment, or set `devicePlugin.deviceListStrategy: cdi-cri` if the cluster supports CDI through CRI. With `cdi-cri` as the only strategy, the device plugin ignores `passDeviceSpecsEnabled`. Verify the `/opt/nvml-mock` mount and `MOCK_NVML_CONFIG` in the test container.

## Allocate and container metrics

Lab 5 sets `CUDA_DISABLE_CONTROL=true` in its test containers. The kubelet still calls `Allocate` for vGPU requests, and `Allocate` omits the `/etc/ld.so.preload` mount that loads HAMi Core.

For partial-GPU allocations in the lab, expect metrics that depend on HAMi Core shared memory, such as `hami_vgpu_memory_used_bytes`, to be absent. For whole-GPU allocations, `vGPUmonitor` can report the same metric if whole-GPU monitoring is enabled and `vGPUmonitor` can initialize the mock NVML library.
