# AMD Instinct vGPU Support (issue:#1707)

## 1. Motivation

This proposal adds per-pod **memory limiting** and **compute-unit (CU) partitioning** for 
AMD ROCm GPUs, so multiple pods can share one Instinct GPU.

## 2. Goals

- Fractional allocation by memory (`amd.com/gpumem`, MiB) and compute (`amd.com/gpucores`, percentage).
- Exclusive, non-overlapping CU partitioning across pods.
- Support all AMD ROCm GPU types; initial CU masking starts on non-WGP
  (CDNA) devices, then extends to WGP-capable (RDNA) with pair-aligned masks.
- Following HAMi's existing architecture design to achieve the functionality.

## 3. Approach

**Why LD_AUDIT instead of LD_PRELOAD?**
In our prototype on ROCm 7.x, LD_PRELOAD broke HIP — interposing HIP symbols leads 
to recursive re-entry through HIP-internal calls. 
Switching to LD_AUDIT (`la_symbind64`), which intercepts only cross-library bindings, 
resolved it. The existing NVIDIA LD_PRELOAD path is unchanged.

**Advantages of CU masking, compared to hardware partitioning (CPX/NPS).**
Masking (`HSA_CU_MASK`) assigns fine-grained, hardware-valid per-pod CU
partitioning at container start ([AMD Docs](https://rocm.docs.amd.com/en/latest/how-to/setting-cus.html)).
On the other hand, hardware partitioning (CPX/NPS) slices only at fixed XCD
granularity and is set per physical GPU.

**WGP pairing and rollout scope.** ROCm documents that not every CU mask is
valid on every device: on GPUs where two CUs form a Work Group Processor
(WGP) and kernels run in WGP mode, disabling only one CU of a pair is
invalid ([setting CUs](https://rocm.docs.amd.com/en/latest/how-to/setting-cus.html)).
WGP is an **RDNA** construct (GFX10+); HIP describes it under the RDNA hardware
model ([HIP hardware implementation](https://rocm.docs.amd.com/projects/HIP/en/latest/understand/hardware_implementation.html)).
**CDNA** devices (e.g. Instinct MI300X, `gfx942`) keep independent CUs and are
not subject to that pairing rule.

The design aims to support **all AMD ROCm GPU types**. Initial implementation
starts on **non-WGP** devices (CDNA / Instinct), where CU ranges can be chosen
without pair alignment. Support for WGP-capable (RDNA) devices follows later
and must select CU masks in adjacent pairs so `HSA_CU_MASK` remains
hardware-valid.

## 4. Protocol (scheduler <-> device-plugin)

For AMD, the scheduler writes AMD-specific pod annotations; the device-plugin
injects `ROCR_VISIBLE_DEVICES`, `HSA_CU_MASK`, and `HIP_DEVICE_MEMORY_LIMIT`
into each container's allocation response.

### 4.1 Node registration (device-plugin -> node annotations)

Registered under `hami.io/node-amd-register`, in JSON format — an array of `DeviceInfo` as follows for example:

```json
[
  {
    "id": "<device-id>",
    "index": 0,
    "count": 1,
    "devmem": 196608,
    "devcore": 304,
    "type": "AMDGPU",
    "numa": 0,
    "mode": "hami-core",
    "health": true
  }
]
```

- `devmem`  = total device memory in MiB (e.g. 196608 for MI300X).
- `devcore` = total CU count (e.g. 304 for MI300X). 
- `id`      = device identifier.


### 4.2 Pod allocation (scheduler -> pod annotations -> Allocate)

The scheduler allocation result is written under the **AMD-specific** keys:

```text
hami.io/amd-devices-to-allocate: <UUID>,AMDGPU,<memMiB>,<cuCount>:;
hami.io/amd-devices-allocated:   <UUID>,AMDGPU,<memMiB>,<cuCount>:;
```

The type field uses the existing `AMDGPU` constant (see `pkg/device/amd/device.go`).

During `Allocate`, the device-plugin reads `hami.io/amd-devices-allocated`
from the pod annotations, converts each device's `cuCount` into a
non-overlapping CU range, and returns the result in the container's
`ContainerAllocateResponse.Envs` (not as a pod annotation):

- `ROCR_VISIBLE_DEVICES` — limits which GPUs the container sees; UUID order
  defines the container-local GPU index.
- `HSA_CU_MASK` — restricts CUs per visible GPU, using `GPU_list:CU_list`
  where the GPU index is resolved **after** `ROCR_VISIBLE_DEVICES` reordering
  (index `0` = first visible GPU, `1` = second, …).

For a multi-GPU pod, the device-plugin pairs each allocated UUID with its
container-local index when building `HSA_CU_MASK`, for example:

```text
# two GPUs: UUID-A (index 0) gets CUs 0-75, UUID-B (index 1) gets CUs 0-75
HSA_CU_MASK=0:0-75;1:0-75
```

Each `CU_list` uses HSA's CU ID-list grammar, for example `0-3,8,10-12`.

Exclusivity of CU ranges across pods on a device is enforced under the AMD
node lock (`AMDDevices.LockNode` and `ReleaseNodeLock` which are unimplemented now).

## 5. Resource model and core_limit -> CU mask

Example Pod request:

```yaml
resources:
  limits:
    amd.com/gpu: 1          # number of physical AMD GPUs
    amd.com/gpumem: 16384   # MiB
    amd.com/gpucores: 25    # percentage of physical CUs
```

### 5.1 Memory limit
`amd.com/gpumem` (MiB) flows through the shared annotation and is injected by
the device plugin as `HIP_DEVICE_MEMORY_LIMIT=<MiB>m`. HAMi's AMD LD_AUDIT
layer enforces this limit at the HIP API boundary; the value must use the
`<MiB>m` format. The limit is scoped to the container and applies to all GPUs
mounted in that container. Each container receives an independent allocation
response and environment.

### 5.2 Core limit -> CU mask

`amd.com/gpucores` is a **percentage** in the inclusive range `1`–`100` (values
outside this range are rejected at admission), not a CU count. The scheduler
converts the requested percentage into a physical CU count with:

```text
cuCount = floor(percentage × devcore / 100)
```

where `devcore` is the device's total CU count from node registration. The
result is clamped to `[1, devcore]`. For example, `25` on a 304-CU MI300X
yields `76` CUs; `33` yields `100` CUs (`floor(100.32)`); `67` yields
`203` CUs (`floor(203.68)`).

The scheduler records this `cuCount` in `hami.io/amd-devices-allocated`. The
device-plugin selects a non-overlapping CU range of that size and injects it
as `HSA_CU_MASK`. On WGP-capable devices, the chosen range must additionally
align to adjacent CU pairs within each WGP.

**Zero-interference is not guaranteed.** Even with non-overlapping masks,
residual interference remains (as reported in #1707).

## 6. Known limitations

- **No `amd-smi` / `rocm-smi` virtualization.** 
  These read sysfs/drm, not HIP, so
  LD_AUDIT cannot intercept them; in-container tools may report physical resources.
- **Mixed GPU types are not supported on one node.**
  The device plugin derives the GPU type from `amd.com/gpu.product-name`, but
  this label cannot describe multiple GPU types.
- **WGP-aware CU allocation is phased.** First ship CU partitioning on
  non-WGP devices (CDNA / Instinct). RDNA (GFX10+) devices need WGP pair
  alignment when building `HSA_CU_MASK`
  ([setting CUs](https://rocm.docs.amd.com/en/latest/how-to/setting-cus.html))
  and land in a follow-up.

## 7. Discussion points

- **Device-plugin layering.** In #1707, the AMD vGPU device-plugin is proposed to be built on ROCm/k8s-device-plugin, which already advertises whole-GPU `amd.com/gpu`. Since kubelet cannot register the same resource from two plugins, the natural path is to **extend the ROCm plugin** so a single plugin owns `amd.com/gpu` and also advertises the fractional `amd.com/gpumem` / `amd.com/gpucores` (optional, which is not a must-have for HAMi but can be useful for other schedulers).
