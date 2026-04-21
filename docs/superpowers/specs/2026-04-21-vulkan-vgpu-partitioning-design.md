# HAMi Vulkan vGPU Partitioning — Design Spec

- Date: 2026-04-21
- Status: Draft (pending implementation)
- Scope: NVIDIA GPU, Vulkan compute + graphics workloads
- Repos impacted: `Project-HAMi/HAMi` (Go), `Project-HAMi/HAMi-core` (C, submodule at `libvgpu/`)

## 1. Problem Statement

HAMi partitions NVIDIA GPUs by `LD_PRELOAD`-hooking the CUDA driver API inside `libvgpu.so` (HAMi-core). Vulkan workloads (compute shaders, `llama.cpp` Vulkan backend, rendering) bypass these hooks because Vulkan is a separate API surface (`libvulkan.so` → ICD). As a result:

- VRAM limits declared via `nvidia.com/gpumem` are **not enforced** for Vulkan allocations.
- SM/core throttling via `nvidia.com/gpucores` is **not enforced** for Vulkan queue submissions.
- Vulkan libraries are **not even mounted** into HAMi-scheduled containers by default — `NVIDIA_DRIVER_CAPABILITIES` is untouched and the NVIDIA Container Toolkit default (`compute,utility`) excludes Vulkan ICD files.

A grep across the repository confirms `vulkan`/`VK_` is referenced in zero files prior to this design.

## 2. Goals

1. Enforce existing `nvidia.com/gpumem` budget on Vulkan memory allocations in the same pod, **sharing the budget with CUDA** (one physical VRAM → one budget).
2. Enforce existing `nvidia.com/gpucores` SM throttle on Vulkan queue submissions.
3. Make Vulkan libraries actually reach the container when requested.
4. Preserve full backward compatibility: pods that do not request Vulkan see no behavior change.

## Non-Goals

- Vulkan partitioning for non-NVIDIA vendors (AMD, Intel, Moore Threads).
- Separate VRAM budgets for CUDA vs Vulkan (physical reality: one VRAM pool).
- Filtering `vkEnumeratePhysicalDevices` beyond what `NVIDIA_VISIBLE_DEVICES` already achieves.
- Graphics frame-pacing guarantees; SM throttling may introduce jitter in rendering workloads, documented but not solved.

## 3. Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Vendor | NVIDIA only | Matches existing HAMi-core CUDA hook architecture. |
| Dimensions | VRAM + SM | Users want both for LLM inference via Vulkan. |
| Resource API | Reuse `nvidia.com/gpumem`, `nvidia.com/gpucores` (shared budget) | Matches physical reality, zero user-facing churn. |
| Activation | Pod annotation `hami.io/vulkan: "true"` | Opt-in avoids adding ~tens of MB of graphics libs to every CUDA-only pod. |
| Interception mechanism | Vulkan implicit layer exposed by HAMi-core `libvgpu.so` | Standard Vulkan loader contract; avoids LD_PRELOAD vs ICD dispatch fragility. |
| Budget sharing | In-process shared counters (existing struct) | Same `libvgpu.so` instance holds CUDA and Vulkan hooks; no new IPC needed. |

## 4. Architecture

```
┌───────────────────────────────────┐
│ Project-HAMi/HAMi    (Go)          │
│  pkg/device/nvidia/device.go       │  ← MutateAdmission 확장
│  (pkg/device/nvidia/device_test.go)│
└────────────┬──────────────────────┘
             │ env: HAMI_VULKAN_ENABLE=1,
             │      NVIDIA_DRIVER_CAPABILITIES⊇graphics
             ▼
┌───────────────────────────────────┐
│ container                          │
│  NVIDIA Container Toolkit mounts   │
│  Vulkan ICD + libGLX_nvidia        │
│  HAMi device-plugin mounts         │
│  /usr/local/vgpu/libvgpu.so        │
└────────────┬──────────────────────┘
             │ Vulkan loader scans implicit_layer.d
             ▼
┌───────────────────────────────────┐
│ Project-HAMi/HAMi-core  (C)        │
│  libvgpu.so                        │
│   ├─ existing CUDA hooks           │
│   ├─ NEW Vulkan layer              │
│   │     src/vulkan/*.c             │
│   └─ shared VRAM/SM counters       │
│  etc/vulkan/implicit_layer.d/      │
│   └─ hami.json (new)               │
└───────────────────────────────────┘
```

## 5. Components

### 5.1 HAMi (Go) — `pkg/device/nvidia/device.go`

New constants:
```go
const (
    VulkanEnableAnno       = "hami.io/vulkan"
    VulkanLayerName        = "VK_LAYER_HAMI_vgpu"
    NvidiaDriverCapsEnvVar = "NVIDIA_DRIVER_CAPABILITIES"
    HamiVulkanEnvVar       = "HAMI_VULKAN_ENABLE"
)
```

`MutateAdmission` extension (only when `hasResource == true`):
1. Read pod annotation `hami.io/vulkan`; proceed only if `"true"`.
2. Compute new `NVIDIA_DRIVER_CAPABILITIES` value:
   - If unset on container: `"compute,utility,graphics"`.
   - If set and contains `"all"`: leave untouched.
   - Otherwise: parse comma-separated tokens, union with `"graphics"`, re-emit.
3. Append `HAMI_VULKAN_ENABLE=1` if not already present.
4. Do not modify `NVIDIA_VISIBLE_DEVICES` or RuntimeClass — already handled.

No change to scheduler extender, resource accounting, device plugin allocation.

### 5.2 HAMi-core (C) — new module `src/vulkan/`

File layout:
```
src/vulkan/
  layer.c            # vkNegotiateLoaderLayerInterfaceVersion,
                     # vk_layerGetInstanceProcAddr,
                     # vk_layerGetDeviceProcAddr
  layer.h
  dispatch.c         # next-layer pointer tables per VkInstance/VkDevice
  hooks_memory.c     # vkAllocateMemory, vkFreeMemory,
                     # vkGetPhysicalDeviceMemoryProperties/2
  hooks_buffer.c     # vkCreateBuffer, vkCreateImage,
                     # vkBindBufferMemory/2 (for accounting hooks if needed)
  hooks_submit.c     # vkQueueSubmit, vkQueueSubmit2
```

Hooked entry points (behavior):

| Function | Behavior |
|----------|----------|
| `vkGetPhysicalDeviceMemoryProperties` | Call next-layer, then clamp each device-local heap `size` to `min(real, pod_budget)`. |
| `vkGetPhysicalDeviceMemoryProperties2` | Same, via `pNext` chain. |
| `vkAllocateMemory` | Lock shared counter; if `used + allocationSize > budget` → unlock, return `VK_ERROR_OUT_OF_DEVICE_MEMORY`. Else tentatively `used += allocationSize`, unlock, call next-layer. On next-layer failure, roll back. Record `VkDeviceMemory → allocationSize` in a per-device map. |
| `vkFreeMemory` | Lookup size, lock, `used -= size`, unlock, call next-layer, erase map entry. |
| `vkQueueSubmit` / `vkQueueSubmit2` | Invoke shared throttle loop (same function used by CUDA `cuLaunchKernel` wrapper): poll `nvmlDeviceGetUtilizationRates`, `usleep(POLL_INTERVAL)` until `util < cores_limit` or max retries. Then call next-layer. |

Entry point contract (loader ↔ layer):
- Export `vkNegotiateLoaderLayerInterfaceVersion` with signature from `vk_layer.h`.
- Populate struct with pointers to `vk_layerGetInstanceProcAddr` / `vk_layerGetDeviceProcAddr`.
- Store next-layer pointers in per-instance dispatch table keyed by `VkInstance` handle (via `VkLayerInstanceCreateInfo`).
- Pass-through any entry point not in the hooked set.

### 5.3 Shared VRAM / SM counters

HAMi-core already maintains per-device `device_memory` structure used by CUDA wrappers. Vulkan wrappers call the **same** API:
```c
// pseudocode
if (!reserve_device_memory(dev_idx, size)) return VK_ERROR_OUT_OF_DEVICE_MEMORY;
```
Mutex inside `reserve_device_memory` serializes CUDA and Vulkan paths. No new IPC; no new shared memory segment.

The SM throttle poll loop is extracted into a common utility (`util_throttle(dev_idx)`) called by both `cuLaunchKernel` wrapper (existing) and `vkQueueSubmit` wrapper (new).

### 5.4 Vulkan layer manifest

File: `etc/vulkan/implicit_layer.d/hami.json`, installed by HAMi-core Dockerfile to `/etc/vulkan/implicit_layer.d/hami.json`.

```json
{
  "file_format_version": "1.2.0",
  "layer": {
    "name": "VK_LAYER_HAMI_vgpu",
    "type": "GLOBAL",
    "library_path": "/usr/local/vgpu/libvgpu.so",
    "api_version": "1.3.0",
    "implementation_version": "1",
    "description": "HAMi Vulkan vGPU limiter",
    "enable_environment":  { "HAMI_VULKAN_ENABLE": "1" },
    "disable_environment": { "HAMI_VULKAN_DISABLE": "1" }
  }
}
```

`enable_environment` gates activation strictly on the env var set by the Go webhook, so the layer is inert for CUDA-only pods even though the manifest is present.

### 5.5 Build

- HAMi-core `Makefile`: append `src/vulkan/*.c` to sources, add `-I$(VULKAN_SDK_INCLUDE)` to CFLAGS, no runtime link (`libvulkan.so` is dlopened by loader, not by us).
- HAMi-core Dockerfile: `apt-get install vulkan-headers` (or equivalent), copy `etc/vulkan/implicit_layer.d/hami.json` into image `/etc/vulkan/implicit_layer.d/`.

## 6. Data Flow

### 6.1 Admission
1. User pod with `nvidia.com/gpumem: 3000`, `nvidia.com/gpucores: 30`, annotation `hami.io/vulkan: "true"`.
2. HAMi webhook `MutateAdmission` — existing path sets `NVIDIA_VISIBLE_DEVICES`, RuntimeClass.
3. New path (annotation present + `hasResource`): merges `graphics` into `NVIDIA_DRIVER_CAPABILITIES`, appends `HAMI_VULKAN_ENABLE=1`.
4. Scheduler/device-plugin flow unchanged.

### 6.2 Container start
1. NVIDIA Container Toolkit prestart hook reads `NVIDIA_DRIVER_CAPABILITIES=compute,utility,graphics`, mounts Vulkan ICD JSON + `libGLX_nvidia.so.0` + `libnvidia-glvkspirv.so`.
2. HAMi-core image already places `libvgpu.so` and `/etc/vulkan/implicit_layer.d/hami.json`.
3. Vulkan loader scans `implicit_layer.d`, sees `HAMI_VULKAN_ENABLE=1` → loads `VK_LAYER_HAMI_vgpu` from `libvgpu.so`.

### 6.3 Runtime
- `vkAllocateMemory(size)` → layer → reserve counter → next-layer or `VK_ERROR_OUT_OF_DEVICE_MEMORY`.
- `vkFreeMemory(mem)` → layer → release counter → next-layer.
- `vkGetPhysicalDeviceMemoryProperties` → next-layer → clamp heap size → return.
- `vkQueueSubmit` → layer throttle poll → next-layer.

### 6.4 Shared budget (CUDA + Vulkan concurrent)
Both paths enter the same `reserve_device_memory(dev, size)` behind one mutex. Sum of live allocations across APIs is invariant ≤ pod budget.

## 7. Error Handling

| Case | Behavior |
|------|----------|
| `HAMI_VULKAN_ENABLE` unset | `enable_environment` gate fails, layer never activates, Vulkan runs unhooked. |
| Manifest missing at runtime | Loader does not discover layer; Vulkan runs unhooked, warning logged by HAMi-core startup probe (future). |
| `vulkan-headers` missing at build time | Compile-time error; no runtime impact. |
| NVML utilization query fails | Throttle skipped (fail-open), error logged with errno. |
| next-layer chain re-entry | Dispatch table lookup routes to stored next pointer; recursion prevented by non-reentrant layer code. |
| Multiple physical devices in container | Per-device counters keyed by PCI bus ID / NVML device handle. `NVIDIA_VISIBLE_DEVICES` already restricts the set. |
| next-layer `vkAllocateMemory` fails after we reserved | Counter rollback; returned error propagated verbatim. |
| App leaks `VkDeviceMemory` (no `vkFreeMemory`) | Counter drift within process; released when process dies and library unloads. |
| Annotation `hami.io/vulkan: true` on non-NVIDIA pod | `hasResource == false` in NVIDIA device; silent no-op. |
| User pre-sets `NVIDIA_DRIVER_CAPABILITIES=all` | Untouched (all ⊇ graphics). |
| User pre-sets `NVIDIA_DRIVER_CAPABILITIES=compute` | Replaced with `compute,graphics` (union). |
| User pre-sets `NVIDIA_DRIVER_CAPABILITIES=compute,graphics` | Untouched (already contains graphics). |

## 8. Testing

### 8.1 Go unit tests — `pkg/device/nvidia/device_test.go`
- `TestMutateAdmission_VulkanAnno_AddsGraphicsCap` — annotation + HAMi resource → env contains `graphics`, `HAMI_VULKAN_ENABLE=1`.
- `TestMutateAdmission_VulkanAnno_MergesExistingCaps` — pre-existing `compute` → merged to `compute,graphics`.
- `TestMutateAdmission_VulkanAnno_AllCaps_NoChange` — pre-existing `all` → untouched.
- `TestMutateAdmission_NoVulkanAnno_NoChange` — annotation absent → no env injection.
- `TestMutateAdmission_VulkanAnno_NoGPUResource` — annotation without any HAMi resource → no-op.
- `TestMutateAdmission_VulkanAnno_IdempotentHamiEnable` — re-applying webhook does not duplicate `HAMI_VULKAN_ENABLE`.

### 8.2 HAMi-core C unit tests
- `vk_layerGetInstanceProcAddr` returns wrapper for hooked names, next-layer pointer for others.
- `vkAllocateMemory`:
  - within budget → next-layer call, counter increments.
  - exceeding budget → `VK_ERROR_OUT_OF_DEVICE_MEMORY`, no next-layer call, counter unchanged.
  - next-layer returns error → counter rolled back.
- Concurrent CUDA `cuMemAlloc` + Vulkan `vkAllocateMemory` under pthread stress: invariant `used_memory ≤ budget` holds; sum of successful allocations ≤ budget.
- `vkGetPhysicalDeviceMemoryProperties` clamp: heap size in returned struct equals `min(real, budget)`.

### 8.3 Integration / E2E
- New example `examples/nvidia/vulkan_example.yaml` — pod with `hami.io/vulkan: "true"`, `nvidia.com/gpumem: 1024`, running `vulkaninfo`. Asserts (manual or scripted):
  - `vulkaninfo | grep heapSize` shows ≤ 1024 MB on device-local heap.
  - A `vkAllocateMemory` test binary (or `vkcube --size-mb 2048`) fails with `OUT_OF_DEVICE_MEMORY`.
- (Manual, not CI) llama.cpp Vulkan backend pod with `gpumem: 4096` and a 7B model — logs show allocation failure when crossing budget. Documented in `docs/vulkan-vgpu-support.md`.

### 8.4 Verification checklist (docs)
- `vulkaninfo` heap size clamp.
- `vkAllocateMemory` budget-exceed returns expected error.
- `nvidia-smi` compute utilization of a submit-heavy Vulkan workload stays near configured `gpucores`.
- CUDA + Vulkan mixed workload in one pod respects combined budget.

## 9. Delivery Plan

Two-repo change, sequenced:

1. **HAMi-core PR** (C): Vulkan layer module, manifest JSON, Dockerfile update, Makefile update, C unit tests. Tag a release (`vX.Y.0`).
2. **HAMi PR** (Go, this repo):
   - `pkg/device/nvidia/device.go` — annotation → env injection.
   - `pkg/device/nvidia/device_test.go` — unit tests.
   - Bump `libvgpu` submodule pointer to the new HAMi-core release.
   - `examples/nvidia/vulkan_example.yaml`.
   - `docs/vulkan-vgpu-support.md` (EN + `_cn.md`).

Rollout: default-off (annotation-gated). No migration or breaking change for existing deployments.

## 10. Open Questions / Future Work

- Frame-pacing for graphics workloads under SM throttle — measure `vkQueueSubmit` jitter; may need configurable throttle mode (`strict` vs `cooperative`) in a follow-up.
- Vulkan Video extensions (`VK_KHR_video_queue`) — not hooked in v1.
- Prometheus metrics for Vulkan allocation denials — follow-up.
- MPS mode interaction — MPS does not expose Vulkan; annotation + MPS mode should error or fall back to `hami-core` mode with a warning (clarify in implementation).
