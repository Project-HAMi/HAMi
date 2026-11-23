---
name: hami-nvml-mock
description: Test the HAMi scheduler or HAMi's NVIDIA device plugin with kind and nvml-mock. Reproduce bugs or verify local fixes without physical GPUs.
---

# Test HAMi with nvml-mock

Use Lab 5 to set up the cluster, run test workloads, and clean up. [Lab 5 in English](https://project-hami.io/tutorials/labs/nvml-mock) · [中文实验](https://project-hami.io/zh/tutorials/labs/nvml-mock)

## Deploy the current checkout

Install `jq` for local deployment. Use a regular HAMi clone with the `libvgpu` submodule initialized. The Docker build reads `.git/modules/libvgpu`.

Follow these steps for the HAMi deployment stage of Lab 5 after you install nvml-mock.

1. Copy the lab's `devicePlugin.nvidiaDriverRoot` and `devicePlugin.nvidiaNodeSelector.gpu` overrides into a Helm values file. [Lab 5 Helm settings](https://project-hami.io/tutorials/labs/nvml-mock#31-install-via-helm)
2. Choose a kubeconfig whose current context is the kind-generated context for the lab cluster, such as `kind-nvml-mock-test`.
3. Run `make local-deploy` with the kubeconfig and values file. The command replaces any existing `hami` release in `hami-system` on that cluster. For an ARM64 kind node, add `TARGET_PLATFORMS=linux/arm64`.

   ```bash
   make local-deploy KUBE_CONF=/path/to/kubeconfig HELM_VALUES_FILE=/path/to/values.yaml
   ```

## Allocate and container metrics

The kubelet calls `Allocate` when it prepares a test container that requests a vGPU. With `CUDA_DISABLE_CONTROL=true`, `Allocate` omits the `/etc/ld.so.preload` mount that loads HAMi Core.

For partial-GPU allocations in the lab, expect metrics that depend on HAMi Core shared memory, such as `hami_vgpu_memory_used_bytes`, to be absent. For whole-GPU allocations, `vGPUmonitor` can report the same metric from NVML. That path requires whole-GPU monitoring and successful initialization of the mock NVML library.
