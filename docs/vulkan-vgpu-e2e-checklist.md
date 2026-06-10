# Vulkan vGPU — Manual E2E Verification Checklist

This checklist must be executed on a Kubernetes cluster with at least one
NVIDIA GPU node running HAMi with the Vulkan-enabled `libvgpu.so`. Automation
is deferred until an NVIDIA-capable CI runner is available.

## Prerequisites

1. HAMi scheduler + device plugin built from `feat/vulkan-vgpu` branch,
   including the bumped `libvgpu` submodule pointer (commit `b60b4e6` or
   later).
2. NVIDIA Container Toolkit installed, default runtime `nvidia`.
3. `libvgpu.so` built from HAMi-core `vulkan-layer` branch (commit `579a421`
   or later) and shipped with the manifest
   `/etc/vulkan/implicit_layer.d/hami.json` in the HAMi vgpu image.

## 1. Heap clamp (`vulkaninfo`)

```
kubectl apply -f examples/nvidia/vulkan_example.yaml
kubectl logs hami-vulkan-example | grep -iE "heap|device local"
```

**Pass criteria:** the reported `heapSize` for the `DEVICE_LOCAL` heap is
**≤ 1073741824 bytes (1 GiB)**, matching `nvidia.com/gpumem: 1024`.

## 2. Allocation exceed → `VK_ERROR_OUT_OF_DEVICE_MEMORY`

Build a tiny allocation-stress image (pseudocode):
```c
for (int i = 0; i < 5; ++i) {
    VkMemoryAllocateInfo info = { .allocationSize = 512*1024*1024 };
    VkResult r = vkAllocateMemory(dev, &info, NULL, &m[i]);
    printf("alloc %d -> %d\n", i, r);
}
```
Package as `ghcr.io/<org>/vulkan-alloc-stress:latest`, deploy with the same
annotation + `gpumem: 1024`.

**Pass criteria:** first two allocations return `VK_SUCCESS (0)`, the third
returns `VK_ERROR_OUT_OF_DEVICE_MEMORY (-2)`.

## 3. SM throttle on `vkQueueSubmit`

Image: any Vulkan compute workload that loops `vkQueueSubmit` continuously
(e.g. `vkcube --headless` loop, or custom compute shader pinging GPU).
Pod spec: add `nvidia.com/gpucores: "30"` annotation.

**Pass criteria:** `nvidia-smi dmon -s u` on the host reports GPU compute
utilization averaged near 30% (± token-bucket refill jitter ±120 ms), not
100%.

## 4. Mixed CUDA + Vulkan shared budget

Image containing both a CUDA `cudaMalloc(512 MiB)` loop and Vulkan
`vkAllocateMemory(512 MiB)` loop.
Pod spec: `gpumem: 1024` + `hami.io/vulkan: "true"`.

**Pass criteria:**
- Sum of successful allocations across CUDA + Vulkan does **not** exceed
  1024 MiB.
- Either path may be the one that starts failing depending on scheduling;
  both `VK_ERROR_OUT_OF_DEVICE_MEMORY` and `cudaErrorMemoryAllocation` are
  valid end states.

## 5. Opt-out still works for CUDA-only pods

Deploy a pod with `nvidia.com/gpumem` but **no** `hami.io/vulkan` annotation.

**Pass criteria:**
- `env | grep NVIDIA_DRIVER_CAPABILITIES` inside the container is unchanged
  from the image default (`compute,utility` unless image overrides).
- `env | grep HAMI_VULKAN_ENABLE` is empty.
- CUDA workloads continue to be throttled/clamped as before.

## Results log

Record cluster name, node GPU model, HAMi image tag, HAMi-core image tag,
and pass/fail for each of the 5 checks in a dated entry below.

| Date | Cluster | GPU | HAMi tag | libvgpu tag | 1 | 2 | 3 | 4 | 5 |
|------|---------|-----|----------|-------------|---|---|---|---|---|
| _pending_ | - | - | - | - | - | - | - | - | - |
