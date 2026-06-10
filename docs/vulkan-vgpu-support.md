# Vulkan vGPU Support

HAMi partitions NVIDIA GPUs for Vulkan workloads by injecting a Vulkan implicit
layer (`VK_LAYER_HAMI_vgpu`) that shares the same VRAM and SM budgets used by
the existing CUDA hooks.

## Enabling Vulkan partitioning

Add the `hami.io/vulkan: "true"` annotation to any pod that uses HAMi NVIDIA
resources. The webhook will:

- Union `graphics` into `NVIDIA_DRIVER_CAPABILITIES` so the NVIDIA Container
  Toolkit mounts the Vulkan ICD and graphics libraries.
- Set `HAMI_VULKAN_ENABLE=1` which activates the HAMi Vulkan layer via its
  `enable_environment` clause in the implicit layer manifest.

Example: `examples/nvidia/vulkan_example.yaml`.

## What gets limited

- `nvidia.com/gpumem` enforces VRAM allocation across **both** CUDA and Vulkan
  in the container, sharing a single budget.
- `nvidia.com/gpucores` throttles Vulkan `vkQueueSubmit[2]` using the same
  token-bucket rate limiter as `cuLaunchKernel`.
- `vkGetPhysicalDeviceMemoryProperties[2]` clamps the device-local heap size
  to the pod budget so apps that size allocations from this value self-limit.

## What is not limited (yet)

- Vulkan Video (`VK_KHR_video_queue`) submissions.
- Frame-pacing jitter introduced by throttling on graphics queues (documented
  behavior; strict/cooperative modes are a future option).

## Troubleshooting

| Symptom | Check |
|---------|-------|
| Container has no `vulkan` CLI / libs | Annotation absent or `NVIDIA_DRIVER_CAPABILITIES` already frozen to `compute` by image. |
| `vkAllocateMemory` always succeeds | Layer did not activate — ensure `HAMI_VULKAN_ENABLE=1` set and `/etc/vulkan/implicit_layer.d/hami.json` exists. |
| `vulkaninfo` still shows full VRAM heap | Layer manifest not loaded; run `VK_LOADER_DEBUG=all vulkaninfo` to see layer scan. |
| Nothing gets throttled | `rate_limiter` no-ops when SM limit is 0, >=100, or HAMi's utilization switch is disabled. Confirm `nvidia.com/gpucores` was requested on the pod. |
